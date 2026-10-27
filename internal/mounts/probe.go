package mounts

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// Probe is one request/reply exchange that identifies a mount family.
type Probe struct {
	// Send is written verbatim.
	Send string
	// Terminator ends the reply when non-zero; otherwise Length bytes are read.
	Terminator byte
	Length     int
	Timeout    time.Duration
	// Match reports whether the reply is the one this family gives.
	Match func(reply string) bool
}

var (
	ErrNoReply         = errors.New("mounts: no reply from mount")
	ErrUnexpectedReply = errors.New("mounts: unexpected reply")
	ErrUnidentified    = errors.New("mounts: no profile answered")
)

// Run sends the probe and reads the reply. On ErrUnexpectedReply the reply is
// still returned. When the mount does not answer in time the reader goroutine
// is abandoned, so the caller must close rw afterwards.
func (p *Probe) Run(ctx context.Context, rw io.ReadWriter) (string, error) {
	if _, err := io.WriteString(rw, p.Send); err != nil {
		return "", fmt.Errorf("mounts: write probe: %w", err)
	}
	type result struct {
		reply string
		err   error
	}
	res := make(chan result, 1)
	go func() {
		reply, err := p.read(rw)
		res <- result{reply, err}
	}()
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = probeTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case r := <-res:
		if r.err != nil {
			if r.reply == "" {
				return "", fmt.Errorf("%w: %v", ErrNoReply, r.err)
			}
			return r.reply, fmt.Errorf("%w: %q", ErrUnexpectedReply, r.reply)
		}
		if p.Match != nil && !p.Match(r.reply) {
			return r.reply, fmt.Errorf("%w: %q", ErrUnexpectedReply, r.reply)
		}
		return r.reply, nil
	case <-timer.C:
		return "", ErrNoReply
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// read collects the reply one byte at a time until the terminator or length.
func (p *Probe) read(r io.Reader) (string, error) {
	var reply []byte
	buf := make([]byte, 1)
	limit := p.Length
	if limit <= 0 {
		limit = 64
	}
	for len(reply) < limit {
		n, err := r.Read(buf)
		if n > 0 {
			reply = append(reply, buf[0])
			if p.Terminator != 0 && buf[0] == p.Terminator {
				return string(reply), nil
			}
		}
		if err != nil {
			return string(reply), err
		}
	}
	if p.Terminator != 0 {
		return string(reply), fmt.Errorf("no terminator within %d bytes", limit)
	}
	return string(reply), nil
}

// Opener opens the serial port at the given baud rate for Identify.
type Opener func(ctx context.Context, baud int) (io.ReadWriteCloser, error)

// Identify tries every profile with a probe, in table order, and returns the
// first one the mount answers along with its reply.
func Identify(ctx context.Context, open Opener, log *slog.Logger) (Profile, string, error) {
	return identify(ctx, open, log, 0)
}

// identify is Identify with an optional timeout override for tests.
func identify(ctx context.Context, open Opener, log *slog.Logger, timeout time.Duration) (Profile, string, error) {
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	for _, p := range profiles {
		if p.Probe == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return Profile{}, "", err
		}
		port, err := open(ctx, p.Baud)
		if err != nil {
			return Profile{}, "", fmt.Errorf("mounts: open at %d baud: %w", p.Baud, err)
		}
		probe := *p.Probe
		if timeout > 0 {
			probe.Timeout = timeout
		}
		reply, err := probe.Run(ctx, port)
		port.Close()
		log.Debug("probe", "profile", p.Name, "baud", p.Baud, "reply", reply, "err", err)
		if err == nil {
			return p, reply, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Profile{}, "", err
		}
	}
	return Profile{}, "", ErrUnidentified
}
