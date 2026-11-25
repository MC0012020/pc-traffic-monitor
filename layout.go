package main

// Fit the connection table to the available width while preserving user-selected
// proportions above readable minimums. Rounding residue goes to the address column.
func fitColumnWidths(reference [6]int, target int) [6]int {
	minimum := [6]int{75, 110, 52, 70, 70, 78}
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
const floatHeight = 52
const floatPadding = 9
const floatGap = 8

func floatDimensions(left, right int) (width, divider, rightX int) {
	left, right = max(1, left), max(1, right)
	divider = floatPadding + left + floatGap
	rightX = divider + 1 + floatGap
	width = rightX + right + floatPadding
	return
}
func dragExceeded(dx, dy int32) bool { return dx > 4 || dx < -4 || dy > 4 || dy < -4 }
