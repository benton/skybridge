package serial

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// listSysfs lists ttyUSB* and ttyACM* devices under devDir and looks up each
// one's USB vendor and product IDs through sysClassTTY (/sys/class/tty).
// It is a plain function of two directories so it can be tested anywhere.
func listSysfs(devDir, sysClassTTY string) ([]PortInfo, error) {
	var ports []PortInfo
	for _, pattern := range []string{"ttyUSB*", "ttyACM*"} {
		matches, err := filepath.Glob(filepath.Join(devDir, pattern))
		if err != nil {
			return nil, err
		}
		for _, path := range matches {
			info := PortInfo{Path: path}
			if dev, err := filepath.EvalSymlinks(filepath.Join(sysClassTTY, filepath.Base(path), "device")); err == nil {
				info.VID, info.PID, info.Product = usbIDsAbove(dev)
			}
			ports = append(ports, info)
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Path < ports[j].Path })
	return ports, nil
}

// usbIDsAbove walks up from dir looking for the idVendor file a USB device
// directory carries, at most four levels.
func usbIDsAbove(dir string) (vid, pid uint16, product string) {
	for i := 0; i < 4; i++ {
		if v, ok := readHex16(filepath.Join(dir, "idVendor")); ok {
			p, _ := readHex16(filepath.Join(dir, "idProduct"))
			name, _ := os.ReadFile(filepath.Join(dir, "product"))
			return v, p, strings.TrimSpace(string(name))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return 0, 0, ""
}

func readHex16(path string) (uint16, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}
