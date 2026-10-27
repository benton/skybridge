package serial

import (
	"regexp"
	"strconv"
)

// usbIDRe matches Windows device instance IDs such as USB\VID_067B&PID_23A3\...
// and FTDIBUS\VID_0403+PID_6001+...; case does not matter.
var usbIDRe = regexp.MustCompile(`(?i)VID_([0-9A-F]{4})[&+]PID_([0-9A-F]{4})`)

// parseUSBInstanceID extracts the USB vendor and product IDs from a Windows
// device instance ID. ok is false when the ID is not a USB one.
func parseUSBInstanceID(s string) (vid, pid uint16, ok bool) {
	m := usbIDRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	v, err1 := strconv.ParseUint(m[1], 16, 16)
	p, err2 := strconv.ParseUint(m[2], 16, 16)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return uint16(v), uint16(p), true
}
