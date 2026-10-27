//go:build windows

package serial

import (
	"errors"
	"testing"
)

func TestOpenMissingCOM(t *testing.T) {
	_, err := Open("COM255", Config{Baud: 9600})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDevicePath(t *testing.T) {
	if got := devicePath("COM3"); got != `\\.\COM3` {
		t.Fatal(got)
	}
	if got := devicePath(`\\.\COM3`); got != `\\.\COM3` {
		t.Fatal(got)
	}
}
