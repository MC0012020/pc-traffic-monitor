package main

type displayMode uint8

const (
	displayFull displayMode = iota
	displayFloat
	displayBackground
	displayExit
)

type displayAction uint8

const (
	actionOpen displayAction = iota
	actionClose
	actionFloat
	actionMinimize
	actionBackground
	actionExit
)

// Pure display transition policy. Background mode requires a working tray so
// that the user can always retrieve the monitor. All visible modes remain
// available even if Explorer temporarily loses the notification icon.
func nextDisplay(current displayMode, action displayAction, trayReady bool) displayMode {
	if current == displayExit {
		return displayExit
	}
	switch action {
	case actionOpen:
		return displayFull
	case actionClose, actionFloat:
		return displayFloat
	case actionMinimize, actionBackground:
		if trayReady {
			return displayBackground
		}
	case actionExit:
		return displayExit
	}
	return current
}
