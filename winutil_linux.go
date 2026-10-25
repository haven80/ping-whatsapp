//go:build linux

package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

// Integrazione con il desktop Linux (KDE, GNOME…, Wayland o X11) tramite D-Bus e i
// programmi di sistema più comuni. Tutto è facoltativo: se qualcosa manca, la funzione
// corrispondente semplicemente non fa nulla.

func runtimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return d
	}
	return os.TempDir()
}

func cacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	d = filepath.Join(d, "ping-whatsapp")
	os.MkdirAll(d, 0o755)
	return d
}

// iconPath: l'icona dell'app su disco (serve alle notifiche).
var iconPath = sync.OnceValue(func() string {
	p := filepath.Join(cacheDir(), "ping.png")
	os.WriteFile(p, appIconPNG, 0o644)
	return p
})

// ---- istanza unica ----

var (
	showRequests = make(chan struct{}, 1)
	instanceLock *os.File
)

// SingleInstance: se Ping è già aperto gli chiede di mostrarsi e restituisce false.
func SingleInstance() bool {
	lock := filepath.Join(runtimeDir(), "ping-whatsapp.lock")
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return true
	}
	sock := filepath.Join(runtimeDir(), "ping-whatsapp.sock")
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		if c, err := net.DialTimeout("unix", sock, time.Second); err == nil {
			fmt.Fprintln(c, "show")
			c.Close()
		}
		return false
	}
	// il file resta aperto (e bloccato) finché Ping è in esecuzione: va tenuto in una variabile
	// globale, altrimenti il garbage collector lo chiude e il blocco sparisce
	instanceLock = f
	os.Remove(sock)
	if l, err := net.Listen("unix", sock); err == nil {
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				bufio.NewReader(c).ReadString('\n')
				c.Close()
				select {
				case showRequests <- struct{}{}:
				default:
				}
			}
		}()
	}
	return true
}

// ---- icona nell'area di notifica (StatusNotifierItem) ----

var tray struct {
	mu      sync.Mutex
	ok      bool
	end     func()
	onClick func()
}

func trayWatcherPresent() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var has bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has)
	return err == nil && has
}

func StartTray(onOpen, onQuit, onClick func()) {
	tray.mu.Lock()
	tray.onClick = onClick
	tray.mu.Unlock()
	go func() {
		for range showRequests {
			onOpen()
		}
	}()
	if os.Getenv("PING_NO_TRAY") != "" || !trayWatcherPresent() {
		return // niente area di notifica (es. GNOME senza estensione): chiudere la finestra chiude Ping
	}
	start, end := systray.RunWithExternalLoop(func() {
		systray.SetIcon(appIconPNG)
		systray.SetTitle("Ping")
		systray.SetTooltip("Ping")
		open := systray.AddMenuItem("Apri Ping", "")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Esci", "")
		systray.SetOnTapped(onOpen)
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					onOpen()
				case <-quit.ClickedCh:
					onQuit()
				}
			}
		}()
	}, nil)
	start()
	tray.mu.Lock()
	tray.ok, tray.end = true, end
	tray.mu.Unlock()
}

func trayAvailable() bool {
	tray.mu.Lock()
	defer tray.mu.Unlock()
	return tray.ok
}

func RemoveTray() {
	tray.mu.Lock()
	end := tray.end
	tray.ok, tray.end = false, nil
	tray.mu.Unlock()
	if end != nil {
		end()
	}
}

func SetTrayTip(s string) {
	if trayAvailable() {
		systray.SetTooltip(s)
	}
}

// ---- notifiche (org.freedesktop.Notifications) ----

var notif struct {
	once sync.Once
	mu   sync.Mutex
	conn *dbus.Conn
	last uint32
}

func notifyConn() *dbus.Conn {
	notif.once.Do(func() {
		conn, err := dbus.ConnectSessionBus()
		if err != nil {
			return
		}
		notif.conn = conn
		// clic sulla notifica: apre la chat che l'ha generata
		conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.Notifications"), dbus.WithMatchMember("ActionInvoked"))
		ch := make(chan *dbus.Signal, 8)
		conn.Signal(ch)
		go func() {
			for s := range ch {
				if s.Name != "org.freedesktop.Notifications.ActionInvoked" || len(s.Body) < 1 {
					continue
				}
				id, _ := s.Body[0].(uint32)
				notif.mu.Lock()
				mine := id == notif.last
				notif.mu.Unlock()
				tray.mu.Lock()
				click := tray.onClick
				tray.mu.Unlock()
				if mine && click != nil {
					click()
				}
			}
		}()
	})
	return notif.conn
}

func Notify(title, text string) {
	go func() {
		conn := notifyConn()
		if conn == nil {
			return
		}
		notif.mu.Lock()
		replace := notif.last
		notif.mu.Unlock()
		obj := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications")
		hints := map[string]dbus.Variant{"desktop-entry": dbus.MakeVariant("ping-whatsapp")}
		var id uint32
		err := obj.Call("org.freedesktop.Notifications.Notify", 0, "Ping", replace, iconPath(), title, text,
			[]string{"default", "Apri"}, hints, int32(-1)).Store(&id)
		if err == nil {
			notif.mu.Lock()
			notif.last = id
			notif.mu.Unlock()
		}
	}()
}

// ---- finestre: su Linux non ci sono handle nativi, le funzioni non fanno nulla ----

func FlashWindow(hwnd uintptr)                {}
func ShowHWND(hwnd uintptr)                   {}
func HideHWND(hwnd uintptr)                   {}
func IsVisibleHWND(hwnd uintptr) bool         { return true }
func SetDarkTitleBar(hwnd uintptr, dark bool) {}
func GetPlace(hwnd uintptr) (WinPlace, bool)  { return WinPlace{}, false }
func SetPlace(hwnd uintptr, p WinPlace)       {}
func openPath(path string)                    { exec.Command("xdg-open", path).Start() }
func portalObj(c *dbus.Conn) dbus.BusObject {
	return c.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")
}
func hasCmd(name string) bool { _, err := exec.LookPath(name); return err == nil }
func withTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

// SystemDarkMode legge la preferenza chiaro/scuro del desktop (portale XDG).
func SystemDarkMode() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	ctx, cancel := withTimeout(time.Second)
	defer cancel()
	var v dbus.Variant
	if err := portalObj(conn).CallWithContext(ctx, "org.freedesktop.portal.Settings.ReadOne", 0,
		"org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
		// portali più vecchi: Read restituisce una variante dentro una variante
		if err := portalObj(conn).CallWithContext(ctx, "org.freedesktop.portal.Settings.Read", 0,
			"org.freedesktop.appearance", "color-scheme").Store(&v); err != nil {
			return false
		}
		if inner, ok := v.Value().(dbus.Variant); ok {
			v = inner
		}
	}
	n, _ := v.Value().(uint32)
	return n == 1 // 1 = preferisce lo scuro
}

// ---- suono ----

var chimeFile = sync.OnceValue(func() string {
	p := filepath.Join(cacheDir(), "ping-chime.wav")
	os.WriteFile(p, chime, 0o644)
	return p
})

func PlayChime() {
	for _, player := range []string{"pw-play", "paplay", "aplay"} {
		if hasCmd(player) {
			args := []string{chimeFile()}
			if player == "aplay" {
				args = append([]string{"-q"}, args...)
			}
			go exec.Command(player, args...).Run()
			return
		}
	}
}

// ---- scelta dei file ----

// PickFile apre la finestra "Apri" del desktop (portale XDG; in alternativa kdialog o zenity).
// filter ha la forma "Immagini|*.png;*.jpg|Tutti i file|*.*".
func PickFile(owner uintptr, title, filter string) string {
	if p, ok := pickPortal(title, filter); ok {
		return p
	}
	var pats []string
	parts := strings.Split(filter, "|")
	for i := 0; i+1 < len(parts); i += 2 {
		if parts[i+1] != "*.*" {
			pats = append(pats, strings.ReplaceAll(parts[i+1], ";", " "))
		}
	}
	var cmd *exec.Cmd
	kde := strings.Contains(strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP")), "KDE")
	switch {
	case hasCmd("kdialog") && (kde || !hasCmd("zenity")):
		f := ""
		if len(pats) > 0 {
			f = strings.Join(pats, " ")
		}
		cmd = exec.Command("kdialog", "--title", title, "--getopenfilename", homeDir(), f)
	case hasCmd("zenity"):
		args := []string{"--file-selection", "--title=" + title}
		for _, p := range pats {
			args = append(args, "--file-filter="+p)
		}
		cmd = exec.Command("zenity", args...)
	default:
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

type portalFilter struct {
	Name     string
	Patterns []struct {
		Kind    uint32
		Pattern string
	}
}

func pickPortal(title, filter string) (string, bool) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", false
	}
	defer conn.Close()
	token := fmt.Sprintf("ping%d", time.Now().UnixNano())
	sender := strings.ReplaceAll(strings.TrimPrefix(conn.Names()[0], ":"), ".", "_")
	reqPath := dbus.ObjectPath("/org/freedesktop/portal/desktop/request/" + sender + "/" + token)
	// la risposta arriva come segnale: ci si iscrive prima di fare la richiesta
	conn.AddMatchSignal(dbus.WithMatchObjectPath(reqPath), dbus.WithMatchInterface("org.freedesktop.portal.Request"), dbus.WithMatchMember("Response"))
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)

	var filters []portalFilter
	parts := strings.Split(filter, "|")
	for i := 0; i+1 < len(parts); i += 2 {
		f := portalFilter{Name: parts[i]}
		for _, p := range strings.Split(parts[i+1], ";") {
			f.Patterns = append(f.Patterns, struct {
				Kind    uint32
				Pattern string
			}{0, p})
		}
		filters = append(filters, f)
	}
	opts := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant(token),
		"modal":        dbus.MakeVariant(true),
	}
	if len(filters) > 0 {
		opts["filters"] = dbus.MakeVariant(filters)
	}
	ctx, cancel := withTimeout(3 * time.Second)
	defer cancel()
	var handle dbus.ObjectPath
	if err := portalObj(conn).CallWithContext(ctx, "org.freedesktop.portal.FileChooser.OpenFile", 0, "", title, opts).Store(&handle); err != nil {
		return "", false // nessun portale: si prova con kdialog/zenity
	}
	for s := range ch { // l'utente può metterci quanto vuole
		if s.Path != handle && s.Path != reqPath || s.Name != "org.freedesktop.portal.Request.Response" || len(s.Body) < 2 {
			continue
		}
		code, _ := s.Body[0].(uint32)
		res, _ := s.Body[1].(map[string]dbus.Variant)
		if code != 0 {
			return "", true // annullato
		}
		uris, _ := res["uris"].Value().([]string)
		if len(uris) == 0 {
			return "", true
		}
		if u, err := url.Parse(uris[0]); err == nil && u.Scheme == "file" {
			return u.Path, true
		}
		return "", true
	}
	return "", true
}
