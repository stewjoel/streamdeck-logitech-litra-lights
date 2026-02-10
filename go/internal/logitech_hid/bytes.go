package logitech_hid

import (
	"fmt"
)

// Logitech expects to receive 20 bytes when given a command.
const byteLength = 20

// Feature IDs for the Litra Beam LX
const (
	FrontLightFID     = 0x06 // Front (white) light feature ID
	BackLightFID      = 0x0a // Back (RGB) light power/brightness feature ID
	BackLightColorFID = 0x0C // Back (RGB) light color zone feature ID
)

// Number of color zones on the back light
const BackLightZoneCount = 7

// LightTarget specifies which light to control
type LightTarget byte

const (
	FrontLight LightTarget = FrontLightFID
	BackLight  LightTarget = BackLightFID
)

// --- Power On/Off ---

func ConvertLightsOn() (b []byte) {
	return ConvertLightsOnTarget(FrontLight)
}

func ConvertLightsOff() (b []byte) {
	return ConvertLightsOffTarget(FrontLight)
}

func ConvertLightsOnTarget(target LightTarget) (b []byte) {
	b = make([]byte, byteLength)
	if target == BackLight {
		// Back light uses function 0x4b for power control
		copy(b, []byte{0x11, 0xff, byte(target), 0x4b, 0x01})
	} else {
		copy(b, []byte{0x11, 0xff, byte(target), 0x1c, 0x01})
	}
	return
}

func ConvertLightsOffTarget(target LightTarget) (b []byte) {
	b = make([]byte, byteLength)
	if target == BackLight {
		// Back light uses function 0x4b for power control
		copy(b, []byte{0x11, 0xff, byte(target), 0x4b, 0x00})
	} else {
		copy(b, []byte{0x11, 0xff, byte(target), 0x1c, 0x00})
	}
	return
}

// --- Brightness ---

const (
	minPercentage = 1
	maxPercentage = 100
)

// ConvertBrightness sets front light brightness (1-100%)
func ConvertBrightness(percentage uint8) ([]byte, error) {
	return ConvertBrightnessTarget(FrontLight, percentage)
}

// ConvertBrightnessTarget sets brightness for a specific light target (1-100%)
func ConvertBrightnessTarget(target LightTarget, percentage uint8) ([]byte, error) {
	if percentage < minPercentage {
		return nil, fmt.Errorf("percentage must be greater than 1, was %d", percentage)
	}
	if percentage > maxPercentage {
		return nil, fmt.Errorf("percentage must be less than 100, was %d", percentage)
	}

	b := make([]byte, byteLength)

	if target == BackLight {
		// Back light brightness uses function 0x2b with direct percentage value
		copy(b, []byte{0x11, 0xff, byte(target), 0x2b, 0x00, percentage})
	} else {
		// Front light brightness uses function 0x4c with mapped value
		copy(b, []byte{0x11, 0xff, byte(target), 0x4c, 0x00, calcBrightness(percentage)})
	}

	return b, nil
}

// --- Temperature ---

const (
	minTemperature = 2700
	maxTemperature = 6500
)

// ConvertTemperature sets front light temperature (2700-6500K)
func ConvertTemperature(temperature uint16) ([]byte, error) {
	return ConvertTemperatureTarget(FrontLight, temperature)
}

// ConvertTemperatureTarget sets temperature for a specific light target (2700-6500K)
func ConvertTemperatureTarget(target LightTarget, temperature uint16) ([]byte, error) {
	if temperature < minTemperature {
		return nil, fmt.Errorf("temperature must be greater than 2700, was %d", temperature)
	}
	if temperature > maxTemperature {
		return nil, fmt.Errorf("temperature must be less than 6500, was %d", temperature)
	}

	b := make([]byte, byteLength)
	b[0] = 0x11
	b[1] = 0xff
	b[2] = byte(target)
	b[3] = 0x9c

	b[4] = byte(temperature >> 8) //nolint:mnd // Split temperature into two bytes
	b[5] = byte(temperature)

	return b, nil
}

// --- Back Light Color ---

// ConvertBackColorZone generates the command to set a single zone's RGB color.
// Zone IDs range from 1 to 7. RGB values are clamped to minimum 1.
// After setting all desired zones, you must call ConvertBackColorCommit() and write it.
func ConvertBackColorZone(zone uint8, r, g, b uint8) []byte {
	// The device freaks out if RGB values are 0, clamp to 1
	if r == 0 {
		r = 1
	}
	if g == 0 {
		g = 1
	}
	if b == 0 {
		b = 1
	}

	buf := make([]byte, byteLength)
	copy(buf, []byte{
		0x11, 0xff, BackLightColorFID, 0x1B,
		zone, r, g, b,
		0xFF, 0x00, 0x00, 0x00,
		0xFF, 0x00, 0x00, 0x00,
		0xFF, 0x00, 0x00, 0x00,
	})
	return buf
}

// ConvertBackColorCommit generates the commit command that must be sent
// after setting zone colors to apply the changes.
func ConvertBackColorCommit() []byte {
	buf := make([]byte, byteLength)
	copy(buf, []byte{0x11, 0xff, BackLightColorFID, 0x7B, 0x00, 0x00, 0x01, 0x00, 0x00})
	return buf
}

// ConvertBackColorAllZones generates a sequence of commands to set ALL 7 zones
// to the same RGB color, including the final commit command.
// Returns a slice of byte slices that should be written in order.
func ConvertBackColorAllZones(r, g, b uint8) [][]byte {
	commands := make([][]byte, BackLightZoneCount+1)
	for i := uint8(1); i <= BackLightZoneCount; i++ {
		commands[i-1] = ConvertBackColorZone(i, r, g, b)
	}
	commands[BackLightZoneCount] = ConvertBackColorCommit()
	return commands
}

// ConvertColorTarget is a convenience wrapper that sets back light color on all zones.
// For the front light, this is a no-op (front light doesn't support RGB).
func ConvertColorTarget(target LightTarget, r, g, b uint8) ([][]byte, error) {
	if target != BackLight {
		return nil, fmt.Errorf("RGB color is only supported on BackLight target")
	}
	return ConvertBackColorAllZones(r, g, b), nil
}

// --- Internal helpers ---

const (
	minBrightnessByte = 0x14
	maxBrightnessByte = 0xfa
)

// Takes 1-100 and returns 20-250
//
// For some reason, the Logitech HID API expects to receive
//
//	    1% brightness as the byte  20 or 0x14, and
//		 100% brightness as the byte 250 or 0xfa,
//		 (and everything in between)
func calcBrightness(brightness uint8) byte {
	return byte(int(float64(brightness-1.0)/(99.0)*(maxBrightnessByte-minBrightnessByte)) + minBrightnessByte)
}
