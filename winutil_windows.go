//go:build windows

package main

import (
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	winmm    = windows.NewLazySystemDLL("winmm.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	comdlg32 = windows.NewLazySystemDLL("comdlg32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassExW    = user32.NewProc("RegisterClassExW")
	pCreateWindowExW     = user32.NewProc("CreateWindowExW")
	pDefWindowProcW      = user32.NewProc("DefWindowProcW")
	pGetMessageW         = user32.NewProc("GetMessageW")
	pTranslateMessage    = user32.NewProc("TranslateMessage")
	pDispatchMessageW    = user32.NewProc("DispatchMessageW")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pShowWindow          = user32.NewProc("ShowWindow")
	pIsWindowVisible     = user32.NewProc("IsWindowVisible")
	pIsIconic            = user32.NewProc("IsIconic")
	pFlashWindowEx       = user32.NewProc("FlashWindowEx")
	pLoadImageW          = user32.NewProc("LoadImageW")
	pGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	pPostMessageW        = user32.NewProc("PostMessageW")
	pFindWindowW         = user32.NewProc("FindWindowW")
	pShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	pPlaySoundW          = winmm.NewProc("PlaySoundW")
	pDwmSetWindowAttr    = dwmapi.NewProc("DwmSetWindowAttribute")
	pGetOpenFileNameW    = comdlg32.NewProc("GetOpenFileNameW")
	pCreateMutexW        = kernel32.NewProc("CreateMutexW")
	pGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmApp          = 0x8000
	wmTray         = wmApp + 1
	wmShowMain     = wmApp + 2
	wmDoNotify     = wmApp + 3
	wmUpdateTip    = wmApp + 4
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmLButtonDbl   = 0x0203
	ninBalloonUser = 0x0405
	trayClass      = "PingTrayWindow"
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type winMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      struct{ X, Y int32 }
}

var tray struct {
	hwnd    uintptr
	nid     notifyIconData
	onOpen  func()
	onQuit  func()
	onClick func() // clic sulla notifica
	pending struct{ title, text string }
	tip     string
}

func setText(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

func loadAppIcon(size int) uintptr {
	h, _, _ := pGetModuleHandleW.Call(0)
	for _, id := range []uintptr{32512, 1} {
		ic, _, _ := pLoadImageW.Call(h, id, 1, uintptr(size), uintptr(size), 0)
		if ic != 0 {
			return ic
		}
	}
	return 0
}

// SingleInstance restituisce false se Ping è già aperto (e lo porta in primo piano).
func SingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString(`Local\PingWhatsAppLite`)
	pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		cls, _ := syscall.UTF16PtrFromString(trayClass)
		if h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0); h != 0 {
			pPostMessageW.Call(h, wmShowMain, 0, 0)
		}
		return false
	}
	return true
}

// StartTray crea l'icona nell'area di notifica su un thread dedicato.
func StartTray(onOpen, onQuit, onClick func()) {
	tray.onOpen, tray.onQuit, tray.onClick = onOpen, onQuit, onClick
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		inst, _, _ := pGetModuleHandleW.Call(0)
		cls, _ := syscall.UTF16PtrFromString(trayClass)
		wc := wndClassEx{
			WndProc:   syscall.NewCallback(trayProc),
			Instance:  inst,
			ClassName: cls,
		}
		wc.Size = uint32(unsafe.Sizeof(wc))
		pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		hwnd, _, _ := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(cls)), 0, 0, 0, 0, 0, 0, 0, 0, inst, 0)
		tray.hwnd = hwnd

		sm, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
		n := &tray.nid
		n.CbSize = uint32(unsafe.Sizeof(*n))
		n.HWnd = hwnd
		n.UID = 1
		n.UFlags = 0x1 | 0x2 | 0x4 // NIF_MESSAGE | NIF_ICON | NIF_TIP
		n.UCallbackMessage = wmTray
		n.HIcon = loadAppIcon(int(sm))
		setText(n.SzTip[:], "Ping")
		pShellNotifyIconW.Call(0, uintptr(unsafe.Pointer(n))) // NIM_ADD
		close(ready)

		var m winMsg
		for {
			r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	<-ready
}

func trayProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case wmTray:
		switch uint32(lp) & 0xffff {
		case wmLButtonUp, wmLButtonDbl:
			if tray.onOpen != nil {
				go tray.onOpen()
			}
		case wmRButtonUp:
			showTrayMenu(hwnd)
		case ninBalloonUser:
			if tray.onClick != nil {
				go tray.onClick()
			}
		}
		return 0
	case wmShowMain:
		if tray.onOpen != nil {
			go tray.onOpen()
		}
		return 0
	case wmDoNotify:
		n := tray.nid
		n.UFlags = 0x10 // NIF_INFO
		setText(n.SzInfoTitle[:], tray.pending.title)
		setText(n.SzInfo[:], tray.pending.text)
		n.DwInfoFlags = 0x4 | 0x10                             // NIIF_USER | NIIF_NOSOUND
		pShellNotifyIconW.Call(1, uintptr(unsafe.Pointer(&n))) // NIM_MODIFY
		return 0
	case wmUpdateTip:
		n := tray.nid
		n.UFlags = 0x4
		setText(n.SzTip[:], tray.tip)
		pShellNotifyIconW.Call(1, uintptr(unsafe.Pointer(&n)))
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func showTrayMenu(hwnd uintptr) {
	menu, _, _ := pCreatePopupMenu.Call()
	open, _ := syscall.UTF16PtrFromString("Apri Ping")
	quit, _ := syscall.UTF16PtrFromString("Esci")
	pAppendMenuW.Call(menu, 0, 1, uintptr(unsafe.Pointer(open)))
	pAppendMenuW.Call(menu, 0x800, 0, 0) // separatore
	pAppendMenuW.Call(menu, 0, 2, uintptr(unsafe.Pointer(quit)))
	var pt struct{ X, Y int32 }
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(menu, 0x100|0x20, uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0) // TPM_RETURNCMD|TPM_BOTTOMALIGN
	pDestroyMenu.Call(menu)
	switch cmd {
	case 1:
		if tray.onOpen != nil {
			go tray.onOpen()
		}
	case 2:
		if tray.onQuit != nil {
			go tray.onQuit()
		}
	}
}

func RemoveTray() {
	if tray.hwnd != 0 {
		pShellNotifyIconW.Call(2, uintptr(unsafe.Pointer(&tray.nid))) // NIM_DELETE
	}
}

// Notify mostra un avviso di Windows dall'icona nell'area di notifica.
func Notify(title, text string) {
	if tray.hwnd == 0 {
		return
	}
	tray.pending.title, tray.pending.text = title, text
	pPostMessageW.Call(tray.hwnd, wmDoNotify, 0, 0)
}

func SetTrayTip(s string) {
	if tray.hwnd == 0 || tray.tip == s {
		return
	}
	tray.tip = s
	pPostMessageW.Call(tray.hwnd, wmUpdateTip, 0, 0)
}

// ---- finestre ----

func FlashWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	fi := struct {
		Size    uint32
		HWnd    uintptr
		Flags   uint32
		Count   uint32
		Timeout uint32
	}{HWnd: hwnd, Flags: 0x2 | 0xC} // FLASHW_TRAY | FLASHW_TIMERNOFG
	fi.Size = uint32(unsafe.Sizeof(fi))
	pFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
}

func ShowHWND(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	if ic, _, _ := pIsIconic.Call(hwnd); ic != 0 {
		pShowWindow.Call(hwnd, 9) // SW_RESTORE
	} else {
		pShowWindow.Call(hwnd, 5) // SW_SHOW
	}
	pSetForegroundWindow.Call(hwnd)
}

func HideHWND(hwnd uintptr) {
	if hwnd != 0 {
		pShowWindow.Call(hwnd, 0)
	}
}

func IsVisibleHWND(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	v, _, _ := pIsWindowVisible.Call(hwnd)
	return v != 0
}

func SetDarkTitleBar(hwnd uintptr, dark bool) {
	if hwnd == 0 {
		return
	}
	v := int32(0)
	if dark {
		v = 1
	}
	pDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4)
}

func SystemDarkMode() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

func openPath(path string) {
	windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(path), nil, nil, 1)
}

var (
	ole32           = windows.NewLazySystemDLL("ole32.dll")
	pCoInitializeEx = ole32.NewProc("CoInitializeEx")
	pCoUninitialize = ole32.NewProc("CoUninitialize")
)

// PickFile apre la finestra "Apri" di Windows. filter è nel formato "Descrizione|*.a;*.b|Altro|*.*".
// Va chiamata fuori dal thread della UI: la finestra di dialogo vuole un thread COM tutto suo.
func PickFile(owner uintptr, title, filter string) (path string) {
	defer func() {
		if r := recover(); r != nil {
			path = ""
		}
	}()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE: senza questo la finestra "Apri" può bloccarsi o chiudere il programma
	hr, _, _ := pCoInitializeEx.Call(0, 0x2|0x4)
	if int32(hr) >= 0 {
		defer pCoUninitialize.Call()
	}
	buf := make([]uint16, 4096)
	// il filtro contiene separatori nulli: StringToUTF16 andrebbe in panic (era la causa del crash)
	f := utf16.Encode([]rune(strings.ReplaceAll(filter, "|", "\x00") + "\x00\x00"))
	t := windows.StringToUTF16(title)
	ofn := struct {
		StructSize    uint32
		Owner         uintptr
		Instance      uintptr
		Filter        *uint16
		CustomFilter  *uint16
		MaxCustFilter uint32
		FilterIndex   uint32
		File          *uint16
		MaxFile       uint32
		FileTitle     *uint16
		MaxFileTitle  uint32
		InitialDir    *uint16
		Title         *uint16
		Flags         uint32
		FileOffset    uint16
		FileExtension uint16
		DefExt        *uint16
		CustData      uintptr
		FnHook        uintptr
		TemplateName  *uint16
		PvReserved    uintptr
		DwReserved    uint32
		FlagsEx       uint32
	}{
		Owner: owner, Filter: &f[0], FilterIndex: 1, File: &buf[0], MaxFile: uint32(len(buf)),
		Title: &t[0], Flags: 0x1000 | 0x800 | 0x80000 | 0x8, // FILEMUSTEXIST|PATHMUSTEXIST|EXPLORER|NOCHANGEDIR
	}
	ofn.StructSize = uint32(unsafe.Sizeof(ofn))
	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	runtime.KeepAlive(f)
	runtime.KeepAlive(t)
	runtime.KeepAlive(buf)
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// ---- suono ----

func PlayChime() {
	// SND_MEMORY | SND_ASYNC | SND_NODEFAULT
	pPlaySoundW.Call(uintptr(unsafe.Pointer(&chime[0])), 0, 0x4|0x1|0x2)
}

// ---- posizione delle finestre ----

var (
	pGetWindowPlacement = user32.NewProc("GetWindowPlacement")
	pSetWindowPlacement = user32.NewProc("SetWindowPlacement")
	pMonitorFromRect    = user32.NewProc("MonitorFromRect")
)

type winPlacement struct {
	Length, Flags, ShowCmd uint32
	MinX, MinY, MaxX, MaxY int32
	L, T, R, B             int32
}

// GetPlace legge posizione e dimensioni (non massimizzate) della finestra.
// Non invia messaggi alla finestra: si può chiamare anche dal suo giro di eventi.
func GetPlace(hwnd uintptr) (WinPlace, bool) {
	if hwnd == 0 {
		return WinPlace{}, false
	}
	var wp winPlacement
	wp.Length = uint32(unsafe.Sizeof(wp))
	if r, _, _ := pGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp))); r == 0 {
		return WinPlace{}, false
	}
	p := WinPlace{X: int(wp.L), Y: int(wp.T), W: int(wp.R - wp.L), H: int(wp.B - wp.T), Max: wp.ShowCmd == 3}
	return p, p.W > 0 && p.H > 0
}

// SetPlace riporta la finestra dove era. Se quello schermo non c'è più non fa nulla.
// Invia messaggi alla finestra: va chiamata fuori dal suo giro di eventi.
func SetPlace(hwnd uintptr, p WinPlace) {
	if hwnd == 0 || p.W < 100 || p.H < 100 {
		return
	}
	rc := [4]int32{int32(p.X), int32(p.Y), int32(p.X + p.W), int32(p.Y + p.H)}
	if mon, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(&rc)), 0); mon == 0 { // MONITOR_DEFAULTTONULL
		return
	}
	wp := winPlacement{ShowCmd: 1, L: rc[0], T: rc[1], R: rc[2], B: rc[3]} // SW_SHOWNORMAL
	if p.Max {
		wp.ShowCmd = 3
	}
	wp.Length = uint32(unsafe.Sizeof(wp))
	pSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&wp)))
}
