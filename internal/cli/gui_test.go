package cli

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestGUIEndToEnd(t *testing.T) {
	withHooks(t, oneCandidate(), responderPort("Kx", "x#", nil))
	cap := captureListen(t)
	var opened atomic.Value
	oldOpen, oldCfg := openBrowser, configPath
	cfg := t.TempDir() + "/settings.json"
	openBrowser = func(url string) error { opened.Store(url); return nil }
	configPath = func() (string, error) { return cfg, nil }
	t.Cleanup(func() { openBrowser, configPath = oldOpen, oldCfg })

	done, cancel, errb := runMainAsync(t, []string{"gui", "--ui", "127.0.0.1:0", "--listen", "127.0.0.1:0", "--port", "/dev/fake"}, noEnv)
	waitForCond(t, "browser opened", func() bool { return opened.Load() != nil })
	url := opened.Load().(string)
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("url %q", url)
	}

	var st map[string]any
	waitForCond(t, "port open", func() bool {
		res, err := http.Get(url + "api/state")
		if err != nil {
			return false
		}
		defer res.Body.Close()
		st = nil
		_ = json.NewDecoder(res.Body).Decode(&st)
		serial, _ := st["serial"].(map[string]any)
		return serial["status"] == "open"
	})
	if st["running"] != true {
		t.Fatalf("state %v", st)
	}
	settings := st["settings"].(map[string]any)
	if settings["port"] != "/dev/fake" || settings["mount"] != "celestron-nexstar" {
		t.Fatalf("settings %v", settings)
	}

	// The bridge really relays.
	conn, err := net.Dial("tcp", st["listen"].(string))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, _ = conn.Write([]byte("Kx"))
	buf := make([]byte, 2)
	if _, err := conn.Read(buf); err != nil || string(buf) != "x#" {
		t.Fatalf("reply %q err %v; stderr %s", buf, err, errb.String())
	}
	conn.Close()

	// Both listeners were created: the bridge's and the panel's.
	if n := len(cap.called()); n != 2 {
		t.Fatalf("listen called %d times: %v", n, cap.called())
	}

	// Hold an event stream open across shutdown: it must end cleanly rather
	// than keep the process alive.
	stream, err := http.Get(url + "api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	first := make([]byte, 12)
	if _, err := io.ReadFull(stream.Body, first); err != nil || string(first) != "event: state" {
		t.Fatalf("stream start %q err %v", first, err)
	}
	streamDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, stream.Body)
		streamDone <- err
	}()

	cancel() // what Ctrl-C or SIGTERM does
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("gui did not exit after cancel")
	}
	select {
	case err := <-streamDone:
		if err != nil {
			t.Fatalf("stream ended with %v, want a clean end", err)
		}
	case <-time.After(time.Second):
		t.Fatal("event stream still open after exit")
	}

	// Both ports are released.
	bridgeAddr := st["listen"].(string)
	for _, addr := range []string{strings.TrimSuffix(strings.TrimPrefix(url, "http://"), "/"), bridgeAddr} {
		if c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			c.Close()
			t.Fatalf("%s still accepting connections after exit", addr)
		}
	}
	if !strings.Contains(errb.String(), "shutting down") || !strings.Contains(errb.String(), "control panel stopped") {
		t.Fatalf("stderr: %s", errb.String())
	}
}

func TestGUIHelpListsCommand(t *testing.T) {
	var out strings.Builder
	if code := Main([]string{"help"}, &out, &out, noEnv); code != 0 {
		t.Fatal(code)
	}
	if !strings.Contains(out.String(), "gui") {
		t.Fatal(out.String())
	}
}

func waitForCond(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
