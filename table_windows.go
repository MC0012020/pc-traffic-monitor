package main

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// Standard Windows report list: resizable headers, native selection, height-adaptive visible records.
// The connection cache is presentation-only; it does not change accounting.
var connectionTable uintptr
var tableImages uintptr
var tableHeader uintptr
var headerHook uintptr
var headerInstalled bool
var tableHeadBrush, tableSelectBrush, tableAltBrush, proxyPillBrush, localPillBrush uintptr
var tableHeaderFont, tableSmallFont uintptr
var columnTitles = [6]string{"程序", "目标地址", "类型", "下载/s", "上传/s", "最近活动"}
var fillRect = user.NewProc("FillRect")
var drawText = user.NewProc("DrawTextW")
var selectObject = gdi.NewProc("SelectObject")
var setTextColor = gdi.NewProc("SetTextColor")
var setBkMode = gdi.NewProc("SetBkMode")
var invalidate = user.NewProc("InvalidateRect")
var getClientRect = user.NewProc("GetClientRect")
var defSubclass = comctl.NewProc("DefSubclassProc")
var removeSubclass = comctl.NewProc("RemoveWindowSubclass")
var setSubclass = comctl.NewProc("SetWindowSubclass")

var comctl = syscall.NewLazyDLL("comctl32.dll")
var displayCells [][6]string
var displayKinds []int
var displayActive []bool

type commonControls struct{ Size, Classes uint32 }
type lvColumn struct {
	Mask, Fmt                                                          uint32
	Width                                                              int32
	Text                                                               *uint16
	TextMax, SubItem, Image, Order, MinWidth, DefaultWidth, IdealWidth int32
}
type lvItem struct {
	Mask             uint32
	Item, SubItem    int32
	State, StateMask uint32
	Text             *uint16
	TextMax, Image   int32
	Param            uintptr
	Indent, GroupID  int32
	Columns          uint32
	Cols, ColFmt     uintptr
	Group            int32
}
type nmHeader struct {
	Hwnd, ID uintptr
	Code     int32
}
type nmCustomDraw struct {
	Header nmHeader
	Stage  uint32
	DC     uintptr
	Rect   rect32
	Item   uintptr
	State  uint32
	Param  uintptr
}
type lvCustomDraw struct {
	Base                  nmCustomDraw
	TextColor, Background uint32
	SubItem               int32
}

func initConnectionTable() {
	c := commonControls{8, 1}
	comctl.NewProc("InitCommonControlsEx").Call(uintptr(unsafe.Pointer(&c)))
	// LVS_REPORT | LVS_SINGLESEL | LVS_SHOWSELALWAYS | WS_TABSTOP
	connectionTable = ctrl("SysListView32", "", 0x0001|0x0004|0x0008|wsTabstop, 16, connectionTableY, 672, 218, 301, font)
	if connectionTable == 0 {
		tableControls = append(tableControls, colored("连接列表初始化失败", 16, connectionTableY+1, 672, 24, font, muted))
		return
	}
	tableControls = append(tableControls, connectionTable)
	// FULLROWSELECT | DOUBLEBUFFER. Delicate horizontal dividers are painted ourselves.
	send.Call(connectionTable, 0x1036, 0, 0x20|0x10000)
	send.Call(connectionTable, 0x1001, 0, 0xffffff) // LVM_SETBKCOLOR
	send.Call(connectionTable, 0x1026, 0, 0xffffff) // LVM_SETTEXTBKCOLOR
	send.Call(connectionTable, 0x1024, 0, dark)     // LVM_SETTEXTCOLOR
	// A small image list sets a consistent, comfortable row height. No row icons.
	tableImages, _, _ = comctl.NewProc("ImageList_Create").Call(1, 30, 0x20, 1, 1)
	if tableImages != 0 {
		send.Call(connectionTable, 0x1003, 1, tableImages)
	}
	columns := []struct {
		title string
		width int32
		fmt   uint32
	}{{"程序", 112, 0}, {"目标地址", 182, 0}, {"类型", 64, 0}, {"下载/s", 98, 1}, {"上传/s", 98, 1}, {"最近活动", 96, 0}}
	for i, col := range columns {
		p, _ := syscall.UTF16PtrFromString(col.title)
		v := lvColumn{Mask: 1 | 2 | 4, Fmt: col.fmt, Width: col.width, Text: p}
		send.Call(connectionTable, 0x1061, uintptr(i), uintptr(unsafe.Pointer(&v)))
		runtime.KeepAlive(p)
	}
	// Keep native header interaction, but paint its surface ourselves. Native
	// hit-testing, resize handles and keyboard focus remain.
	tableHeadBrush = brush(0xf9f6f3)
	tableSelectBrush = brush(0xf8f0e4)
	tableAltBrush = brush(0xfdfcfb)
	proxyPillBrush = brush(0xe6f1fc)
	localPillBrush = brush(0xe8f5e6)
	tableHeaderFont = makeFont(13, 600)
	tableSmallFont = makeFont(12, 400)
	tableHeader, _, _ = send.Call(connectionTable, 0x101f, 0, 0) // LVM_GETHEADER
	if tableHeader != 0 {
		headerHook = syscall.NewCallback(headerProc)
		r, _, _ := setSubclass.Call(tableHeader, headerHook, 1, 0)
		headerInstalled = r != 0
		send.Call(tableHeader, wmSetFont, tableHeaderFont, 1)
		// Recalculate the list's internal header geometry after subclassing.
		setPos.Call(connectionTable, 0, 16, connectionTableY, 672, 218, 0x0004|0x0010|0x0020)
	}
}

func releaseConnectionTable() {
	if headerInstalled && tableHeader != 0 {
		removeSubclass.Call(tableHeader, headerHook, 1)
		headerInstalled = false
	}
	for _, h := range []uintptr{tableHeadBrush, tableSelectBrush, tableAltBrush, proxyPillBrush, localPillBrush, tableHeaderFont, tableSmallFont} {
		if h != 0 {
			gdi.NewProc("DeleteObject").Call(h)
		}
	}

	if tableImages != 0 {
		if connectionTable != 0 {
			send.Call(connectionTable, 0x1003, 1, 0)
		}
		comctl.NewProc("ImageList_Destroy").Call(tableImages)
		tableImages = 0
	}
}
func setCell(i, j int, s string, insert bool) {
	p, _ := syscall.UTF16PtrFromString(s)
	v := lvItem{Mask: 1, Item: int32(i), SubItem: int32(j), Text: p}
	if insert {
		send.Call(connectionTable, 0x104d, 0, uintptr(unsafe.Pointer(&v)))
	} else {
		send.Call(connectionTable, 0x1074, uintptr(i), uintptr(unsafe.Pointer(&v)))
	}
	runtime.KeepAlive(p)
}
func renderConnectionTable(rows []RecentRow, u Update) {
	if connectionTable == 0 {
		return
	}
	now := time.Now()
	cells := make([][6]string, 0, len(rows))
	kinds := make([]int, 0, len(rows))
	active := make([]bool, 0, len(rows))
	for _, r := range rows {
		kind := "代理"
		if r.Kind == Direct {
			kind = "本地"
		}
		down, up := r.Down, r.Up
		if (r.Kind == Proxy && (!u.Connected || u.ProxySeconds <= 0)) || (r.Kind == Direct && !u.LocalReady) {
			down, up = "—", "—"
		}
		cells = append(cells, [6]string{r.Name, r.Target, kind, down, up, seenText(r, now)})
		kinds = append(kinds, r.Kind)
		active = append(active, r.Active)
	}
	if len(cells) == 0 {
		cells = append(cells, [6]string{"暂无连接记录"})
		kinds = append(kinds, -1)
		active = append(active, false)
	}
	// Reuse rows and set only changed cells: no clearing, no resetting column widths or scroll.
	old := displayCells
	displayCells = cells
	displayKinds = kinds
	displayActive = active
	send.Call(connectionTable, 0x000b, 0, 0) // WM_SETREDRAW off
	for i := len(old) - 1; i >= len(cells); i-- {
		send.Call(connectionTable, 0x1008, uintptr(i), 0)
	}
	for i, row := range cells {
		for j, v := range row {
			if i >= len(old) {
				setCell(i, j, v, j == 0)
			} else if old[i][j] != v {
				setCell(i, j, v, false)
			}
		}
	}
	send.Call(connectionTable, 0x000b, 1, 0)
	user.NewProc("InvalidateRect").Call(connectionTable, 0, 0)
}

// Paint helpers use only cached GDI objects; no per-row fonts or brushes.
func fill(dc uintptr, r rect32, b uintptr) {
	if r.R > r.L && r.B > r.T {
		fillRect.Call(dc, uintptr(unsafe.Pointer(&r)), b)
	}
}
func drawLabel(dc uintptr, s string, r rect32, f, c uintptr, align uintptr) {
	if r.R <= r.L || r.B <= r.T || s == "" {
		return
	}
	p, _ := syscall.UTF16PtrFromString(s)
	old, _, _ := selectObject.Call(dc, f)
	setBkMode.Call(dc, 1)
	setTextColor.Call(dc, c)
	drawText.Call(dc, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x20|0x4|0x8000|0x800|align) // single line, center vertically, ellipsis, no prefix
	selectObject.Call(dc, old)
	runtime.KeepAlive(p)
}
func inset(r rect32, x int32) rect32 { r.L += x; r.R -= x; return r }
func headerRect(i int) (rect32, bool) {
	var r rect32
	v, _, _ := send.Call(tableHeader, 0x1207, uintptr(i), uintptr(unsafe.Pointer(&r)))
	return r, v != 0
}

type paintStruct struct {
	DC                 uintptr
	Erase              int32
	Rect               rect32
	Restore, IncUpdate int32
	Reserved           [32]byte
}
type headerLayout struct {
	Rect *rect32
	Pos  *windowPos
}
type windowPos struct {
	Hwnd, After  uintptr
	X, Y, CX, CY int32
	Flags        uint32
}

func headerProc(h uintptr, m uint32, w, l, id, ref uintptr) uintptr {
	switch m {
	case 0x1205: // HDM_LAYOUT: retain native width/position, give header breathing room
		result, _, _ := defSubclass.Call(h, uintptr(m), w, l)
		if result != 0 && l != 0 {
			v := (*headerLayout)(unsafe.Pointer(l))
			if v.Pos != nil && v.Rect != nil {
				delta := int32(34) - v.Pos.CY
				v.Pos.CY = 34
				v.Rect.T += delta
			}
		}
		return result
	case 0x000f: // WM_PAINT
		var p paintStruct
		dc, _, _ := user.NewProc("BeginPaint").Call(h, uintptr(unsafe.Pointer(&p)))
		if dc != 0 {
			paintHeader(h, dc)
		}
		user.NewProc("EndPaint").Call(h, uintptr(unsafe.Pointer(&p)))
		return 0
	case 0x0318: // WM_PRINTCLIENT
		if w != 0 {
			paintHeader(h, w)
		}
		return 0
	case 0x0014:
		return 1 // no classic background erase
	case 0x0082: // WM_NCDESTROY
		removeSubclass.Call(h, headerHook, 1)
		headerInstalled = false
	}
	r, _, _ := defSubclass.Call(h, uintptr(m), w, l)
	if m == 0x0202 || m == 0x0215 { // mouse up / capture lost after resizing a column
		user.NewProc("PostMessageW").Call(mainWindow, 0x8002, 0, 0)
	}
	switch m {
	case 0x0200, 0x0201, 0x0202, 0x0047, 0x120c, 0x1204, 0x1203, 0x1201:
		invalidate.Call(h, 0, 0)
	}
	return r
}
func paintHeader(h, dc uintptr) {
	var client rect32
	getClientRect.Call(h, uintptr(unsafe.Pointer(&client)))
	fill(dc, client, tableHeadBrush)
	for i, title := range columnTitles {
		r, ok := headerRect(i)
		if !ok {
			continue
		}
		r.T = 0
		r.B = client.B
		a := uintptr(0)
		if i == 2 {
			a = 1
		}
		if i == 3 || i == 4 {
			a = 2
		}
		drawLabel(dc, title, inset(r, 10), tableHeaderFont, muted, a)
		if i < len(columnTitles)-1 {
			fill(dc, rect32{r.R - 1, r.T + 10, r.R, r.B - 10}, lineBrush)
		}
	}
	fill(dc, rect32{0, client.B - 1, client.R, client.B}, lineBrush)
}
func drawPill(dc uintptr, r rect32, s string, kind int) {
	if r.R-r.L < 34 {
		drawLabel(dc, s, inset(r, 4), tableSmallFont, muted, 1)
		return
	}
	w := int32(42)
	if r.R-r.L-8 < w {
		w = r.R - r.L - 8
	}
	x := (r.L + r.R - w) / 2
	y := (r.T + r.B - 20) / 2
	b, c := proxyPillBrush, uintptr(orange)
	if kind == Direct {
		b, c = localPillBrush, green
	}
	old, _, _ := selectObject.Call(dc, b)
	pen, _, _ := gdi.NewProc("GetStockObject").Call(8)
	oldPen, _, _ := selectObject.Call(dc, pen)
	gdi.NewProc("RoundRect").Call(dc, uintptr(x), uintptr(y), uintptr(x+w), uintptr(y+20), 8, 8)
	selectObject.Call(dc, old)
	selectObject.Call(dc, oldPen)
	drawLabel(dc, s, rect32{x, y, x + w, y + 20}, tableSmallFont, c, 1)
}
func paintRow(d *lvCustomDraw, i int) bool {
	if !headerInstalled || i < 0 || i >= len(displayCells) {
		return false
	}
	var r rect32
	r.L = 0 // LVIR_BOUNDS
	v, _, _ := send.Call(connectionTable, 0x100e, uintptr(i), uintptr(unsafe.Pointer(&r)))
	if v == 0 {
		return false
	}
	var client rect32
	getClientRect.Call(connectionTable, uintptr(unsafe.Pointer(&client)))
	r.L = 0
	r.R = client.R
	// Use header coordinates mapped to list-view coordinates; preserves horizontal scroll.
	var cells [6]rect32
	for j := range cells {
		c, ok := headerRect(j)
		if !ok {
			return false
		}
		user.NewProc("MapWindowPoints").Call(tableHeader, connectionTable, uintptr(unsafe.Pointer(&c)), 2)
		c.T = r.T
		c.B = r.B
		cells[j] = c
	}
	dc := d.Base.DC
	b := whiteBrush
	if i%2 == 1 {
		b = tableAltBrush
	}
	if d.Base.State&1 != 0 {
		b = tableSelectBrush
	}
	fill(dc, r, b)
	kind := displayKinds[i]
	active := displayActive[i]
	for j, s := range displayCells[i] {
		c := cells[j]
		if j == 2 && (kind == Proxy || kind == Direct) {
			drawPill(dc, c, s, kind)
			continue
		}
		color := uintptr(dark)
		f := font
		a := uintptr(0)
		if j == 1 || j == 5 {
			color = muted
		}
		if j == 5 {
			f = tableSmallFont
		}
		if j == 3 || j == 4 {
			a = 2
			color = muted
			if active && kind == Proxy {
				color = orange
			} else if active && kind == Direct {
				color = green
			}
		}
		drawLabel(dc, s, inset(c, 10), f, color, a)
	}
	fill(dc, rect32{0, r.B - 1, client.R, r.B}, lineBrush)
	return true
}
func tableNotify(l uintptr) (uintptr, bool) {
	if l == 0 || connectionTable == 0 {
		return 0, false
	}
	h := (*nmHeader)(unsafe.Pointer(l))
	if h.Hwnd == tableHeader {
		if h.Code == -307 || h.Code == -327 { // HDN_ENDTRACK A/W
			user.NewProc("PostMessageW").Call(mainWindow, 0x8002, 0, 0)
		}
		return 0, false
	}
	if h.Hwnd != connectionTable || h.Code != -12 {
		return 0, false
	}
	d := (*lvCustomDraw)(unsafe.Pointer(l))
	switch d.Base.Stage {
	case 1:
		return 0x20, true
	case 0x10001:
		if paintRow(d, int(d.Base.Item)) {
			return 4, true
		} // CDRF_SKIPDEFAULT
		return 0, true
	}
	return 0, true
}

// Preserve the user's proportions across repeated size messages. Only take a
// new reference when the user actually changes a column width.
var columnReference, columnApplied [6]int
var columnReferenceReady bool

func fitColumns(width int) {
	if connectionTable == 0 {
		return
	}
	target := width - 4 // visible rows fit; no scrollbar gutter
	var actual [6]int
	for i := range actual {
		v, _, _ := send.Call(connectionTable, 0x101d, uintptr(i), 0)
		actual[i] = int(v)
	}
	if !columnReferenceReady || actual != columnApplied {
		columnReference = actual
		columnReferenceReady = true
	}
	sizes := fitColumnWidths(columnReference, target)
	for i, n := range sizes {
		send.Call(connectionTable, 0x101e, uintptr(i), uintptr(n))
	}
	columnApplied = sizes
	invalidate.Call(connectionTable, 0, 0)
	if tableHeader != 0 {
		invalidate.Call(tableHeader, 0, 0)
	}
}

// Read the native geometry rather than assuming that the image-list height
// equals the final report-row height (Windows may add padding).
func tableGeometry() (height, header, row int) {
	header, row = 34, 32
	if connectionTable == 0 {
		return
	}
	var r rect32
	getClientRect.Call(connectionTable, uintptr(unsafe.Pointer(&r)))
	height = int(r.B - r.T)
	if tableHeader != 0 {
		var h rect32
		if ok, _, _ := getClientRect.Call(tableHeader, uintptr(unsafe.Pointer(&h))); ok != 0 && h.B > h.T {
			header = int(h.B - h.T)
		}
	}
	if len(displayCells) > 0 {
		r = rect32{}
		if ok, _, _ := send.Call(connectionTable, 0x100e, 0, uintptr(unsafe.Pointer(&r))); ok != 0 && r.B > r.T {
			row = int(r.B - r.T)
		}
	}
	return
}
func tableCapacity() int {
	h, header, row := tableGeometry()
	n := visibleRowCount(h, header, row)
	// No native item should be even partially clipped, including while the
	// user is dragging the window and before the next traffic update.
	return n
}
func expandedMinimum() int {
	_, header, row := tableGeometry()
	return max(minExpandedHeight, connectionTableInset+minTableHeight(header, row))
}
