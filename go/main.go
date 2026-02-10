package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"os/signal"
	"syscall"

	logitech "github.com/michaelabon/streamdeck-logitech-litra/internal/logitech_hid"
	"github.com/samwho/streamdeck"
	"github.com/sstallion/go-hid"
)

// Settings for the existing "Set Brightness & Temperature" action
type Settings struct {
	Temperature uint16 `json:"temperature,string"`
	Brightness  uint8  `json:"brightness,string"`
}

// CycleSettings stores a list of preset values and the current index
type CycleSettings struct {
	Presets []string `json:"presets"`
	Index   int      `json:"cycleIndex"`
}

// RGBSettings for the back light color picker
type RGBSettings struct {
	Red    uint8  `json:"red,string"`
	Green  uint8  `json:"green,string"`
	Blue   uint8  `json:"blue,string"`
	Color  string `json:"color"`  // hex color like "#ff0000"
	Color2 string `json:"color2"` // second hex color for gradient
	Mode   string `json:"mode"`   // "solid" or "gradient"
}

// GetRGB returns the resolved R, G, B values. If a hex color is set, it takes priority.
func (s *RGBSettings) GetRGB() (uint8, uint8, uint8) {
	if s.Color != "" && len(s.Color) == 7 {
		r, g, b := hexToRGB(s.Color)
		return r, g, b
	}
	return s.Red, s.Green, s.Blue
}

// GetRGB2 returns the second color for gradient mode.
func (s *RGBSettings) GetRGB2() (uint8, uint8, uint8) {
	if s.Color2 != "" && len(s.Color2) == 7 {
		return hexToRGB(s.Color2)
	}
	return 255, 255, 255
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

// Preset represents a saved color or gradient
type Preset struct {
	Mode   string `json:"mode"`   // "solid" or "gradient"
	Color  string `json:"color"`  // hex color like "#ff0000"
	Color2 string `json:"color2"` // second hex color for gradient
}

// PresetCycleSettings stores a list of presets and the current index
type PresetCycleSettings struct {
	Presets []Preset `json:"presets"`
	Index   int      `json:"cycleIndex"`
}

// --- Internal helpers for color conversion ---

func getRGBFromPreset(p Preset) (r, g, b, r2, g2, b2 uint8) {
	r, g, b = hexToRGB(p.Color)
	if p.Mode == "gradient" {
		r2, g2, b2 = hexToRGB(p.Color2)
	}
	return
}

const (
	VID       = 0x046d
	PID       = 0xc903
	UsagePage = 0xff43

	maxRetries  = 3
	retryDelay  = 500 * time.Millisecond
	reopenDelay = 1 * time.Second
)

// DeviceManager maintains a persistent HID connection to the Litra device.
// All writes are serialized through a mutex to prevent overlapped I/O conflicts.
type DeviceManager struct {
	mu     sync.Mutex
	device *hid.Device
	path   string
}

var deviceMgr = &DeviceManager{}

// connect finds and opens the Litra HID device. Must be called with mu held.
func (dm *DeviceManager) connect() error {
	if dm.device != nil {
		return nil // already connected
	}

	if err := hid.Init(); err != nil {
		return fmt.Errorf("hid.Init: %w", err)
	}

	var foundPath string
	err := hid.Enumerate(VID, PID, func(info *hid.DeviceInfo) error {
		if info.UsagePage == UsagePage {
			foundPath = info.Path
		}
		return nil
	})
	if err != nil {
		hid.Exit()
		return fmt.Errorf("hid.Enumerate: %w", err)
	}

	if foundPath == "" {
		hid.Exit()
		return fmt.Errorf("no Litra device found (VID=0x%04x PID=0x%04x UsagePage=0x%04x)", VID, PID, UsagePage)
	}

	d, err := hid.OpenPath(foundPath)
	if err != nil {
		hid.Exit()
		return fmt.Errorf("hid.OpenPath(%s): %w", foundPath, err)
	}

	dm.device = d
	dm.path = foundPath
	log.Printf("HID device connected: %s", foundPath)
	return nil
}

// reconnect closes the current connection and opens a new one. Must be called with mu held.
func (dm *DeviceManager) reconnect() error {
	if dm.device != nil {
		dm.device.Close()
		dm.device = nil
	}
	hid.Exit()
	time.Sleep(reopenDelay)
	return dm.connect()
}

// Close shuts down the device connection.
func (dm *DeviceManager) Close() {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if dm.device != nil {
		dm.device.Close()
		dm.device = nil
	}
	hid.Exit()
}

// WriteCommands sends one or more byte sequences to the device, with retry on failure.
func (dm *DeviceManager) WriteCommands(commands ...[]byte) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := dm.connect(); err != nil {
			log.Printf("Connect failed (attempt %d/%d): %v", attempt+1, maxRetries+1, err)
			if attempt < maxRetries {
				time.Sleep(retryDelay)
				dm.reconnect()
			}
			continue
		}

		var writeErr error
		for _, cmd := range commands {
			if _, err := dm.device.Write(cmd); err != nil {
				writeErr = err
				break
			}
		}

		if writeErr == nil {
			return nil // success
		}

		log.Printf("Write failed (attempt %d/%d): %v", attempt+1, maxRetries+1, writeErr)
		if attempt < maxRetries {
			time.Sleep(retryDelay)
			if err := dm.reconnect(); err != nil {
				log.Printf("Reconnect failed: %v", err)
			}
		} else {
			return writeErr
		}
	}

	return fmt.Errorf("all %d write attempts failed", maxRetries+1)
}

func main() {
	exitCode := 0
	defer func() {
		os.Exit(exitCode)
	}()

	fileName := "streamdeck-logitech-litra-lights.log"
	f, err := os.CreateTemp("logs", fileName)
	if err != nil {
		log.Printf("error creating temp file: %v", err)
		exitCode = 83

		return
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			log.Printf("unable to close file \u201c%s\u201d: %v\n", fileName, err)
		}
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

	client := streamdeck.NewClient(ctx, params)
	setup(client)

	// Set up signal handling for graceful shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stop
		log.Println("Received termination signal, turning off lights...")
		turnOffAllLights()
		deviceMgr.Close()
		os.Exit(0)
	}()

	err = client.Run()

	// Power off all lights when Stream Deck quits
	log.Println("Plugin exiting, turning off lights...")
	turnOffAllLights()
	deviceMgr.Close()

	return err
}

func setup(client *streamdeck.Client) {
	settings := make(map[string]*Settings)

	// Existing actions
	setupSetLightsAction(client, settings)
	setupTurnOffLightsAction(client)

	// Litra Beam LX actions
	setupFrontPowerAction(client)
	setupBackPowerAction(client)
	setupFrontTempCycleAction(client)
	setupFrontBrightnessCycleAction(client)
	setupBackBrightnessCycleAction(client)
	setupBackColorCycleAction(client)
	setupBackGradientCycleAction(client)
}

// ColorCycleSettings stores configurable solid color presets
type ColorCycleSettings struct {
	ColorPresets []string `json:"colorPresets"`
	Index        int      `json:"cycleIndex"`
}

var defaultColorPresets = []string{"#FF0000", "#00FF00", "#0000FF", "#FF00FF", "#FFFF00", "#00FFFF"}

// --- Back Color Cycle (configurable solid color presets) ---
func setupBackColorCycleAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.back.color")
	settings := make(map[string]*ColorCycleSettings)

	handler := func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		p := streamdeck.WillAppearPayload{}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}

		s, ok := settings[event.Context]
		if !ok {
			s = &ColorCycleSettings{
				ColorPresets: append([]string{}, defaultColorPresets...),
				Index:        0,
			}
			settings[event.Context] = s
		}

		if err := json.Unmarshal(p.Settings, s); err != nil {
			return err
		}

		// Use defaults if no presets configured
		if len(s.ColorPresets) == 0 {
			s.ColorPresets = append([]string{}, defaultColorPresets...)
		}

		if event.Event == streamdeck.KeyDown {
			idx := s.Index % len(s.ColorPresets)
			hex := s.ColorPresets[idx]
			r, g, b := hexToRGB(hex)

			// Advance index for next press
			s.Index = (s.Index + 1) % len(s.ColorPresets)
			client.SetSettings(ctx, s)

			log.Printf("Back Color Cycle: %s (%d, %d, %d) [%d/%d]\n", hex, r, g, b, idx+1, len(s.ColorPresets))

			onBytes := logitech.ConvertLightsOnTarget(logitech.BackLight)
			colorCmds, err := logitech.ConvertColorTarget(logitech.BackLight, r, g, b)
			if err != nil {
				log.Println("Error building color commands:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}
			commands := append([][]byte{onBytes}, colorCmds...)

			if err := deviceMgr.WriteCommands(commands...); err != nil {
				log.Println("Error setting back color:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			// Remember for back power restore
			lastBackLightCmds = colorCmds

			// Show a short label
			return client.SetTitle(ctx, fmt.Sprintf("%d/%d", idx+1, len(s.ColorPresets)), streamdeck.HardwareAndSoftware)
		}

		// Non-keydown: show current position
		if len(s.ColorPresets) > 0 {
			idx := s.Index % len(s.ColorPresets)
			client.SetTitle(ctx, fmt.Sprintf("%d/%d", idx+1, len(s.ColorPresets)), streamdeck.HardwareAndSoftware)
		}

		return nil
	}

	action.RegisterHandler(streamdeck.WillAppear, handler)
	action.RegisterHandler(streamdeck.DidReceiveSettings, handler)
	action.RegisterHandler(streamdeck.KeyDown, handler)
}

// --- Back Gradient Cycle ---
func setupBackGradientCycleAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.back.presets")
	settings := make(map[string]*PresetCycleSettings)

	handler := func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
		p := streamdeck.WillAppearPayload{}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return err
		}

		s, ok := settings[event.Context]
		if !ok {
			s = &PresetCycleSettings{
				Presets: []Preset{},
				Index:   0,
			}
			settings[event.Context] = s
		}

		if err := json.Unmarshal(p.Settings, s); err != nil {
			return err
		}

		if len(s.Presets) > 0 {
			idx := s.Index % len(s.Presets)
			preset := s.Presets[idx]
			title := "Set"
			if preset.Mode == "gradient" {
				title = "Grad"
			}
			client.SetTitle(ctx, fmt.Sprintf("%s\n%d/%d", title, idx+1, len(s.Presets)), streamdeck.HardwareAndSoftware)
		} else {
			client.SetTitle(ctx, "None", streamdeck.HardwareAndSoftware)
		}

		if event.Event == streamdeck.KeyDown {
			if len(s.Presets) == 0 {
				return nil
			}

			idx := s.Index % len(s.Presets)
			preset := s.Presets[idx]

			// Advance index for next time
			s.Index = (s.Index + 1) % len(s.Presets)
			client.SetSettings(ctx, s)

			r1, g1, b1, r2, g2, b2 := getRGBFromPreset(preset)
			log.Printf("Back Preset Cycle: Applying preset %d/%d (mode=%s)\n", idx+1, len(s.Presets), preset.Mode)

			onBytes := logitech.ConvertLightsOnTarget(logitech.BackLight)
			commands := [][]byte{onBytes}
			if preset.Mode == "gradient" {
				commands = append(commands, logitech.ConvertBackColorGradient(r1, g1, b1, r2, g2, b2)...)
			} else {
				colorCmds, _ := logitech.ConvertColorTarget(logitech.BackLight, r1, g1, b1)
				commands = append(commands, colorCmds...)
			}

			if err := deviceMgr.WriteCommands(commands...); err != nil {
				log.Println("Error applying preset:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			// Remember for back power restore (commands without the onBytes)
			lastBackLightCmds = commands[1:]
		}

		return nil
	}

	action.RegisterHandler(streamdeck.WillAppear, handler)
	action.RegisterHandler(streamdeck.DidReceiveSettings, handler)
	action.RegisterHandler(streamdeck.KeyDown, handler)
}

// --- Power States (in-memory tracking) ---
var (
	frontOn           = make(map[string]bool)
	backOn            = make(map[string]bool)
	lastBackLightCmds [][]byte // tracks the last color/gradient commands sent to the back light
)

// --- Front Power On/Off ---
func setupFrontPowerAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.front.power")

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			isOn := frontOn[event.Context]
			frontOn[event.Context] = !isOn

			var byteSequence []byte
			if !isOn {
				log.Println("Front Power: ON")
				byteSequence = logitech.ConvertLightsOnTarget(logitech.FrontLight)
			} else {
				log.Println("Front Power: OFF")
				byteSequence = logitech.ConvertLightsOffTarget(logitech.FrontLight)
			}

			if err := deviceMgr.WriteCommands(byteSequence); err != nil {
				log.Println("Error toggling front power:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if !isOn {
				return client.SetTitle(ctx, "ON", streamdeck.HardwareAndSoftware)
			}
			return client.SetTitle(ctx, "OFF", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Back Power On/Off ---
func setupBackPowerAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.back.power")

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			isOn := backOn[event.Context]
			backOn[event.Context] = !isOn

			if !isOn {
				log.Println("Back Power: ON")
				onBytes := logitech.ConvertLightsOnTarget(logitech.BackLight)
				// Re-apply last color if available
				if len(lastBackLightCmds) > 0 {
					cmds := append([][]byte{onBytes}, lastBackLightCmds...)
					if err := deviceMgr.WriteCommands(cmds...); err != nil {
						log.Println("Error toggling back power:", err)
						return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
					}
				} else {
					if err := deviceMgr.WriteCommands(onBytes); err != nil {
						log.Println("Error toggling back power:", err)
						return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
					}
				}
			} else {
				log.Println("Back Power: OFF")
				if err := deviceMgr.WriteCommands(logitech.ConvertLightsOffTarget(logitech.BackLight)); err != nil {
					log.Println("Error toggling back power:", err)
					return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
				}
			}

			if !isOn {
				return client.SetTitle(ctx, "ON", streamdeck.HardwareAndSoftware)
			}
			return client.SetTitle(ctx, "OFF", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Front Temperature Cycle ---
func setupFrontTempCycleAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.front.temperature")
	cycleIndexes := make(map[string]int)

	defaultTemps := []uint16{2700, 3200, 4000, 5000, 6500}

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			idx := cycleIndexes[event.Context]
			temp := defaultTemps[idx%len(defaultTemps)]
			cycleIndexes[event.Context] = (idx + 1) % len(defaultTemps)

			log.Printf("Front Temp Cycle: %dK\n", temp)

			onBytes := logitech.ConvertLightsOnTarget(logitech.FrontLight)
			tempBytes, err := logitech.ConvertTemperatureTarget(logitech.FrontLight, temp)
			if err != nil {
				log.Println("Error building temp command:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if err := deviceMgr.WriteCommands(onBytes, tempBytes); err != nil {
				log.Println("Error setting front temp:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			return client.SetTitle(ctx, strconv.Itoa(int(temp))+"K", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Front Brightness Cycle ---
func setupFrontBrightnessCycleAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.front.brightness")
	cycleIndexes := make(map[string]int)

	defaultBrightness := []uint8{20, 40, 60, 80, 100}

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			idx := cycleIndexes[event.Context]
			brightness := defaultBrightness[idx%len(defaultBrightness)]
			cycleIndexes[event.Context] = (idx + 1) % len(defaultBrightness)

			log.Printf("Front Brightness Cycle: %d%%\n", brightness)

			brightBytes, err := logitech.ConvertBrightnessTarget(logitech.FrontLight, brightness)
			if err != nil {
				log.Println("Error building brightness command:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if err := deviceMgr.WriteCommands(brightBytes); err != nil {
				log.Println("Error setting front brightness:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			return client.SetTitle(ctx, strconv.Itoa(int(brightness))+"%", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Back Brightness Cycle ---
func setupBackBrightnessCycleAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.back.brightness")
	cycleIndexes := make(map[string]int)

	defaultBrightness := []uint8{20, 40, 60, 80, 100}

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			idx := cycleIndexes[event.Context]
			brightness := defaultBrightness[idx%len(defaultBrightness)]
			cycleIndexes[event.Context] = (idx + 1) % len(defaultBrightness)

			log.Printf("Back Brightness Cycle: %d%%\n", brightness)

			brightBytes, err := logitech.ConvertBrightnessTarget(logitech.BackLight, brightness)
			if err != nil {
				log.Println("Error building brightness command:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			if err := deviceMgr.WriteCommands(brightBytes); err != nil {
				log.Println("Error setting back brightness:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}

			return client.SetTitle(ctx, strconv.Itoa(int(brightness))+"%", streamdeck.HardwareAndSoftware)
		},
	)
}

// --- Legacy actions ---

func setupTurnOffLightsAction(client *streamdeck.Client) {
	turnOffLightsAction := client.Action("ca.michaelabon.logitech-litra-lights.off")

	turnOffLightsAction.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			return handleTurnOffLights(ctx, client)
		},
	)
}

func setupSetLightsAction(client *streamdeck.Client, settings map[string]*Settings) {
	setLightsAction := client.Action("ca.michaelabon.logitech-litra-lights.set")

	setLightsAction.RegisterHandler(
		streamdeck.WillAppear,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			p := streamdeck.WillAppearPayload{}
			if err := json.Unmarshal(event.Payload, &p); err != nil {
				return err
			}

			s, ok := settings[event.Context]
			if !ok {
				s = &Settings{}
				settings[event.Context] = s
			}

			if err := json.Unmarshal(p.Settings, s); err != nil {
				return err
			}

			if s.Temperature == 0 {
				s.Temperature = 3200
				s.Brightness = 50
			}

			background, err := streamdeck.Image(generateBackground(*s))
			if err != nil {
				log.Println("Error while generating streamdeck image", err)

				return err
			}

			if err := client.SetImage(ctx, background, streamdeck.HardwareAndSoftware); err != nil {
				return err
			}

			err = client.SetTitle(
				ctx,
				strconv.Itoa(int(s.Temperature)),
				streamdeck.HardwareAndSoftware,
			)

			return err
		},
	)

	setLightsAction.RegisterHandler(
		streamdeck.DidReceiveSettings,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			p := streamdeck.DidReceiveSettingsPayload{}
			if err := json.Unmarshal(event.Payload, &p); err != nil {
				return err
			}

			s, ok := settings[event.Context]
			if !ok {
				s = &Settings{}
				settings[event.Context] = s
			}

			if err := json.Unmarshal(p.Settings, s); err != nil {
				return err
			}

			background, err := streamdeck.Image(generateBackground(*s))
			if err != nil {
				log.Println("Error while generating streamdeck image", err)

				return err
			}

			if err := client.SetImage(ctx, background, streamdeck.HardwareAndSoftware); err != nil {
				return err
			}

			err = client.SetTitle(
				ctx,
				strconv.Itoa(int(s.Temperature)),
				streamdeck.HardwareAndSoftware,
			)

			return err
		},
	)

	setLightsAction.RegisterHandler(
		streamdeck.WillDisappear,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			s := settings[event.Context]

			return client.SetSettings(ctx, s)
		},
	)

	setLightsAction.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			return handleSetLights(ctx, client, event, settings)
		},
	)
}

func handleTurnOffLights(ctx context.Context, client *streamdeck.Client) error {
	if err := turnOffAllLights(); err != nil {
		log.Println("Error: ", err)
		return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
	}
	return nil
}

func handleSetLights(
	ctx context.Context,
	client *streamdeck.Client,
	event streamdeck.Event,
	settings map[string]*Settings,
) error {
	s, ok := settings[event.Context]
	if !ok {
		return fmt.Errorf("couldn't find settings for context %v", event.Context)
	}

	log.Printf("KeyDown with payload %+v\n", event.Payload)

	if err := client.SetSettings(ctx, s); err != nil {
		return err
	}

	background, err := streamdeck.Image(generateBackground(*s))
	if err != nil {
		log.Println("Error while generating streamdeck image", err)
		return err
	}

	// Build commands: turn on, set brightness, set temperature
	onBytes := logitech.ConvertLightsOn()
	brightBytes, err := logitech.ConvertBrightness(s.Brightness)
	if err != nil {
		log.Println("Error building brightness:", err)
		return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
	}
	tempBytes, err := logitech.ConvertTemperature(s.Temperature)
	if err != nil {
		log.Println("Error building temperature:", err)
		return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
	}

	if err := deviceMgr.WriteCommands(onBytes, brightBytes, tempBytes); err != nil {
		log.Println("Error: ", err)
		return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
	}

	if err := client.SetImage(ctx, background, streamdeck.HardwareAndSoftware); err != nil {
		log.Println("Error while setting the light background", err)
		return err
	}

	return client.SetTitle(ctx, strconv.Itoa(int(s.Temperature)), streamdeck.HardwareAndSoftware)
}

// turnOffAllLights sends commands to turn off both front and back lights.
func turnOffAllLights() error {
	frontOff := logitech.ConvertLightsOffTarget(logitech.FrontLight)
	backOff := logitech.ConvertLightsOffTarget(logitech.BackLight)
	return deviceMgr.WriteCommands(frontOff, backOff)
}
