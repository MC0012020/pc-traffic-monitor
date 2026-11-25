package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

// Native tooltips track the pin icon button even when the main window is resized.
var buttonTips, pinActiveBrush uintptr
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
	buttonTips, _, _ = createWindow.Call(8, uintptr(unsafe.Pointer(cls)), 0, 0x80000003, 0, 0, 0, 0, mainWindow, 0, instance, 0)
	runtime.KeepAlive(cls)
	if buttonTips == 0 {
		return
	}
	setPos.Call(buttonTips, ^uintptr(0), 0, 0, 0, 0, 0x0013)
	for h, label := range map[uintptr]string{topButton: "取消置顶", updateButton: "软件更新"} {
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

// An outline pushpin and a subtle green active background distinguish the toggle
// without introducing text or changing the button's dimensions.
func drawPinIcon(dc uintptr, r rect32, on bool) {
	x, y := (r.L+r.R)/2, (r.T+r.B)/2
	c := uintptr(dark)
	if on {
		c = green
	}
	pen, _, _ := gdi.NewProc("CreatePen").Call(0, 1, c)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, pen)
	outline := []point{{x - 4, y - 8}, {x + 4, y - 8}, {x + 3, y - 5}, {x + 3, y - 1}, {x + 6, y + 2}, {x + 6, y + 3}, {x - 6, y + 3}, {x - 6, y + 2}, {x - 3, y - 1}, {x - 3, y - 5}, {x - 4, y - 8}}
	gdi.NewProc("Polyline").Call(dc, uintptr(unsafe.Pointer(&outline[0])), uintptr(len(outline)))
	gdi.NewProc("MoveToEx").Call(dc, uintptr(x), uintptr(y+3), 0)
	gdi.NewProc("LineTo").Call(dc, uintptr(x), uintptr(y+10))
	gdi.NewProc("SelectObject").Call(dc, old)
	gdi.NewProc("DeleteObject").Call(pen)
	runtime.KeepAlive(outline)
}
