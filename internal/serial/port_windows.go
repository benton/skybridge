package serial

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/windows"
)

// readSliceMs is how long ReadFile waits for data before returning empty so
// the loop can notice Close.
const readSliceMs = 250

// DCB flag bits (see the DCB structure in the Windows API).
const (
	dcbBinary         = 1 << 0
	dcbDTRControlMask = 3 << 4
	dcbDTREnable      = 1 << 4
	dcbRTSControlMask = 3 << 12
	dcbRTSEnable      = 1 << 12
	dcbFlowMask       = 1<<1 | 1<<2 | 1<<3 | 1<<6 | 1<<7 | 1<<8 | 1<<9 | 1<<10 | 1<<11 | 1<<14
)

type windowsPort struct {
	h      windows.Handle
	closed atomic.Bool
	// mu serialises Close against in-flight Read/Write calls so the handle
	// is never used after it is closed.
	mu sync.RWMutex
}

// devicePath turns COM3 into \\.\COM3, which is required for ports above COM9.
func devicePath(name string) string {
	if strings.HasPrefix(name, `\\.\`) {
		return name
	}
	return `\\.\` + name
}

func open(path string, cfg Config) (Port, error) {
	name, err := windows.UTF16PtrFromString(devicePath(path))
	if err != nil {
		return nil, fmt.Errorf("serial: %s: %w", path, err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("serial: open %s: %w", path, err)
	}
	if err := configure(h, cfg.Baud); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("serial: configure %s: %w", path, err)
	}
	p := &windowsPort{h: h}
	_ = p.Flush()
	return p, nil
}

func configure(h windows.Handle, baud int) error {
	var dcb windows.DCB
	dcb.DCBlength = uint32(unsafeSizeofDCB)
	if err := windows.GetCommState(h, &dcb); err != nil {
		return fmt.Errorf("GetCommState: %w", err)
	}
	dcb.BaudRate = uint32(baud)
	dcb.ByteSize = 8
	dcb.Parity = windows.NOPARITY
	dcb.StopBits = windows.ONESTOPBIT
	dcb.Flags &^= dcbFlowMask | dcbDTRControlMask | dcbRTSControlMask
	dcb.Flags |= dcbBinary | dcbDTREnable | dcbRTSEnable
	if err := windows.SetCommState(h, &dcb); err != nil {
		return fmt.Errorf("SetCommState: %w", err)
	}
	t := windows.CommTimeouts{
		ReadIntervalTimeout:        ^uint32(0),
		ReadTotalTimeoutMultiplier: ^uint32(0),
		ReadTotalTimeoutConstant:   readSliceMs,
	}
	if err := windows.SetCommTimeouts(h, &t); err != nil {
		return fmt.Errorf("SetCommTimeouts: %w", err)
	}
	return nil
}

func (p *windowsPort) Read(buf []byte) (int, error) {
	for {
		n, err, again := p.readSlice(buf)
		if again {
			continue
		}
		return n, err
	}
}

// readSlice performs one ReadFile under the read lock; the comm timeouts make
// it return within readSliceMs when nothing arrives, in which case again is set.
func (p *windowsPort) readSlice(buf []byte) (n int, err error, again bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return 0, ErrClosed, false
	}
	var got uint32
	if err := windows.ReadFile(p.h, buf, &got, nil); err != nil {
		if p.closed.Load() {
			return 0, ErrClosed, false
		}
		return 0, fmt.Errorf("serial: read: %w", err), false
	}
	if got == 0 {
		return 0, nil, true
	}
	return int(got), nil, false
}

func (p *windowsPort) Write(buf []byte) (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return 0, ErrClosed
	}
	written := 0
	for written < len(buf) {
		var n uint32
		if err := windows.WriteFile(p.h, buf[written:], &n, nil); err != nil {
			if p.closed.Load() {
				return written, ErrClosed
			}
			return written, fmt.Errorf("serial: write: %w", err)
		}
		if n == 0 {
			return written, errors.New("serial: write: device went away")
		}
		written += int(n)
	}
	return written, nil
}

func (p *windowsPort) Flush() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return ErrClosed
	}
	return windows.PurgeComm(p.h, windows.PURGE_RXCLEAR|windows.PURGE_TXCLEAR)
}

// Close marks the port closed, cancels a ReadFile in flight, waits for it to
// release the read lock, then closes the handle.
func (p *windowsPort) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	_ = windows.CancelIoEx(p.h, nil)
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := windows.CloseHandle(p.h); err != nil {
		return fmt.Errorf("serial: close: %w", err)
	}
	return nil
}
