package wizardweb

import (
	"context"
	"unsafe"

	"golang.org/x/sys/windows"
)

func desktopSession(context.Context) bool {
	const (
		uoiFlags   = 1
		wsfVisible = 1
	)
	user32 := windows.NewLazySystemDLL("user32.dll")
	station, _, _ := user32.NewProc("GetProcessWindowStation").Call()
	if station == 0 {
		return false
	}
	var flags struct {
		Inherit, Reserved, Flags uint32 // USEROBJECTFLAGS uses 32-bit BOOL and DWORD values.
	}
	var needed uint32
	ok, _, _ := user32.NewProc("GetUserObjectInformationW").Call(station, uoiFlags,
		uintptr(unsafe.Pointer(&flags)), unsafe.Sizeof(flags), uintptr(unsafe.Pointer(&needed)))
	return ok != 0 && flags.Flags&wsfVisible != 0
}
