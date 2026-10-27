// Package serial opens serial ports with the fixed framing telescope mounts use
// (8 data bits, no parity, 1 stop bit, no flow control) and lists the ports a
// machine has. It is the only package in skybridge with OS-specific code.
package serial

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Config holds the only tunable: the baud rate. Framing is always 8N1.
type Config struct {
	Baud int
}

// Port is an open serial port.
type Port interface {
	io.ReadWriteCloser
	// Flush discards everything pending in the OS input and output buffers.
	Flush() error
}

var (
	ErrNotFound        = errors.New("serial: port not found")
	ErrClosed          = errors.New("serial: port closed")
	ErrUnsupportedBaud = errors.New("serial: unsupported baud rate")
)

// SupportedBauds lists the rates every platform backend accepts.
var SupportedBauds = []int{4800, 9600, 19200, 38400, 57600, 115200}

// SupportedBaudList formats SupportedBauds for error messages: "4800, 9600, ...".
func SupportedBaudList() string {
	parts := make([]string, len(SupportedBauds))
	for i, b := range SupportedBauds {
		parts[i] = fmt.Sprint(b)
	}
	return strings.Join(parts, ", ")
}

// Open opens the port at path with cfg. The baud rate is validated before the
// device is touched. A missing device returns an error wrapping ErrNotFound.
func Open(path string, cfg Config) (Port, error) {
	if !slices.Contains(SupportedBauds, cfg.Baud) {
		return nil, fmt.Errorf("%w: %d (supported: %s)", ErrUnsupportedBaud, cfg.Baud, SupportedBaudList())
	}
	return open(path, cfg)
}
