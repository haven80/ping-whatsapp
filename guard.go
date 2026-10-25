package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

// Gli errori imprevisti vengono scritti in crash.log invece di chiudere il programma.

var (
	crashPath string
	crashMu   sync.Mutex
)

func logHandled(where string, r any) {
	crashMu.Lock()
	defer crashMu.Unlock()
	if crashPath == "" {
		return
	}
	f, err := os.OpenFile(crashPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n--- %s · errore gestito (%s): %v\n%s\n", time.Now().Format("2006-01-02 15:04:05"), where, r, debug.Stack())
}

// guardFrame disegna un fotogramma; se qualcosa va storto lo salta invece di chiudere tutto.
func guardFrame(where string, f func()) {
	defer func() {
		if r := recover(); r != nil {
			logHandled("finestra "+where, r)
		}
	}()
	f()
}

// supervise esegue un lavoro di sottofondo e lo riavvia se va in errore.
func supervise(where string, f func()) {
	for {
		ok := func() (ok bool) {
			defer func() {
				if r := recover(); r != nil {
					logHandled(where, r)
				}
			}()
			f()
			return true
		}()
		if ok {
			return
		}
		time.Sleep(time.Second)
	}
}

// safeGo avvia una goroutine protetta.
func safeGo(where string, f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logHandled(where, r)
			}
		}()
		f()
	}()
}
