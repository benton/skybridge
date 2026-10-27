package discovery

import (
	"testing"

	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

func TestRankVIDBeatsName(t *testing.T) {
	got := Rank("darwin", []serial.PortInfo{
		{Path: "/dev/cu.usbserial-1"},
		{Path: "/dev/cu.PL2303G-USBtoUART110", VID: 0x067B, PID: 0x23A3},
	})
	if got[0].Path != "/dev/cu.PL2303G-USBtoUART110" || got[0].Score != 100 || got[0].Adapter != "Prolific PL2303" {
		t.Fatalf("[0] = %+v", got[0])
	}
	if got[1].Path != "/dev/cu.usbserial-1" || got[1].Score != 50 || got[1].Adapter != "" {
		t.Fatalf("[1] = %+v", got[1])
	}
}

func TestRankDarwinPatterns(t *testing.T) {
	cases := map[string]int{
		"/dev/cu.Bluetooth-Incoming-Port": 0,
		"/dev/cu.debug-console":           0,
		"/dev/cu.SLAB_USBtoUART":          50,
		"/dev/cu.usbmodem1101":            50,
		"/dev/cu.wchusbserial1420":        50,
		"/dev/cu.PL2303G-USBtoUART110":    50,
	}
	for path, want := range cases {
		got := Rank("darwin", []serial.PortInfo{{Path: path}})
		if got[0].Score != want {
			t.Errorf("%s: score %d want %d", path, got[0].Score, want)
		}
	}
}

func TestRankLinuxPatterns(t *testing.T) {
	got := Rank("linux", []serial.PortInfo{{Path: "/dev/ttyS0"}, {Path: "/dev/ttyACM0"}, {Path: "/dev/ttyUSB0"}})
	want := []struct {
		path  string
		score int
	}{{"/dev/ttyUSB0", 50}, {"/dev/ttyACM0", 40}, {"/dev/ttyS0", 0}}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Score != w.score {
			t.Fatalf("[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

func TestRankWindows(t *testing.T) {
	got := Rank("windows", []serial.PortInfo{{Path: "COM1"}, {Path: "COM3", VID: 0x10C4, PID: 0xEA60}})
	if got[0].Path != "COM3" || got[0].Score != 100 || got[0].Adapter != "Silicon Labs CP210x" {
		t.Fatalf("[0] = %+v", got[0])
	}
	if got[1].Path != "COM1" || got[1].Score != 50 {
		t.Fatalf("[1] = %+v", got[1])
	}
}

func TestRankStableOrder(t *testing.T) {
	got := Rank("linux", []serial.PortInfo{{Path: "/dev/ttyUSB1"}, {Path: "/dev/ttyUSB0"}})
	if got[0].Path != "/dev/ttyUSB0" || got[1].Path != "/dev/ttyUSB1" {
		t.Fatalf("order %s, %s", got[0].Path, got[1].Path)
	}
}

func TestRankAllAdapters(t *testing.T) {
	for _, a := range KnownAdapters {
		got := Rank("linux", []serial.PortInfo{{Path: "/dev/ttyS9", VID: a.VID}})
		if got[0].Adapter != a.Name || got[0].Score != 100 {
			t.Errorf("%s: %+v", a.Name, got[0])
		}
	}
}
