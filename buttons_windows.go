package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Native tooltip for the update button.
var buttonTips, updateActiveBrush uintptr
var tipStrings = map[uintptr][]uint16{}

type toolInfo struct {
	Size, Flags     uint32
	Hwnd, ID        uintptr
	Rect            rect32
	Instance        uintptr
	Text            *uint16
	Param, Reserved uintptr
}

func initButtonTips() {
	c := commonControls{8, 0xff} // ICC_WIN95_CLASSES
	comctl.NewProc("InitCommonControlsEx").Call(uintptr(unsafe.Pointer(&c)))
	cls, _ := syscall.UTF16PtrFromString("tooltips_class32")
	buttonTips, _, _ = createWindow.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0x80000003, 0, 0, 0, 0, mainWindow, 0, instance, 0)
	runtime.KeepAlive(cls)
	if buttonTips == 0 {
		return
	}
	for h, label := range map[uintptr]string{updateButton: "软件更新"} {
		if h == 0 {
			continue
		}
		v, _ := syscall.UTF16FromString(label)
		tipStrings[h] = v
		ti := toolInfo{Flags: 0x11, Hwnd: mainWindow, ID: h, Text: &v[0]}
		ti.Size = uint32(unsafe.Sizeof(ti))
		send.Call(buttonTips, 0x432, 0, uintptr(unsafe.Pointer(&ti))) // TTM_ADDTOOLW
		runtime.KeepAlive(v)
	}
	send.Call(buttonTips, 0x403, 3, 450) // TTM_SETDELAYTIME, TTDT_INITIAL
}
func updateButtonTip(h uintptr, label string) {
	if buttonTips == 0 || h == 0 {
		return
	}
	old := tipStrings[h]
	v, _ := syscall.UTF16FromString(label)
	tipStrings[h] = v
	ti := toolInfo{Flags: 0x11, Hwnd: mainWindow, ID: h, Text: &v[0]}
	ti.Size = uint32(unsafe.Sizeof(ti))
	send.Call(buttonTips, 0x439, 0, uintptr(unsafe.Pointer(&ti))) // TTM_UPDATETIPTEXTW
	runtime.KeepAlive(old)
	runtime.KeepAlive(v)
}
func hideButtonTip() {
	if buttonTips != 0 {
		send.Call(buttonTips, 0x41c, 0, 0)
	}
} // TTM_POP
func releaseButtonTips() {
	if buttonTips != 0 {
		user.NewProc("DestroyWindow").Call(buttonTips)
		buttonTips = 0
	}
	clear(tipStrings)
}

