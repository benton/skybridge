// Package discovery decides which of a machine's serial ports is most likely
// to have a telescope hand controller on the other end.
package discovery

import (
	"context"
	"runtime"
	"sort"
	"strings"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

// Adapter is a USB-to-serial chipset family identified by its vendor ID.
type Adapter struct {
	Name string
	VID  uint16
}

// KnownAdapters are the chipsets found in telescope cables and in the
// controllers that have USB built in. Celestron's USB hand controllers use a
// Prolific chip, so they need no entry of their own.
var KnownAdapters = []Adapter{
	{"Prolific PL2303", 0x067B},
	{"FTDI", 0x0403},
	{"Silicon Labs CP210x", 0x10C4},
	{"WCH CH340/CH341", 0x1A86},
}

// Candidate is a port with a confidence score. 100 means a known adapter was
// seen on the USB bus, 40–60 means the name looks right, 0 means unlikely.
type Candidate struct {
	serial.PortInfo
	Adapter string `json:"adapter,omitempty"`
	Score   int    `json:"score"`
}

const (
	scoreAdapter = 100
	scoreName    = 50
	scoreACM     = 40
)

// Rank scores every port for the given GOOS and returns them best first,
// ties broken by path. Ports that score 0 are included so list-ports can show
// them.
func Rank(goos string, ports []serial.PortInfo) []Candidate {
	out := make([]Candidate, 0, len(ports))
	for _, p := range ports {
		c := Candidate{PortInfo: p}
		if a, ok := adapterFor(p.VID); ok {
			c.Adapter, c.Score = a.Name, scoreAdapter
		} else {
			c.Score = nameScore(goos, p.Path)
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func adapterFor(vid uint16) (Adapter, bool) {
	if vid == 0 {
		return Adapter{}, false
	}
	for _, a := range KnownAdapters {
		if a.VID == vid {
			return a, true
		}
	}
	return Adapter{}, false
}

var darwinNames = []string{"usbserial", "PL2303", "SLAB_USBtoUART", "wchusbserial", "usbmodem"}

func nameScore(goos, path string) int {
	switch goos {
	case "darwin":
		if !strings.HasPrefix(path, "/dev/cu.") {
			return 0
		}
		for _, n := range darwinNames {
			if strings.Contains(path, n) {
				return scoreName
			}
		}
	case "linux":
		if strings.HasPrefix(path, "/dev/ttyUSB") {
			return scoreName
		}
		if strings.HasPrefix(path, "/dev/ttyACM") {
			return scoreACM
		}
	case "windows":
		if strings.HasPrefix(strings.ToUpper(path), "COM") {
			return scoreName
		}
	}
	return 0
}

// findTimeout bounds how long enumeration may take; discovery must never stall the bridge.
const findTimeout = time.Second

// Find lists the machine's ports and returns the plausible ones, best first.
// An empty result with a nil error means nothing looks like a mount.
func Find(ctx context.Context) ([]Candidate, error) {
	ctx, cancel := context.WithTimeout(ctx, findTimeout)
	defer cancel()
	ports, err := serial.List(ctx)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, c := range Rank(runtime.GOOS, ports) {
		if c.Score > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}
