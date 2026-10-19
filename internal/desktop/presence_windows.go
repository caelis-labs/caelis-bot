package desktop

import (
	"github.com/caelis-labs/caelis-bot/internal/scheduledata"
	"golang.org/x/sys/windows"
	"unsafe"
)

func windowsPresenceSample() scheduledata.Sample {
	// Open the actual input desktop. A secure/locked or inaccessible desktop is
	// unavailable, never inferred from a local setting or animation.
	h, _, _ := winUser.NewProc("OpenInputDesktop").Call(0, 0, 1)
	if h == 0 {
		return scheduledata.Sample{}
	}
	defer winUser.NewProc("CloseDesktop").Call(h)
	var name [256]uint16
	var used uint32
	ok, _, _ := winUser.NewProc("GetUserObjectInformationW").Call(h, 2, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)*2), uintptr(unsafe.Pointer(&used)))
	if ok == 0 {
		return scheduledata.Sample{}
	}
	unlocked := windows.UTF16ToString(name[:]) == "Default"
	var input struct{ Size, Tick uint32 }
	input.Size = uint32(unsafe.Sizeof(input))
	if ok, _, _ := winUser.NewProc("GetLastInputInfo").Call(uintptr(unsafe.Pointer(&input))); ok == 0 {
		return scheduledata.Sample{}
	}
	tick, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount64").Call()
	return scheduledata.Sample{Presence: scheduledata.Presence{Awake: unlocked, Unlocked: &unlocked}, IdleSeconds: float64(uint32(tick)-input.Tick) / 1000}
}
