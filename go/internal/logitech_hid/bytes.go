package logitech_hid

import (
	"fmt"
)

// Logitech expects to receive 20 bytes when given a command.
const byteLength = 20

// Feature IDs for the Litra Beam LX
const (
	FrontLightFID = 0x06 // Front (white) light feature ID
	BackLightFID  = 0x0a // Back (RGB) light feature ID
)

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
	copy(b, []byte{0x11, 0xff, byte(target), 0x1c, 0x01})
	return
}

func ConvertLightsOffTarget(target LightTarget) (b []byte) {
	b = make([]byte, byteLength)
	copy(b, []byte{0x11, 0xff, byte(target), 0x1c, 0x00})
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
	copy(b, []byte{0x11, 0xff, byte(target), 0x4c, 0x00, calcBrightness(percentage)})

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
