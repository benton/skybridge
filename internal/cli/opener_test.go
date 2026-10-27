package cli

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/discovery"
	"git.h.usky.io/huskynet/skybridge/internal/mounts"
	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func nexstar(t *testing.T) mounts.Profile {
	t.Helper()
	p, _ := mounts.Lookup("nexstar")
	return p
}

// candidatesAfter returns nothing for the first n calls, then one candidate.
func candidatesAfter(n int, calls *atomic.Int32) func(context.Context) ([]discovery.Candidate, error) {
	return func(context.Context) ([]discovery.Candidate, error) {
		if int(calls.Add(1)) <= n {
			return nil, nil
		}
		return []discovery.Candidate{{PortInfo: serial.PortInfo{Path: "/dev/fake"}, Score: 50}}, nil
	}
}

func TestOpenerWaitsForPort(t *testing.T) {
	var finds atomic.Int32
	withHooks(t, candidatesAfter(2, &finds), responderPort("Kx", "x#", nil))
	open := newOpener(serveOptions{Profile: nexstar(t), Retry: 5 * time.Millisecond}, quietLog())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	port, err := open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port.Close()
	if finds.Load() != 3 {
		t.Fatalf("findPorts called %d times, want 3", finds.Load())
	}
}

func TestOpenerNotFoundRaceRetries(t *testing.T) {
	var opens atomic.Int32
	good := responderPort("Kx", "x#", nil)
	withHooks(t, oneCandidate(), func(path string, cfg serial.Config) (serial.Port, error) {
		if opens.Add(1) == 1 {
			return nil, serial.ErrNotFound
		}
		return good(path, cfg)
	})
	open := newOpener(serveOptions{Profile: nexstar(t), Retry: 5 * time.Millisecond}, quietLog())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	port, err := open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port.Close()
	if opens.Load() != 2 {
		t.Fatalf("openPort called %d times, want 2", opens.Load())
	}
}

func TestOpenerVerifyRetriesOnProbeFailure(t *testing.T) {
	var opens atomic.Int32
	silent := responderPort("Kx", "", nil)
	good := responderPort("Kx", "x#", nil)
	var first serial.Port
	withHooks(t, oneCandidate(), func(path string, cfg serial.Config) (serial.Port, error) {
		if opens.Add(1) == 1 {
			p, err := silent(path, cfg)
			first = p
			return p, err
		}
		return good(path, cfg)
	})
	open := newOpener(serveOptions{Profile: nexstar(t), Verify: true, Retry: 5 * time.Millisecond, probeTimeout: 50 * time.Millisecond}, quietLog())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	port, err := open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	if opens.Load() != 2 {
		t.Fatalf("openPort called %d times, want 2", opens.Load())
	}
	if _, err := first.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("first port not closed: %v", err)
	}
}

func TestOpenerReturnsOnCancel(t *testing.T) {
	withHooks(t, func(context.Context) ([]discovery.Candidate, error) { return nil, nil }, responderPort("Kx", "x#", nil))
	open := newOpener(serveOptions{Profile: nexstar(t), Retry: 5 * time.Millisecond}, quietLog())
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(30 * time.Millisecond); cancel() }()
	_, err := open(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want Canceled, got %v", err)
	}
}

func TestOpenerExplicitPortSkipsDiscovery(t *testing.T) {
	withHooks(t, func(context.Context) ([]discovery.Candidate, error) {
		t.Fatal("findPorts must not be called with --port")
		return nil, nil
	}, responderPort("Kx", "x#", nil))
	open := newOpener(serveOptions{Port: "/dev/explicit", Profile: nexstar(t), Retry: 5 * time.Millisecond}, quietLog())
	port, err := open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	port.Close()
}

// Keep net imported for the pipePort type used across cli tests.
var _ net.Conn
