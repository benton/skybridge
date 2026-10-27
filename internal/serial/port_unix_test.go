//go:build darwin || linux

package serial

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ptyPair starts socat with two linked pseudo-terminals and returns their paths.
func ptyPair(t *testing.T) (a, b string) {
	t.Helper()
	if _, err := exec.LookPath("socat"); err != nil {
		t.Skip("socat not found; skipping pty test")
	}
	dir := t.TempDir()
	a, b = filepath.Join(dir, "a"), filepath.Join(dir, "b")
	cmd := exec.Command("socat", "pty,raw,echo=0,link="+a, "pty,raw,echo=0,link="+b)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	deadline := time.Now().Add(3 * time.Second)
	for {
		_, errA := os.Stat(a)
		_, errB := os.Stat(b)
		if errA == nil && errB == nil {
			return a, b
		}
		if time.Now().After(deadline) {
			t.Fatal("socat did not create the pty links")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPtyRoundTrip(t *testing.T) {
	a, b := ptyPair(t)
	port, err := Open(a, Config{Baud: 9600})
	if err != nil {
		t.Fatal(err)
	}
	defer port.Close()
	far, err := os.OpenFile(b, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer far.Close()

	if _, err := port.Write([]byte("Kx")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := far.Read(buf); err != nil || string(buf) != "Kx" {
		t.Fatalf("far got %q, %v", buf, err)
	}
	if _, err := far.Write([]byte("x#")); err != nil {
		t.Fatal(err)
	}
	if n, err := port.Read(buf); err != nil || string(buf[:n]) != "x#" {
		t.Fatalf("port got %q, %v", buf[:n], err)
	}
}

func TestPtyBaudApplied(t *testing.T) {
	a, _ := ptyPair(t)
	fd, err := unix.Open(a, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := configure(fd, 19200); err != nil {
		t.Fatal(err)
	}
	tio, err := getTermios(fd)
	if err != nil {
		t.Fatal(err)
	}
	if got := speedOf(tio); got != 19200 {
		t.Fatalf("speed %d, want 19200", got)
	}
}

func TestCloseUnblocksRead(t *testing.T) {
	a, _ := ptyPair(t)
	port, err := Open(a, Config{Baud: 9600})
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() {
		_, err := port.Read(make([]byte, 16))
		errc <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if err := port.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errc:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("want ErrClosed, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not return after Close")
	}
}

func TestReadAfterCloseFails(t *testing.T) {
	a, _ := ptyPair(t)
	port, err := Open(a, Config{Baud: 9600})
	if err != nil {
		t.Fatal(err)
	}
	port.Close()
	if _, err := port.Read(make([]byte, 1)); !errors.Is(err, ErrClosed) {
		t.Fatalf("read: want ErrClosed, got %v", err)
	}
	if _, err := port.Write([]byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("write: want ErrClosed, got %v", err)
	}
}
