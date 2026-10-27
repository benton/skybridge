package bridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakePort is the bridge's end of a net.Pipe; the test holds the other end as the mount.
type fakePort struct {
	net.Conn
	flushes atomic.Int32
}

func (f *fakePort) Flush() error { f.flushes.Add(1); return nil }

// mount pairs a fakePort with its far end.
type mount struct {
	port *fakePort
	far  net.Conn
}

func newMount() *mount {
	a, b := net.Pipe()
	return &mount{port: &fakePort{Conn: a}, far: b}
}

// harness runs Run with a controllable opener.
type harness struct {
	addr   string
	done   chan error
	cancel context.CancelFunc
	opens  atomic.Int32
	mu     sync.Mutex
	queue  []*mount // mounts handed out by the opener, in order
}

func start(t *testing.T, mounts ...*mount) *harness {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{addr: ln.Addr().String(), done: make(chan error, 1), cancel: cancel, queue: mounts}
	open := func(ctx context.Context) (io.ReadWriteCloser, error) {
		h.opens.Add(1)
		h.mu.Lock()
		if len(h.queue) > 0 {
			m := h.queue[0]
			h.queue = h.queue[1:]
			h.mu.Unlock()
			return m.port, nil
		}
		h.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	go func() {
		h.done <- Run(ctx, Config{
			Listener:   ln,
			Open:       open,
			Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
			RetryDelay: 10 * time.Millisecond,
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-h.done:
		case <-time.After(2 * time.Second):
			t.Error("Run did not return after cancel")
		}
	})
	if len(mounts) > 0 {
		h.waitOpened(t, 1)
	}
	return h
}

// waitOpened blocks until the opener has handed out n ports and the relay has
// had a moment to start serving the last one.
func (h *harness) waitOpened(t *testing.T, n int32) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for h.opens.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("opener called %d times, want %d", h.opens.Load(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
}

func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	return c
}

func readN(t *testing.T, r io.Reader, n int) string {
	t.Helper()
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		t.Fatalf("read %d bytes: %v", n, err)
	}
	return string(buf)
}

// exchange sends "Kx" from the client and answers "x#" from the mount.
func exchange(t *testing.T, client net.Conn, m *mount) {
	t.Helper()
	_ = m.far.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Write([]byte("Kx")); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, m.far, 2); got != "Kx" {
		t.Fatalf("mount got %q", got)
	}
	if _, err := m.far.Write([]byte("x#")); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, client, 2); got != "x#" {
		t.Fatalf("client got %q", got)
	}
}

func TestRefusesWhileAbsent(t *testing.T) {
	h := start(t)
	c := dial(t, h.addr)
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	_, err := c.Read(make([]byte, 1))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
}

func TestRelaysBothDirections(t *testing.T) {
	m := newMount()
	h := start(t, m)
	c := dial(t, h.addr)
	defer c.Close()
	exchange(t, c, m)
}

func TestNewClientReplacesOld(t *testing.T) {
	m := newMount()
	h := start(t, m)
	c1 := dial(t, h.addr)
	defer c1.Close()
	exchange(t, c1, m)
	c2 := dial(t, h.addr)
	defer c2.Close()
	_ = c1.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := c1.Read(make([]byte, 1)); err == nil {
		t.Fatal("old client still open")
	}
	exchange(t, c2, m)
	if n := m.port.flushes.Load(); n != 2 {
		t.Fatalf("flushes = %d, want 2", n)
	}
}

func TestClientDropKeepsPort(t *testing.T) {
	m := newMount()
	h := start(t, m)
	c1 := dial(t, h.addr)
	exchange(t, c1, m)
	c1.Close()
	time.Sleep(50 * time.Millisecond)
	c2 := dial(t, h.addr)
	defer c2.Close()
	exchange(t, c2, m)
	if n := h.opens.Load(); n != 1 {
		t.Fatalf("opens = %d, want 1", n)
	}
}

func TestSerialLossReopens(t *testing.T) {
	m1, m2 := newMount(), newMount()
	h := start(t, m1, m2)
	c1 := dial(t, h.addr)
	exchange(t, c1, m1)
	c1.Close()
	m1.far.Close()
	h.waitOpened(t, 2)
	c2 := dial(t, h.addr)
	defer c2.Close()
	exchange(t, c2, m2)
}

func TestSerialDataWithoutClientIsDropped(t *testing.T) {
	m := newMount()
	h := start(t, m)
	_ = m.far.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := m.far.Write([]byte("stale")); err != nil {
		t.Fatalf("write with no client blocked: %v", err)
	}
	c := dial(t, h.addr)
	defer c.Close()
	time.Sleep(20 * time.Millisecond)
	if _, err := m.far.Write([]byte("fresh")); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, c, 5); got != "fresh" {
		t.Fatalf("client got %q", got)
	}
}

func TestCancelStopsRunAndClosesListener(t *testing.T) {
	h := start(t, newMount())
	h.cancel()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run did not return")
	}
	if c, err := net.DialTimeout("tcp", h.addr, 200*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("listener still accepting")
	}
	h.done <- nil // satisfy cleanup
}

func TestManyReconnectsDoNotLeak(t *testing.T) {
	m := newMount()
	h := start(t, m)
	c0 := dial(t, h.addr)
	exchange(t, c0, m)
	baseline := runtime.NumGoroutine()
	// The fake port is a synchronous pipe, so something must read what the clients send.
	_ = m.far.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	drained := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, m.far); close(drained) }()
	for i := 0; i < 200; i++ {
		c := dial(t, h.addr)
		_, _ = c.Write([]byte("K"))
		c.Close()
	}
	<-drained
	_ = m.far.SetReadDeadline(time.Time{})
	time.Sleep(200 * time.Millisecond)
	if n := runtime.NumGoroutine(); n > baseline+4 {
		t.Fatalf("goroutines %d, baseline %d", n, baseline)
	}
	c := dial(t, h.addr)
	defer c.Close()
	exchange(t, c, m)
}

func TestRejectsHTTPLookingClient(t *testing.T) {
	m := newMount()
	h := start(t, m)
	// A browser tricked into fetch("http://127.0.0.1:4030") sends an HTTP request line.
	c := dial(t, h.addr)
	defer c.Close()
	if _, err := c.Write([]byte("POST / HTTP/1.1\r\nHost: 127.0.0.1:4030\r\n\r\nKx")); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("http-looking client was not closed")
	}
	// Nothing must have reached the mount; a real client still works afterwards.
	_ = m.far.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if n, _ := m.far.Read(make([]byte, 64)); n != 0 {
		t.Fatalf("%d bytes leaked to the mount", n)
	}
	_ = m.far.SetReadDeadline(time.Time{})
	c2 := dial(t, h.addr)
	defer c2.Close()
	exchange(t, c2, m)
}

func TestLooksLikeHTTP(t *testing.T) {
	yes := []string{"GET / HTTP/1.1\r\n", "POST /x", "PUT ", "OPTIONS * HTTP/1.1", "HEAD /", "DELETE /", "PATCH /", "CONNECT a:1", "TRACE /",
		// fetch() allows custom method tokens, so the request line must be caught on its version marker too
		"KX / HTTP/1.1\r\nHost: 127.0.0.1:4030\r\n", "get / HTTP/1.1", "Kx /slew HTTP/2"}
	no := []string{"Kx", "V", ":GVP#", "P\x01\x10\xfe\x00\x00\x00\x02", "e", "GETX", "Post /", "", "G"}
	for _, s := range yes {
		if !looksLikeHTTP([]byte(s)) {
			t.Errorf("%q should look like HTTP", s)
		}
	}
	for _, s := range no {
		if looksLikeHTTP([]byte(s)) {
			t.Errorf("%q should not look like HTTP", s)
		}
	}
}
