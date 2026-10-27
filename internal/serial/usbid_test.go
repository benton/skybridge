package serial

import "testing"

func TestParseUSBInstanceID(t *testing.T) {
	cases := []struct {
		in       string
		vid, pid uint16
		ok       bool
	}{
		{`USB\VID_067B&PID_23A3\6&1a2b`, 0x067B, 0x23A3, true},
		{`usb\vid_0403&pid_6001\x`, 0x0403, 0x6001, true},
		{`FTDIBUS\VID_0403+PID_6001+A50285BIA\0000`, 0x0403, 0x6001, true},
		{`ACPI\PNP0501\1`, 0, 0, false},
		{``, 0, 0, false},
	}
	for _, c := range cases {
		vid, pid, ok := parseUSBInstanceID(c.in)
		if vid != c.vid || pid != c.pid || ok != c.ok {
			t.Errorf("parseUSBInstanceID(%q) = %04x,%04x,%v; want %04x,%04x,%v", c.in, vid, pid, ok, c.vid, c.pid, c.ok)
		}
	}
}
