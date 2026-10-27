package ui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"git.h.usky.io/huskynet/skybridge/internal/bridge"
	"git.h.usky.io/huskynet/skybridge/internal/discovery"
	"git.h.usky.io/huskynet/skybridge/internal/serial"
)

// echoPort is a fake serial port that answers "Kx" with "x#".
type echoPort struct{ net.Conn }

func (echoPort) Flush() error { return nil }

func newEchoPort() io.ReadWriteCloser {
	near, far := net.Pipe()
	go func() {
		defer far.Close()
		buf := make([]byte, 64)
		for {
			n, err := far.Read(buf)
			if err != nil {
				return
			}
			if string(buf[:n]) == "Kx" {
				if _, err := far.Write([]byte("x#")); err != nil {
					return
				}
			}
		}
	}()
	return echoPort{near}
}

func fakeDeps(t *testing.T) (Deps, *[]Settings) {
	t.Helper()
	var saved []Settings
	return Deps{
		Open: func(s Settings, _ *slog.Logger, opened func(string, int)) (bridge.Opener, error) {
			if s.Mount == "" {
				return nil, errors.New("unknown mount profile")
			}
			return func(ctx context.Context) (io.ReadWriteCloser, error) {
				if opened != nil {
					opened("/dev/fake", 9600)
				}
				return newEchoPort(), nil
			}, nil
		},
		Ports: func(context.Context) ([]discovery.Candidate, error) {
			return []discovery.Candidate{{PortInfo: serial.PortInfo{Path: "/dev/fake"}, Score: 50}}, nil
		},
		Probe: func(context.Context, Settings, *slog.Logger) (ProbeResult, error) {
			return ProbeResult{Profile: "celestron-nexstar", Path: "/dev/fake", Baud: 9600, Reply: "x#"}, nil
		},
		Listen:  net.Listen,
		Save:    func(s Settings) error { saved = append(saved, s); return nil },
		Retry:   10 * time.Millisecond,
		Version: "test",
	}, &saved
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func testSettings() Settings {
	s := DefaultSettings
	s.Listen = "127.0.0.1:0"
	return s
}

func TestControllerStartRelaysAndStops(t *testing.T) {
	deps, _ := fakeDeps(t)
	c := NewController(deps, testSettings(), slog.LevelDebug, nil)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	waitFor(t, "port open", func() bool { return c.State().Serial.Status == "open" })
	st := c.State()
	if !st.Running || st.Serial.Path != "/dev/fake" || st.Serial.Baud != 9600 {
		t.Fatalf("state %+v", st)
	}

	conn, err := net.Dial("tcp", st.Listen)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("Kx")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "x#" {
		t.Fatalf("reply %q err %v", buf, err)
	}
	waitFor(t, "client state", func() bool { return c.State().Client.Connected })
	waitFor(t, "traffic", func() bool {
		tr := c.State().Traffic
		return tr.ToMount == 2 && tr.ToApp == 2
	})

	c.Stop()
	st = c.State()
	if st.Running || st.Serial.Status != "closed" || st.Client.Connected {
		t.Fatalf("after stop: %+v", st)
	}
	// Idempotent.
	c.Stop()
}

func TestClientGracePeriodHidesReconnects(t *testing.T) {
	old := clientGrace
	clientGrace = 150 * time.Millisecond
	t.Cleanup(func() { clientGrace = old })

	deps, _ := fakeDeps(t)
	c := NewController(deps, testSettings(), slog.LevelDebug, nil)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	waitFor(t, "port open", func() bool { return c.State().Serial.Status == "open" })
	addr := c.State().Listen

	dial := func() net.Conn {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write([]byte("Kx")); err != nil {
			t.Fatal(err)
		}
		return conn
	}
	conn := dial()
	waitFor(t, "client", func() bool { return c.State().Client.Connected })
	since := c.State().Client.Since

	// SkySafari's dance: drop and come straight back, a few times.
	for i := 0; i < 3; i++ {
		conn.Close()
		time.Sleep(30 * time.Millisecond)
		if st := c.State().Client; !st.Connected || st.Since != since {
			t.Fatalf("round %d: flickered: %+v", i, st)
		}
		conn = dial()
		time.Sleep(30 * time.Millisecond)
		if st := c.State().Client; !st.Connected || st.Since != since {
			t.Fatalf("round %d: after reconnect: %+v", i, st)
		}
	}

	// A real disconnect shows after the grace period.
	conn.Close()
	time.Sleep(50 * time.Millisecond)
	if !c.State().Client.Connected {
		t.Fatal("dropped before the grace period ended")
	}
	waitFor(t, "disconnect", func() bool { return !c.State().Client.Connected })
}

func TestControllerStartErrorIsRecorded(t *testing.T) {
	deps, _ := fakeDeps(t)
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	s := testSettings()
	s.Listen = taken.Addr().String()
	c := NewController(deps, s, slog.LevelDebug, nil)
	if err := c.Start(); err == nil {
		t.Fatal("expected a listen error")
	}
	st := c.State()
	if st.Running || !strings.Contains(st.Error, "address already in use") && !strings.Contains(st.Error, "bind") {
		t.Fatalf("state %+v", st)
	}
	// The failure also landed in the log.
	found := false
	for _, l := range c.Logs() {
		if strings.Contains(l.Text, "could not start") {
			found = true
		}
	}
	if !found {
		t.Fatalf("log: %+v", c.Logs())
	}
}

func TestApplyValidatesSavesAndRestarts(t *testing.T) {
	deps, saved := fakeDeps(t)
	c := NewController(deps, testSettings(), slog.LevelDebug, nil)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	first := c.State().Listen

	bad := testSettings()
	bad.Mount = ""
	if err := c.Apply(bad); err == nil {
		t.Fatal("bad settings were accepted")
	}
	if len(*saved) != 0 || !c.Running() {
		t.Fatal("bad settings had an effect")
	}

	good := testSettings()
	good.Port = "/dev/other"
	if err := c.Apply(good); err != nil {
		t.Fatal(err)
	}
	if len(*saved) != 1 || (*saved)[0].Port != "/dev/other" {
		t.Fatalf("saved %+v", *saved)
	}
	st := c.State()
	if !st.Running || st.Settings.Port != "/dev/other" || st.Listen == first {
		t.Fatalf("after apply: %+v (first listen %s)", st, first)
	}
}

func TestProbeNeedsStoppedBridge(t *testing.T) {
	deps, _ := fakeDeps(t)
	c := NewController(deps, testSettings(), slog.LevelDebug, nil)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Probe(context.Background()); !errors.Is(err, ErrRunning) {
		t.Fatalf("err %v", err)
	}
	c.Stop()
	res, err := c.Probe(context.Background())
	if err != nil || res.Profile != "celestron-nexstar" || res.At == "" {
		t.Fatalf("res %+v err %v", res, err)
	}
	if c.State().Probe == nil {
		t.Fatal("probe not in state")
	}
}

func TestSubscribeSeesStateAndLogs(t *testing.T) {
	deps, _ := fakeDeps(t)
	c := NewController(deps, testSettings(), slog.LevelDebug, nil)
	ch, stop := c.Subscribe()
	defer stop()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	var sawState, sawLog bool
	deadline := time.After(2 * time.Second)
	for !(sawState && sawLog) {
		select {
		case ev := <-ch:
			switch ev.Name {
			case "state":
				var st State
				if err := json.Unmarshal(ev.Data, &st); err != nil {
					t.Fatal(err)
				}
				sawState = true
			case "log":
				sawLog = true
			}
		case <-deadline:
			t.Fatalf("state %v log %v", sawState, sawLog)
		}
	}
}

// HTTP layer

func newTestServer(t *testing.T) (*httptest.Server, *Controller) {
	t.Helper()
	deps, _ := fakeDeps(t)
	c := NewController(deps, testSettings(), slog.LevelDebug, nil)
	srv := httptest.NewServer(Handler(c, "127.0.0.1:0"))
	t.Cleanup(func() { srv.Close(); c.Stop() })
	return srv, c
}

func post(t *testing.T, url, body string) (*http.Response, map[string]any) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(res.Body).Decode(&m)
	return res, m
}

func TestHTTPStartSettingsStop(t *testing.T) {
	srv, c := newTestServer(t)

	res, m := post(t, srv.URL+"/api/start", "{}")
	if res.StatusCode != 200 || m["running"] != true {
		t.Fatalf("start: %d %v", res.StatusCode, m)
	}
	waitFor(t, "port open", func() bool { return c.State().Serial.Status == "open" })

	res, m = post(t, srv.URL+"/api/settings", `{"mount":"","listen":"127.0.0.1:0"}`)
	if res.StatusCode != 400 || m["error"] == nil {
		t.Fatalf("bad settings: %d %v", res.StatusCode, m)
	}
	res, m = post(t, srv.URL+"/api/settings", `{"mount":"celestron-nexstar","listen":""}`)
	if res.StatusCode != 400 {
		t.Fatalf("empty listen: %d %v", res.StatusCode, m)
	}
	res, m = post(t, srv.URL+"/api/settings", `{"port":"/dev/x","mount":"celestron-nexstar","listen":"127.0.0.1:0","verify":true}`)
	if res.StatusCode != 200 || m["running"] != true {
		t.Fatalf("settings: %d %v", res.StatusCode, m)
	}
	if got := c.Settings(); got.Port != "/dev/x" || !got.Verify {
		t.Fatalf("settings %+v", got)
	}

	res, m = post(t, srv.URL+"/api/probe", "{}")
	if res.StatusCode != 409 {
		t.Fatalf("probe while running: %d %v", res.StatusCode, m)
	}

	res, m = post(t, srv.URL+"/api/stop", "{}")
	if res.StatusCode != 200 || m["running"] != false {
		t.Fatalf("stop: %d %v", res.StatusCode, m)
	}
	res, m = post(t, srv.URL+"/api/probe", "{}")
	if res.StatusCode != 200 || m["profile"] != "celestron-nexstar" {
		t.Fatalf("probe: %d %v", res.StatusCode, m)
	}
}

func TestHTTPGuards(t *testing.T) {
	srv, _ := newTestServer(t)

	// A form post (no JSON content type) is refused.
	res, err := http.Post(srv.URL+"/api/start", "application/x-www-form-urlencoded", strings.NewReader("a=b"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form post: %d", res.StatusCode)
	}

	// A request for another host name is refused.
	req, _ := http.NewRequest("GET", srv.URL+"/api/state", nil)
	req.Host = "evil.example:4031"
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("rebind: %d", res.StatusCode)
	}

	// localhost and any IP literal are fine.
	for _, h := range []string{"localhost:4031", "192.168.1.5:4031", "[::1]:4031"} {
		req, _ = http.NewRequest("GET", srv.URL+"/api/state", nil)
		req.Host = h
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("host %s: %d", h, res.StatusCode)
		}
	}
}

func TestHTTPStaticAndLists(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, p := range []string{"/", "/static/app.css", "/static/app.js", "/static/favicon.svg"} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 || len(b) == 0 {
			t.Fatalf("%s: %d %d bytes", p, res.StatusCode, len(b))
		}
	}
	res, err := http.Get(srv.URL + "/api/mounts")
	if err != nil {
		t.Fatal(err)
	}
	var ms []MountInfo
	if err := json.NewDecoder(res.Body).Decode(&ms); err != nil || len(ms) == 0 || ms[0].Name != "celestron-nexstar" {
		t.Fatalf("mounts: %v %v", ms, err)
	}
	res.Body.Close()
	res, err = http.Get(srv.URL + "/api/ports")
	if err != nil {
		t.Fatal(err)
	}
	var ps []discovery.Candidate
	if err := json.NewDecoder(res.Body).Decode(&ps); err != nil || len(ps) != 1 {
		t.Fatalf("ports: %v %v", ps, err)
	}
	res.Body.Close()
}

func TestHTTPEventsReplayThenStream(t *testing.T) {
	srv, c := newTestServer(t)
	c.Logger().Info("hello from the test")
	res, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 8192)
		var all strings.Builder
		for {
			n, err := res.Body.Read(buf)
			all.Write(buf[:n])
			s := all.String()
			if strings.Contains(s, "event: logs") && strings.Contains(s, "hello from the test") && strings.Contains(s, `"running":true`) {
				got <- s
				return
			}
			if err != nil {
				got <- s
				return
			}
		}
	}()
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if !strings.HasPrefix(s, "event: state\ndata: {") {
			t.Fatalf("stream starts with %q", s[:40])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no events")
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	path := t.TempDir() + "/sub/settings.json"
	got, err := Load(path)
	if err != nil || got != DefaultSettings {
		t.Fatalf("missing file: %+v %v", got, err)
	}
	want := Settings{Port: "/dev/x", Mount: "meade-lx200", Baud: 19200, Listen: "0.0.0.0:4030", Verify: true}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	if got, err = Load(path); err != nil || got != want {
		t.Fatalf("round trip: %+v %v", got, err)
	}
}
