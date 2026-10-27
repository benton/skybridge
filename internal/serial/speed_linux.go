package serial

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const (
	ioctlGetTermios = unix.TCGETS
	ioctlSetTermios = unix.TCSETS
)

var linuxSpeeds = map[int]uint32{
	4800:   unix.B4800,
	9600:   unix.B9600,
	19200:  unix.B19200,
	38400:  unix.B38400,
	57600:  unix.B57600,
	115200: unix.B115200,
}

func setSpeed(tio *unix.Termios, baud int) error {
	code, ok := linuxSpeeds[baud]
	if !ok {
		return fmt.Errorf("%w: %d", ErrUnsupportedBaud, baud)
	}
	tio.Cflag &^= unix.CBAUD
	tio.Cflag |= code
	tio.Ispeed = code
	tio.Ospeed = code
	return nil
}

// speedOf reports the baud rate a termios is set to, or 0 if unknown.
func speedOf(tio *unix.Termios) int {
	code := tio.Cflag & unix.CBAUD
	for baud, c := range linuxSpeeds {
		if c == code {
			return baud
		}
	}
	return 0
}

func flush(fd int) error {
	return unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH)
}
