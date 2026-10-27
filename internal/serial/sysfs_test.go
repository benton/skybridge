// sysfs interface directories are named like 1-1:1.0, and NTFS rejects
// the colon, so the fixture cannot be built on Windows. listSysfs itself is
// only used on Linux.

//go:build !windows

package serial

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSysfsFake(t *testing.T) {
	root := t.TempDir()
	dev := filepath.Join(root, "dev")
	sys := filepath.Join(root, "sys", "class", "tty")
	for _, name := range []string{"ttyUSB0", "ttyACM0", "ttyS0"} {
		if err := os.MkdirAll(dev, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dev, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// ttyUSB0 -> device -> (ttyUSB0 interface dir) -> parent USB device with ids
	usbDev := filepath.Join(root, "sys", "devices", "usb1", "1-1")
	iface := filepath.Join(usbDev, "1-1:1.0")
	if err := os.MkdirAll(filepath.Join(iface, "ttyUSB0"), 0o755); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"idVendor": "067b\n", "idProduct": "23a3\n", "product": "USB-Serial Controller\n"} {
		if err := os.WriteFile(filepath.Join(usbDev, k), []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(sys, "ttyUSB0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(iface, "ttyUSB0"), filepath.Join(sys, "ttyUSB0", "device")); err != nil {
		t.Fatal(err)
	}
	// ttyACM0 has a device link but no USB ids anywhere above it
	acm := filepath.Join(root, "sys", "devices", "platform", "serial0")
	if err := os.MkdirAll(acm, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sys, "ttyACM0"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(acm, filepath.Join(sys, "ttyACM0", "device")); err != nil {
		t.Fatal(err)
	}

	got, err := listSysfs(dev, sys)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d ports: %+v", len(got), got)
	}
	if got[0].Path != filepath.Join(dev, "ttyACM0") || got[0].VID != 0 {
		t.Fatalf("[0] = %+v", got[0])
	}
	if got[1].Path != filepath.Join(dev, "ttyUSB0") || got[1].VID != 0x067B || got[1].PID != 0x23A3 || got[1].Product != "USB-Serial Controller" {
		t.Fatalf("[1] = %+v", got[1])
	}
}
