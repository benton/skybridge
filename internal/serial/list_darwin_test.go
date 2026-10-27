package serial

import (
	"os"
	"testing"
)

func TestParseIORegFixture(t *testing.T) {
	f, err := os.Open("testdata/ioreg-nexstar.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got := parseIOReg(f)
	info, ok := got["/dev/cu.PL2303G-USBtoUART110"]
	if !ok {
		t.Fatalf("callout device missing from %v", got)
	}
	if info.VID != 0x067B || info.PID != 0x23A3 || info.Product != "USB-Serial Controller" {
		t.Fatalf("got %+v", info)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one serial device, got %v", got)
	}
}
