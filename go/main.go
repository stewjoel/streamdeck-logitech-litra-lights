package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	logitech "github.com/stewjoel/streamdeck-logitech-litra-lights/internal/logitech_hid"
	"github.com/samwho/streamdeck"
)

// TargetSettings can be embedded in any action settings struct to allow targeting
// "synced" (default), "all", "index:0" (Light 1), "index:1" (Light 2), or a device Serial/ID.
type TargetSettings struct {
	DeviceTarget string   `json:"deviceTarget,omitempty"`
	PairedIDs    []string `json:"pairedIds,omitempty"`
}

func (t *TargetSettings) EffectiveTarget() string {
	if t == nil || t.DeviceTarget == "" {
		return TargetSynced
	}
	return t.DeviceTarget
}

// Settings for the "Set Brightness & Temperature" action
type Settings struct {
	Temperature  uint16 `json:"temperature,string"`
	Brightness   uint8  `json:"brightness,string"`
	DeviceTarget string `json:"deviceTarget,omitempty"`
}

// Preset represents a saved color or gradient
type Preset struct {
	Mode   string `json:"mode"`   // "solid" or "gradient"
	Color  string `json:"color"`  // hex color like "#ff0000"
	Color2 string `json:"color2"` // second hex color for gradient
}

// PresetCycleSettings stores a list of presets and the current index
type PresetCycleSettings struct {
	Presets      []Preset `json:"presets"`
	Index        int      `json:"cycleIndex"`
	DeviceTarget string   `json:"deviceTarget,omitempty"`
}

// ColorCycleSettings stores configurable solid color presets
type ColorCycleSettings struct {
	ColorPresets []string `json:"colorPresets"`
	Index        int      `json:"cycleIndex"`
	DeviceTarget string   `json:"deviceTarget,omitempty"`
}

// SyncActionSettings configures the "Pair & Sync Lights" action
type SyncActionSettings struct {
	Behavior  string   `json:"behavior,omitempty"` // "toggle_mode" (default) or "force_sync"
	PairedIDs []string `json:"pairedIds,omitempty"`
}

func hexToRGB(hex string) (uint8, uint8, uint8) {
	if len(hex) != 7 || hex[0] != '#' {
		return 255, 255, 255
	}
	r, _ := strconv.ParseUint(hex[1:3], 16, 8)
	g, _ := strconv.ParseUint(hex[3:5], 16, 8)
	b, _ := strconv.ParseUint(hex[5:7], 16, 8)
	return uint8(r), uint8(g), uint8(b)
}

func getRGBFromPreset(p Preset) (r, g, b, r2, g2, b2 uint8) {
	r, g, b = hexToRGB(p.Color)
	if p.Mode == "gradient" {
		r2, g2, b2 = hexToRGB(p.Color2)
	}
	return
}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	_ = os.MkdirAll("logs", 0o755)
	fileName := "streamdeck-logitech-litra-lights.log"
	f, err := os.CreateTemp("logs", fileName)
	if err != nil {
		log.Printf("error creating temp file: %v", err)
		exitCode = 83
		return
	}
	defer func(f *os.File) {
		_ = f.Close()
	}(f)

	log.SetOutput(f)

	ctx := context.Background()
	if err := run(ctx); err != nil {
		log.Printf("Fatal error: %v\n", err)
		exitCode = 1
		return
	}
}

func run(ctx context.Context) error {
	params, err := streamdeck.ParseRegistrationParams(os.Args)
	if err != nil {
		return err
	}

	if err := deviceMgr.Init(); err != nil {
		log.Printf("Initial HID init warning: %v", err)
	}

	client := streamdeck.NewClient(ctx, params)
	setup(client)

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		log.Println("Received termination signal, turning off lights...")
		_ = turnOffAllLights(TargetAll)
		deviceMgr.Close()
		os.Exit(0)
	}()

	err = client.Run()

	// Power off all lights when Stream Deck quits
	log.Println("Plugin exiting, turning off lights...")
	_ = turnOffAllLights(TargetAll)
	deviceMgr.Close()

	return err
}

func setup(client *streamdeck.Client) {
	setupSetLightsAction(client)
	setupTurnOffLightsAction(client)

	// Litra Beam LX actions
	setupFrontPowerAction(client)
	setupBackPowerAction(client)
	setupFrontTempCycleAction(client)
	setupFrontBrightnessCycleAction(client)
	setupBackBrightnessCycleAction(client)
	setupBackColorCycleAction(client)
	setupBackGradientCycleAction(client)

	// Multi-light Pairing & Sync action
	setupSyncLightsAction(client)
}

var (
	rawSettingsMu    sync.Mutex
	rawSettingsCache = make(map[string]map[string]any)
)

// pushDeviceListToPI injects the current connected Litra devices into the action's settings
// so the Property Inspector dropdown and pairing checkboxes stay up to date automatically.
func pushDeviceListToPI(ctx context.Context, client *streamdeck.Client, actionCtx string, rawSettings json.RawMessage) {
	rawSettingsMu.Lock()
	m := make(map[string]any)
	for k, v := range rawSettingsCache[actionCtx] {
		m[k] = v
	}
	if len(rawSettings) > 0 {
		_ = json.Unmarshal(rawSettings, &m)
	}
	rawSettingsCache[actionCtx] = m
	rawSettingsMu.Unlock()

	// Apply any saved pairedIds from settings to the manager
	if rawPaired, ok := m["pairedIds"].([]any); ok && len(rawPaired) > 0 {
		var ids []string
		for _, v := range rawPaired {
			if s, ok := v.(string); ok {
				ids = append(ids, s)
			}
		}
		if len(ids) > 0 {
			deviceMgr.SetPairedDevices(ids)
		}
	}

	m["connectedDevices"] = deviceMgr.GetSummaries()
	_ = client.SetSettings(ctx, m)
}

func extractTargetFromPayload(rawPayload json.RawMessage) string {
	var p struct {
		Settings TargetSettings `json:"settings"`
	}
	if err := json.Unmarshal(rawPayload, &p); err == nil {
		if len(p.Settings.PairedIDs) > 0 {
			deviceMgr.SetPairedDevices(p.Settings.PairedIDs)
		}
		return p.Settings.EffectiveTarget()
	}
	return TargetSynced
}

var defaultColorPresets = []string{"#FF0000", "#00FF00", "#0000FF", "#FF00FF", "#FFFF00", "#00FFFF"}

// --- Back Color Cycle ---
func setupBackColorCycleAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.back.color")
	var mu sync.Mutex
	settings := make(map[string]*ColorCycleSettings)

	handler := func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		p := streamdeck.WillAppearPayload{}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}

		mu.Lock()
		s, ok := settings[event.Context]
		if !ok {
			s = &ColorCycleSettings{
				ColorPresets: append([]string{}, defaultColorPresets...),
				Index:        0,
				DeviceTarget: TargetSynced,
			}
			settings[event.Context] = s
		}
		_ = json.Unmarshal(p.Settings, s)
		if len(s.ColorPresets) == 0 {
			s.ColorPresets = append([]string{}, defaultColorPresets...)
		}

		if event.Event == streamdeck.KeyDown {
			idx := s.Index % len(s.ColorPresets)
			hex := s.ColorPresets[idx]
			r, g, b := hexToRGB(hex)
			target := s.DeviceTarget
			if target == "" {
				target = TargetSynced
			}

			s.Index = (s.Index + 1) % len(s.ColorPresets)
			mu.Unlock()

			onBytes := logitech.ConvertLightsOnTarget(logitech.BackLight)
			colorCmds, err := logitech.ConvertColorTarget(logitech.BackLight, r, g, b)
			if err != nil {
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}
			commands := append([][]byte{onBytes}, colorCmds...)

			if err := deviceMgr.WriteCommandsToTarget(target, commands...); err != nil {
				log.Println("Error setting back color:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			deviceMgr.stateMu.Lock()
			deviceMgr.backOn = true
			deviceMgr.lastBackLightCmds = colorCmds
			deviceMgr.stateMu.Unlock()

			_ = client.SetSettings(ctx, s)
			return client.SetTitle(ctx, fmt.Sprintf("%d/%d", idx+1, len(s.ColorPresets)), streamdeck.HardwareAndSoftware)
		}

		idx := s.Index % len(s.ColorPresets)
		total := len(s.ColorPresets)
		mu.Unlock()

		if event.Event == streamdeck.WillAppear {
			pushDeviceListToPI(ctx, client, event.Context, p.Settings)
		}
		return client.SetTitle(ctx, fmt.Sprintf("%d/%d", idx+1, total), streamdeck.HardwareAndSoftware)
	}

	action.RegisterHandler(streamdeck.WillAppear, handler)
	action.RegisterHandler(streamdeck.DidReceiveSettings, handler)
	action.RegisterHandler(streamdeck.KeyDown, handler)
	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})
}

// --- Back Gradient Cycle ---
func setupBackGradientCycleAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.back.presets")
	var mu sync.Mutex
	settings := make(map[string]*PresetCycleSettings)

	handler := func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		p := streamdeck.WillAppearPayload{}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}

		mu.Lock()
		s, ok := settings[event.Context]
		if !ok {
			s = &PresetCycleSettings{
				Presets:      []Preset{},
				Index:        0,
				DeviceTarget: TargetSynced,
			}
			settings[event.Context] = s
		}
		_ = json.Unmarshal(p.Settings, s)

		if event.Event == streamdeck.KeyDown {
			if len(s.Presets) == 0 {
				mu.Unlock()
				return client.SetTitle(ctx, "None", streamdeck.HardwareAndSoftware)
			}

			idx := s.Index % len(s.Presets)
			preset := s.Presets[idx]
			total := len(s.Presets)
			target := s.DeviceTarget
			if target == "" {
				target = TargetSynced
			}
			s.Index = (s.Index + 1) % len(s.Presets)
			mu.Unlock()

			r1, g1, b1, r2, g2, b2 := getRGBFromPreset(preset)
			onBytes := logitech.ConvertLightsOnTarget(logitech.BackLight)
			commands := [][]byte{onBytes}
			if preset.Mode == "gradient" {
				commands = append(commands, logitech.ConvertBackColorGradient(r1, g1, b1, r2, g2, b2)...)
			} else {
				colorCmds, _ := logitech.ConvertColorTarget(logitech.BackLight, r1, g1, b1)
				commands = append(commands, colorCmds...)
			}

			if err := deviceMgr.WriteCommandsToTarget(target, commands...); err != nil {
				log.Println("Error applying preset:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			deviceMgr.stateMu.Lock()
			deviceMgr.backOn = true
			deviceMgr.lastBackLightCmds = commands[1:]
			deviceMgr.stateMu.Unlock()

			_ = client.SetSettings(ctx, s)
			title := "Set"
			if preset.Mode == "gradient" {
				title = "Grad"
			}
			return client.SetTitle(ctx, fmt.Sprintf("%s\n%d/%d", title, idx+1, total), streamdeck.HardwareAndSoftware)
		}

		var titleText string
		if len(s.Presets) > 0 {
			idx := s.Index % len(s.Presets)
			preset := s.Presets[idx]
			title := "Set"
			if preset.Mode == "gradient" {
				title = "Grad"
			}
			titleText = fmt.Sprintf("%s\n%d/%d", title, idx+1, len(s.Presets))
		} else {
			titleText = "None"
		}
		mu.Unlock()

		if event.Event == streamdeck.WillAppear {
			pushDeviceListToPI(ctx, client, event.Context, p.Settings)
		}
		return client.SetTitle(ctx, titleText, streamdeck.HardwareAndSoftware)
	}

	action.RegisterHandler(streamdeck.WillAppear, handler)
	action.RegisterHandler(streamdeck.DidReceiveSettings, handler)
	action.RegisterHandler(streamdeck.KeyDown, handler)
	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})
}

// --- Front Power On/Off ---
func setupFrontPowerAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.front.power")

	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			target := extractTargetFromPayload(event.Payload)

			deviceMgr.stateMu.Lock()
			nextState := !deviceMgr.frontOn
			deviceMgr.stateMu.Unlock()

			var byteSequence []byte
			if nextState {
				byteSequence = logitech.ConvertLightsOnTarget(logitech.FrontLight)
			} else {
				byteSequence = logitech.ConvertLightsOffTarget(logitech.FrontLight)
			}

			if err := deviceMgr.WriteCommandsToTarget(target, byteSequence); err != nil {
				log.Println("Error toggling front power:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			deviceMgr.stateMu.Lock()
			deviceMgr.frontOn = nextState
			deviceMgr.stateMu.Unlock()

			if nextState {
				return client.SetTitle(ctx, "ON", streamdeck.HardwareAndSoftware)
			}
			return client.SetTitle(ctx, "OFF", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Back Power On/Off ---
func setupBackPowerAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.back.power")

	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			target := extractTargetFromPayload(event.Payload)

			deviceMgr.stateMu.Lock()
			nextState := !deviceMgr.backOn
			restoreCmds := append([][]byte(nil), deviceMgr.lastBackLightCmds...)
			deviceMgr.stateMu.Unlock()

			var cmds [][]byte
			if nextState {
				onBytes := logitech.ConvertLightsOnTarget(logitech.BackLight)
				cmds = append([][]byte{onBytes}, restoreCmds...)
			} else {
				cmds = [][]byte{logitech.ConvertLightsOffTarget(logitech.BackLight)}
			}

			if err := deviceMgr.WriteCommandsToTarget(target, cmds...); err != nil {
				log.Println("Error toggling back power:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			deviceMgr.stateMu.Lock()
			deviceMgr.backOn = nextState
			deviceMgr.stateMu.Unlock()

			if nextState {
				return client.SetTitle(ctx, "ON", streamdeck.HardwareAndSoftware)
			}
			return client.SetTitle(ctx, "OFF", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Front Temperature Cycle ---
func setupFrontTempCycleAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.front.temperature")
	var mu sync.Mutex
	cycleIndexes := make(map[string]int)
	defaultTemps := []uint16{2700, 3200, 4000, 5000, 6500}

	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			target := extractTargetFromPayload(event.Payload)

			mu.Lock()
			idx := cycleIndexes[event.Context]
			temp := defaultTemps[idx%len(defaultTemps)]
			cycleIndexes[event.Context] = (idx + 1) % len(defaultTemps)
			mu.Unlock()

			onBytes := logitech.ConvertLightsOnTarget(logitech.FrontLight)
			tempBytes, err := logitech.ConvertTemperatureTarget(logitech.FrontLight, temp)
			if err != nil {
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if err := deviceMgr.WriteCommandsToTarget(target, onBytes, tempBytes); err != nil {
				log.Println("Error setting front temp:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			deviceMgr.stateMu.Lock()
			deviceMgr.frontOn = true
			deviceMgr.lastFrontCmds = [][]byte{tempBytes}
			deviceMgr.stateMu.Unlock()

			return client.SetTitle(ctx, strconv.Itoa(int(temp))+"K", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Front Brightness Cycle ---
func setupFrontBrightnessCycleAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.front.brightness")
	var mu sync.Mutex
	cycleIndexes := make(map[string]int)
	defaultBrightness := []uint8{20, 40, 60, 80, 100}

	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			target := extractTargetFromPayload(event.Payload)

			mu.Lock()
			idx := cycleIndexes[event.Context]
			brightness := defaultBrightness[idx%len(defaultBrightness)]
			cycleIndexes[event.Context] = (idx + 1) % len(defaultBrightness)
			mu.Unlock()

			brightBytes, err := logitech.ConvertBrightnessTarget(logitech.FrontLight, brightness)
			if err != nil {
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if err := deviceMgr.WriteCommandsToTarget(target, brightBytes); err != nil {
				log.Println("Error setting front brightness:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			return client.SetTitle(ctx, strconv.Itoa(int(brightness))+"%", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Back Brightness Cycle ---
func setupBackBrightnessCycleAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.back.brightness")
	var mu sync.Mutex
	cycleIndexes := make(map[string]int)
	defaultBrightness := []uint8{20, 40, 60, 80, 100}

	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			target := extractTargetFromPayload(event.Payload)

			mu.Lock()
			idx := cycleIndexes[event.Context]
			brightness := defaultBrightness[idx%len(defaultBrightness)]
			cycleIndexes[event.Context] = (idx + 1) % len(defaultBrightness)
			mu.Unlock()

			brightBytes, err := logitech.ConvertBrightnessTarget(logitech.BackLight, brightness)
			if err != nil {
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if err := deviceMgr.WriteCommandsToTarget(target, brightBytes); err != nil {
				log.Println("Error setting back brightness:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			return client.SetTitle(ctx, strconv.Itoa(int(brightness))+"%", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Pair & Sync Lights Action (NEW) ---
func setupSyncLightsAction(client *streamdeck.Client) {
	action := client.Action("com.stewjoel.logitech-litra-lights.sync")

	updateTitle := func(ctx context.Context) error {
		summaries := deviceMgr.GetSummaries()
		pairedCount := 0
		for _, s := range summaries {
			if s.Paired {
				pairedCount++
			}
		}
		mode := deviceMgr.GetSyncMode()
		if mode == SyncModeAllPaired {
			return client.SetTitle(ctx, fmt.Sprintf("SYNC\n%d/%d", pairedCount, len(summaries)), streamdeck.HardwareAndSoftware)
		}
		return client.SetTitle(ctx, mode.String(), streamdeck.HardwareAndSoftware)
	}

	action.RegisterHandler(streamdeck.WillAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		p := streamdeck.WillAppearPayload{}
		_ = json.Unmarshal(event.Payload, &p)
		pushDeviceListToPI(ctx, client, event.Context, p.Settings)
		return updateTitle(ctx)
	})

	action.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return updateTitle(ctx)
	})

	action.RegisterHandler(streamdeck.DidReceiveSettings, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		p := streamdeck.DidReceiveSettingsPayload{}
		if err := json.Unmarshal(event.Payload, &p); err == nil {
			var s SyncActionSettings
			if err := json.Unmarshal(p.Settings, &s); err == nil && len(s.PairedIDs) > 0 {
				deviceMgr.SetPairedDevices(s.PairedIDs)
			}
		}
		return updateTitle(ctx)
	})

	action.RegisterHandler(streamdeck.KeyDown, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		_ = deviceMgr.Refresh()
		nextMode := deviceMgr.CycleSyncMode()

		// When switching into SyncModeAllPaired, immediately synchronize current state across all paired lights
		if nextMode == SyncModeAllPaired {
			deviceMgr.stateMu.Lock()
			var syncCmds [][]byte
			if deviceMgr.frontOn {
				syncCmds = append(syncCmds, logitech.ConvertLightsOnTarget(logitech.FrontLight))
				syncCmds = append(syncCmds, deviceMgr.lastFrontCmds...)
			} else {
				syncCmds = append(syncCmds, logitech.ConvertLightsOffTarget(logitech.FrontLight))
			}
			if deviceMgr.backOn {
				syncCmds = append(syncCmds, logitech.ConvertLightsOnTarget(logitech.BackLight))
				syncCmds = append(syncCmds, deviceMgr.lastBackLightCmds...)
			} else {
				syncCmds = append(syncCmds, logitech.ConvertLightsOffTarget(logitech.BackLight))
			}
			deviceMgr.stateMu.Unlock()

			if len(syncCmds) > 0 {
				if err := deviceMgr.WriteCommandsToTarget(TargetSynced, syncCmds...); err != nil {
					log.Printf("Sync state broadcast warning: %v", err)
				}
			}
		}

		return updateTitle(ctx)
	})
}

// --- Turn Off All Lights Action ---
func setupTurnOffLightsAction(client *streamdeck.Client) {
	turnOffLightsAction := client.Action("com.stewjoel.logitech-litra-lights.off")

	turnOffLightsAction.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})

	turnOffLightsAction.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			target := extractTargetFromPayload(event.Payload)
			if err := turnOffAllLights(target); err != nil {
				log.Println("Error turning off lights:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}
			return client.SetTitle(ctx, "Off", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Set Brightness & Temperature Action ---
func setupSetLightsAction(client *streamdeck.Client) {
	setLightsAction := client.Action("com.stewjoel.logitech-litra-lights.set")
	var mu sync.Mutex
	settings := make(map[string]*Settings)

	setLightsAction.RegisterHandler(streamdeck.PropertyInspectorDidAppear, func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		pushDeviceListToPI(ctx, client, event.Context, nil)
		return nil
	})

	setLightsAction.RegisterHandler(
		streamdeck.WillAppear,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			p := streamdeck.WillAppearPayload{}
			if err := json.Unmarshal(event.Payload, &p); err != nil {
				return err
			}

			mu.Lock()
			s, ok := settings[event.Context]
			if !ok {
				s = &Settings{DeviceTarget: TargetSynced}
				settings[event.Context] = s
			}
			_ = json.Unmarshal(p.Settings, s)
			if s.Temperature == 0 {
				s.Temperature = 3200
				s.Brightness = 50
			}
			sCopy := *s
			mu.Unlock()

			pushDeviceListToPI(ctx, client, event.Context, p.Settings)

			background, err := streamdeck.Image(generateBackground(sCopy))
			if err != nil {
				return err
			}
			if err := client.SetImage(ctx, background, streamdeck.HardwareAndSoftware); err != nil {
				return err
			}
			return client.SetTitle(ctx, strconv.Itoa(int(sCopy.Temperature)), streamdeck.HardwareAndSoftware)
		},
	)

	setLightsAction.RegisterHandler(
		streamdeck.DidReceiveSettings,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			p := streamdeck.DidReceiveSettingsPayload{}
			if err := json.Unmarshal(event.Payload, &p); err != nil {
				return err
			}

			mu.Lock()
			s, ok := settings[event.Context]
			if !ok {
				s = &Settings{DeviceTarget: TargetSynced}
				settings[event.Context] = s
			}
			_ = json.Unmarshal(p.Settings, s)
			if s.Temperature == 0 {
				s.Temperature = 3200
				s.Brightness = 50
			}
			sCopy := *s
			mu.Unlock()

			background, err := streamdeck.Image(generateBackground(sCopy))
			if err != nil {
				return err
			}
			if err := client.SetImage(ctx, background, streamdeck.HardwareAndSoftware); err != nil {
				return err
			}
			return client.SetTitle(ctx, strconv.Itoa(int(sCopy.Temperature)), streamdeck.HardwareAndSoftware)
		},
	)

	setLightsAction.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			mu.Lock()
			s, ok := settings[event.Context]
			if !ok {
				mu.Unlock()
				return fmt.Errorf("couldn't find settings for context %v", event.Context)
			}
			sCopy := *s
			mu.Unlock()

			target := sCopy.DeviceTarget
			if target == "" {
				target = TargetSynced
			}

			onBytes := logitech.ConvertLightsOn()
			brightBytes, err := logitech.ConvertBrightness(sCopy.Brightness)
			if err != nil {
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}
			tempBytes, err := logitech.ConvertTemperature(sCopy.Temperature)
			if err != nil {
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if err := deviceMgr.WriteCommandsToTarget(target, onBytes, brightBytes, tempBytes); err != nil {
				log.Println("Error setting lights:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			deviceMgr.stateMu.Lock()
			deviceMgr.frontOn = true
			deviceMgr.lastFrontCmds = [][]byte{brightBytes, tempBytes}
			deviceMgr.stateMu.Unlock()

			background, err := streamdeck.Image(generateBackground(sCopy))
			if err == nil {
				_ = client.SetImage(ctx, background, streamdeck.HardwareAndSoftware)
			}
			return client.SetTitle(ctx, strconv.Itoa(int(sCopy.Temperature)), streamdeck.HardwareAndSoftware)
		},
	)
}

func turnOffAllLights(target string) error {
	frontOff := logitech.ConvertLightsOffTarget(logitech.FrontLight)
	backOff := logitech.ConvertLightsOffTarget(logitech.BackLight)
	err := deviceMgr.WriteCommandsToTarget(target, frontOff, backOff)
	if err == nil {
		deviceMgr.stateMu.Lock()
		deviceMgr.frontOn = false
		deviceMgr.backOn = false
		deviceMgr.stateMu.Unlock()
	}
	return err
}
