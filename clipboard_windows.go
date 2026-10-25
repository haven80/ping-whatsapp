//go:build windows

package main

import (
	"bytes"
	"errors"
	"image"
	_ "image/png"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	pOpenClipboard              = user32.NewProc("OpenClipboard")
	pCloseClipboard             = user32.NewProc("CloseClipboard")
	pIsClipboardFormatAvailable = user32.NewProc("IsClipboardFormatAvailable")
	pGetClipboardData           = user32.NewProc("GetClipboardData")
	pRegisterClipboardFormatW   = user32.NewProc("RegisterClipboardFormatW")
	pGlobalLock                 = kernel32.NewProc("GlobalLock")
	pGlobalUnlock               = kernel32.NewProc("GlobalUnlock")
	pGlobalSize                 = kernel32.NewProc("GlobalSize")
	pDragQueryFileW             = shell32.NewProc("DragQueryFileW")
)

const (
	cfUnicodeText = 13
	cfHDrop       = 15
	cfDIB         = 8
	cfDIBV5       = 17
)

// ClipboardContent è ciò che si trova negli appunti al momento dell'incolla.
type ClipboardContent struct {
	Image image.Image // un'immagine copiata (screenshot, da browser, Paint…)
	Files []string    // file copiati in Esplora risorse
	Text  string
}

func globalBytes(h uintptr) []byte {
	if h == 0 {
		return nil
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer pGlobalUnlock.Call(h)
	n, _, _ := pGlobalSize.Call(h)
	src := unsafe.Slice((*byte)(unsafe.Pointer(p)), n)
	out := make([]byte, n)
	copy(out, src)
	return out
}

func available(f uintptr) bool {
	r, _, _ := pIsClipboardFormatAvailable.Call(f)
	return r != 0
}

// ReadClipboard legge gli appunti: prima file, poi immagini (PNG o bitmap), infine testo.
func ReadClipboard(owner uintptr) (c ClipboardContent, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errors.New("appunti non leggibili")
		}
	}()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ok := false
	for i := 0; i < 5 && !ok; i++ { // un altro programma può tenerli aperti per un attimo
		r, _, _ := pOpenClipboard.Call(0)
		ok = r != 0
		if !ok {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if !ok {
		return c, errors.New("appunti occupati")
	}
	defer pCloseClipboard.Call()

	if available(cfHDrop) {
		h, _, _ := pGetClipboardData.Call(cfHDrop)
		if h != 0 {
			n, _, _ := pDragQueryFileW.Call(h, 0xFFFFFFFF, 0, 0)
			for i := uintptr(0); i < n && i < 20; i++ {
				buf := make([]uint16, 1024)
				pDragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
				c.Files = append(c.Files, syscall.UTF16ToString(buf))
			}
			if len(c.Files) > 0 {
				return c, nil
			}
		}
	}
	// formato "PNG" (browser, Strumento di cattura): conserva la trasparenza
	name, _ := syscall.UTF16PtrFromString("PNG")
	if f, _, _ := pRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(name))); f != 0 && available(f) {
		h, _, _ := pGetClipboardData.Call(f)
		if data := globalBytes(h); len(data) > 8 {
			if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
				c.Image = img
				return c, nil
			}
		}
	}
	for _, f := range []uintptr{cfDIBV5, cfDIB} {
		if !available(f) {
			continue
		}
		h, _, _ := pGetClipboardData.Call(f)
		if img, err := decodeDIB(globalBytes(h)); err == nil {
			c.Image = img
			return c, nil
		}
	}
	if available(cfUnicodeText) {
		h, _, _ := pGetClipboardData.Call(cfUnicodeText)
		if data := globalBytes(h); len(data) >= 2 {
			u := unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])), len(data)/2)
			c.Text = syscall.UTF16ToString(u)
		}
	}
	return c, nil
}
