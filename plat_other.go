//go:build !windows

package main

import "gioui.org/io/event"

func hwndOf(e event.Event) (uintptr, bool) { return 0, false }
func trayAvailable() bool                  { return false }
