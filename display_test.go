package main

import "testing"

func TestDisplayTransitions(t *testing.T) {
	tests := []struct {
		from   displayMode
		action displayAction
		tray   bool
		want   displayMode
	}{
		{displayFull, actionClose, true, displayFloat},
		{displayFull, actionMinimize, true, displayBackground},
		{displayFloat, actionOpen, true, displayFull},
		{displayFloat, actionBackground, true, displayBackground},
		{displayBackground, actionOpen, true, displayFull},
		{displayBackground, actionFloat, true, displayFloat},
		{displayFull, actionClose, false, displayFloat},
		{displayFloat, actionBackground, false, displayFloat},
		{displayFull, actionMinimize, false, displayFull},
		{displayFull, actionExit, true, displayExit},
		{displayFloat, actionExit, true, displayExit},
		{displayBackground, actionExit, true, displayExit},
		{displayExit, actionOpen, true, displayExit},
	}
	for _, x := range tests {
		if got := nextDisplay(x.from, x.action, x.tray); got != x.want {
			t.Fatalf("%+v: %v", x, got)
		}
	}
}
