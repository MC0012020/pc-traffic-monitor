package main

// Fit the connection table to the available width while preserving user-selected
// proportions above readable minimums. Rounding residue goes to the address column.
func fitColumnWidths(reference [6]int, target int) [6]int {
 return fitColumnWidthsScaled(reference,target,100)
}
func fitColumnWidthsScaled(reference [6]int,target,scale int)[6]int {
 minimum := [6]int{75,110,52,70,70,78}
 for i:=range minimum{minimum[i]=floatScaled(minimum[i],scale)}
	weights := [6]int{}
	minSum, totalWeight := 0, 0
	for i, v := range reference {
		minSum += minimum[i]
		if v > minimum[i] {
			weights[i] = v - minimum[i]
			totalWeight += weights[i]
		}
	}
	if target < minSum {
		target = minSum
	}
	extra := target - minSum
	out := minimum
	remain := extra
	if totalWeight > 0 {
		for i, w := range weights {
			n := extra * w / totalWeight
			out[i] += n
			remain -= n
		}
	}
	out[1] += remain
	return out
}

// The table uses measured native header/item heights, with a small bottom
// margin so partial rows never force a vertical scrollbar.
const minVisibleRows = 5
const maxRecentRows = 200
const tableBottomMargin = 8

func visibleRowCount(height, header, row int) int {
	if row <= 0 {
		return 0
	}
	n := (height - header - tableBottomMargin) / row
	if n < 0 {
		return 0
	}
	if n > maxRecentRows {
		return maxRecentRows
	}
	return n
}
func minTableHeight(header, row int) int {
	return header + minVisibleRows*row + tableBottomMargin
}

// The capsule is sized from the two measured text extents, not fixed columns.
const headerY, headerH = 10, 29
const connectionTableY = 185
const connectionTableInset = connectionTableY + 13

const floatHeight = 48
const floatSpeedY, floatSpeedH = 4, 24
const floatDayY, floatDayH = 28, 16
const floatPadding = 9
const floatGap = 8

// Resolution scaling is relative to Full HD, independent of screen orientation.
// Cap at 200% to preserve the compact capsule on very large displays.
func floatResolutionScale(width, height int) int {
 if width<=0||height<=0{return 100}
 long,short:=max(width,height),min(width,height)
 return max(100,min(200,min((long*100+960)/1920,(short*100+540)/1080)))
}
func floatScaled(value,percent int) int {return (value*percent+50)/100}
func floatDimensions(left,right int)(width,divider,rightX int){
 return floatDimensionsScaled(left,right,100)
}
func floatDimensionsScaled(left,right,percent int)(width,divider,rightX int){
 left,right=max(1,left),max(1,right)
 padding,gap:=floatScaled(floatPadding,percent),floatScaled(floatGap,percent)
 divider=padding+left+gap
 rightX=divider+floatScaled(1,percent)+gap
 width=rightX+right+padding
 return
}
func dragExceeded(dx, dy int32) bool { return dx > 4 || dx < -4 || dy > 4 || dy < -4 }

// Convert signed offsets with symmetric rounding (icons have negative offsets).
func scaledOffset(v,scale int)int{if v<0{return -floatScaled(-v,scale)};return floatScaled(v,scale)}
func scaleBetween(v,oldScale,newScale int)int{if oldScale<=0{oldScale=100};return (v*newScale+oldScale/2)/oldScale}


// All periodic placement operations preserve Z-order and never activate.
// Only an explicit show may raise a normal window in its existing layer.
func floatPlacementFlags(resized,moved,show bool)uintptr{
 flags:=uintptr(0x0004|0x0010|0x0200)
 if !resized{flags|=1};if !moved{flags|=2};if show{flags=(flags&^0x0004)|0x0040};return flags
}

// One shared initial pin state. Set the extended style at creation rather
// than raising a visible window. Runtime toggles remain explicit user actions.
const defaultWindowTop = true
func windowTopStyle(base uintptr,on bool)uintptr{
 if on{return base|8};return base&^8
}
func windowTopTip(on bool)string{
 if on{return "取消置顶"};return "置顶"
}
