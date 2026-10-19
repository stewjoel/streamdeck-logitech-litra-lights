package main

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sstallion/go-hid"
)

const (
	VID       = 0x046d
	UsagePage = 0xff43

	maxDeviceRetries = 2
	baseRetryDelay   = 150 * time.Millisecond
	interPacketDelay = 4 * time.Millisecond
)

// Target constants for selecting which device(s) an action controls
const (
	TargetSynced = "synced" // Default: follows active sync mode / paired group
	TargetAll    = "all"    // Broadcast to all connected lights
	TargetLight1 = "index:0"
	TargetLight2 = "index:1"
)

// SyncMode controls what TargetSynced resolves to when toggled from Stream Deck
type SyncMode int

const (
	SyncModeAllPaired SyncMode = iota // Control all paired lights simultaneously
	SyncModeLight1                    // Control only Light 1
	SyncModeLight2                    // Control only Light 2
)

func (m SyncMode) String() string {
	switch m {
	case SyncModeAllPaired:
		return "SYNC\nALL"
	case SyncModeLight1:
		return "SOLO\n#1"
	case SyncModeLight2:
		return "SOLO\n#2"
	default:
		return "SYNC"
	}
}

// Supported Litra Product IDs (both USB and Bluetooth)
var litraPIDs = map[uint16]string{
	0xc900: "Litra Glow",
	0xb900: "Litra Glow (BT)",
	0xc901: "Litra Beam",
	0xb901: "Litra Beam (BT)",
	0xc903: "Litra Beam LX",
	0xb903: "Litra Beam LX (BT)",
}

// DeviceSummary is sent to the Property Inspector so users can see/pair their lights.
type DeviceSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Model  string `json:"model"`
	Serial string `json:"serial"`
	IsUSB  bool   `json:"isUSB"`
	Paired bool   `json:"paired"`
}

// LitraDevice represents a single physical Logitech Litra light with isolated I/O locking.
type LitraDevice struct {
	ID     string
	Path   string
	Model  string
	Serial string
	PID    uint16
	IsUSB  bool

	mu     sync.Mutex
	handle *hid.Device
}

func (ld *LitraDevice) closeHandleLocked() {
	if ld.handle != nil {
		_ = ld.handle.Close()
		ld.handle = nil
	}
}

func (ld *LitraDevice) ensureOpenLocked() error {
	if ld.handle != nil {
		return nil
	}
	h, err := hid.OpenPath(ld.Path)
	if err != nil {
		return fmt.Errorf("open %s (%s): %w", ld.ID, ld.Model, err)
	}
	ld.handle = h
	log.Printf("[HID] Opened device %s (%s) at %s", ld.ID, ld.Model, ld.Path)
	return nil
}

// WriteCommands writes a sequence of 20-byte HID++ packets to this specific device.
// Thread-safe per device and paced to prevent Windows Overlapped I/O hangs.
func (ld *LitraDevice) WriteCommands(commands ...[]byte) error {
	ld.mu.Lock()
	defer ld.mu.Unlock()

	delay := baseRetryDelay

	for attempt := 0; attempt <= maxDeviceRetries; attempt++ {
		if err := ld.ensureOpenLocked(); err != nil {
			if attempt < maxDeviceRetries {
				time.Sleep(delay)
				delay *= 2
				continue
			}
			return err
		}

		var writeErr error
		for i, cmd := range commands {
			if i > 0 {
				// Critical pacing for Windows HID++ driver stability
				time.Sleep(interPacketDelay)
			}
			if _, err := ld.handle.Write(cmd); err != nil {
				writeErr = err
				break
			}
		}

		if writeErr == nil {
			return nil
		}

		log.Printf("[HID] Write error on %s (attempt %d/%d): %v", ld.ID, attempt+1, maxDeviceRetries+1, writeErr)
		ld.closeHandleLocked()

		if attempt < maxDeviceRetries {
			time.Sleep(delay)
			delay *= 2
		} else {
			return fmt.Errorf("device %s write failed: %w", ld.ID, writeErr)
		}
	}

	return fmt.Errorf("device %s unreachable", ld.ID)
}

// MultiDeviceManager manages discovery, pairing, state tracking, and parallel dispatch across multiple Litra lights.
type MultiDeviceManager struct {
	mu             sync.RWMutex
	hidInitialized bool
	devices        []*LitraDevice          // Ordered deterministically
	byID           map[string]*LitraDevice // Keyed by Device ID (Serial or Path hash)
	unpairedIDs    map[string]bool         // Devices explicitly excluded from sync group (paired by default)
	syncMode       SyncMode
	lastEnum       time.Time

	// Shared hardware state for accurate toggling & instant sync
	stateMu           sync.Mutex
	frontOn           bool
	backOn            bool
	lastFrontCmds     [][]byte
	lastBackLightCmds [][]byte
}

var deviceMgr = &MultiDeviceManager{
	byID:        make(map[string]*LitraDevice),
	unpairedIDs: make(map[string]bool),
	syncMode:    SyncModeAllPaired,
}

// Init initializes libhidapi once for the lifetime of the plugin process.
func (m *MultiDeviceManager) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.hidInitialized {
		if err := hid.Init(); err != nil {
			return fmt.Errorf("hid.Init: %w", err)
		}
		m.hidInitialized = true
	}
	return m.refreshDevicesLocked()
}

// Close cleanly closes all open device handles and exits libhidapi on process shutdown.
func (m *MultiDeviceManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, dev := range m.devices {
		dev.mu.Lock()
		dev.closeHandleLocked()
		dev.mu.Unlock()
	}
	m.devices = nil
	m.byID = make(map[string]*LitraDevice)

	if m.hidInitialized {
		_ = hid.Exit()
		m.hidInitialized = false
	}
}

// Refresh enumerates connected Litra devices and updates the manager's device table.
func (m *MultiDeviceManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.refreshDevicesLocked()
}

func (m *MultiDeviceManager) refreshDevicesLocked() error {
	if !m.hidInitialized {
		if err := hid.Init(); err != nil {
			return fmt.Errorf("hid.Init: %w", err)
		}
		m.hidInitialized = true
	}

	type candidate struct {
		id     string
		path   string
		model  string
		serial string
		pid    uint16
		isUSB  bool
	}

	discovered := make(map[string]candidate)

	err := hid.Enumerate(VID, 0, func(info *hid.DeviceInfo) error {
		if info.UsagePage != UsagePage {
			return nil
		}
		model, ok := litraPIDs[info.ProductID]
		if !ok {
			// Universal fallback: recognize any Logitech Litra device by product/interface string
			pStr := strings.ToLower(info.ProductStr)
			if strings.Contains(pStr, "litra") {
				model = info.ProductStr
			} else if strings.Contains(pStr, "beam") || strings.Contains(pStr, "glow") {
				model = "Logitech " + info.ProductStr
			} else {
				return nil
			}
		}

		isUSB := (info.ProductID & 0xf000) == 0xc000
		serial := strings.TrimSpace(info.SerialNbr)
		id := serial
		if id == "" {
			id = normalizePathID(info.Path)
		}

		// Deduplicate: If same physical light is connected via both USB and BT, prefer USB
		if existing, exists := discovered[id]; exists {
			if existing.isUSB && !isUSB {
				return nil
			}
		}

		discovered[id] = candidate{
			id:     id,
			path:   info.Path,
			model:  model,
			serial: serial,
			pid:    info.ProductID,
			isUSB:  isUSB,
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("hid.Enumerate: %w", err)
	}

	// Reconcile existing open devices vs newly discovered
	nextByID := make(map[string]*LitraDevice, len(discovered))
	nextSlice := make([]*LitraDevice, 0, len(discovered))

	for id, cand := range discovered {
		if existing, ok := m.byID[id]; ok {
			existing.mu.Lock()
			if existing.Path != cand.path {
				existing.closeHandleLocked()
				existing.Path = cand.path
			}
			existing.Model = cand.model
			existing.PID = cand.pid
			existing.IsUSB = cand.isUSB
			existing.mu.Unlock()
			nextByID[id] = existing
			nextSlice = append(nextSlice, existing)
		} else {
			dev := &LitraDevice{
				ID:     cand.id,
				Path:   cand.path,
				Model:  cand.model,
				Serial: cand.serial,
				PID:    cand.pid,
				IsUSB:  cand.isUSB,
			}
			nextByID[id] = dev
			nextSlice = append(nextSlice, dev)
			log.Printf("[HID] Discovered Litra device: ID=%s Model=%s USB=%v", dev.ID, dev.Model, dev.IsUSB)
		}
	}

	// Close handles for removed devices
	for id, oldDev := range m.byID {
		if _, stillPresent := nextByID[id]; !stillPresent {
			oldDev.mu.Lock()
			oldDev.closeHandleLocked()
			oldDev.mu.Unlock()
			log.Printf("[HID] Device disconnected: ID=%s", id)
		}
	}

	// Sort deterministically by ID so Light #1 and Light #2 never flip order
	sort.Slice(nextSlice, func(i, j int) bool {
		return nextSlice[i].ID < nextSlice[j].ID
	})

	m.byID = nextByID
	m.devices = nextSlice
	m.lastEnum = time.Now()
	return nil
}

func normalizePathID(path string) string {
	cleaned := strings.ToLower(path)
	if len(cleaned) > 24 {
		return "path-" + cleaned[len(cleaned)-24:]
	}
	return "path-" + cleaned
}

// GetSummaries returns a snapshot of all connected devices for the Property Inspector.
func (m *MultiDeviceManager) GetSummaries() []DeviceSummary {
	m.mu.Lock()
	_ = m.refreshDevicesLocked()
	defer m.mu.Unlock()

	out := make([]DeviceSummary, 0, len(m.devices))
	for i, d := range m.devices {
		shortSerial := d.Serial
		if len(shortSerial) > 6 {
			shortSerial = "…" + shortSerial[len(shortSerial)-6:]
		} else if shortSerial == "" {
			shortSerial = fmt.Sprintf("#%d", i+1)
		}
		out = append(out, DeviceSummary{
			ID:     d.ID,
			Name:   fmt.Sprintf("Light %d: %s (%s)", i+1, d.Model, shortSerial),
			Model:  d.Model,
			Serial: d.Serial,
			IsUSB:  d.IsUSB,
			Paired: !m.unpairedIDs[d.ID],
		})
	}
	return out
}

// SetPairedDevices updates which device IDs are paired together in the sync group.
func (m *MultiDeviceManager) SetPairedDevices(pairedIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If no paired IDs are supplied, default to pairing ALL connected devices
	if len(pairedIDs) == 0 {
		m.unpairedIDs = make(map[string]bool)
		return
	}

	pairedSet := make(map[string]bool, len(pairedIDs))
	hasAnyOverlap := false
	for _, id := range pairedIDs {
		pairedSet[id] = true
		if _, exists := m.byID[id]; exists {
			hasAnyOverlap = true
		}
	}

	// If none of the saved IDs match any currently connected device (e.g., imported
	// profile from another user or machine), do not unpair the user's lights.
	if !hasAnyOverlap {
		m.unpairedIDs = make(map[string]bool)
		return
	}

	m.unpairedIDs = make(map[string]bool)
	for _, d := range m.devices {
		if !pairedSet[d.ID] {
			m.unpairedIDs[d.ID] = true
		}
	}
}

// SetDevicePaired toggles pairing status for a single device ID.
func (m *MultiDeviceManager) SetDevicePaired(deviceID string, paired bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if paired {
		delete(m.unpairedIDs, deviceID)
	} else {
		m.unpairedIDs[deviceID] = true
	}
}

// CycleSyncMode cycles between Sync All Paired -> Solo Light 1 -> Solo Light 2
func (m *MultiDeviceManager) CycleSyncMode() SyncMode {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.devices) <= 1 {
		m.syncMode = SyncModeAllPaired
		return m.syncMode
	}

	switch m.syncMode {
	case SyncModeAllPaired:
		m.syncMode = SyncModeLight1
	case SyncModeLight1:
		m.syncMode = SyncModeLight2
	default:
		m.syncMode = SyncModeAllPaired
	}
	return m.syncMode
}

func (m *MultiDeviceManager) GetSyncMode() SyncMode {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.syncMode
}

// resolveTargets determines which physical LitraDevice instances should receive a command.
func (m *MultiDeviceManager) resolveTargets(targetSpec string) ([]*LitraDevice, error) {
	m.mu.Lock()
	if len(m.devices) == 0 || time.Since(m.lastEnum) > 5*time.Second {
		_ = m.refreshDevicesLocked()
	}
	devices := append([]*LitraDevice(nil), m.devices...)
	syncMode := m.syncMode
	unpaired := make(map[string]bool, len(m.unpairedIDs))
	for k, v := range m.unpairedIDs {
		unpaired[k] = v
	}
	m.mu.Unlock()

	if len(devices) == 0 {
		return nil, fmt.Errorf("no Litra devices connected")
	}

	if targetSpec == "" || targetSpec == TargetSynced {
		switch syncMode {
		case SyncModeLight1:
			return []*LitraDevice{devices[0]}, nil
		case SyncModeLight2:
			if len(devices) >= 2 {
				return []*LitraDevice{devices[1]}, nil
			}
			return []*LitraDevice{devices[0]}, nil
		default:
			var paired []*LitraDevice
			for _, d := range devices {
				if !unpaired[d.ID] {
					paired = append(paired, d)
				}
			}
			if len(paired) == 0 {
				return devices, nil
			}
			return paired, nil
		}
	}

	if targetSpec == TargetAll {
		return devices, nil
	}

	if targetSpec == TargetLight1 {
		return []*LitraDevice{devices[0]}, nil
	}

	if targetSpec == TargetLight2 {
		if len(devices) < 2 {
			return nil, fmt.Errorf("Light #2 is not connected")
		}
		return []*LitraDevice{devices[1]}, nil
	}

	// Match by exact device ID / Serial
	for _, d := range devices {
		if d.ID == targetSpec || (d.Serial != "" && d.Serial == targetSpec) {
			return []*LitraDevice{d}, nil
		}
	}

	return devices, nil
}

// WriteCommands sends commands to the default synced target group in parallel.
func (m *MultiDeviceManager) WriteCommands(commands ...[]byte) error {
	return m.WriteCommandsToTarget(TargetSynced, commands...)
}

// WriteCommandsToTarget dispatches commands concurrently across all matching Litra devices.
// Isolates failures: if 1 of 2 synced lights is unplugged, the online light still responds immediately.
func (m *MultiDeviceManager) WriteCommandsToTarget(targetSpec string, commands ...[]byte) error {
	targets, err := m.resolveTargets(targetSpec)
	if err != nil {
		_ = m.Refresh()
		targets, err = m.resolveTargets(targetSpec)
		if err != nil {
			return err
		}
	}

	var (
		wg           sync.WaitGroup
		mu           sync.Mutex
		errs         []error
		successCount int
	)

	for _, dev := range targets {
		wg.Add(1)
		go func(d *LitraDevice) {
			defer wg.Done()
			if err := d.WriteCommands(commands...); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}
			mu.Lock()
			successCount++
			mu.Unlock()
		}(dev)
	}

	wg.Wait()

	if successCount > 0 {
		if len(errs) > 0 {
			log.Printf("[HID] Partial sync success (%d/%d devices updated). Warnings: %v",
				successCount, len(targets), errors.Join(errs...))
		}
		return nil
	}

	return errors.Join(errs...)
}
