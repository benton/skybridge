//go:build darwin || linux

package serial

import (
	"errors"
	"fmt"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

// pollSlice is how long a Read waits in the kernel before checking whether
// Close was called. It bounds how long Close takes to unblock a reader.
const pollSliceMs = 250

type unixPort struct {
	fd     int
	closed atomic.Bool
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
		if p.closed.Load() {
			return 0, ErrClosed
		}
		fds := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, pollSliceMs)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return 0, fmt.Errorf("serial: poll: %w", err)
		}
		if n == 0 {
			continue
		}
		if p.closed.Load() {
			return 0, ErrClosed
		}
		if fds[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 && fds[0].Revents&unix.POLLIN == 0 {
			return 0, errors.New("serial: device went away")
		}
		n, err = unix.Read(p.fd, buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return 0, fmt.Errorf("serial: read: %w", err)
		}
		if n == 0 {
			return 0, errors.New("serial: device went away")
		}
		return n, nil
	}
}

func (p *unixPort) Write(buf []byte) (int, error) {
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
	if p.closed.Load() {
		return ErrClosed
	}
	return flush(p.fd)
}

func (p *unixPort) Close() error {
	if p.closed.Swap(true) {
		return nil
	}
	if err := unix.Close(p.fd); err != nil {
		return fmt.Errorf("serial: close: %w", err)
	}
	return nil
}
