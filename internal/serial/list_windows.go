package serial

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// guidDevInterfaceComPort is GUID_DEVINTERFACE_COMPORT {86E0D1E0-8089-11D0-9CE4-08003E301F73}.
var guidDevInterfaceComPort = windows.GUID{
	Data1: 0x86E0D1E0, Data2: 0x8089, Data3: 0x11D0,
	Data4: [8]byte{0x9C, 0xE4, 0x08, 0x00, 0x3E, 0x30, 0x1F, 0x73},
}

// list enumerates COM ports through SetupAPI, which also tells us the USB
// vendor and product IDs and the friendly name shown in Device Manager.
func list(_ context.Context) ([]PortInfo, error) {
	set, err := windows.SetupDiGetClassDevsEx(&guidDevInterfaceComPort, "", 0, windows.DIGCF_PRESENT|windows.DIGCF_DEVICEINTERFACE, 0, "")
	if err != nil {
		return nil, fmt.Errorf("serial: SetupDiGetClassDevsEx: %w", err)
	}
	defer windows.SetupDiDestroyDeviceInfoList(set)

	var ports []PortInfo
	for i := 0; ; i++ {
		data, err := windows.SetupDiEnumDeviceInfo(set, i)
		if err != nil {
			break // ERROR_NO_MORE_ITEMS or a broken entry; either way we are done
		}
		name, err := portName(set, data)
		if err != nil || name == "" {
			continue
		}
		info := PortInfo{Path: name}
		if id, err := windows.SetupDiGetDeviceInstanceId(set, data); err == nil {
			info.VID, info.PID, _ = parseUSBInstanceID(id)
		}
		if v, err := windows.SetupDiGetDeviceRegistryProperty(set, data, windows.SPDRP_FRIENDLYNAME); err == nil {
			if s, ok := v.(string); ok {
				info.Product = s
			}
		}
		ports = append(ports, info)
	}
	return ports, nil
}

// portName reads the PortName value (COM3 and so on) from the device's registry key.
func portName(set windows.DevInfo, data *windows.DevInfoData) (string, error) {
	key, err := windows.SetupDiOpenDevRegKey(set, data, windows.DICS_FLAG_GLOBAL, 0, windows.DIREG_DEV, windows.KEY_READ)
	if err != nil {
		return "", err
	}
	k := registry.Key(key)
	defer k.Close()
	name, _, err := k.GetStringValue("PortName")
	return name, err
}
