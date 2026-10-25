//go:build linux

package main

import (
	"gioui.org/app"
	"gioui.org/io/event"
)

// stesso nome del file .desktop: così il desktop abbina finestre, icona e notifiche
func init() { app.ID = "ping-whatsapp" }

func hwndOf(e event.Event) (uintptr, bool) { return 0, false }

const closeToTray = true // Wayland non permette di nascondere una finestra: la si chiude e poi si ricrea
