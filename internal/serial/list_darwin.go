package serial

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// list globs /dev/cu.* and decorates the result with USB IDs from ioreg when
// that works. ioreg is Apple's own tool and the only way to read the I/O
// registry without cgo; a failure leaves the IDs at zero.
func list(ctx context.Context) ([]PortInfo, error) {
	paths, err := filepath.Glob("/dev/cu.*")
	if err != nil {
		return nil, err
	}
	usb := map[string]PortInfo{}
	if out, err := exec.CommandContext(ctx, "ioreg", "-r", "-c", "IOUSBHostDevice", "-l", "-w0").Output(); err == nil {
		usb = parseIOReg(strings.NewReader(string(out)))
	}
	var ports []PortInfo
	for _, p := range paths {
		if strings.Contains(p, "Bluetooth") {
			continue
		}
		info := PortInfo{Path: p}
		if u, ok := usb[p]; ok {
			info.VID, info.PID, info.Product = u.VID, u.PID, u.Product
		}
		ports = append(ports, info)
	}
	return ports, nil
}

// ioregNode is one object in the registry tree while parsing.
type ioregNode struct {
	indent  int
	vid     uint16
	pid     uint16
	product string
}

// parseIOReg reads `ioreg -r -c IOUSBHostDevice -l -w0` output and maps each
// callout device path to the USB IDs of its nearest USB device ancestor.
func parseIOReg(r io.Reader) map[string]PortInfo {
	out := map[string]PortInfo{}
	var stack []ioregNode
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "+-o "); i >= 0 && strings.TrimLeft(line[:i], " |") == "" {
			for len(stack) > 0 && stack[len(stack)-1].indent >= i {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, ioregNode{indent: i})
			continue
		}
		if len(stack) == 0 {
			continue
		}
		key, value, ok := ioregProperty(line)
		if !ok {
			continue
		}
		top := &stack[len(stack)-1]
		switch key {
		case "idVendor":
			if n, err := strconv.ParseUint(value, 10, 16); err == nil {
				top.vid = uint16(n)
			}
		case "idProduct":
			if n, err := strconv.ParseUint(value, 10, 16); err == nil {
				top.pid = uint16(n)
			}
		case "USB Product Name":
			top.product = strings.Trim(value, `"`)
		case "IOCalloutDevice":
			path := strings.Trim(value, `"`)
			if info, ok := usbAncestor(stack); ok {
				info.Path = path
				out[path] = info
			}
		}
	}
	return out
}

// ioregProperty splits a `"key" = value` line, ignoring the tree prefix.
func ioregProperty(line string) (key, value string, ok bool) {
	s := strings.TrimLeft(line, " |")
	if !strings.HasPrefix(s, `"`) {
		return "", "", false
	}
	end := strings.Index(s[1:], `"`)
	if end < 0 {
		return "", "", false
	}
	key = s[1 : 1+end]
	rest := strings.TrimSpace(s[2+end:])
	if !strings.HasPrefix(rest, "=") {
		return "", "", false
	}
	return key, strings.TrimSpace(rest[1:]), true
}

// usbAncestor returns the IDs of the nearest ancestor carrying a vendor ID
// and the product name of the nearest ancestor that has one. Driver nodes
// repeat the IDs of their device but not its name, so the two can differ.
func usbAncestor(stack []ioregNode) (PortInfo, bool) {
	var info PortInfo
	found := false
	for j := len(stack) - 1; j >= 0; j-- {
		if !found && stack[j].vid != 0 {
			info.VID, info.PID, found = stack[j].vid, stack[j].pid, true
		}
		if info.Product == "" && stack[j].product != "" {
			info.Product = stack[j].product
		}
	}
	return info, found
}
