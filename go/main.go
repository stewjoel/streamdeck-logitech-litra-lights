package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"

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

const (
	VID       = 0x046d
	PID       = 0xc903
	UsagePage = 0xff43
)

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

	return client.Run()
}

func setup(client *streamdeck.Client) {
	settings := make(map[string]*Settings)

	// Existing actions
	setupSetLightsAction(client, settings)
	setupTurnOffLightsAction(client)

	// New Litra Beam LX actions
	setupFrontPowerAction(client)
	setupBackPowerAction(client)
	setupFrontTempCycleAction(client)
	setupFrontBrightnessCycleAction(client)
	setupBackBrightnessCycleAction(client)
}

// --- Front Power On/Off ---
func setupFrontPowerAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.front.power")

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			log.Println("Front Power toggle")
			// Toggle: try turning on. If already on, turn off.
			err := writeToLights(func(deviceInfo *hid.DeviceInfo) error {
				d, err := hid.OpenPath(deviceInfo.Path)
				if err != nil {
					return err
				}
				defer d.Close()

				// Send ON command (if already on, sending ON again is harmless)
				byteSequence := logitech.ConvertLightsOnTarget(logitech.FrontLight)
				_, err = d.Write(byteSequence)
				return err
			})
			if err != nil {
				log.Println("Error toggling front power:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}
			return nil
		},
	)
}

// --- Back Power On/Off ---
func setupBackPowerAction(client *streamdeck.Client) {
	action := client.Action("ca.michaelabon.logitech-litra-lights.back.power")

	action.RegisterHandler(
		streamdeck.KeyDown,
		func(ctx context.Context, client *streamdeck.Client, event streamdeck.Event) error {
			log.Println("Back Power toggle")
			err := writeToLights(func(deviceInfo *hid.DeviceInfo) error {
				d, err := hid.OpenPath(deviceInfo.Path)
				if err != nil {
					return err
				}
				defer d.Close()

				byteSequence := logitech.ConvertLightsOnTarget(logitech.BackLight)
				_, err = d.Write(byteSequence)
				return err
			})
			if err != nil {
				log.Println("Error toggling back power:", err)
				return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
			}
			return nil
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

			err := writeToLights(func(deviceInfo *hid.DeviceInfo) error {
				d, err := hid.OpenPath(deviceInfo.Path)
				if err != nil {
					return err
				}
				defer d.Close()

				// Turn on first
				onBytes := logitech.ConvertLightsOnTarget(logitech.FrontLight)
				if _, err := d.Write(onBytes); err != nil {
					return err
				}

				tempBytes, err := logitech.ConvertTemperatureTarget(logitech.FrontLight, temp)
				if err != nil {
					return err
				}
				_, err = d.Write(tempBytes)
				return err
			})

			if err != nil {
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

			err := writeToLights(func(deviceInfo *hid.DeviceInfo) error {
				d, err := hid.OpenPath(deviceInfo.Path)
				if err != nil {
					return err
				}
				defer d.Close()

				brightBytes, err := logitech.ConvertBrightnessTarget(logitech.FrontLight, brightness)
				if err != nil {
					return err
				}
				_, err = d.Write(brightBytes)
				return err
			})

			if err != nil {
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

			err := writeToLights(func(deviceInfo *hid.DeviceInfo) error {
				d, err := hid.OpenPath(deviceInfo.Path)
				if err != nil {
					return err
				}
				defer d.Close()

				brightBytes, err := logitech.ConvertBrightnessTarget(logitech.BackLight, brightness)
				if err != nil {
					return err
				}
				_, err = d.Write(brightBytes)
				return err
			})

			if err != nil {
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
	err := writeToLights(sendTurnOffLights())
	if err != nil {
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

	err = writeToLights(sendBrightnessAndTemperature(*s))
	if err != nil {
		log.Println("Error: ", err)

		return client.SetTitle(ctx, "Err", streamdeck.HardwareAndSoftware)
	}

	err = client.SetImage(ctx, background, streamdeck.HardwareAndSoftware)
	if err != nil {
		log.Println("Error while setting the light background", err)

		return err
	}

	return client.SetTitle(ctx, strconv.Itoa(int(s.Temperature)), streamdeck.HardwareAndSoftware)
}

// writeToLights opens a connection to each light attached to the computer
// and then invokes theFunc for each light.
// Updated to use UsagePage filter for correct HID interface on Litra Beam LX.
func writeToLights(theFunc hid.EnumFunc) error {
	var err error

	if err = hid.Init(); err != nil {
		log.Println("Unable to hid.Init()", err)
		log.Println(err)
	}
	defer func() {
		err := hid.Exit()
		if err != nil {
			log.Println("unable to hid.Exit()", err)
		}
	}()

	err = hid.Enumerate(VID, PID, func(deviceInfo *hid.DeviceInfo) error {
		// Filter by UsagePage to ensure we hit the correct HID interface
		if deviceInfo.UsagePage == UsagePage {
			return theFunc(deviceInfo)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}

func sendBrightnessAndTemperature(settings Settings) hid.EnumFunc {
	return func(deviceInfo *hid.DeviceInfo) error {
		d, err := hid.OpenPath(deviceInfo.Path)
		if err != nil {
			log.Println("Unable to open", err)
			return err
		}
		defer func(d *hid.Device) {
			err := d.Close()
			if err != nil {
				log.Println("unable to hid.Device.Close()", err)
			}
		}(d)

		byteSequence := logitech.ConvertLightsOn()
		if _, err := d.Write(byteSequence); err != nil {
			log.Println(err)
			return err
		}

		byteSequence, err = logitech.ConvertBrightness(settings.Brightness)
		if err != nil {
			log.Println(err)
			return err
		}
		if _, err := d.Write(byteSequence); err != nil {
			log.Println("Unable to write bytes with set brightness", err)
			return err
		}
		byteSequence, err = logitech.ConvertTemperature(settings.Temperature)
		if err != nil {
			log.Println(err)
			return err
		}
		if _, err := d.Write(byteSequence); err != nil {
			log.Println("Unable to write bytes with set temperature", err)
			return err
		}

		return nil
	}
}

func sendTurnOffLights() hid.EnumFunc {
	return func(deviceInfo *hid.DeviceInfo) error {
		d, err := hid.OpenPath(deviceInfo.Path)
		if err != nil {
			log.Println("unable to open", err)
			return err
		}
		defer func(d *hid.Device) {
			err := d.Close()
			if err != nil {
				log.Println("unable to hid.Device.Close()", err)
			}
		}(d)

		// Turn off both lights
		byteSequence := logitech.ConvertLightsOffTarget(logitech.FrontLight)
		if _, err := d.Write(byteSequence); err != nil {
			log.Println("unable to write bytes with front lights off", err)
			return err
		}

		byteSequence = logitech.ConvertLightsOffTarget(logitech.BackLight)
		if _, err := d.Write(byteSequence); err != nil {
			log.Println("unable to write bytes with back lights off", err)
			return err
		}

		return nil
	}
}
