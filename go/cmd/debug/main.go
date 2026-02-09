package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/sstallion/go-hid"
)

const (
	VID = 0x046d
	PID = 0xc903
)

func main() {
	if err := hid.Init(); err != nil {
		log.Fatal(err)
	}
	defer hid.Exit()

	fmt.Printf("Searching for Litra Beam LX (0x%04x:0x%04x)...\n", VID, PID)
	var devices []*hid.DeviceInfo
	hid.Enumerate(VID, PID, func(info *hid.DeviceInfo) error {
		devices = append(devices, info)
		fmt.Printf("[%d] Path: %s | Interface: %d\n", len(devices)-1, info.Path, info.InterfaceNbr)
		return nil
	})

	if len(devices) == 0 {
		fmt.Println("Litra Beam LX not found.")
		return
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nSelect device index (usually 0): ")
	idxStr, _ := reader.ReadString('\n')
	idx, err := strconv.Atoi(strings.TrimSpace(idxStr))
	if err != nil || idx < 0 || idx >= len(devices) {
		idx = 0
	}

	selected := devices[idx]
	dev, err := hid.OpenPath(selected.Path)
	if err != nil {
		log.Fatal(err)
	}
	defer dev.Close()

	fmt.Println("\nConnected to Index 1 (Col02). Testing RGB Mode for Back Light (0x0a):")
	fmt.Println("Commands:")
	fmt.Println("1. Power ON:    11 ff 0a 1c 01")
	fmt.Println("2. Power OFF:   11 ff 0a 1c 00")
	fmt.Println("3. Red Static:  11 ff 0a 4c 01 ff 00 00 (Mode 0x01 + RGB)")
	fmt.Println("4. Red Cycle:   11 ff 0a 4c 02 ff 00 00 (Mode 0x02 + RGB)")
	fmt.Println("5. Red Other:   11 ff 0a 4c 03 ff 00 00 (Mode 0x03 + RGB)")
	fmt.Println("6. Brightness:  11 ff 0a 4c 00 00 00 00 (Try zeroing colors)")
	fmt.Println("Enter number 1-6 or direct hex. 'q' to quit.")

	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "q" {
			break
		}

		var cmd []byte
		switch input {
		case "1":
			cmd = []byte{0x11, 0xff, 0x0a, 0x1c, 0x01}
		case "2":
			cmd = []byte{0x11, 0xff, 0x0a, 0x1c, 0x00}
		case "3":
			cmd = []byte{0x11, 0xff, 0x0a, 0x4c, 0x01, 0xff, 0x00, 0x00}
		case "4":
			cmd = []byte{0x11, 0xff, 0x0a, 0x4c, 0x02, 0xff, 0x00, 0x00}
		case "5":
			cmd = []byte{0x11, 0xff, 0x0a, 0x4c, 0x03, 0xff, 0x00, 0x00}
		case "6":
			cmd = []byte{0x11, 0xff, 0x0a, 0x4c, 0x00, 0x00, 0x00, 0x00}
		default:
			parts := strings.Fields(input)
			for _, p := range parts {
				val, err := strconv.ParseUint(p, 16, 8)
				if err == nil {
					cmd = append(cmd, byte(val))
				}
			}
		}

		if len(cmd) > 0 {
			padded := make([]byte, 20)
			copy(padded, cmd)
			fmt.Printf("Sending: % x\n", padded)
			dev.Write(padded)
		}
	}
}
