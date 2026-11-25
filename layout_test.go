package main

import "testing"

func TestFitColumnWidths(t *testing.T) {
	ref := [6]int{112, 182, 64, 98, 98, 96}
	for _, target := range []int{500, 650, 800, 1900, 400} {
		result := fitColumnWidths(ref, target)
		sum := 0
		min := [6]int{75, 110, 52, 70, 70, 78}
		for i, n := range result {
			if n < min[i] {
				t.Fatal(result)
			}
			sum += n
		}
		want := target
		if want < 455 {
			want = 455
		}
		if sum != want {
			t.Fatalf("target %d got %d", target, sum)
		}
	}
	if got := fitColumnWidths(ref, 650); got != ref {
		t.Fatalf("same size changed: %v", got)
	}
}

func TestVisibleRowCount(t *testing.T) {
	for _, tc := range []struct{ h, header, row, want int }{
		{218, 34, 30, 5}, {218, 34, 32, 5}, {222, 34, 30, 6},
		{362, 34, 32, 10}, {842, 34, 32, 25}, {218, 34, 36, 4},
		{10, 34, 30, 0}, {10000, 34, 30, 200}, {100, 34, 0, 0},
	} {
		if got := visibleRowCount(tc.h, tc.header, tc.row); got != tc.want {
			t.Fatalf("%+v got %d", tc, got)
		}
	}
	for _, row := range []int{30, 32, 36, 48} {
		h := minTableHeight(34, row)
		if visibleRowCount(h, 34, row) != 5 || visibleRowCount(h+row, 34, row) != 6 {
			t.Fatal(row, h)
		}
	}
}

func TestFloatDimensions(t *testing.T) {
	for _, pair := range [][2]int{{32, 32}, {1, 1}, {80, 74}, {-1, 0}} {
		w, d, r := floatDimensions(pair[0], pair[1])
		l, rr := max(1, pair[0]), max(1, pair[1])
		if d != floatPadding+l+floatGap || r != d+1+floatGap || w != r+rr+floatPadding {
			t.Fatal(pair, w, d, r)
		}
	}
	if !dragExceeded(5, 0) || !dragExceeded(0, -5) || dragExceeded(4, -4) {
		t.Fatal("drag threshold")
	}
}
