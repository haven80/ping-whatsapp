//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"
)

// Trascinamento di file da Esplora risorse: Gio non lo gestisce su Windows, quindi
// la finestra viene "sottoclassata" per ricevere il messaggio WM_DROPFILES.

var (
	pDragAcceptFiles  = shell32.NewProc("DragAcceptFiles")
	pDragFinish       = shell32.NewProc("DragFinish")
	pSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	pGetWindowLongPtr = user32.NewProc("GetWindowLongPtrW")
	pCallWindowProcW  = user32.NewProc("CallWindowProcW")

	dropMu       sync.Mutex
	dropTargets  = map[uintptr]dropTarget{}
	dropProcOnce sync.Once
	dropProcPtr  uintptr
)

type dropTarget struct {
	old uintptr
	fn  func([]string)
}

const (
	wmDropFiles = 0x0233
	wmNCDestroy = 0x0082
	gwlpWndProc = ^uintptr(3) // -4
)

// EnableFileDrop fa sì che la finestra accetti file trascinati; fn riceve i percorsi.
// fn viene chiamata sul thread della finestra: deve solo registrare i file e chiedere un ridisegno.
//
// Attenzione: nessuna chiamata a Windows va fatta tenendo dropMu, perché Windows può inviare
// subito messaggi alla finestra, che passano da dropProc e chiedono lo stesso lucchetto.
func EnableFileDrop(hwnd uintptr, fn func([]string)) {
	if hwnd == 0 {
		return
	}
	dropProcOnce.Do(func() { dropProcPtr = syscall.NewCallback(dropProc) })

	dropMu.Lock()
	if t, ok := dropTargets[hwnd]; ok {
		t.fn = fn
		dropTargets[hwnd] = t
		dropMu.Unlock()
		return
	}
	dropMu.Unlock()

	// 1) registro la procedura originale PRIMA di installare la mia
	old, _, _ := pGetWindowLongPtr.Call(hwnd, gwlpWndProc)
	if old == 0 {
		return
	}
	dropMu.Lock()
	dropTargets[hwnd] = dropTarget{old: old, fn: fn}
	dropMu.Unlock()
	// 2) installo la mia (a lucchetto aperto)
	if prev, _, _ := pSetWindowLongPtr.Call(hwnd, gwlpWndProc, dropProcPtr); prev == 0 {
		dropMu.Lock()
		delete(dropTargets, hwnd)
		dropMu.Unlock()
		return
	}
	// 3) attivo la ricezione dei file (manda messaggi alla finestra: lucchetto aperto)
	pDragAcceptFiles.Call(hwnd, 1)
}

func dropProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	dropMu.Lock()
	t, ok := dropTargets[hwnd]
	dropMu.Unlock()
	if !ok {
		// non dovrebbe succedere: in ogni caso il messaggio non va mai perso
		r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
		return r
	}
	if msg == wmDropFiles {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logHandled("file trascinati", r)
				}
			}()
			n, _, _ := pDragQueryFileW.Call(wp, 0xFFFFFFFF, 0, 0)
			var paths []string
			for i := uintptr(0); i < n && i < 20; i++ {
				buf := make([]uint16, 1024)
				pDragQueryFileW.Call(wp, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
				paths = append(paths, syscall.UTF16ToString(buf))
			}
			pDragFinish.Call(wp)
			if len(paths) > 0 && t.fn != nil {
				t.fn(paths)
			}
		}()
		return 0
	}
	r, _, _ := pCallWindowProcW.Call(t.old, hwnd, uintptr(msg), wp, lp)
	if msg == wmNCDestroy {
		dropMu.Lock()
		delete(dropTargets, hwnd)
		dropMu.Unlock()
	}
	return r
}
