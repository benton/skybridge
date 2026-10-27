package mounts

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

// responder answers a fixed request on the far end of a pipe, or stays silent.
func responder(t *testing.T, far net.Conn, want, reply string) {
	t.Helper()
	go func() {
		buf := make([]byte, 64)
		n, err := far.Read(buf)
		if err != nil {
			return
		}
		if string(buf[:n]) == want {
			_, _ = far.Write([]byte(reply))
		}
	}()
}

func probeOf(t *testing.T, name string) *Probe {
	t.Helper()
	p, ok := Lookup(name)
	if !ok || p.Probe == nil {
		t.Fatalf("no probe for %s", name)
	}
	pr := *p.Probe
	pr.Timeout = 200 * time.Millisecond
	return &pr
}

func TestProbeRunMatches(t *testing.T) {
	near, far := net.Pipe()
	defer near.Close()
	responder(t, far, "Kx", "x#")
	reply, err := probeOf(t, "nexstar").Run(context.Background(), near)
	if err != nil || reply != "x#" {
		t.Fatalf("reply %q err %v", reply, err)
	}
}

func TestProbeRunFixedLength(t *testing.T) {
	near, far := net.Pipe()
	defer near.Close()
	responder(t, far, ":MountInfo#", "0030")
	reply, err := probeOf(t, "ioptron").Run(context.Background(), near)
	if err != nil || reply != "0030" {
		t.Fatalf("reply %q err %v", reply, err)
	}
}

func TestProbeRunNoReply(t *testing.T) {
	near, far := net.Pipe()
	defer near.Close()
	responder(t, far, "never", "")
	p := probeOf(t, "nexstar")
	p.Timeout = 50 * time.Millisecond
	_, err := p.Run(context.Background(), near)
	if !errors.Is(err, ErrNoReply) {
		t.Fatalf("want ErrNoReply, got %v", err)
	}
}

func TestProbeRunMismatch(t *testing.T) {
	near, far := net.Pipe()
	defer near.Close()
	responder(t, far, "Kx", "zz#")
	reply, err := probeOf(t, "nexstar").Run(context.Background(), near)
	if !errors.Is(err, ErrUnexpectedReply) || reply != "zz#" {
		t.Fatalf("reply %q err %v", reply, err)
	}
}

// fakeOpener records the bauds it was asked for and serves a responder per port.
func fakeOpener(t *testing.T, bauds *[]int, want, reply string) Opener {
	return func(ctx context.Context, baud int) (io.ReadWriteCloser, error) {
		*bauds = append(*bauds, baud)
		near, far := net.Pipe()
		responder(t, far, want, reply)
		return near, nil
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestIdentifyStopsAtFirstMatch(t *testing.T) {
	var bauds []int
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, reply, err := identify(ctx, fakeOpener(t, &bauds, ":GVP#", "Losmandy Gemini#"), quiet(), 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "losmandy-gemini" || reply != "Losmandy Gemini#" {
		t.Fatalf("got %s %q", p.Name, reply)
	}
	if want := []int{9600, 9600, 9600}; !equalInts(bauds, want) {
		t.Fatalf("bauds %v want %v", bauds, want)
	}
}

func TestIdentifyNothingAnswers(t *testing.T) {
	var bauds []int
	_, _, err := identify(context.Background(), fakeOpener(t, &bauds, "never", ""), quiet(), 20*time.Millisecond)
	if !errors.Is(err, ErrUnidentified) {
		t.Fatalf("want ErrUnidentified, got %v", err)
	}
	// every profile with a probe, in order; generic never
	if want := []int{9600, 9600, 9600, 9600, 9600, 115200}; !equalInts(bauds, want) {
		t.Fatalf("bauds %v want %v", bauds, want)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
