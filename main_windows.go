package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var user = syscall.NewLazyDLL("user32.dll")
var gdi = syscall.NewLazyDLL("gdi32.dll")
var shell = syscall.NewLazyDLL("shell32.dll")
var createWindow = user.NewProc("CreateWindowExW")
var send = user.NewProc("SendMessageW")
var setText = user.NewProc("SetWindowTextW")
var getText = user.NewProc("GetWindowTextW")
var getTextLen = user.NewProc("GetWindowTextLengthW")
var defWindow = user.NewProc("DefWindowProcW")
var setPos = user.NewProc("SetWindowPos")
var showWindow = user.NewProc("ShowWindow")
var module = kernel.NewProc("GetModuleHandleW")
var instance, mainWindow, font, largeFont uintptr
var status, rates, daily, monthly, quotaLine, detail, adapter, addrEdit, secretEdit, quotaEdit, topButton, startButton uintptr
var top = true
var trayIcon, taskbarCreated uintptr
var trayReady bool
var currentDisplay = displayFull
var running = true
var done = make(chan struct{})
var stopped = make(chan struct{})
var updates = make(chan Update, 1)
var settings = make(chan Settings, 1)
var last Update
var children []uintptr
var colors = map[uintptr]uintptr{}
var backgrounds = map[uintptr]uintptr{}
var whiteBrush, orangeBrush, greenBrush, grayBrush, lineBrush uintptr
var proxyDown, directDown, proxyUp, directUp, proxyDay, directDay, proxyPct, directPct, unknownLine, barP, barD, barU, sourceLine uintptr

const minExpandedHeight = 416 // baseline; measured native row heights can raise it

var recent RecentRows
var cachedText = map[uintptr]string{}
var cachedVisible = map[uintptr]bool{}
var tableControls []uintptr
var exitRequested bool
var proxyState, localState uintptr
var headerBtns = map[uintptr]bool{}
var paleBrush uintptr
var numberFont, smallFont, floatFont, floatDayFont uintptr

const windowStyle = 0x00CF0000 // WS_OVERLAPPEDWINDOW: sizing frame, maximize and restore
var horizontalLine, verticalLine uintptr
var metricLabels [2][5]uintptr
var layoutReady bool
var lastTableWidth int
var lastVisibleCapacity = -1
var mainScale = 100
var mainSizing, mainRescaling bool
func mainPx(v int) int {return floatScaled(v,mainScale)}
func mainOffset(v int) int32 {return int32(scaledOffset(v,mainScale))}
func mainMonitorScale() (int,rect32) {
 m,_,_:=user.NewProc("MonitorFromWindow").Call(mainWindow,2)
 mi:=monitorInfo{Size:uint32(unsafe.Sizeof(monitorInfo{}))}
 if r,_,_:=user.NewProc("GetMonitorInfoW").Call(m,uintptr(unsafe.Pointer(&mi)));r!=0 {
  return floatResolutionScale(int(mi.Monitor.R-mi.Monitor.L),int(mi.Monitor.B-mi.Monitor.T)),mi.Work
 }
 return mainScale,workArea(mainWindow)
}

const orange = 0x307AC7
const green = 0x728C13
const muted = 0x80736A
const dark = 0x302B24

func brush(c uintptr) uintptr { r, _, _ := gdi.NewProc("CreateSolidBrush").Call(c); return r }
func colored(s string, x, y, w, h int, f, c uintptr) uintptr {
	r := ctrl("STATIC", s, 0x4200, x, y, w, h, 0, f)
	colors[r] = c
	return r
}
func block(x, y, w, h int, b uintptr) uintptr {
	r := label("", x, y, w, h)
	backgrounds[r] = b
	return r
}
func visible(h uintptr, on bool) {
	if old, ok := cachedVisible[h]; ok && old == on {
		return
	}
	cachedVisible[h] = on
	v := uintptr(0)
	if on {
		v = 5
	}
	showWindow.Call(h, v)
}
func resizeBar(h uintptr, x, w int) {
	visible(h, w > 0)
	if w > 0 {
		setPos.Call(h, 0, uintptr(x), 221, uintptr(w), 9, 0x0010)
	}
}

const (
	wmDestroy        = 2
	wmCommand        = 0x111
	wmTimer          = 0x113
	wmClose          = 0x10
	wmSetFont        = 0x30
	wsChild          = 0x40000000
	wsVisible        = 0x10000000
	wsTabstop        = 0x10000
	wsBorder         = 0x800000
	wsVscroll        = 0x200000
	idApply          = 101
	idTop            = 102
	idStart          = 104
	idFolder         = 105
	idTrayOpen       = 201
	idTrayExit       = 202
	idTrayFloat      = 203
	idTrayBackground = 204
)

func ptr(s string) uintptr {
	p, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	return uintptr(unsafe.Pointer(p))
}

// Win32 text pointers are passed directly into calls; Go's current GC is nonmoving.
func text(h uintptr, s string) {
	if old, ok := cachedText[h]; ok && old == s {
		return
	}
	cachedText[h] = s
	p, _ := syscall.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	setText.Call(h, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}
func read(h uintptr) string {
	n, _, _ := getTextLen.Call(h)
	if n > 8192 {
		n = 8192
	}
	b := make([]uint16, n+1)
	getText.Call(h, uintptr(unsafe.Pointer(&b[0])), n+1)
	return syscall.UTF16ToString(b)
}
func ctrl(class, label string, style uint32, x, y, w, h int, id int, f uintptr) uintptr {
	c, _ := syscall.UTF16PtrFromString(class)
	l, _ := syscall.UTF16PtrFromString(label)
	r, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(l)), uintptr(style|wsChild|wsVisible), uintptr(mainPx(x)), uintptr(mainPx(y)), uintptr(mainPx(w)), uintptr(mainPx(h)), mainWindow, uintptr(id), instance, 0)
	runtime.KeepAlive(c)
	runtime.KeepAlive(l)
	if f == 0 {
		f = font
	}
	send.Call(r, wmSetFont, f, 1)
	return r
}
func label(s string, x, y, w, h int) uintptr { return ctrl("STATIC", s, 0, x, y, w, h, 0, 0) }
func btn(s string, x, y, w, h, id int) uintptr {
	r := ctrl("BUTTON", s, wsTabstop|0x0000000B, x, y, w, h, id, 0)
	headerBtns[r] = true
	return r
}
func edit(s string, x, y, w, h int, style uint32) uintptr {
	return ctrl("EDIT", s, wsBorder|wsTabstop|style, x, y, w, h, 0, 0)
}
func makeFont(height int, weight int) uintptr {
	return makeFontQuality(height, weight, 5)
}
func makeFontQuality(height, weight int, quality uintptr) uintptr {
	p, _ := syscall.UTF16PtrFromString("Microsoft YaHei UI")
	r, _, _ := gdi.NewProc("CreateFontW").Call(uintptr(-height), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, quality, 0, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
	return r
}
func setTop() {
	v := ^uintptr(0)
	if !top {
		v = ^uintptr(1)
	}
	setPos.Call(mainWindow, v, 0, 0, 0, 0, 0x0001|0x0002|0x0010)
	if topButton != 0 {
		updateButtonTip(topButton, map[bool]string{true: "取消置顶", false: "置顶"}[top])
		user.NewProc("InvalidateRect").Call(topButton, 0, 1)
	}
}
func clientSize() (int, int) {
	var r rect32
	getClientRect.Call(mainWindow, uintptr(unsafe.Pointer(&r)))
	return int(r.R - r.L), int(r.B - r.T)
}
func moveControl(h uintptr, x, y, w, height int) {
	if h == 0 {
		return
	}
	if w < 1 {
		w = 1
	}
	if height < 1 {
		height = 1
	}
	setPos.Call(h, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(height), 0x0004|0x0010)
}
func layoutWindow() {
	if !layoutReady {
		return
	}
	w, h := clientSize()
	if w < 1 || h < 1 {
		return
	}
	p:=mainPx
	moveControl(status,p(16),p(headerY),w-p(126),p(headerH))
	moveControl(topButton,w-p(57),p(headerY),p(40),p(headerH))
	moveControl(updateButton,w-p(103),p(headerY),p(40),p(headerH))
	moveControl(horizontalLine,p(16),p(45),w-p(32),p(1))
	moveControl(verticalLine,w/2,p(59),p(1),p(105))
	for i:=0;i<2;i++{
		x,width:=p(20),w/2-p(40)
		if i==1{x=w/2+p(8);width=w-x-p(32)}
		a:=metricLabels[i]
		moveControl(a[0],x,p(56),width,p(21))
		moveControl(a[1],x,p(79),width,p(39))
		moveControl(a[2],x,p(121),width,p(22))
		moveControl(a[3],x,p(144),p(56),p(27))
		moveControl(a[4],x+p(60),p(144),width-p(60),p(27))
	}
	tw:=w-p(32)
	for _,v:=range tableControls{
		moveControl(v,p(16),p(connectionTableY),tw,h-p(connectionTableInset))
	}
	if tw != lastTableWidth {
		fitColumns(tw)
		lastTableWidth = tw
	}
	// Resize immediately from the full recent cache, without waiting for a new sample.
	capacity := tableCapacity()
	if capacity != lastVisibleCapacity {
		renderRows(lastRows, last)
	}
}
func resizeClient(height int) {
	w, _ := clientSize()
	if mainScale!=100 || w < mainPx(600) {
		w = mainPx(704)
	}
	r := rect32{0, 0, int32(w), int32(height)}
	user.NewProc("AdjustWindowRectEx").Call(uintptr(unsafe.Pointer(&r)), windowStyle, 0, 0)
	_,area:=mainMonitorScale()
	ow,oh:=int(r.R-r.L),int(r.B-r.T)
	var current rect32
	user.NewProc("GetWindowRect").Call(mainWindow,uintptr(unsafe.Pointer(&current)))
	if area.R>area.L&&area.B>area.T{ow=min(ow,int(area.R-area.L));oh=min(oh,int(area.B-area.T))}
	p:=clampPoint(point{current.L,current.T},ow,oh,area)
	setPos.Call(mainWindow,0,uintptr(p.X),uintptr(p.Y),uintptr(ow),uintptr(oh),0x0004|0x0010)
}

var lastRows []RecentRow

func initControls() {
	font = makeFont(mainPx(14), 400)
	largeFont = makeFont(mainPx(20), 600)
	numberFont = makeFont(mainPx(30), 700)
	smallFont = makeFont(mainPx(13), 400)
	floatFont = makeFontQuality(17, 600, 4)
	floatDayFont = makeFontQuality(12, 500, 4)
	// Keep status and both header buttons on the same vertical centerline.
	status = ctrl("STATIC", "正在启动 Windows 网络采集…", 0x4280, 16, headerY, 576, headerH, 0, smallFont)
	colors[status] = muted
	topButton = btn("置顶", 647, 10, 40, 29, idTop)
	updateButton = btn("更新", 601, 10, 40, 29, idUpdate)
	visible(updateButton, false)
	horizontalLine = block(16, 45, 672, 1, lineBrush)
	verticalLine = block(352, 59, 1, 105, lineBrush)
	for i := 0; i < 2; i++ {
		x := 20 + i*340
		c := uintptr(orange)
		name := "代理"
		if i == 1 {
			c = green
			name = "本地"
		}
		labelDown := colored(name+"下载速度", x, 56, 312, 21, font, c)
		down := colored("—", x, 79, 312, 39, numberFont, c)
		up := colored("上传 —", x, 121, 312, 22, font, c)
		labelDay := colored("今日累计", x, 144, 56, 27, smallFont, c)
		day := colored("0 B", x+60, 144, 252, 27, largeFont, c)
		metricLabels[i] = [5]uintptr{labelDown, down, up, labelDay, day}
		if i == 0 {
			proxyDown, proxyUp, proxyDay = down, up, day
		} else {
			directDown, directUp, directDay = down, up, day
		}
	}
	initConnectionTable()
	initButtonTips()
	layoutReady = true
	layoutWindow()
	renderRows(nil, Update{})
}
// Rebuild native GDI fonts only when the main window changes monitors.
// Window dimensions scale from the previous client size; maximized windows
// keep their native state. All operations run on the window thread.
func syncMainScale(){
 if !layoutReady||mainSizing||mainRescaling||currentDisplay!=displayFull{return}
 scale,area:=mainMonitorScale()
 if scale==mainScale{return}
 if r,_,_:=user.NewProc("IsIconic").Call(mainWindow);r!=0{return}
 mainRescaling=true;defer func(){mainRescaling=false}()
 hideButtonTip()
 oldScale:=mainScale
 w,h:=clientSize()
 next:=[4]uintptr{makeFont(floatScaled(14,scale),400),makeFont(floatScaled(20,scale),600),makeFont(floatScaled(30,scale),700),makeFont(floatScaled(13,scale),400)}
 for _,f:=range next{if f==0{for _,v:=range next{if v!=0{gdi.NewProc("DeleteObject").Call(v)}};return}}
 old:=[4]uintptr{font,largeFont,numberFont,smallFont}
 mainScale=scale
 font,largeFont,numberFont,smallFont=next[0],next[1],next[2],next[3]
 send.Call(status,wmSetFont,smallFont,0)
 for _,v:=range []uintptr{topButton,updateButton,horizontalLine,verticalLine}{if v!=0{send.Call(v,wmSetFont,font,0)}}
 for _,a:=range metricLabels{for i,v:=range a{if v!=0{f:=font;switch i{case 1:f=numberFont;case 3:f=smallFont;case 4:f=largeFont};send.Call(v,wmSetFont,f,0)}}}
 for _,v:=range tableControls{send.Call(v,wmSetFont,font,0)}
 rescaleConnectionTable(oldScale)
 for _,v:=range old{if v!=0{gdi.NewProc("DeleteObject").Call(v)}}
 lastTableWidth=0;lastVisibleCapacity=-1
 if zoom,_,_:=user.NewProc("IsZoomed").Call(mainWindow);zoom==0{
  w=max(mainPx(600),scaleBetween(w,oldScale,scale))
  h=max(expandedMinimum(),scaleBetween(h,oldScale,scale))
  r:=rect32{0,0,int32(w),int32(h)}
  user.NewProc("AdjustWindowRectEx").Call(uintptr(unsafe.Pointer(&r)),windowStyle,0,0)
  ow,oh:=int(r.R-r.L),int(r.B-r.T)
  if area.R>area.L&&area.B>area.T{ow=min(ow,int(area.R-area.L));oh=min(oh,int(area.B-area.T))}
  var current rect32
  user.NewProc("GetWindowRect").Call(mainWindow,uintptr(unsafe.Pointer(&current)))
  p:=clampPoint(point{current.L,current.T},ow,oh,area)
  setPos.Call(mainWindow,0,uintptr(p.X),uintptr(p.Y),uintptr(ow),uintptr(oh),0x0004|0x0010)
 }
 layoutWindow()
 user.NewProc("RedrawWindow").Call(mainWindow,0,0,0x0085)
}
func seenText(r RecentRow, now time.Time) string {
	if r.Active {
		return "实时"
	}
	sec := int(now.Sub(r.Seen).Seconds())
	if sec < 1 {
		return "刚刚"
	}
	if sec < 60 {
		return fmt.Sprintf("%d 秒前", sec)
	}
	if sec < 3600 {
		return fmt.Sprintf("%d 分钟前", sec/60)
	}
	return fmt.Sprintf("%d 小时前", sec/3600)
}
func renderRows(rows []RecentRow, u Update) {
	capacity := tableCapacity()
	lastVisibleCapacity = capacity
	if len(rows) > capacity {
		rows = rows[:capacity]
	}
	renderConnectionTable(rows, u)
}
func updateView(u Update) {
	text(status, updateStatusText(u.Status+"  |  "+u.ProxyStatus))
	ps, pu, ds, du := "—", "上传 —", "—", "上传 —"
	if u.Connected && u.ProxySeconds > 0 {
		ps = human(float64(u.Speed[Proxy].Down)/u.ProxySeconds) + "/s"
		pu = "上传 " + human(float64(u.Speed[Proxy].Up)/u.ProxySeconds) + "/s"
	} else if !u.Connected && u.ProxyStatus == "Clash 未开启" {
		ps = "0 B/s"
		pu = "上传 0 B/s"
	}
	if u.LocalReady && u.Seconds > 0 {
		ds = human(float64(u.Speed[Direct].Down)/u.Seconds) + "/s"
		du = "上传 " + human(float64(u.Speed[Direct].Up)/u.Seconds) + "/s"
	}
	text(proxyDown, ps)
	text(proxyUp, pu)
	text(directDown, ds)
	text(directUp, du)
	text(proxyDay, sumText(u.Day[Proxy]))
	text(directDay, sumText(u.Day[Direct]))
	lastRows = recent.Update(u.Rows, time.Now(), maxRecentRows)
	renderRows(lastRows, u)
	last = u
	if currentDisplay == displayFloat {
		refreshFloat()
	}
}
func msg(s string) {
	p, _ := syscall.UTF16PtrFromString(s)
	title, _ := syscall.UTF16PtrFromString("网络流量监控 V"+appVersion())
	user.NewProc("MessageBoxW").Call(mainWindow, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(title)), 0x40)
	runtime.KeepAlive(p)
	runtime.KeepAlive(title)
}

var adv = syscall.NewLazyDLL("advapi32.dll")

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const runValue = "ClashTrafficMonitor"

func registry(write bool) (syscall.Handle, error) {
	var h syscall.Handle
	p, _ := syscall.UTF16PtrFromString(runKey)
	access := uint32(1)
	if write {
		access = 3
	}
	r, _, _ := adv.NewProc("RegOpenKeyExW").Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(p)), 0, uintptr(access), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return h, nil
}
func startup() bool {
	h, e := registry(false)
	if e != nil {
		return false
	}
	defer adv.NewProc("RegCloseKey").Call(uintptr(h))
	p, _ := syscall.UTF16PtrFromString(runValue)
	r, _, _ := adv.NewProc("RegQueryValueExW").Call(uintptr(h), uintptr(unsafe.Pointer(p)), 0, 0, 0, uintptr(unsafe.Pointer(new(uint32))))
	return r == 0
}
func updateStart() {
	if startup() {
		text(startButton, "关闭开机自启")
	} else {
		text(startButton, "开启开机自启")
	}
}
func toggleStart() {
	on := startup()
	h, e := registry(true)
	if e != nil {
		msg("无法修改开机自启：" + e.Error())
		return
	}
	defer adv.NewProc("RegCloseKey").Call(uintptr(h))
	p, _ := syscall.UTF16PtrFromString(runValue)
	var r uintptr
	if on {
		r, _, _ = adv.NewProc("RegDeleteValueW").Call(uintptr(h), uintptr(unsafe.Pointer(p)))
	} else {
		exe, e := os.Executable()
		if e != nil {
			msg(e.Error())
			return
		}
		v, _ := syscall.UTF16FromString("\"" + exe + "\"")
		r, _, _ = adv.NewProc("RegSetValueExW").Call(uintptr(h), uintptr(unsafe.Pointer(p)), 0, 1, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
		runtime.KeepAlive(v)
	}
	if r != 0 {
		msg(fmt.Sprint("无法设置开机自启：", syscall.Errno(r)))
	}
	updateStart()
}
func openFolder() {
	p := filepath.Dir(dataPath())
	_ = os.MkdirAll(p, 0700)
	v, _ := syscall.UTF16PtrFromString(p)
	o, _ := syscall.UTF16PtrFromString("open")
	shell.NewProc("ShellExecuteW").Call(mainWindow, uintptr(unsafe.Pointer(o)), uintptr(unsafe.Pointer(v)), 0, 0, 1)
	runtime.KeepAlive(v)
}

// Notification icon uses the Unicode NOTIFYICONDATAW structure (Windows x64).
type notifyData struct {
	Size                uint32
	Hwnd                uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Timeout             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	Guid                [16]byte
	BalloonIcon         uintptr
}

const trayMessage = 0x8001

func notifyTray(op uintptr) bool {
	n := notifyData{Hwnd: mainWindow, ID: 1, Flags: 7, Callback: trayMessage, Icon: trayIcon}
	n.Size = uint32(unsafe.Sizeof(n))
	v, _ := syscall.UTF16FromString("网络流量监控 - 点击恢复窗口")
	copy(n.Tip[:], v)
	r, _, _ := shell.NewProc("Shell_NotifyIconW").Call(op, uintptr(unsafe.Pointer(&n)))
	return r != 0
}

// All display changes run on the window thread. No worker or history reset.
func changeDisplay(action displayAction) {
	target := nextDisplay(currentDisplay, action, trayReady)
	if target == displayExit {
		if !exitRequested {
			exitRequested = true
			currentDisplay = displayExit
			user.NewProc("DestroyWindow").Call(mainWindow)
		}
		return
	}
	if target == displayFloat {
		if currentDisplay != displayFloat {
			enterFloating()
		}
		return
	}
	if target == displayBackground {
		if currentDisplay == displayBackground {
			return
		}
		hideButtonTip()
		currentDisplay = displayBackground
		hideFloat()
		showWindow.Call(mainWindow, 0)
		return
	}
	if target == displayFull && (currentDisplay != displayFull || action == actionOpen) {
		currentDisplay = displayFull
		hideFloat()
		if r, _, _ := user.NewProc("IsIconic").Call(mainWindow); r != 0 {
			showWindow.Call(mainWindow, 9) // restore an iconified window
		} else {
			showWindow.Call(mainWindow, 5)
		} // keep maximized/restored size
		setTop()
		user.NewProc("SetForegroundWindow").Call(mainWindow)
		requestUpdateCheck()
	}
}
func restoreTray() { changeDisplay(actionOpen) }

// Tray menu is the only context menu. Minimize the main window for background mode.
func trayContextMenu() {
	menu, _, _ := user.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer user.NewProc("DestroyMenu").Call(menu)
	appendItem := func(id uintptr, title string, disabled bool) {
		p, _ := syscall.UTF16PtrFromString(title)
		var flags uintptr
		if disabled {
			flags = 0x0001
		}
		user.NewProc("AppendMenuW").Call(menu, flags, id, uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
	}
	appendItem(idTrayOpen, "打开窗口", false)
	appendItem(idTrayFloat, "悬浮显示", false)
	user.NewProc("AppendMenuW").Call(menu, 0x0800, 0, 0)
	appendItem(idTrayExit, "退出程序", false)
	p := cursorPoint()
	owner := mainWindow
	if currentDisplay == displayFloat && floatWindow != 0 {
		owner = floatWindow
	}
	user.NewProc("SetForegroundWindow").Call(owner)
	cmd, _, _ := user.NewProc("TrackPopupMenu").Call(menu, 0x0102, uintptr(p.X), uintptr(p.Y), 0, owner, 0)
	user.NewProc("PostMessageW").Call(owner, 0, 0, 0)
	switch cmd {
	case idTrayOpen:
		changeDisplay(actionOpen)
	case idTrayFloat:
		changeDisplay(actionFloat)
	case idTrayExit:
		changeDisplay(actionExit)
	}
}
func wndProc(h uintptr, m uint32, w, l uintptr) uintptr {
	if taskbarCreated != 0 && uintptr(m) == taskbarCreated {
		trayReady = notifyTray(0)
		if !trayReady && currentDisplay == displayBackground {
			restoreTray()
		}
		return 0
	}
	if m == trayMessage {
		if l == 0x205 {
			trayContextMenu()
		} else if l == 0x202 || l == 0x203 {
			restoreTray()
		}
		return 0
	}
	if m == 5 && w == 1 && currentDisplay == displayFull && trayReady {
		changeDisplay(actionMinimize)
		return 0
	}

	switch m {
	case 0x0024: // WM_GETMINMAXINFO: keep both metrics readable when dragging.
		if l != 0 {
			mm := (*minMaxInfo)(unsafe.Pointer(l))
			height := expandedMinimum()
			r := rect32{0, 0, int32(mainPx(600)), int32(height)}
			user.NewProc("AdjustWindowRectEx").Call(uintptr(unsafe.Pointer(&r)), windowStyle, 0, 0)
			mm.MinTrack = point{r.R - r.L, r.B - r.T}
		}
		return 0
	case 0x0231: // WM_ENTERSIZEMOVE
		mainSizing=true
	case 0x0232: // WM_EXITSIZEMOVE
		mainSizing=false
		syncMainScale()
	case 5: // WM_SIZE, including maximize and restore
		if w != 1 && layoutReady {

			layoutWindow()
		}
		return 0
	case 0x8002: // Column drag ended; restore a scrollbar-free fit.
		if layoutReady {
			w, _ := clientSize()
			fitColumns(w - mainPx(32))
		}
		return 0
	case 0x004e: // WM_NOTIFY: list view custom drawing
		if result, handled := tableNotify(l); handled {
			return result
		}
	case 0x002b:
		if l == 0 {
			return 0
		}
		d := (*drawItem)(unsafe.Pointer(l))
		if !headerBtns[d.Hwnd] {
			break
		}
		r := d.Rect
		fill := paleBrush
		if (d.Hwnd == topButton && top) || d.Hwnd == updateButton {
			fill = pinActiveBrush
		}
		if d.State&1 != 0 {
			fill = lineBrush
		}
		user.NewProc("FillRect").Call(d.DC, uintptr(unsafe.Pointer(&r)), whiteBrush)
		old, _, _ := gdi.NewProc("SelectObject").Call(d.DC, fill)
		pen, _, _ := gdi.NewProc("GetStockObject").Call(8)
		oldPen, _, _ := gdi.NewProc("SelectObject").Call(d.DC, pen)
		gdi.NewProc("RoundRect").Call(d.DC, uintptr(r.L), uintptr(r.T), uintptr(r.R), uintptr(r.B), uintptr(mainPx(12)), uintptr(mainPx(12)))
		gdi.NewProc("SelectObject").Call(d.DC, old)
		gdi.NewProc("SelectObject").Call(d.DC, oldPen)
		if d.Hwnd == updateButton {
			drawUpdateIcon(d.DC, r)
			return 1
		}
		if d.Hwnd == topButton {
			drawPinIcon(d.DC, r, top)
			return 1
		}
		gdi.NewProc("SetBkMode").Call(d.DC, 1)
		gdi.NewProc("SetTextColor").Call(d.DC, dark)
		oldFont, _, _ := gdi.NewProc("SelectObject").Call(d.DC, font)
		v, _ := syscall.UTF16PtrFromString(read(d.Hwnd))
		user.NewProc("DrawTextW").Call(d.DC, uintptr(unsafe.Pointer(v)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x25)
		gdi.NewProc("SelectObject").Call(d.DC, oldFont)
		runtime.KeepAlive(v)
		return 1
	case 0x0138: // WM_CTLCOLORSTATIC
		if bg, ok := backgrounds[l]; ok {
			return bg
		}
		c, ok := colors[l]
		if !ok {
			c = dark
		}
		gdi.NewProc("SetTextColor").Call(w, c)
		gdi.NewProc("SetBkColor").Call(w, 0xffffff)
		return whiteBrush
	case 0x0133: // WM_CTLCOLOREDIT
		gdi.NewProc("SetTextColor").Call(w, dark)
		gdi.NewProc("SetBkColor").Call(w, 0xffffff)
		return whiteBrush
	case wmCommand:
		switch int(w & 0xffff) {
		case idApply:
		case idTrayOpen:
			restoreTray()
		case idTrayExit:
			changeDisplay(actionExit)
		case idTrayFloat:
			changeDisplay(actionFloat)
		case idUpdate:
			startUpdate()
		case idTop:
			top = !top
			setTop()
		case idStart:
			toggleStart()
		case idFolder:
			if trayReady {
				changeDisplay(actionBackground)
			} else {
				showWindow.Call(h, 6)
			}
		}
		return 0
	case wmTimer:
		updateFloatGameVisibility()
		syncMainScale()
		pollUpdateUI()
		select {
		case u := <-updates:
			updateView(u)
		default:
		}
		return 0
	case wmClose:
		if !exitRequested {
			changeDisplay(actionClose)
		} else {
			user.NewProc("DestroyWindow").Call(h)
		}
		return 0
	case 0x0016: // WM_ENDSESSION
		if w != 0 {
			exitRequested = true
			user.NewProc("DestroyWindow").Call(h)
		}
		return 0
	case wmDestroy:
		destroyFloat()
		releaseButtonTips()
		releaseConnectionTable()
		if trayReady {
			notifyTray(2)
			trayReady = false
		}
		if running {
			running = false
			close(done)
		}
		user.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	r, _, _ := defWindow.Call(h, uintptr(m), w, l)
	return r
}

type rect32 struct{ L, T, R, B int32 }
type drawItem struct {
	Type, ID, ItemID, Action, State uint32
	Hwnd, DC                        uintptr
	Rect                            rect32
	Data                            uintptr
}
type wndClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	IconSm                             uintptr
}
type point struct{ X, Y int32 }
type minMaxInfo struct{ Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack point }
type message struct {
	Hwnd           uintptr
	Message        uint32
	Wparam, Lparam uintptr
	Time           uint32
	Pt             point
	Private        uint32
}

func main() {
	runtime.LockOSThread()
	if len(os.Args)>1 && os.Args[1]=="--apply-update" { applyUpdateHelper(os.Args); return }
	// ETW system logger requires elevated rights. Request them through the standard Windows UAC prompt.
	admin, _, _ := isAdmin.Call()
	if admin == 0 {
		exe, e := os.Executable()
		if e != nil {
			msg(e.Error())
			return
		}
		r, _, _ := shell.NewProc("ShellExecuteW").Call(0, ptr("runas"), ptr(exe), 0, 0, 1)
		if r <= 32 {
			msg("需要管理员权限以读取 Windows 实时网络事件。")
		}
		return
	}

	name, _ := syscall.UTF16PtrFromString("Local\\PcTrafficMonitorV31-single-instance")
	mutex, _, err := kernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
	if mutex != 0 {
		defer syscall.CloseHandle(syscall.Handle(mutex))
		if err == syscall.Errno(183) {
			msg("监控程序已经运行，请查看任务栏或系统托盘。")
			return
		}
	}
	user.NewProc("SetProcessDPIAware").Call()
	instance, _, _ = module.Call(0)
	whiteBrush = brush(0xffffff)
	orangeBrush = brush(orange)
	greenBrush = brush(green)
	grayBrush = brush(0xd9d9d9)
	lineBrush = brush(0xeee9e5)
	paleBrush = brush(0xf5f2ef)
	pinActiveBrush = brush(0xe9f5eb)
	cn, _ := syscall.UTF16PtrFromString("PcTrafficMonitorV31Window")
	title, _ := syscall.UTF16PtrFromString("网络流量监控 V"+appVersion())
	cursor, _, _ := user.NewProc("LoadCursorW").Call(0, 32512)
	icon, _, _ := user.NewProc("LoadIconW").Call(instance, 1)
	if icon == 0 {
		icon, _, _ = user.NewProc("LoadIconW").Call(0, 32512)
	}
	wc := wndClass{Style: 3, Proc: syscall.NewCallback(wndProc), Instance: instance, Icon: icon, Cursor: cursor, Background: whiteBrush, ClassName: cn, IconSm: icon}
	wc.Size = uint32(unsafe.Sizeof(wc))
	r, _, e := user.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		msg("窗口创建失败：" + e.Error())
		return
	}
	mainWindow, _, e = createWindow.Call(0, uintptr(unsafe.Pointer(cn)), uintptr(unsafe.Pointer(title)), windowStyle, 0x80000000, 0x80000000, 720, 475, 0, 0, instance, 0)
	if mainWindow == 0 {
		msg("窗口创建失败：" + e.Error())
		return
	}
	mainScale,_=mainMonitorScale()
	initControls()
	resizeClient(expandedMinimum())
	setTop()
	trayIcon = icon
	trayReady = notifyTray(0)
	taskbarName, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	taskbarCreated, _, _ = user.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(taskbarName)))
	if len(os.Args)==3 && os.Args[1]=="--cleanup-update" { go cleanupUpdate(os.Args[2]) }
	go runWorker(settings, updates, done, stopped)
	go updateCheckLoop()
	user.NewProc("SetTimer").Call(mainWindow, 1, 250, 0)
	showWindow.Call(mainWindow, 5)
	user.NewProc("UpdateWindow").Call(mainWindow)
	var m message
	for {
		r, _, _ := user.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		user.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
		user.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
	}
	if running {
		close(done)
	}
	select {
	case <-stopped:
	case <-time.After(8 * time.Second):
	}
	gdi.NewProc("DeleteObject").Call(font)
	gdi.NewProc("DeleteObject").Call(largeFont)
	for _, r := range []uintptr{numberFont, smallFont, floatFont, floatDayFont, whiteBrush, orangeBrush, greenBrush, grayBrush, lineBrush, paleBrush, pinActiveBrush} {
		gdi.NewProc("DeleteObject").Call(r)
	}
}
