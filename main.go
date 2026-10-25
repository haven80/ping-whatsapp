package main

import (
	"flag"
	"os"
	"path/filepath"
	"runtime/debug"

	"gioui.org/app"
)

func main() {
	demo := flag.Bool("demo", false, "mostra dati di esempio senza collegarsi")
	flag.Parse()

	// tiene basso il consumo di memoria: il GC lavora un po' di più, la RAM resta contenuta
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(120 << 20)

	if !SingleInstance() {
		return
	}

	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	dir := filepath.Join(base, "Ping")
	if *demo {
		dir = filepath.Join(os.TempDir(), "ping-demo")
		os.RemoveAll(dir)
	}
	os.MkdirAll(dir, 0o755)

	// in caso di crash l'errore finisce in crash.log (non c'è una console)
	crashPath = filepath.Join(dir, "crash.log")
	if f, err := os.OpenFile(filepath.Join(dir, "crash.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		debug.SetCrashOutput(f, debug.CrashOptions{})
	}

	be, err := NewBackend(dir)
	if err != nil {
		panic(err)
	}
	ui := NewUI(be, dir)
	StartTray(ui.ShowMain, ui.Quit, ui.OpenNotified)
	ui.RunMain()
	if *demo {
		go fillDemo(be, ui)
	} else {
		go be.Start()
	}
	app.Main()
}
