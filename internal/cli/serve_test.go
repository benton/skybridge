package cli

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// listenCapture records what the listen hook was asked for and what it made.
type listenCapture struct {
	mu    sync.Mutex
	ln    net.Listener
	addrs []string
}

func (c *listenCapture) addr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ln == nil {
		return ""
	}
	return c.ln.Addr().String()
}

func (c *listenCapture) called() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.addrs...)
}

// captureListen replaces the listen hook for the test's lifetime.
func captureListen(t *testing.T) *listenCapture {
	t.Helper()
	c := &listenCapture{}
	old := listen
	listen = func(network, address string) (net.Listener, error) {
		c.mu.Lock()
		c.addrs = append(c.addrs, address)
		c.mu.Unlock()
		l, err := net.Listen(network, address)
		if err == nil {
			c.mu.Lock()
			c.ln = l
			c.mu.Unlock()
		}
		return l, err
	}
	t.Cleanup(func() { listen = old })
	return c
}

func runMainAsync(t *testing.T, args []string, env Env) (done <-chan int, cancel context.CancelFunc, errb *bytes.Buffer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	old := rootCtx
	rootCtx = ctx
	t.Cleanup(func() { rootCtx = old })
	errb = &bytes.Buffer{}
	ch := make(chan int, 1)
	go func() { ch <- Main(args, io.Discard, errb, env) }()
	return ch, cancel, errb
}

func TestServeEndToEnd(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	cap := captureListen(t)
	done, cancel, errb := runMainAsync(t, []string{"serve", "--listen", "127.0.0.1:0"}, noEnv)
	deadline := time.Now().Add(2 * time.Second)
	for cap.addr() == "" {
		if time.Now().After(deadline) {
			t.Fatalf("listener never created: %s", errb.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	// The port opens asynchronously; retry until the bridge relays.
	var reply string
	for time.Now().Before(deadline) && reply != "x#" {
		c, err := net.Dial("tcp", cap.addr())
		if err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(time.Now().Add(300 * time.Millisecond))
		_, _ = c.Write([]byte("Kx"))
		buf := make([]byte, 2)
		if _, err := io.ReadFull(c, buf); err == nil {
			reply = string(buf)
		}
		c.Close()
	}
	if reply != "x#" {
		t.Fatalf("no relay; stderr: %s", errb.String())
	}
	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit after cancel")
	}
}

func TestServeBadBaudExit1(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	cap := captureListen(t)
	var errb bytes.Buffer
	if code := Main([]string{"serve", "--baud", "abc"}, io.Discard, &errb, noEnv); code != ExitUsage {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "4800, 9600, 19200, 38400, 57600, 115200") {
		t.Fatal(errb.String())
	}
	if len(cap.called()) != 0 {
		t.Fatal("listen was called")
	}
}

func TestServeGenericNeedsBaud(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	var errb bytes.Buffer
	if code := Main([]string{"serve", "--mount", "generic"}, io.Discard, &errb, noEnv); code != ExitUsage {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "--baud") {
		t.Fatal(errb.String())
	}
}

func TestServeListenInUseExit2(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	var errb bytes.Buffer
	if code := Main([]string{"serve", "--listen", taken.Addr().String()}, io.Discard, &errb, noEnv); code != ExitListen {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "listen") || !strings.Contains(errb.String(), taken.Addr().String()) {
		t.Fatal(errb.String())
	}
}

func TestServeEnvListen(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	cap := captureListen(t)
	env := func(k string) string {
		if k == "SKYBRIDGE_LISTEN" {
			return "127.0.0.1:0"
		}
		return ""
	}
	done, cancel, _ := runMainAsync(t, []string{"serve"}, env)
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if got := cap.called(); len(got) != 1 || got[0] != "127.0.0.1:0" {
		t.Fatalf("listen called with %v", got)
	}
}

func TestBareInvocationIsServe(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	cap := captureListen(t)
	done, cancel, _ := runMainAsync(t, nil, noEnv)
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	if got := cap.called(); len(got) != 1 || got[0] != "127.0.0.1:4030" {
		t.Fatalf("listen called with %v", got)
	}
}
