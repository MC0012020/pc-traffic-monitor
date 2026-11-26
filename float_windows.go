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
var floatScale = 100
var floatPlacedWidth,floatPlacedHeight int
var floatPlacedAnchor point
var floatPlaced bool
func floatPx(v int) int {return floatScaled(v,floatScale)}
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
func pointMonitor(p point) uintptr {
 v:=uintptr(uint64(uint32(p.X))|uint64(uint32(p.Y))<<32)
 m,_,_:=user.NewProc("MonitorFromPoint").Call(v,2)
 return m
}
func floatMonitor() uintptr {
 if floatDragging{return pointMonitor(cursorPoint())}
 return pointMonitor(floatAnchor)
}
func syncFloatScale(m uintptr) bool {
 mi:=monitorInfo{Size:uint32(unsafe.Sizeof(monitorInfo{}))}
 r,_,_:=user.NewProc("GetMonitorInfoW").Call(m,uintptr(unsafe.Pointer(&mi)))
 if r==0{return false}
 scale:=floatResolutionScale(int(mi.Monitor.R-mi.Monitor.L),int(mi.Monitor.B-mi.Monitor.T))
 if scale==floatScale{return false}
 speed:=makeFontQuality(floatScaled(17,scale),600,4)
 day:=makeFontQuality(floatScaled(12,scale),500,4)
 if speed==0||day==0{
  if speed!=0{gdi.NewProc("DeleteObject").Call(speed)}
  if day!=0{gdi.NewProc("DeleteObject").Call(day)}
  return false
 }
 oldSpeed,oldDay:=floatFont,floatDayFont
 floatFont,floatDayFont,floatScale=speed,day,scale
 gdi.NewProc("DeleteObject").Call(oldSpeed)
 gdi.NewProc("DeleteObject").Call(oldDay)
 return true
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
// Measure a reserved symmetric column once per display scale, not per sample.
// Typical B through TB values fit without changing the capsule's geometry.
func measureFloat() {
 dc,_,_:=user.NewProc("GetDC").Call(mainWindow)
 cell:=floatPx(116)
 if dc!=0{
  old,_,_:=gdi.NewProc("SelectObject").Call(dc,floatFont)
  for _,sample:=range []string{"1024 B/s","8888.88 KB/s","8888.88 MB/s","8888.88 GB/s","8888.88 TB/s"}{cell=max(cell,floatMeasure(dc,sample)+floatPx(4))}
  gdi.NewProc("SelectObject").Call(dc,floatDayFont)
  for _,sample:=range []string{"8888.88 KB","8888.88 MB","8888.88 GB","8888.88 TB"}{cell=max(cell,floatMeasure(dc,sample)+floatPx(4))}
  gdi.NewProc("SelectObject").Call(dc,old)
  user.NewProc("ReleaseDC").Call(mainWindow,dc)
 }
 floatWidth,floatDivider,floatRightX=floatDimensionsScaled(cell,cell,floatScale)
}
func floatPlace() {
 if floatWindow==0{return}
 m:=floatMonitor()
 if syncFloatScale(m){measureFloat()}
 area:=monitorWork(m)
 height:=floatPx(floatHeight)
 floatAnchor=clampPoint(floatAnchor,floatWidth,height,area)
 resized:=!floatPlaced||floatPlacedWidth!=floatWidth||floatPlacedHeight!=height
 moved:=!floatPlaced||floatPlacedAnchor!=floatAnchor
 shown,_,_:=user.NewProc("IsWindowVisible").Call(floatWindow)
 // Preserve the existing topmost state; geometry updates never change Z-order.
 // A hidden window is shown without activation only on explicit user action.
 if resized||moved||shown==0{
  flags:=floatPlacementFlags(resized,moved,shown==0)
  after:=uintptr(0) // HWND_TOP, only for an explicitly shown normal window
  if shown==0&&top{flags|=0x0004} // keep existing topmost position
  if ok,_,_:=setPos.Call(floatWindow,after,uintptr(floatAnchor.X),uintptr(floatAnchor.Y),uintptr(floatWidth),uintptr(height),flags);ok==0{return}
  floatPlacedWidth,floatPlacedHeight,floatPlacedAnchor,floatPlaced=floatWidth,height,floatAnchor,true
 }
 if resized{
  region,_,_:=gdi.NewProc("CreateRoundRectRgn").Call(0,0,uintptr(floatWidth+1),uintptr(height+1),uintptr(floatPx(12)),uintptr(floatPx(12)))
  if region!=0{if ok,_,_:=user.NewProc("SetWindowRgn").Call(floatWindow,region,1);ok==0{gdi.NewProc("DeleteObject").Call(region)}}
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
	scaleChanged:=floatWindow!=0&&syncFloatScale(floatMonitor())
	if !scaleChanged && l == floatLeftText && r == floatRightText && ld == floatLeftDay && rd == floatRightDay && floatWidth > 0 {
		return
	}
	floatLeftText, floatRightText, floatLeftDay, floatRightDay = l, r, ld, rd
	if scaleChanged || floatWidth<=0 {
		measureFloat()
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
		m,_,_:=user.NewProc("MonitorFromWindow").Call(mainWindow,2)
		syncFloatScale(m)
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
		floatAnchor = point{a.R-int32(floatWidth+floatPx(18)),a.B-int32(floatPx(floatHeight)+floatPx(18))}
		exStyle:=windowTopStyle(0x08000080,top) // NOACTIVATE | TOOLWINDOW, plus optional TOPMOST
		floatWindow, _, _ = createWindow.Call(exStyle, uintptr(unsafe.Pointer(cn)), 0, 0x80000000, uintptr(floatAnchor.X), uintptr(floatAnchor.Y), uintptr(floatWidth), uintptr(floatPx(floatHeight)), 0, 0, instance, 0)
		runtime.KeepAlive(cn)
		if floatWindow == 0 {
			msg("悬浮窗创建失败")
			return
		}
	}
	hideButtonTip()
	currentDisplay = displayFloat
	resetPinRecovery()
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
		floatPlaced=false
	}
}
func floatText(dc uintptr, s string, x, y, w, h int, f, c uintptr) {
	r := rect32{int32(x), int32(y), int32(x + w), int32(y + h)}
	gdi.NewProc("SetTextColor").Call(dc, c)
	old, _, _ := gdi.NewProc("SelectObject").Call(dc, f)
	v, _ := syscall.UTF16PtrFromString(s)
	user.NewProc("DrawTextW").Call(dc, uintptr(unsafe.Pointer(v)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x8824) // DT_END_ELLIPSIS for exceptional oversized values
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
	gdi.NewProc("RoundRect").Call(dc,0,0,uintptr(r.R),uintptr(r.B),uintptr(floatPx(12)),uintptr(floatPx(12)))
	gdi.NewProc("SelectObject").Call(dc, op)
	gdi.NewProc("SelectObject").Call(dc, old)
	gdi.NewProc("DeleteObject").Call(pen)
	padding,gap:=floatPx(floatPadding),floatPx(floatGap)
	speedY,dayY:=floatPx(floatSpeedY),floatPx(floatDayY)
	speedH,dayH:=floatPx(floatSpeedY+floatSpeedH)-speedY,floatPx(floatDayY+floatDayH)-dayY
	floatLine(dc,floatDivider,floatPx(8),floatPx(1),floatPx(floatHeight)-2*floatPx(8),lineBrush)
	floatText(dc,floatLeftText,padding,speedY,floatDivider-gap-padding,speedH,floatFont,orange)
	floatText(dc,floatRightText,floatRightX,speedY,floatWidth-floatRightX-padding,speedH,floatFont,green)
	floatText(dc,floatLeftDay,padding,dayY,floatDivider-gap-padding,dayH,floatDayFont,orange)
	floatText(dc,floatRightDay,floatRightX,dayY,floatWidth-floatRightX-padding,dayH,floatDayFont,green)
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
