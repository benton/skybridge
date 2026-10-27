package serial

import (
	"context"
	"sort"
)

// PortInfo describes one serial port on the machine. VID and PID are zero when
// the port is not behind a recognisable USB device.
type PortInfo struct {
	Path    string `json:"path"`
	VID     uint16 `json:"vid"`
	PID     uint16 `json:"pid"`
	Product string `json:"product,omitempty"`
}

// List enumerates the machine's serial ports, sorted by path. It never needs
// elevated privileges and finishes within the context's deadline.
func List(ctx context.Context) ([]PortInfo, error) {
	ports, err := list(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Path < ports[j].Path })
	return ports, nil
}
