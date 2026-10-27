package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"

	"git.h.usky.io/huskynet/skybridge/internal/discovery"
	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

// pipePort is a serial.Port backed by one end of a net.Pipe.
type pipePort struct{ net.Conn }

func (pipePort) Flush() error { return nil }

// responderPort returns an opener whose ports answer `want` with `reply`.
// Any other request, or an empty reply, leaves the mount silent.
func responderPort(want, reply string, opened *atomic.Int32) func(string, serial.Config) (serial.Port, error) {
	return func(path string, cfg serial.Config) (serial.Port, error) {
		if opened != nil {
			opened.Add(1)
		}
		near, far := net.Pipe()
		go func() {
			defer far.Close()
			buf := make([]byte, 64)
			for {
				n, err := far.Read(buf)
				if err != nil {
					return
				}
				if string(buf[:n]) == want && reply != "" {
					if _, err := far.Write([]byte(reply)); err != nil {
						return
					}
				}
			}
		}()
		return pipePort{near}, nil
	}
}

func oneCandidate() func(context.Context) ([]discovery.Candidate, error) {
	return func(context.Context) ([]discovery.Candidate, error) {
		return []discovery.Candidate{{PortInfo: serial.PortInfo{Path: "/dev/fake"}, Score: 50}}, nil
	}
}

func withHooks(t *testing.T, find func(context.Context) ([]discovery.Candidate, error), open func(string, serial.Config) (serial.Port, error)) {
	t.Helper()
	oldFind, oldOpen := findPorts, openPort
	findPorts, openPort = find, open
	t.Cleanup(func() { findPorts, openPort = oldFind, oldOpen })
}

func TestProbeWithMount(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	var out, errb bytes.Buffer
	if code := Main([]string{"probe", "--mount", "nexstar"}, &out, &errb, noEnv); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if want := "celestron-nexstar at /dev/fake (9600 baud): reply \"x#\"\n"; out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
}

func TestProbeIdentifies(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort(":GVP#", "Losmandy Gemini#", nil))
	var out, errb bytes.Buffer
	if code := Main([]string{"probe"}, &out, &errb, noEnv); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.HasPrefix(out.String(), "losmandy-gemini at /dev/fake (9600 baud)") {
		t.Fatal(out.String())
	}
}

func TestProbeJSON(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	var out bytes.Buffer
	if code := Main([]string{"probe", "--mount", "nexstar", "--json"}, &out, io.Discard, noEnv); code != 0 {
		t.Fatal(out.String())
	}
	var got struct {
		Profile string `json:"profile"`
		Path    string `json:"path"`
		Baud    int    `json:"baud"`
		Reply   string `json:"reply"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Profile != "celestron-nexstar" || got.Path != "/dev/fake" || got.Baud != 9600 || got.Reply != "x#" {
		t.Fatalf("%+v", got)
	}
}

func TestProbeNoAnswerExit3(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "", nil))
	var errb bytes.Buffer
	if code := Main([]string{"probe", "--mount", "nexstar"}, io.Discard, &errb, noEnv); code != ExitProbe {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "no reply") {
		t.Fatal(errb.String())
	}
}

func TestProbeNoPortsExit3(t *testing.T) {
	withHooks(t, func(context.Context) ([]discovery.Candidate, error) { return nil, nil }, responderPort("Kx", "x#", nil))
	var errb bytes.Buffer
	if code := Main([]string{"probe"}, io.Discard, &errb, noEnv); code != ExitProbe {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "no serial port found") {
		t.Fatal(errb.String())
	}
}

func TestProbeUnknownMountExit1(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	var errb bytes.Buffer
	if code := Main([]string{"probe", "--mount", "zzz"}, io.Discard, &errb, noEnv); code != ExitUsage {
		t.Fatal(code)
	}
	if !strings.Contains(errb.String(), `unknown mount profile "zzz"`) || !strings.Contains(errb.String(), "skybridge mounts") {
		t.Fatal(errb.String())
	}
}

func TestProbeBadBaudExit1(t *testing.T) {
	var opened atomic.Int32
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", &opened))
	var errb bytes.Buffer
	if code := Main([]string{"probe", "--mount", "nexstar", "--baud", "1200"}, io.Discard, &errb, noEnv); code != ExitUsage {
		t.Fatal(code)
	}
	if !strings.Contains(errb.String(), "4800, 9600, 19200, 38400, 57600, 115200") {
		t.Fatal(errb.String())
	}
	if opened.Load() != 0 {
		t.Fatal("port was opened despite bad baud")
	}
}

func TestProbeExplicitMissingPortExit1(t *testing.T) {
	withHooks(t, oneCandidate(), func(path string, cfg serial.Config) (serial.Port, error) {
		return nil, errors.Join(serial.ErrNotFound, errors.New(path))
	})
	var errb bytes.Buffer
	if code := Main([]string{"probe", "--mount", "nexstar", "--port", "/nope"}, io.Discard, &errb, noEnv); code != ExitUsage {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "/nope") {
		t.Fatal(errb.String())
	}
}
