package serial

import (
	"errors"
	"testing"
)

func TestOpenMissingPath(t *testing.T) {
	_, err := Open("/nonexistent/skybridge-tty", Config{Baud: 9600})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestOpenUnsupportedBaud(t *testing.T) {
	_, err := Open("/nonexistent/skybridge-tty", Config{Baud: 1200})
	if !errors.Is(err, ErrUnsupportedBaud) {
		t.Fatalf("want ErrUnsupportedBaud, got %v", err)
	}
}
