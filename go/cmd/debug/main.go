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
		fmt.Printf("[%d] Path: %s\n", len(devices)-1, info.Path)
		fmt.Printf("    Interface: %d | UsagePage: 0x%04x | Usage: 0x%04x\n", info.InterfaceNbr, info.UsagePage, info.Usage)
		fmt.Printf("    Product: %s\n", info.ProductStr)
		return nil
	})

	if len(devices) == 0 {
		fmt.Println("Litra Beam LX not found.")
		return
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("\nSelect device index: ")
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

	fmt.Printf("\nConnected to [%d] (UsagePage: 0x%04x)\n", idx, selected.UsagePage)
	fmt.Println("\nCommands (back light FID=0x0a):")
	fmt.Println("  1. Back Power ON:    11 ff 0a 1c 01")
	fmt.Println("  2. Back Power OFF:   11 ff 0a 1c 00")
	fmt.Println("  3. Back Red Static:  11 ff 0a 4c 01 ff 00 00")
	fmt.Println("  4. Back Red Cycle:   11 ff 0a 4c 02 ff 00 00")
	fmt.Println("  5. Back Red Mode3:   11 ff 0a 4c 03 ff 00 00")
	fmt.Println("\nCommands (front light FID=0x06):")
	fmt.Println("  6. Front Power ON:   11 ff 06 1c 01")
	fmt.Println("  7. Front Power OFF:  11 ff 06 1c 00")
	fmt.Println("\nAlternate back light FIDs to try:")
	fmt.Println("  8.  FID=0x07 ON:     11 ff 07 1c 01")
	fmt.Println("  9.  FID=0x08 ON:     11 ff 08 1c 01")
	fmt.Println("  10. FID=0x09 ON:     11 ff 09 1c 01")
	fmt.Println("  11. FID=0x0b ON:     11 ff 0b 1c 01")
	fmt.Println("  12. FID=0x0c ON:     11 ff 0c 1c 01")
	fmt.Println("  13. FID=0x0d ON:     11 ff 0d 1c 01")
	fmt.Println("\nOr type hex directly (e.g. '11 ff 0a 1c 01'). 'r' to read response. 'q' to quit.")

	for {
		fmt.Print("> ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "q" {
			break
		}
		if input == "r" {
			buf := make([]byte, 20)
			n, err := dev.ReadWithTimeout(buf, 1000)
			if err != nil {
				fmt.Printf("Read error: %v\n", err)
			} else {
				fmt.Printf("Read %d bytes: % x\n", n, buf[:n])
			}
			continue
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
			cmd = []byte{0x11, 0xff, 0x06, 0x1c, 0x01}
		case "7":
			cmd = []byte{0x11, 0xff, 0x06, 0x1c, 0x00}
		case "8":
			cmd = []byte{0x11, 0xff, 0x07, 0x1c, 0x01}
		case "9":
			cmd = []byte{0x11, 0xff, 0x08, 0x1c, 0x01}
		case "10":
			cmd = []byte{0x11, 0xff, 0x09, 0x1c, 0x01}
		case "11":
			cmd = []byte{0x11, 0xff, 0x0b, 0x1c, 0x01}
		case "12":
			cmd = []byte{0x11, 0xff, 0x0c, 0x1c, 0x01}
		case "13":
			cmd = []byte{0x11, 0xff, 0x0d, 0x1c, 0x01}
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
			n, err := dev.Write(padded)
			if err != nil {
				fmt.Printf("Write error: %v\n", err)
			} else {
				fmt.Printf("Wrote %d bytes\n", n)
			}
			// Try to read response
			resp := make([]byte, 20)
			rn, rerr := dev.ReadWithTimeout(resp, 500)
			if rerr == nil && rn > 0 {
				fmt.Printf("Response: % x\n", resp[:rn])
			}
		}
	}
}
