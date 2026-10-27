package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"git.h.usky.io/huskynet/skybridge/internal/discovery"
	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

func fakePorts(ports ...serial.PortInfo) func(context.Context) ([]serial.PortInfo, error) {
	return func(context.Context) ([]serial.PortInfo, error) { return ports, nil }
}

var twoPorts = []serial.PortInfo{
	{Path: "/dev/cu.Bluetooth-Incoming-Port"},
	{Path: "/dev/cu.PL2303G-USBtoUART110", VID: 0x067B, PID: 0x23A3, Product: "USB-Serial Controller"},
}

func TestListPortsTable(t *testing.T) {
	listPorts = fakePorts(twoPorts...)
	defer func() { listPorts = serial.List }()
	var out, errb bytes.Buffer
	if code := Main([]string{"list-ports"}, &out, &errb, noEnv); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 2 rows, got %q", out.String())
	}
	if !strings.HasPrefix(lines[0], "PATH") || !strings.Contains(lines[0], "VID:PID") {
		t.Fatal(lines[0])
	}
	if !strings.HasPrefix(lines[1], "/dev/cu.PL2303G") || !strings.Contains(lines[1], "Prolific PL2303") || !strings.Contains(lines[1], "067b:23a3") {
		t.Fatal(lines[1])
	}
	if !strings.HasPrefix(lines[2], "/dev/cu.Bluetooth") || !strings.Contains(lines[2], "-") {
		t.Fatal(lines[2])
	}
}

func TestListPortsJSON(t *testing.T) {
	listPorts = fakePorts(twoPorts...)
	defer func() { listPorts = serial.List }()
	var out bytes.Buffer
	if code := Main([]string{"list-ports", "--json"}, &out, &out, noEnv); code != 0 {
		t.Fatal(out.String())
	}
	var got []discovery.Candidate
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err, out.String())
	}
	if len(got) != 2 || got[0].Score != 100 || got[0].Adapter != "Prolific PL2303" {
		t.Fatalf("%+v", got)
	}
}

func TestListPortsNone(t *testing.T) {
	listPorts = fakePorts()
	defer func() { listPorts = serial.List }()
	var out, errb bytes.Buffer
	if code := Main([]string{"list-ports"}, &out, &errb, noEnv); code != 0 {
		t.Fatal(code)
	}
	if out.Len() != 0 || !strings.Contains(errb.String(), "no serial ports found") {
		t.Fatalf("out %q err %q", out.String(), errb.String())
	}
}
