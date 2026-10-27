//go:build darwin || linux

package serial

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

// pollSlice is how long a Read waits in the kernel before checking whether
// Close was called. It bounds how long Close takes to unblock a reader.
const pollSliceMs = 250

type unixPort struct {
	fd     int
	closed atomic.Bool
	// mu serialises Close against in-flight Read/Write calls so the
	// descriptor is never used after it is closed (and possibly reused).
	mu sync.RWMutex
}

func open(path string, cfg Config) (Port, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, path)
		}
		return nil, fmt.Errorf("serial: open %s: %w", path, err)
	}
	if err := configure(fd, cfg.Baud); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("serial: configure %s: %w", path, err)
	}
	// Reads are paced by poll below; writes may block in the kernel.
	if err := unix.SetNonblock(fd, false); err != nil {
		unix.Close(fd)
		return nil, fmt.Errorf("serial: %s: %w", path, err)
	}
	p := &unixPort{fd: fd}
	_ = p.Flush()
	return p, nil
}

// configure puts fd in raw 8N1 mode at the given baud rate.
func configure(fd int, baud int) error {
	tio, err := getTermios(fd)
	if err != nil {
		return err
	}
	tio.Iflag = 0
	tio.Oflag = 0
	tio.Lflag = 0
	tio.Cflag = unix.CS8 | unix.CREAD | unix.CLOCAL
	tio.Cc[unix.VMIN] = 1
	tio.Cc[unix.VTIME] = 0
	if err := setSpeed(tio, baud); err != nil {
		return err
	}
	return unix.IoctlSetTermios(fd, ioctlSetTermios, tio)
}

func getTermios(fd int) (*unix.Termios, error) {
	return unix.IoctlGetTermios(fd, ioctlGetTermios)
}

func (p *unixPort) Read(buf []byte) (int, error) {
	for {
		n, err, again := p.readSlice(buf)
		if again {
			continue
		}
		return n, err
	}
}

// readSlice waits one poll slice for input under the read lock. again asks
// the caller to loop: nothing arrived yet, or a signal interrupted the call.
func (p *unixPort) readSlice(buf []byte) (n int, err error, again bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return 0, ErrClosed, false
	}
	fds := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
	ready, err := unix.Poll(fds, pollSliceMs)
	if err != nil {
		if errors.Is(err, unix.EINTR) {
			return 0, nil, true
		}
		return 0, fmt.Errorf("serial: poll: %w", err), false
	}
	if ready == 0 {
		return 0, nil, true
	}
	if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 && fds[0].Revents&unix.POLLIN == 0 {
		return 0, errors.New("serial: device went away"), false
	}
	n, err = unix.Read(p.fd, buf)
	if err != nil {
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			return 0, nil, true
		}
		return 0, fmt.Errorf("serial: read: %w", err), false
	}
	if n == 0 {
		return 0, errors.New("serial: device went away"), false
	}
	return n, nil, false
}

func (p *unixPort) Write(buf []byte) (int, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return 0, ErrClosed
	}
	written := 0
	for written < len(buf) {
		n, err := unix.Write(p.fd, buf[written:])
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if p.closed.Load() {
				return written, ErrClosed
			}
			return written, fmt.Errorf("serial: write: %w", err)
		}
		written += n
	}
	return written, nil
}

func (p *unixPort) Flush() error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed.Load() {
		return ErrClosed
	}
	return flush(p.fd)
}

// Close marks the port closed, waits for any in-flight Read or Write to
// finish its current slice (at most pollSliceMs), then closes the descriptor.
func (p *unixPort) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := unix.Close(p.fd); err != nil {
		return fmt.Errorf("serial: close: %w", err)
	}
	return nil
}
