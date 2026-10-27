package serial

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

const unsafeSizeofDCB = unsafe.Sizeof(windows.DCB{})
