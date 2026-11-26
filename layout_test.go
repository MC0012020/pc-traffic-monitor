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

func TestCompactHeaderAndFloatRows(t *testing.T) {
	if headerY+headerH >= 45 || connectionTableInset-connectionTableY != 13 { t.Fatal("header or table spacing") }
	if floatSpeedY+floatSpeedH > floatDayY || floatDayY+floatDayH > floatHeight-4 { t.Fatal("floating rows overlap or clip") }
}

func TestFloatResolutionScaling(t *testing.T){
 for _,tc:=range []struct{w,h,scale,speed,day,height int}{
 {1920,1080,100,17,12,48},{2560,1600,133,23,16,64},{2560,1440,133,23,16,64},
 {1600,2560,133,23,16,64},{3840,2160,200,34,24,96},{1366,768,100,17,12,48},
 {0,0,100,17,12,48},{7680,4320,200,34,24,96},
 }{
  scale:=floatResolutionScale(tc.w,tc.h)
  if scale!=tc.scale||floatScaled(17,scale)!=tc.speed||floatScaled(12,scale)!=tc.day||floatScaled(floatHeight,scale)!=tc.height{t.Fatal(tc,scale)}
  w,d,r:=floatDimensionsScaled(100,90,scale)
  if d!=100+floatScaled(floatPadding,scale)+floatScaled(floatGap,scale)||r!=d+floatScaled(1,scale)+floatScaled(floatGap,scale)||w!=r+90+floatScaled(floatPadding,scale){t.Fatal(tc,w,d,r)}
  if floatScaled(floatSpeedY+floatSpeedH,scale)>floatScaled(floatDayY,scale)||floatScaled(floatDayY+floatDayH,scale)>floatScaled(floatHeight,scale){t.Fatal("row overlap",tc)}
 }
}

func TestMainResolutionScale(t *testing.T){
 for _,scale:=range []int{100,133,200}{
  px:=func(v int)int{return floatScaled(v,scale)}
  if px(56)+px(21)>px(79)||px(79)+px(39)>px(121)||px(144)+px(27)>px(connectionTableY){t.Fatal("overlapping metrics",scale)}
  w:=px(704)-px(32);ref:=[6]int{px(112),px(182),px(64),px(98),px(98),px(96)};cols:=fitColumnWidthsScaled(ref,w-px(4),scale)
  sum:=0;for i,n:=range cols{sum+=n;if n<px(([6]int{75,110,52,70,70,78})[i]){t.Fatal("column below minimum",scale,cols)}};if sum!=w-px(4){t.Fatal("column total",scale,sum,w)}
  if scaledOffset(-8,scale)!=-px(8)||scaleBetween(704,100,scale)!=px(704){t.Fatal("scale arithmetic",scale)}
 }
}


func TestFloatPlacementNoActivation(t *testing.T){
 for _,resize:=range []bool{false,true}{for _,move:=range []bool{false,true}{
  flags:=floatPlacementFlags(resize,move,false)
  if flags&0x0014!=0x0014||flags&0x0040!=0{t.Fatalf("periodic placement must preserve Z-order and focus: %x",flags)}
  if (flags&1==0)!=resize||(flags&2==0)!=move{t.Fatal("unexpected geometry flags",flags)}
  show:=floatPlacementFlags(resize,move,true);if show&0x0050!=0x0050{t.Fatal("explicit show activates",show)}
 }}
}
