package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

var floatWindow uintptr
var floatDragging, floatMoved bool
var floatAnchor, floatStart, floatOld point
var floatWidth, floatDivider, floatRightX int
var floatLeftText, floatRightText, floatLeftDay, floatRightDay string

type monitorInfo struct {
	Size          uint32
	Monitor, Work rect32
	Flags         uint32
}
type floatPaintStruct struct {
	DC              uintptr
	Erase           int32
	Rect            rect32
	Restore, Update int32
	Reserved        [32]byte
}

func cursorPoint() point {
	var p point
	user.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&p)))
	return p
}
func monitorWork(m uintptr) rect32 {
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	r, _, _ := user.NewProc("GetMonitorInfoW").Call(m, uintptr(unsafe.Pointer(&mi)))
	if r != 0 {
		return mi.Work
	}
	var a rect32
	user.NewProc("SystemParametersInfoW").Call(48, 0, uintptr(unsafe.Pointer(&a)), 0)
	return a
}
func workArea(h uintptr) rect32 {
	m, _, _ := user.NewProc("MonitorFromWindow").Call(h, 2)
	return monitorWork(m)
}
func pointWork(p point) rect32 {
	v := uintptr(uint64(uint32(p.X)) | uint64(uint32(p.Y))<<32)
	m, _, _ := user.NewProc("MonitorFromPoint").Call(v, 2)
	return monitorWork(m)
}
func clampPoint(p point, w, h int, area rect32) point {
	return point{max(area.L, min(p.X, area.R-int32(w))), max(area.T, min(p.Y, area.B-int32(h)))}
}
func floatMeasure(dc uintptr, s string) int {
	var size point
	v, _ := syscall.UTF16FromString(s)
	gdi.NewProc("GetTextExtentPoint32W").Call(dc, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)-1), uintptr(unsafe.Pointer(&size)))
	runtime.KeepAlive(v)
	return max(1, int(size.X)) + 1
}
func measureFloat() {
	dc, _, _ := user.NewProc("GetDC").Call(mainWindow)
	if dc == 0 {
		return
	}
	defer user.NewProc("ReleaseDC").Call(mainWindow, dc)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, floatFont)
	left, right := floatMeasure(dc, floatLeftText), floatMeasure(dc, floatRightText)
	gdi.NewProc("SelectObject").Call(dc, floatDayFont)
	left, right = max(left, floatMeasure(dc, floatLeftDay)), max(right, floatMeasure(dc, floatRightDay))
	floatWidth, floatDivider, floatRightX = floatDimensions(left, right)
	gdi.NewProc("SelectObject").Call(dc, old)
}
func floatPlace() {
	if floatWindow == 0 {
		return
	}
	area := pointWork(floatAnchor)
	if floatDragging {
		area = pointWork(cursorPoint())
	}
	floatAnchor = clampPoint(floatAnchor, floatWidth, floatHeight, area)
	setPos.Call(floatWindow, ^uintptr(0), uintptr(floatAnchor.X), uintptr(floatAnchor.Y), uintptr(floatWidth), floatHeight, 0x0010|0x0040)
	region, _, _ := gdi.NewProc("CreateRoundRectRgn").Call(0, 0, uintptr(floatWidth+1), floatHeight+1, 12, 12)
	if region != 0 {
		r, _, _ := user.NewProc("SetWindowRgn").Call(floatWindow, region, 1)
		if r == 0 {
			gdi.NewProc("DeleteObject").Call(region)
		}
	}
	invalidateFloat()
}
func invalidateFloat() {
	if floatWindow != 0 {
		user.NewProc("InvalidateRect").Call(floatWindow, 0, 0)
	}
}
func refreshFloat() {
	l, r := cachedText[proxyDown], cachedText[directDown]
	if l == "" {
		l = "—"
	}
	if r == "" {
		r = "—"
	}
	ld, rd := cachedText[proxyDay], cachedText[directDay]
	if ld == "" { ld = "—" }; if rd == "" { rd = "—" }
	if l == floatLeftText && r == floatRightText && ld == floatLeftDay && rd == floatRightDay && floatWidth > 0 {
		return
	}
	floatLeftText, floatRightText, floatLeftDay, floatRightDay = l, r, ld, rd
	oldWidth := floatWidth
	measureFloat()
	if oldWidth != floatWidth {
		floatPlace()
	} else {
		invalidateFloat()
	}
}
func enterFloating() {
	if currentDisplay == displayFloat {
		return
	}
	if floatWindow == 0 {
		floatLeftText, floatRightText = "—", "—"
		measureFloat()
		cn, _ := syscall.UTF16PtrFromString("PcTrafficMonitorFloat311")
		cursor, _, _ := user.NewProc("LoadCursorW").Call(0, 32649)
		wc := wndClass{Style: 3, Proc: syscall.NewCallback(floatProc), Instance: instance, Cursor: cursor, Background: whiteBrush, ClassName: cn}
		wc.Size = uint32(unsafe.Sizeof(wc))
		r, _, _ := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))
		if r == 0 {
			msg("悬浮窗初始化失败")
			return
		}
		a := workArea(mainWindow)
		floatAnchor = point{a.R - int32(floatWidth) - 18, a.B - floatHeight - 18}
		floatWindow, _, _ = createWindow.Call(0x08000088, uintptr(unsafe.Pointer(cn)), 0, 0x80000000, uintptr(floatAnchor.X), uintptr(floatAnchor.Y), uintptr(floatWidth), floatHeight, 0, 0, instance, 0)
		runtime.KeepAlive(cn)
		if floatWindow == 0 {
			msg("悬浮窗创建失败")
			return
		}
	}
	hideButtonTip()
	currentDisplay = displayFloat
	floatDragging = false
	floatMoved = false
	showWindow.Call(mainWindow, 0)
	refreshFloat()
	floatPlace()
}
func hideFloat() {
	if floatDragging {
		floatDragging = false
		user.NewProc("ReleaseCapture").Call()
	}
	if floatWindow != 0 {
		showWindow.Call(floatWindow, 0)
	}
}
func leaveFloating() { changeDisplay(actionOpen) }
func destroyFloat() {
	if floatWindow != 0 {
		user.NewProc("DestroyWindow").Call(floatWindow)
		floatWindow = 0
	}
}
func floatText(dc uintptr, s string, x, y, w, h int, f, c uintptr) {
	r := rect32{int32(x), int32(y), int32(x + w), int32(y + h)}
	gdi.NewProc("SetTextColor").Call(dc, c)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, f)
	v, _ := syscall.UTF16PtrFromString(s)
	user.NewProc("DrawTextW").Call(dc, uintptr(unsafe.Pointer(v)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x0824)
	gdi.NewProc("SelectObject").Call(dc, old)
	runtime.KeepAlive(v)
}
func floatLine(dc uintptr, x, y, w, h int, b uintptr) {
	r := rect32{int32(x), int32(y), int32(x + w), int32(y + h)}
	user.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&r)), b)
}
func floatPaint(h uintptr) {
	var ps floatPaintStruct
	dc, _, _ := user.NewProc("BeginPaint").Call(h, uintptr(unsafe.Pointer(&ps)))
	if dc == 0 {
		return
	}
	defer user.NewProc("EndPaint").Call(h, uintptr(unsafe.Pointer(&ps)))
	var r rect32
	getClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	user.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&r)), whiteBrush)
	gdi.NewProc("SetBkMode").Call(dc, 1)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, whiteBrush)
	pen, _, _ := gdi.NewProc("CreatePen").Call(0, 1, 0xddd8d3)
	op, _, _ := gdi.NewProc("SelectObject").Call(dc, pen)
	gdi.NewProc("RoundRect").Call(dc, 0, 0, uintptr(r.R), uintptr(r.B), 12, 12)
	gdi.NewProc("SelectObject").Call(dc, op)
	gdi.NewProc("SelectObject").Call(dc, old)
	gdi.NewProc("DeleteObject").Call(pen)
	floatLine(dc, floatDivider, 8, 1, floatHeight-16, lineBrush)
	floatText(dc, floatLeftText, floatPadding, floatSpeedY, floatDivider-floatGap-floatPadding, floatSpeedH, floatFont, orange)
	floatText(dc, floatRightText, floatRightX, floatSpeedY, floatWidth-floatRightX-floatPadding, floatSpeedH, floatFont, green)
	floatText(dc, floatLeftDay, floatPadding, floatDayY, floatDivider-floatGap-floatPadding, floatDayH, floatDayFont, orange)
	floatText(dc, floatRightDay, floatRightX, floatDayY, floatWidth-floatRightX-floatPadding, floatDayH, floatDayFont, green)
}
func floatProc(h uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case 0x0021:
		return 3
	case 0x0014:
		return 1
	case 0x000f:
		floatPaint(h)
		return 0
	case 0x0201:
		floatDragging = true
		floatMoved = false
		floatStart = cursorPoint()
		floatOld = floatAnchor
		user.NewProc("SetCapture").Call(h)
		return 0
	case 0x0200:
		if floatDragging {
			p := cursorPoint()
			dx, dy := p.X-floatStart.X, p.Y-floatStart.Y
			if dragExceeded(dx, dy) {
				floatMoved = true
			}
			if floatMoved {
				floatAnchor = point{floatOld.X + dx, floatOld.Y + dy}
				floatPlace()
			}
		}
		return 0
	case 0x0202:
		if floatDragging {
			p := cursorPoint()
			if dragExceeded(p.X-floatStart.X, p.Y-floatStart.Y) {
				floatMoved = true
				floatAnchor = point{floatOld.X + p.X - floatStart.X, floatOld.Y + p.Y - floatStart.Y}
				floatPlace()
			}
			floatDragging = false
			user.NewProc("ReleaseCapture").Call()
			if !floatMoved {
				leaveFloating()
			}
		}
		return 0
	case 0x0204, 0x0205, 0x0206, 0x007b: // Ignore all right-click / context-menu messages.
		return 0
	case 0x0215:
		floatDragging = false
		return 0
	case 0x0010:
		leaveFloating()
		return 0
	}
	r, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return r
}
