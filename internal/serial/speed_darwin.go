package serial

import (
	"golang.org/x/sys/unix"
)

const (
	ioctlGetTermios = unix.TIOCGETA
	ioctlSetTermios = unix.TIOCSETA
)

// On Darwin the termios speed fields hold the rate itself.
func setSpeed(tio *unix.Termios, baud int) error {
	tio.Ispeed = uint64(baud)
	tio.Ospeed = uint64(baud)
	return nil
}

func speedOf(tio *unix.Termios) int {
	return int(tio.Ospeed)
}

// TIOCFLUSH takes a pointer to FREAD|FWRITE (1|2) to discard both queues.
func flush(fd int) error {
	return unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, 0x3)
}
