package main

import (
	"encoding/json"
	"fmt"
	"gioui.org/io/event"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	qrcode "github.com/skip2/go-qrcode"
)

type Settings struct {
	ShowPhotos   bool     `json:"show_photos"`
	Sound        bool     `json:"sound"`
	Notify       bool     `json:"notify"`
	Theme        string   `json:"theme"` // "auto", "light" o "dark"
	Recent       []string `json:"recent_emoji"`
	ChatPhotos   bool     `json:"chat_photos"` // foto del mittente accanto ai messaggi
	KlipyKey     string   `json:"klipy_key"`
	CustomerID   string   `json:"customer_id"`
	Single       bool     `json:"single_window"` // chat e lista nella stessa finestra
	ChatLeft     bool     `json:"chat_left"`     // finestra unica: chat a sinistra della lista
	UnreadTop    bool     `json:"unread_first"`  // chat non lette in cima (sotto quelle fissate)
	Color        string   `json:"color"`         // tema di colore (vedi themes.go)
	ShowDeleted  bool     `json:"show_deleted"`  // mostra i messaggi eliminati da chi li ha scritti
	CheckUpdates bool     `json:"check_updates"` // avvisa quando c'è una nuova versione
	SkipVersion  string   `json:"skip_version"`  // versione di cui l'utente ha chiuso l'avviso

	Places map[string]WinPlace `json:"places,omitempty"` // posizione delle finestre (letta solo con place/savePlace)
}

// WinPlace: posizione e dimensioni di una finestra sullo schermo, in pixel.
type WinPlace struct {
	X, Y, W, H int
	Max        bool
}

func (u *UI) place(key string) (WinPlace, bool) {
	u.setMu.Lock()
	defer u.setMu.Unlock()
	p, ok := u.settings.Places[key]
	return p, ok
}

// rememberPlace salva dove si trova adesso la finestra, per riaprirla lì.
func (u *UI) rememberPlace(key string, hwnd uintptr) {
	if p, ok := GetPlace(hwnd); ok {
		u.storePlace(key, p)
	}
}

// storePlace salva una posizione già letta (dal thread della finestra).
func (u *UI) storePlace(key string, p WinPlace) {
	u.setMu.Lock()
	if u.settings.Places == nil {
		u.settings.Places = map[string]WinPlace{}
	}
	same := u.settings.Places[key] == p
	u.settings.Places[key] = p
	u.setMu.Unlock()
	if !same {
		u.saveSettings()
	}
}

// restorePlace riporta la finestra dove l'utente l'aveva lasciata (fuori dal suo giro di eventi).
func (u *UI) restorePlace(key string, hwnd uintptr) {
	if p, ok := u.place(key); ok {
		SetPlace(hwnd, p)
	}
}

// placeKeyNow: come placeKey, ma sicura da chiamare da altri goroutine.
func (m *mainWin) placeKeyNow() string {
	if m.u.Settings().Single {
		return "main-single"
	}
	return "main"
}

func (m *mainWin) placeKey() string {
	if m.single {
		return "main-single"
	}
	return "main"
}

type UI struct {
	be       *Backend
	setMu    sync.Mutex // protegge pal e settings (lette da più finestre)
	pal      Pal
	dir      string
	settings Settings

	main *mainWin

	mu        sync.Mutex
	chats     map[string]*chatWin
	notifyJID string
	idleTimer *time.Timer
}

func NewUI(be *Backend, dir string) *UI {
	u := &UI{be: be, dir: dir, chats: map[string]*chatWin{}}
	u.settings = Settings{ShowPhotos: true, Sound: true, Notify: true, Theme: "auto", CheckUpdates: true}
	if b, err := os.ReadFile(filepath.Join(dir, "settings.json")); err == nil {
		json.Unmarshal(b, &u.settings)
	}
	if u.settings.CustomerID == "" { // identificativo anonimo richiesto da KLIPY
		u.setMu.Lock()
		u.settings.CustomerID = fmt.Sprintf("ping-%x", time.Now().UnixNano())
		u.setMu.Unlock()
		u.saveSettings()
	}
	if t := os.Getenv("PING_THEME"); t != "" {
		u.setMu.Lock()
		u.settings.Theme = t
		u.setMu.Unlock()
	}
	u.applyTheme()

	be.OnChange = u.invalidateAll
	be.OnIncoming = u.incoming
	be.IsChatOpen = u.isChatFocused
	return u
}

// Pal restituisce la palette corrente (sicuro da qualsiasi finestra).
func (u *UI) Pal() Pal {
	u.setMu.Lock()
	defer u.setMu.Unlock()
	return u.pal
}

// Settings restituisce una copia delle impostazioni.
func (u *UI) Settings() Settings {
	u.setMu.Lock()
	defer u.setMu.Unlock()
	s := u.settings
	s.Recent = append([]string(nil), s.Recent...)
	s.Places = nil // si legge solo con place()
	return s
}

// Update modifica le impostazioni in modo sicuro e le salva.
func (u *UI) Update(f func(s *Settings)) {
	u.setMu.Lock()
	f(&u.settings)
	u.setMu.Unlock()
	u.saveSettings()
}

func (u *UI) saveSettings() {
	u.setMu.Lock()
	b, _ := json.MarshalIndent(u.settings, "", "  ")
	u.setMu.Unlock()
	os.WriteFile(filepath.Join(u.dir, "settings.json"), b, 0o644)
}

func (u *UI) invalidateAll() {
	if u.main != nil {
		u.main.w.Invalidate()
	}
	u.mu.Lock()
	for _, c := range u.chats {
		c.w.Invalidate()
	}
	u.mu.Unlock()
	// suggerimento sull'icona nell'area di notifica
	cs, _ := u.be.Snapshot()
	n := 0
	for _, c := range cs {
		if c.Unread > 0 {
			n++
		}
	}
	switch n {
	case 0:
		SetTrayTip("Ping")
	case 1:
		SetTrayTip("Ping · 1 chat da leggere")
	default:
		SetTrayTip("Ping · " + strconv.Itoa(n) + " chat da leggere")
	}
}

func (u *UI) isChatFocused(jid string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	c := u.chats[jid]
	if c != nil && c.focused {
		return true
	}
	return u.main != nil && u.main.focused && u.main.embed != nil && u.main.embed.jid == jid
}

func (u *UI) anyFocused() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.main != nil && u.main.focused {
		return true
	}
	for _, c := range u.chats {
		if c.focused {
			return true
		}
	}
	return false
}

// focusChanged informa WhatsApp quando l'utente è davanti al client (con un breve ritardo
// per non segnalare "inattivo" passando da una finestra all'altra).
func (u *UI) focusChanged() {
	u.mu.Lock()
	if u.idleTimer != nil {
		u.idleTimer.Stop()
	}
	u.mu.Unlock()
	if u.anyFocused() {
		u.be.SetActive(true)
		return
	}
	u.mu.Lock()
	u.idleTimer = time.AfterFunc(20*time.Second, func() {
		if !u.anyFocused() {
			u.be.SetActive(false)
		}
	})
	u.mu.Unlock()
}

func (u *UI) incoming(c Chat, m *Message) {
	if u.isChatFocused(c.JID) {
		return
	}
	set := u.Settings()
	if set.Sound {
		PlayChime()
	}
	u.mu.Lock()
	cw := u.chats[c.JID]
	u.notifyJID = c.JID
	u.mu.Unlock()
	if cw != nil {
		FlashWindow(cw.hwnd)
	} else if u.main != nil {
		FlashWindow(u.main.hwnd)
	}
	if set.Notify && !u.anyFocused() {
		title := c.Name
		if c.IsGroup && m.SenderName != "" {
			title = m.SenderName + " · " + c.Name
		}
		Notify(title, previewOf(m.Kind, m.Text, m.FileName))
	}
}

func (u *UI) ShowMain() {
	if m := u.main; m != nil && m.closed.Load() {
		select {
		case m.reopen <- struct{}{}:
		default:
		}
		return
	}
	if u.main != nil {
		h := u.main.hwnd
		go ShowHWND(h)
		u.main.w.Perform(system.ActionRaise)
	}
}

func (u *UI) OpenNotified() {
	u.mu.Lock()
	jid := u.notifyJID
	u.mu.Unlock()
	if jid != "" {
		u.OpenChat(jid)
	} else {
		u.ShowMain()
	}
}

// OpenChatWith apre la chat privata con un partecipante (anche se non c'è ancora una conversazione).
func (u *UI) OpenChatWith(jid, name string) {
	jid = u.be.PrivateJID(jid, name)
	if jid != "" {
		u.OpenChat(jid)
	}
}

// OpenProfile apre la chat e ci mostra la scheda del contatto o del gruppo.
func (u *UI) OpenProfile(jid string) {
	u.OpenChat(jid)
	u.mu.Lock()
	cw := u.chats[jid]
	if m := u.main; m != nil && m.embed != nil && m.embed.jid == jid && u.Settings().Single {
		cw = m.embed
	}
	u.mu.Unlock()
	if cw != nil {
		cw.RequestProfile()
	}
}

func (u *UI) OpenChat(jid string) {
	if m := u.main; m != nil && u.Settings().Single {
		u.mu.Lock()
		if m.embed == nil || m.embed.jid != jid {
			m.embed = newEmbeddedChat(u, m, jid)
		}
		focused := m.focused
		u.mu.Unlock()
		if focused {
			go u.be.MarkRead(jid)
		}
		u.ShowMain()
		m.w.Invalidate()
		return
	}
	u.mu.Lock()
	cw := u.chats[jid]
	u.mu.Unlock()
	if cw != nil {
		h := cw.hwnd
		go ShowHWND(h) // mai chiamate sincrone verso un'altra finestra dal disegno: si bloccherebbero a vicenda
		cw.w.Perform(system.ActionRaise)
		return
	}
	cw = newChatWin(u, jid)
	u.mu.Lock()
	u.chats[jid] = cw
	u.mu.Unlock()
	go cw.run()
}

// ================= finestra principale =================

type mainWin struct {
	u       *UI
	th      *material.Theme
	pal     Pal
	w       *app.Window
	hwnd    uintptr
	focused bool

	search      widget.Editor
	list        widget.List
	rows        map[string]*widget.Clickable
	avBtns      map[string]*widget.Clickable // foto nelle righe: aprono la scheda
	rowTags     map[string]*rowTagT          // aree per il tasto destro sulle righe
	menuFor     string                       // chat con il menu del tasto destro aperto
	menuErr     string
	menuPin     widget.Clickable
	menuRead    widget.Clickable
	menuOpen    widget.Clickable
	updOpen     widget.Clickable // avviso di nuova versione
	updClose    widget.Clickable
	tabChats    widget.Clickable
	tabBook     widget.Clickable
	tab         int
	btnPhotos   widget.Clickable
	btnSound    widget.Clickable
	btnLogout   widget.Clickable
	btnRetry    widget.Clickable
	btnArchived widget.Clickable
	showArch    bool
	logoutAsk   time.Time
	btnOptions  widget.Clickable
	showOptions bool
	opt         optionsState

	qrText string
	qrOp   paint.ImageOp
	qrSize int

	embed  *chatWin // finestra unica: la chat mostrata a destra (protetta da u.mu)
	single bool     // modalità con cui è disegnata adesso
	placed bool
	closed atomic.Bool   // Linux: finestra chiusa, Ping resta nell'area di notifica
	reopen chan struct{} // richiesta di riaprirla

	bookVer  uint64
	toldTray bool
	book     []Chat
	quit     bool
}

func (u *UI) RunMain() {
	m := &mainWin{u: u, w: new(app.Window), th: newTheme(), rows: map[string]*widget.Clickable{}, avBtns: map[string]*widget.Clickable{}, rowTags: map[string]*rowTagT{}, reopen: make(chan struct{}, 1)}
	m.search.SingleLine = true
	m.list.Axis = layout.Vertical
	u.main = m
	u.startUpdateChecks()
	go func() {
		for range time.Tick(5 * time.Second) {
			u.invalidateAll()
		}
	}()
	m.single = u.Settings().Single
	m.w.Option(app.Title("Ping"), m.sizeOption(), app.MinSize(unit.Dp(280), unit.Dp(420)))
	go m.run()
}

// sizeOption: dimensioni iniziali della finestra principale nella modalità attuale.
func (m *mainWin) sizeOption() app.Option {
	if m.single {
		return app.Size(unit.Dp(980), unit.Dp(680))
	}
	return app.Size(unit.Dp(340), unit.Dp(660))
}

// setSingle passa da finestre separate a finestra unica e viceversa.
func (m *mainWin) setSingle(on bool) {
	if on == m.single {
		return
	}
	u := m.u
	u.rememberPlace(m.placeKey(), m.hwnd)
	u.Update(func(s *Settings) { s.Single = on })
	m.single = on
	if !on {
		u.mu.Lock()
		m.embed = nil
		u.mu.Unlock()
	}
	if _, ok := u.place(m.placeKey()); ok {
		h := m.hwnd
		go u.restorePlace(m.placeKey(), h)
	} else {
		m.w.Option(m.sizeOption())
	}
}

func (u *UI) Quit() {
	if m := u.main; m != nil {
		u.rememberPlace(m.placeKey(), m.hwnd)
	}
	u.mu.Lock()
	open := make([]*chatWin, 0, len(u.chats))
	for _, cw := range u.chats {
		open = append(open, cw)
	}
	u.mu.Unlock()
	for _, cw := range open {
		u.rememberPlace("chat:"+cw.jid, cw.hwnd)
	}
	u.be.SetActive(false)
	u.be.Close()
	RemoveTray()
	os.Exit(0)
}

func (m *mainWin) run() {
	var ops op.Ops
	for {
		switch e := m.w.Event().(type) {
		case app.DestroyEvent:
			if closeToTray && m.closed.Load() && trayAvailable() {
				// Linux: la finestra è chiusa davvero ma Ping resta nell'area di notifica;
				// "Apri Ping" la ricrea (la stessa app.Window si riapre alla prossima Event)
				m.u.mu.Lock()
				m.focused = false
				m.u.mu.Unlock()
				m.u.focusChanged()
				<-m.reopen
				m.w.Option(app.Title("Ping"), m.sizeOption(), app.MinSize(unit.Dp(280), unit.Dp(420)))
				m.closed.Store(false)
				continue
			}
			m.u.Quit()
			return
		case *app.ClosingEvent:
			// chiudere la finestra la nasconde nell'area di notifica
			m.u.rememberPlace(m.placeKey(), m.hwnd)
			if trayAvailable() && closeToTray {
				m.closed.Store(true)
				if !m.toldTray {
					m.toldTray = true
					Notify("Ping è ancora attivo", "Lo trovi nell'area di notifica. Per chiuderlo: clic sull'icona › Esci.")
				}
			} else if trayAvailable() {
				e.Abort()
				// Windows sta aspettando la nostra risposta a WM_CLOSE: nasconderla adesso, dallo
				// stesso giro, bloccherebbe tutto. Lo si fa subito dopo, in parallelo.
				h := m.hwnd
				go HideHWND(h)
				if !m.toldTray {
					m.toldTray = true
					Notify("Ping è ancora attivo", "Lo trovi nell'area di notifica. Per chiuderlo: clic destro sull'icona › Esci.")
				}
			}
		case app.ConfigEvent:
			m.u.mu.Lock()
			was := m.focused
			m.focused = e.Config.Focused
			embed := m.embed
			m.u.mu.Unlock()
			if m.focused && !was && embed != nil {
				go m.u.be.MarkRead(embed.jid)
			}
			m.u.focusChanged()
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			m.pal = m.u.Pal()
			applyPalette(m.th, m.pal)
			guardFrame("principale", func() { m.layout(gtx) })
			e.Frame(gtx.Ops)
		default:
			if h, ok := hwndOf(e); ok {
				m.hwnd = h
				dark := m.u.Pal().Dark
				first := !m.placed
				m.placed = true
				key := m.placeKey()
				go func() { // mai chiamate sincrone verso la finestra dal suo giro di eventi
					SetDarkTitleBar(h, dark)
					if first {
						WatchPlace(h, func(p WinPlace) { m.u.storePlace(m.placeKeyNow(), p) })
						EnableFileDrop(h, m.onDrop)
						m.u.restorePlace(key, h)
					}
				}()
			}
		}
	}
}

func (m *mainWin) layout(gtx C) D {
	p := m.pal
	fillRect(gtx, p.Bg, image.Rectangle{Max: gtx.Constraints.Max})
	st, qr, errText := m.u.be.State()
	switch st {
	case StateStarting:
		return m.centerMsg(gtx, "Avvio…", true)
	case StateQR, StateQRExpired:
		return m.loginView(gtx, qr, st == StateQRExpired)
	case StateError:
		return m.centerMsg(gtx, "Errore: "+errText, false)
	}
	// l'ingranaggio cambia la vista: va letto PRIMA di decidere cosa disegnare,
	// altrimenti il cambio si vedrebbe solo al prossimo movimento del mouse
	if m.btnOptions.Clicked(gtx) {
		m.showOptions = !m.showOptions
		gtx.Execute(op.InvalidateCmd{})
	}
	cs, _ := m.u.be.Snapshot()
	if st == StateSyncing && len(cs) == 0 {
		return m.centerMsg(gtx, "Sincronizzazione delle chat…", true)
	}
	if m.single && gtx.Constraints.Max.X >= dp(gtx, 620) {
		return m.splitView(gtx, st)
	}
	return m.sidebar(gtx, st)
}

// sidebar: la colonna con profilo, ricerca e chat (tutta la finestra nella modalità classica).
func (m *mainWin) sidebar(gtx C, st ConnState) D {
	if m.showOptions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(m.header),
			layout.Flexed(1, m.optionsView),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(m.header),
		layout.Rigid(m.searchBar),
		layout.Rigid(m.tabs),
		layout.Rigid(m.updateBanner),
		layout.Flexed(1, m.contactList),
		layout.Rigid(func(gtx C) D { return m.statusBar(gtx, st) }),
	)
}

func (m *mainWin) centerMsg(gtx C, s string, spin bool) D {
	th, p := m.th, m.pal
	return layout.Center.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if !spin {
					return D{}
				}
				gtx.Constraints = layout.Exact(image.Pt(dp(gtx, 28), dp(gtx, 28)))
				l := material.Loader(th)
				l.Color = p.Accent
				return l.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, dp(gtx, 260))
				l := lbl(th, 13, p.Muted, s)
				l.Alignment = 2 // centro
				return l.Layout(gtx)
			}),
		)
	})
}

// ---- schermata di collegamento ----

func (m *mainWin) loginView(gtx C, qr string, expired bool) D {
	th, p := m.th, m.pal
	if qr != m.qrText && qr != "" {
		if q, err := qrcode.New(qr, qrcode.Medium); err == nil {
			q.DisableBorder = true
			img := q.Image(256)
			m.qrOp = paint.NewImageOp(img)
			m.qrOp.Filter = paint.FilterNearest
			m.qrSize = img.Bounds().Dx()
			m.qrText = qr
		}
	}
	if m.btnRetry.Clicked(gtx) {
		m.u.be.RetryQR()
	}
	return layout.Center.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return logo(gtx, th, p, 46) }),
			layout.Rigid(layout.Spacer{Height: 12}.Layout),
			layout.Rigid(bold(lbl(th, 18, p.Text, "Collega WhatsApp")).Layout),
			layout.Rigid(layout.Spacer{Height: 18}.Layout),
			layout.Rigid(func(gtx C) D {
				sz := dp(gtx, 210)
				pad := dp(gtx, 18)
				r := image.Rectangle{Max: image.Pt(sz+2*pad, sz+2*pad)}
				fillRRect(gtx, rgb(0xFFFFFF), r, dp(gtx, 14))
				if m.qrText != "" && !expired {
					defer op.Offset(image.Pt(pad, pad)).Push(gtx.Ops).Pop()
					st := clip.Rect{Max: image.Pt(sz, sz)}.Push(gtx.Ops)
					sc := float32(sz) / float32(max(1, m.qrSize))
					tr := op.Affine(f32Scale(sc)).Push(gtx.Ops)
					m.qrOp.Add(gtx.Ops)
					paint.PaintOp{}.Add(gtx.Ops)
					tr.Pop()
					st.Pop()
				} else if expired {
					g := gtx
					g.Constraints = layout.Exact(r.Max)
					layout.Center.Layout(g, func(gtx C) D {
						return m.btnRetry.Layout(gtx, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							return rounded(gtx, p.Accent, 10, 16, 9, bold(lbl(th, 13, p.AccentTxt, "Nuovo codice")).Layout)
						})
					})
				}
				return D{Size: r.Max}
			}),
			layout.Rigid(layout.Spacer{Height: 18}.Layout),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, dp(gtx, 250))
				steps := []string{
					"1.  Apri WhatsApp sul telefono",
					"2.  Impostazioni › Dispositivi collegati",
					"3.  Collega un dispositivo e inquadra il codice",
				}
				if expired {
					steps = []string{"Il codice è scaduto. Generane uno nuovo."}
				}
				var ch []layout.FlexChild
				for _, s := range steps {
					ch = append(ch, layout.Rigid(lbl(th, 12.5, p.Muted, s).Layout), layout.Rigid(layout.Spacer{Height: 5}.Layout))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
			}),
		)
	})
}

// splitView: finestra unica, lista a sinistra e chat scelta a destra.
func (m *mainWin) splitView(gtx C, st ConnState) D {
	p := m.pal
	m.u.mu.Lock()
	cw, focused := m.embed, m.focused
	if cw != nil {
		cw.focused = focused
	}
	m.u.mu.Unlock()
	side := min(dp(gtx, 330), gtx.Constraints.Max.X*2/5)
	list := layout.Rigid(func(gtx C) D {
		gtx.Constraints = layout.Exact(image.Pt(side, gtx.Constraints.Max.Y))
		return m.sidebar(gtx, st)
	})
	divider := layout.Rigid(func(gtx C) D {
		r := image.Rect(0, 0, 1, gtx.Constraints.Max.Y)
		fillRect(gtx, p.Divider, r)
		return D{Size: r.Max}
	})
	chat := layout.Flexed(1, func(gtx C) D {
		gtx.Constraints.Min = gtx.Constraints.Max
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
		if cw == nil {
			return m.emptyChat(gtx)
		}
		cw.pal, cw.set = m.pal, m.u.Settings()
		var d D
		guardFrame("chat", func() { d = cw.layout(gtx) })
		return D{Size: gtx.Constraints.Max, Baseline: d.Baseline}
	})
	if m.u.Settings().ChatLeft {
		return layout.Flex{}.Layout(gtx, chat, divider, list)
	}
	return layout.Flex{}.Layout(gtx, list, divider, chat)
}

func (m *mainWin) emptyChat(gtx C) D {
	th, p := m.th, m.pal
	fillRect(gtx, p.Bg, image.Rectangle{Max: gtx.Constraints.Max})
	return layout.Center.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return logo(gtx, th, p, 56) }),
			layout.Rigid(layout.Spacer{Height: 14}.Layout),
			layout.Rigid(bold(lbl(th, 15, p.Text, "Ping")).Layout),
			layout.Rigid(layout.Spacer{Height: 4}.Layout),
			layout.Rigid(lbl(th, 12.5, p.Muted, "Scegli una chat dall'elenco").Layout),
		)
	})
}

func (m *mainWin) embedded() *chatWin {
	m.u.mu.Lock()
	defer m.u.mu.Unlock()
	return m.embed
}

// onDrop: file trascinati sulla finestra principale vanno alla chat aperta a destra.
func (m *mainWin) onDrop(paths []string) {
	m.u.mu.Lock()
	cw := m.embed
	m.u.mu.Unlock()
	if cw != nil {
		cw.onDrop(paths)
	}
}

// logo: l'icona dell'app.
func logo(gtx C, th *material.Theme, p Pal, size float32) D {
	px := dp(gtx, size)
	if appIcon == nil {
		return D{Size: image.Pt(px, px)}
	}
	op := scaledOp(appIcon, px)
	op.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return D{Size: image.Pt(px, px)}
}

// ---- intestazione con il proprio profilo ----

func (m *mainWin) header(gtx C) D {
	th, p, u := m.th, m.pal, m.u
	if m.btnPhotos.Clicked(gtx) {
		u.setMu.Lock()
		u.settings.ShowPhotos = !u.settings.ShowPhotos
		u.setMu.Unlock()
		u.saveSettings()
	}
	if m.btnSound.Clicked(gtx) {
		u.setMu.Lock()
		u.settings.Sound = !u.settings.Sound
		u.setMu.Unlock()
		u.saveSettings()
		if u.settings.Sound {
			PlayChime()
		}
	}
	meJID, meName := u.be.Me()
	m2 := op.Record(gtx.Ops)
	d := layout.Inset{Left: 14, Right: 8, Top: 14, Bottom: 12}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return avatar(gtx, th, p, u.be, meJID, meName, 44, false, true, p.Surface)
			}),
			layout.Rigid(layout.Spacer{Width: 12}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(one(bold(lbl(th, 15, p.Text, meName))).Layout),
					layout.Rigid(layout.Spacer{Height: 2}.Layout),
					layout.Rigid(one(lbl(th, 12, p.Online, "● Disponibile")).Layout),
				)
			}),
			layout.Rigid(func(gtx C) D {
				g := icPhoto
				if !u.settings.ShowPhotos {
					g = icList
				}
				return iconButton(gtx, th, p, &m.btnPhotos, g, false, p.Muted)
			}),
			layout.Rigid(func(gtx C) D {
				g := icVolume
				if !u.settings.Sound {
					g = icMute
				}
				return iconButton(gtx, th, p, &m.btnSound, g, false, p.Muted)
			}),
			layout.Rigid(func(gtx C) D {
				return iconButton(gtx, th, p, &m.btnOptions, "\uE713", m.showOptions, p.Muted)
			}),
		)
	})
	call := m2.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	call.Add(gtx.Ops)
	return d
}

func (m *mainWin) searchBar(gtx C) D {
	th, p := m.th, m.pal
	m2 := op.Record(gtx.Ops)
	d := layout.Inset{Left: 12, Right: 12, Bottom: 10}.Layout(gtx, func(gtx C) D {
		return rounded(gtx, p.Field, 10, 10, 8, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(icon(th, 12, p.Faint, icSearch).Layout),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Flexed(1, func(gtx C) D {
					e := material.Editor(th, &m.search, "Cerca")
					e.TextSize = unit.Sp(13)
					e.Color = p.Text
					e.HintColor = p.Faint
					e.SelectionColor = selColor(p)
					return e.Layout(gtx)
				}),
			)
		})
	})
	call := m2.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	call.Add(gtx.Ops)
	return d
}

func (m *mainWin) tabs(gtx C) D {
	th, p := m.th, m.pal
	if m.tabChats.Clicked(gtx) {
		m.tab = 0
		m.list.Position = layout.Position{}
	}
	if m.tabBook.Clicked(gtx) {
		m.tab = 1
		m.list.Position = layout.Position{}
	}
	tab := func(cl *widget.Clickable, label string, active bool) layout.FlexChild {
		return layout.Flexed(1, func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				d := layout.Inset{Top: 8, Bottom: 9}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					c := p.Muted
					if active {
						c = p.Text
					}
					l := lbl(th, 12.5, c, label)
					l.Alignment = 2
					if active {
						l.Font.Weight = font.SemiBold
					}
					return l.Layout(gtx)
				})
				if active {
					w := dp(gtx, 28)
					x := (d.Size.X - w) / 2
					fillRRect(gtx, p.Accent, image.Rect(x, d.Size.Y-dp(gtx, 3), x+w, d.Size.Y), dp(gtx, 2))
				}
				return d
			})
		})
	}
	m2 := op.Record(gtx.Ops)
	d := layout.Flex{}.Layout(gtx, tab(&m.tabChats, "Chat", m.tab == 0), tab(&m.tabBook, "Rubrica", m.tab == 1))
	call := m2.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, d.Size.Y-1, d.Size.X, d.Size.Y))
	call.Add(gtx.Ops)
	return d
}

type rowItem struct {
	chat    Chat
	special string // "arch" = riga delle archiviate
	count   int
}

func (m *mainWin) items() []rowItem {
	q := strings.ToLower(strings.TrimSpace(m.search.Text()))
	match := func(c Chat) bool {
		return q == "" || strings.Contains(strings.ToLower(c.Name), q) || strings.Contains(c.JID, q)
	}
	var out []rowItem
	if m.tab == 1 {
		_, ver := m.u.be.Snapshot()
		if m.book == nil || ver != m.bookVer {
			m.book = m.u.be.Contacts()
			m.bookVer = ver
		}
		for _, c := range m.book {
			if match(c) {
				out = append(out, rowItem{chat: c})
			}
		}
		return out
	}
	cs, _ := m.u.be.Snapshot()
	var arch []rowItem
	archUnread := 0
	for _, c := range cs {
		if c.LastTime.Unix() <= 0 && c.LastText == "" {
			continue // chat senza messaggi
		}
		if !match(c) {
			continue
		}
		if c.Archived && q == "" {
			arch = append(arch, rowItem{chat: c})
			if c.Unread > 0 {
				archUnread++
			}
			continue
		}
		out = append(out, rowItem{chat: c})
	}
	sortChatRows(out, m.u.Settings().UnreadTop)
	if len(arch) > 0 {
		out = append(out, rowItem{special: "arch", count: len(arch)})
		if m.showArch {
			out = append(out, arch...)
		}
	}
	return out
}

func (m *mainWin) contactList(gtx C) D {
	th, p := m.th, m.pal
	items := m.items()
	if m.btnArchived.Clicked(gtx) {
		m.showArch = !m.showArch
	}
	if len(items) == 0 {
		msg := "Nessuna chat"
		if m.search.Text() != "" {
			msg = "Nessun risultato"
		} else if m.tab == 1 {
			msg = "Rubrica vuota (si riempie dopo la sincronizzazione)"
		}
		return layout.Center.Layout(gtx, lbl(th, 12.5, p.Faint, msg).Layout)
	}
	// clic sulle foto (prima delle righe, che le contengono)
	for jid, cl := range m.avBtns {
		if cl.Clicked(gtx) {
			m.u.OpenProfile(jid)
		}
	}
	// clic sulle righe
	for jid, cl := range m.rows {
		if cl.Clicked(gtx) {
			m.menuFor = ""
			m.u.OpenChat(jid)
		}
	}
	// menu del tasto destro
	if m.menuFor != "" {
		c, _ := m.u.be.ChatByJID(m.menuFor)
		if m.menuPin.Clicked(gtx) {
			if err := m.u.be.SetPin(c.JID, c.Pinned == 0); err != nil {
				m.menuErr = err.Error()
			} else {
				m.menuFor, m.menuErr = "", ""
				m.list.Position = layout.Position{} // la chat fissata va in cima: si torna su
			}
			gtx.Execute(op.InvalidateCmd{})
		}
		if m.menuRead.Clicked(gtx) {
			jid := c.JID
			m.menuFor = ""
			go m.u.be.MarkRead(jid)
		}
		if m.menuOpen.Clicked(gtx) {
			jid := c.JID
			m.menuFor = ""
			m.u.OpenChat(jid)
		}
	}
	ls := material.List(th, &m.list)
	ls.AnchorStrategy = material.Overlay
	ls.Indicator.Color = argb(0x888888, 0x50)
	return ls.Layout(gtx, len(items), func(gtx C, i int) D {
		it := items[i]
		if it.special == "arch" {
			return m.archRow(gtx, it.count)
		}
		cl := m.rows[it.chat.JID]
		if cl == nil {
			cl = new(widget.Clickable)
			m.rows[it.chat.JID] = cl
		}
		if m.tab != 0 {
			return m.row(gtx, cl, it.chat)
		}
		// tasto destro: menu della chat
		jid := it.chat.JID
		tag := m.rowTags[jid]
		if tag == nil {
			tag = &rowTagT{jid}
			m.rowTags[jid] = tag
		}
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
			if !ok {
				break
			}
			if pe, ok := ev.(pointer.Event); ok && pe.Buttons.Contain(pointer.ButtonSecondary) {
				if m.menuFor == jid {
					m.menuFor = ""
				} else {
					m.menuFor, m.menuErr = jid, ""
				}
				gtx.Execute(op.InvalidateCmd{})
			}
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				mc := op.Record(gtx.Ops)
				d := m.row(gtx, cl, it.chat)
				call := mc.Stop()
				st := clip.Rect{Max: d.Size}.Push(gtx.Ops)
				event.Op(gtx.Ops, tag)
				call.Add(gtx.Ops)
				st.Pop()
				return d
			}),
			layout.Rigid(func(gtx C) D {
				if m.menuFor != jid {
					return D{}
				}
				return m.rowMenu(gtx, it.chat)
			}),
		)
	})
}

// sortChatRows: prima le chat fissate (l'ultima fissata in cima), poi, se richiesto, quelle
// non lette; per il resto resta l'ordine per ora dell'ultimo messaggio.
func sortChatRows(out []rowItem, unreadTop bool) {
	sort.SliceStable(out, func(i, k int) bool {
		a, b := out[i].chat, out[k].chat
		if (a.Pinned > 0) != (b.Pinned > 0) {
			return a.Pinned > 0
		}
		if a.Pinned > 0 {
			return a.Pinned > b.Pinned
		}
		if unreadTop && (a.Unread > 0) != (b.Unread > 0) {
			return a.Unread > 0
		}
		return false
	})
}

// updateBanner: avviso di una nuova versione su GitHub.
func (m *mainWin) updateBanner(gtx C) D {
	th, p, u := m.th, m.pal, m.u
	upd := AvailableUpdate()
	if upd == nil || u.Settings().SkipVersion == upd.Tag || !u.Settings().CheckUpdates {
		return D{}
	}
	if m.updClose.Clicked(gtx) {
		tag := upd.Tag
		u.Update(func(s *Settings) { s.SkipVersion = tag })
		gtx.Execute(op.InvalidateCmd{})
		return D{}
	}
	if m.updOpen.Clicked(gtx) {
		go openPath(upd.URL)
	}
	return layout.Inset{Left: 10, Right: 10, Top: 8, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				return m.updOpen.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					return rounded(gtx, p.Mine, 10, 12, 8, func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Constraints.Max.X
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(one(bold(lbl(th, 12.5, p.Text, "È disponibile Ping "+upd.Tag))).Layout),
							layout.Rigid(one(lbl(th, 11.5, p.Accent, "Clicca per scaricarla da GitHub")).Layout),
						)
					})
				})
			}),
			layout.Rigid(func(gtx C) D { return iconButton(gtx, th, p, &m.updClose, "\uE711", false, p.Muted) }),
		)
	})
}

// rowMenu: azioni sulla chat (tasto destro sulla riga).
func (m *mainWin) rowMenu(gtx C, c Chat) D {
	th, p := m.th, m.pal
	chip := func(cl *widget.Clickable, glyph, label string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					bg := p.Field
					if cl.Hovered() {
						bg = p.Hover
					}
					return rounded(gtx, bg, 9, 10, 6, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								if glyph == "" {
									return D{}
								}
								return layout.Inset{Right: 6}.Layout(gtx, icon(th, 11, p.Accent, glyph).Layout)
							}),
							layout.Rigid(lbl(th, 12, p.Text, label).Layout),
						)
					})
				})
			})
		})
	}
	pin := "Fissa in alto"
	if c.Pinned > 0 {
		pin = "Togli da in alto"
	}
	items := []layout.FlexChild{chip(&m.menuPin, icPin, pin)}
	if c.Unread > 0 {
		items = append(items, chip(&m.menuRead, icCheck, "Segna come letta"))
	}
	items = append(items, chip(&m.menuOpen, "", "Apri"))
	return layout.Inset{Left: 66, Right: 12, Top: 2}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D { return layout.Flex{}.Layout(gtx, items...) }),
			layout.Rigid(func(gtx C) D {
				if m.menuErr == "" {
					return D{}
				}
				return layout.Inset{Bottom: 6}.Layout(gtx, lbl(th, 11.5, p.Danger, m.menuErr).Layout)
			}),
		)
	})
}

func (m *mainWin) archRow(gtx C, n int) D {
	th, p := m.th, m.pal
	return m.btnArchived.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Inset{Left: 16, Right: 16, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
			g := icChevron
			if m.showArch {
				g = icChevUp
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(icon(th, 10, p.Faint, g).Layout),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(lbl(th, 12, p.Muted, "Archiviate · "+strconv.Itoa(n)).Layout),
			)
		})
	})
}

func (m *mainWin) row(gtx C, cl *widget.Clickable, c Chat) D {
	th, p, u := m.th, m.pal, m.u
	pres := u.be.PresenceOf(c.JID)
	typing := isTyping(pres)
	compact := !u.settings.ShowPhotos
	return cl.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		m2 := op.Record(gtx.Ops)
		var d D
		if compact {
			d = layout.Inset{Left: 16, Right: 14, Top: 7, Bottom: 7}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						s := dp(gtx, 8)
						col := p.Divider
						if pres.Online {
							col = p.Online
						}
						if c.IsGroup {
							return icon(th, 10, p.Faint, icGroup).Layout(gtx)
						}
						fillCircle(gtx, col, image.Rectangle{Max: image.Pt(s, s)})
						return D{Size: image.Pt(dp(gtx, 10), s)}
					}),
					layout.Rigid(layout.Spacer{Width: 10}.Layout),
					layout.Flexed(1, func(gtx C) D {
						l := one(lbl(th, 13, p.Text, c.Name))
						if c.Unread > 0 {
							l.Font.Weight = font.SemiBold
						}
						if typing {
							l.Color = p.Accent
						}
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D { return m.pinMark(gtx, c) }),
					layout.Rigid(func(gtx C) D { return m.badge(gtx, c.Unread) }),
				)
			})
		} else {
			d = layout.Inset{Left: 12, Right: 14, Top: 8, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						ab := m.avBtns[c.JID]
						if ab == nil {
							ab = new(widget.Clickable)
							m.avBtns[c.JID] = ab
						}
						return ab.Layout(gtx, func(gtx C) D {
							return avatar(gtx, th, p, u.be, c.JID, c.Name, 44, c.IsGroup, pres.Online && !c.IsGroup, p.Bg)
						})
					}),
					layout.Rigid(layout.Spacer{Width: 12}.Layout),
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
									layout.Flexed(1, func(gtx C) D {
										l := one(lbl(th, 14, p.Text, c.Name))
										l.Font.Weight = font.Medium
										if c.Unread > 0 {
											l.Font.Weight = font.Bold
										}
										return l.Layout(gtx)
									}),
									layout.Rigid(func(gtx C) D {
										col := p.Faint
										if c.Unread > 0 {
											col = p.Accent
										}
										return lbl(th, 11, col, listTime(c.LastTime)).Layout(gtx)
									}),
								)
							}),
							layout.Rigid(layout.Spacer{Height: 3}.Layout),
							layout.Rigid(func(gtx C) D {
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Flexed(1, func(gtx C) D {
										if typing {
											l := one(lbl(th, 12.5, p.Accent, "sta scrivendo…"))
											l.Font.Style = font.Italic
											return l.Layout(gtx)
										}
										t := c.LastText
										if c.LastFromMe && t != "" {
											t = "Tu: " + t
										}
										return one(lbl(th, 12.5, p.Muted, t)).Layout(gtx)
									}),
									layout.Rigid(func(gtx C) D { return m.pinMark(gtx, c) }),
									layout.Rigid(func(gtx C) D { return m.badge(gtx, c.Unread) }),
								)
							}),
						)
					}),
				)
			})
		}
		call := m2.Stop()
		if e := m.embedded(); m.single && e != nil && e.jid == c.JID {
			fillRRect(gtx, p.Hover, image.Rect(dp(gtx, 4), 0, d.Size.X-dp(gtx, 4), d.Size.Y), dp(gtx, 8))
			fillRRect(gtx, p.Accent, image.Rect(dp(gtx, 4), dp(gtx, 8), dp(gtx, 7), d.Size.Y-dp(gtx, 8)), dp(gtx, 2))
		} else if cl.Hovered() {
			fillRRect(gtx, p.Hover, image.Rect(dp(gtx, 4), 0, d.Size.X-dp(gtx, 4), d.Size.Y), dp(gtx, 8))
		}
		call.Add(gtx.Ops)
		return d
	})
}

// pinMark: puntina accanto alle chat fissate in alto.
func (m *mainWin) pinMark(gtx C, c Chat) D {
	if c.Pinned == 0 || m.tab != 0 {
		return D{}
	}
	return layout.Inset{Left: 6}.Layout(gtx, icon(m.th, 11, m.pal.Faint, icPin).Layout)
}

func (m *mainWin) badge(gtx C, n int) D {
	if n <= 0 {
		return D{}
	}
	th, p := m.th, m.pal
	s := strconv.Itoa(n)
	if n > 99 {
		s = "99+"
	}
	return layout.Inset{Left: 8}.Layout(gtx, func(gtx C) D {
		mm := op.Record(gtx.Ops)
		d := layout.Inset{Left: 6, Right: 6, Top: 1, Bottom: 2}.Layout(gtx, bold(lbl(th, 10.5, p.AccentTxt, s)).Layout)
		call := mm.Stop()
		h := d.Size.Y
		w := max(d.Size.X, h)
		fillRRect(gtx, p.Accent, image.Rect(0, 0, w, h), h/2)
		st := op.Offset(image.Pt((w-d.Size.X)/2, 0)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		return D{Size: image.Pt(w, h)}
	})
}

func (m *mainWin) statusBar(gtx C, st ConnState) D {
	th, p := m.th, m.pal
	var s string
	switch st {
	case StateSyncing:
		s = "Sincronizzazione…"
	case StateOffline:
		s = "Non connesso · riprovo…"
	default:
		return D{}
	}
	return layout.Inset{Top: 6, Bottom: 6, Left: 14}.Layout(gtx, lbl(th, 11, p.Faint, s).Layout)
}
