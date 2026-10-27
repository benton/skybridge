package serial

import "context"

func list(_ context.Context) ([]PortInfo, error) {
	return listSysfs("/dev", "/sys/class/tty")
}
