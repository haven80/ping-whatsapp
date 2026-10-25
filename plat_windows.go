//go:build windows

package main

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

func hwndOf(e event.Event) (uintptr, bool) {
	if v, ok := e.(app.Win32ViewEvent); ok {
		return v.HWND, v.HWND != 0
	}
	return 0, false
}

func trayAvailable() bool { return tray.hwnd != 0 }
