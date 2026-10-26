package main

import (
	"gioui.org/x/richtext"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	icCheck = ""
	icClock = ""
	icError = ""
)

type chatRow struct {
	day   string
	msg   *Message
	first bool // primo di una serie dello stesso mittente
}

type chatWin struct {
	u       *UI
	th      *material.Theme // ogni finestra ha il suo motore del testo: niente blocchi tra finestre
	pal     Pal
	set     Settings // copia delle impostazioni, presa a ogni fotogramma
	jid     string
	w       *app.Window // nil quando la chat è dentro la finestra principale
	hwnd    uintptr
	focused bool
	host    *mainWin                             // finestra unica: la finestra principale che ospita la chat
	rich    map[string]*richtext.InteractiveText // testi con link cliccabili
	lp      linkDraft                            // anteprima del link mentre si scrive

	profiles    []*profileView // schede aperte (l'ultima è quella visibile)
	wantProfile bool           // chiesta da un'altra finestra (protetta da noticeMu)
	gioPaste    bool           // incollare il testo con il meccanismo di Gio
	showDel     bool           // con che impostazione sono stati caricati i messaggi eliminati

	list      widget.List
	editor    widget.Editor
	btnSend   widget.Clickable
	btnAttach widget.Clickable
	btnEmoji  widget.Clickable
	media     map[string]*widget.Clickable
	actions   map[string]func()
	sel       map[string]*widget.Selectable
	emoji     emojiPanel

	reply       *Message       // messaggio a cui si sta rispondendo
	editing     *Message       // messaggio che si sta modificando
	deleteFor   string         // id del messaggio con la conferma di cancellazione aperta
	menuFor     string         // id del messaggio con il menu (tasto destro) aperto
	fwd         *forwardPicker // scelta dei destinatari per l'inoltro
	mention     mentionState   // tendina "@" nei gruppi
	copyText    string         // testo da copiare negli appunti al prossimo fotogramma
	rowTags     map[string]*rowTagT
	reactFor    string   // id del messaggio con la barra delle reazioni aperta
	reactPick   *Message // il pannello emoji sceglie una reazione per questo messaggio
	hover       map[string]*widget.Clickable
	btnNoReply  widget.Clickable
	pending     []*pendingAtt // allegati incollati in attesa di invio
	dropMu      sync.Mutex
	dropped     []string // file trascinati, da aggiungere agli allegati
	noticeMu    sync.Mutex
	notice      string
	noticeOK    bool
	noticeUntil time.Time

	ver         uint64
	loaded      bool
	msgs        []Message
	rows        []chatRow
	focusSet    bool
	focusEditor bool
	title       string
}

func newChatWin(u *UI, jid string) *chatWin {
	cw := &chatWin{u: u, jid: jid, w: new(app.Window), th: newTheme(),
		media: map[string]*widget.Clickable{}, actions: map[string]func(){}, sel: map[string]*widget.Selectable{},
		hover: map[string]*widget.Clickable{}, rowTags: map[string]*rowTagT{}}
	cw.list.Axis = layout.Vertical
	cw.list.ScrollToEnd = true
	cw.mention.dismissed = -1
	cw.editor.Submit = true
	c, _ := u.be.ChatByJID(jid)
	cw.title = c.Name
	if cw.title == "" {
		cw.title = jid
	}
	cw.w.Option(app.Title(cw.title), app.Size(unit.Dp(430), unit.Dp(580)), app.MinSize(unit.Dp(320), unit.Dp(360)))
	return cw
}

func (cw *chatWin) run() {
	var ops op.Ops
	for {
		switch e := cw.w.Event().(type) {
		case *app.ClosingEvent:
			cw.u.rememberPlace("chat:"+cw.jid, cw.hwnd) // alla prossima apertura torna qui
		case app.DestroyEvent:
			if e.Err != nil {
				logHandled("chiusura finestra chat", e.Err)
			}
			cw.u.mu.Lock()
			delete(cw.u.chats, cw.jid)
			cw.u.mu.Unlock()
			cw.u.focusChanged()
			return
		case app.ConfigEvent:
			was := cw.focused
			cw.u.mu.Lock()
			cw.focused = e.Config.Focused
			cw.u.mu.Unlock()
			if cw.focused && !was {
				cw.u.be.MarkRead(cw.jid)
			}
			cw.u.focusChanged()
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			cw.pal, cw.set = cw.u.Pal(), cw.u.Settings()
			applyPalette(cw.th, cw.pal)
			guardFrame("chat", func() { cw.layout(gtx) })
			e.Frame(gtx.Ops)
		default:
			if h, ok := hwndOf(e); ok && h != cw.hwnd {
				cw.hwnd = h
				dark := cw.u.Pal().Dark
				key := "chat:" + cw.jid
				go func() { // fuori dal giro di eventi della finestra, che sta aspettando noi
					WatchPlace(h, func(p WinPlace) { cw.u.storePlace(key, p) })
					EnableFileDrop(h, cw.onDrop)
					SetDarkTitleBar(h, dark)
					cw.u.restorePlace(key, h)
				}()
			}
		}
	}
}

func (cw *chatWin) reload() {
	v := cw.u.be.ChatVersion(cw.jid)
	if cw.loaded && v == cw.ver && cw.showDel == cw.set.ShowDeleted {
		return
	}
	cw.ver, cw.loaded, cw.showDel = v, true, cw.set.ShowDeleted
	cw.msgs = cw.u.be.db.Messages(cw.jid, 400)
	for i := range cw.msgs {
		if m := &cw.msgs[i]; m.Deleted && !cw.showDel {
			// opzione spenta: come su WhatsApp, del messaggio resta solo l'avviso
			m.Kind, m.Text, m.Thumb, m.Media, m.Deleted = KDeleted, "", nil, nil, false
			m.Extra = MsgExtra{}
		}
	}
	cw.rows = cw.rows[:0]
	lastDay, lastSender := "", ""
	for i := range cw.msgs {
		m := &cw.msgs[i]
		d := dayLabel(m.Time)
		if d != lastDay {
			cw.rows = append(cw.rows, chatRow{day: d})
			lastDay, lastSender = d, ""
		}
		who := m.Sender
		if m.FromMe {
			who = "me"
		}
		cw.rows = append(cw.rows, chatRow{msg: m, first: who != lastSender})
		lastSender = who
	}
	if c, ok := cw.u.be.ChatByJID(cw.jid); ok && c.Name != "" && c.Name != cw.title {
		cw.title = c.Name
		if cw.w != nil {
			cw.w.Option(app.Title(c.Name))
		}
	}
	if cw.focused {
		go cw.u.be.MarkRead(cw.jid)
	}
}

func (cw *chatWin) layout(gtx C) D {
	p := cw.pal
	cw.reload()
	fillRect(gtx, p.Bg, image.Rectangle{Max: gtx.Constraints.Max})

	// Azioni dei pulsanti (messaggi, pannello sticker/GIF, menu): vanno lette PRIMA di disegnare
	// qualunque cosa, perché un pulsante consuma i propri clic quando viene disegnato e il
	// pannello (a dimensione fissa) viene disegnato prima dell'elenco dei messaggi.
	for k, cl := range cw.media {
		if cl.Clicked(gtx) {
			if act := cw.actions[k]; act != nil {
				act() // le azioni lente avviano da sole un goroutine
			}
		}
	}

	// file trascinati da Esplora risorse
	cw.dropMu.Lock()
	dropped := cw.dropped
	cw.dropped = nil
	cw.dropMu.Unlock()
	for _, f := range dropped {
		cw.addPending(f)
	}
	if len(dropped) > 0 {
		gtx.Execute(key.FocusCmd{Tag: &cw.editor})
	}

	// Ctrl+V: va letto prima dell'editor, che altrimenti incollerebbe solo testo
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &cw.editor, Name: "V", Required: key.ModShortcut})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			cw.paste()
		}
	}
	if cw.gioPaste {
		cw.gioPaste = false
		gtx.Execute(clipboard.ReadCmd{Tag: &cw.editor})
	}
	for i := range cw.pending {
		if cw.pending[i].remove.Clicked(gtx) {
			cw.pending = append(cw.pending[:i], cw.pending[i+1:]...)
			break
		}
	}

	// tendina delle menzioni: frecce/Invio/Tab/Esc prima che li veda il campo di testo
	cw.mentionKeys(gtx)

	// eventi dell'editor
	for {
		ev, ok := cw.editor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			cw.send()
		case widget.ChangeEvent:
			if cw.editor.Len() > 0 {
				cw.u.be.Typing(cw.jid)
			}
		}
	}
	cw.updateMention()
	if cw.btnSend.Clicked(gtx) {
		cw.send()
		gtx.Execute(key.FocusCmd{Tag: &cw.editor})
	}
	if cw.btnEmoji.Clicked(gtx) {
		cw.toggleEmoji()
		gtx.Execute(key.FocusCmd{Tag: &cw.editor})
	}
	if cw.btnAttach.Clicked(gtx) {
		owner, jid, reply := cw.owner(), cw.jid, cw.reply
		cw.reply = nil
		go func() {
			path := PickFile(owner, "Invia un file",
				"Foto e documenti|*.jpg;*.jpeg;*.png;*.pdf;*.doc;*.docx;*.xls;*.xlsx;*.ppt;*.pptx;*.zip;*.txt|Tutti i file|*.*")
			if path == "" {
				return
			}
			lower := strings.ToLower(path)
			switch {
			case strings.HasSuffix(lower, ".gif"):
				if err := cw.u.be.SendGIFFile(jid, path, reply); err != nil {
					cw.showNotice("GIF non inviata: " + err.Error())
				}
			case strings.HasSuffix(lower, ".mp4"):
				cw.u.be.SendGIFFile(jid, path, reply) // breve video: inviato come GIF
			default:
				cw.u.be.SendFile(jid, path, reply)
			}
		}()
	}
	if cw.btnNoReply.Clicked(gtx) {
		if cw.editing != nil {
			cw.editing = nil
			cw.editor.SetText("")
		}
		cw.reply = nil
	}
	// Esc: chiude la barra reazioni, il pannello o annulla la risposta
	for {
		ev, ok := gtx.Event(key.Filter{Focus: &cw.editor, Name: key.NameEscape})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok && ke.State == key.Press {
			switch {
			case len(cw.profiles) > 0:
				cw.closeProfile()
			case cw.fwd != nil:
				cw.fwd = nil
			case cw.menuFor != "":
				cw.menuFor = ""
			case cw.reactFor != "":
				cw.reactFor = ""
			case cw.deleteFor != "":
				cw.deleteFor = ""
			case cw.editing != nil:
				cw.editing = nil
				cw.editor.SetText("")
			case cw.emoji.open:
				cw.emoji.open, cw.reactPick = false, nil
			case cw.reply != nil:
				cw.reply = nil
			}
		}
	}
	if cw.copyText != "" {
		gtx.Execute(clipboard.WriteCmd{Type: "application/text", Data: io.NopCloser(strings.NewReader(cw.copyText))})
		cw.copyText = ""
		cw.notify("Testo copiato", true)
	}
	if cw.focusEditor {
		gtx.Execute(key.FocusCmd{Tag: &cw.editor})
		cw.focusEditor = false
	}
	if !cw.focusSet {
		gtx.Execute(key.FocusCmd{Tag: &cw.editor})
		cw.focusSet = true
		if cw.isGroup() {
			cw.u.be.GroupMembers(cw.jid) // carica subito i partecipanti, per la tendina "@"
		}
		go cw.u.be.MarkRead(cw.jid)
	}

	cw.noticeMu.Lock()
	want := cw.wantProfile
	cw.wantProfile = false
	cw.noticeMu.Unlock()
	if want {
		cw.showProfile(cw.jid, cw.title)
	}
	if len(cw.profiles) > 0 {
		return cw.profileView(gtx)
	}
	if cw.fwd != nil {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(cw.header),
			layout.Flexed(1, cw.forwardView),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(cw.header),
		layout.Flexed(1, cw.messages),
		layout.Rigid(cw.emojiPanel),
		layout.Rigid(cw.replyStrip),
		layout.Rigid(cw.pendingStrip),
		layout.Rigid(cw.linkStrip),
		layout.Rigid(cw.mentionPopup),
		layout.Rigid(cw.composer),
	)
}

func (cw *chatWin) send() {
	if cw.editing != nil {
		m := cw.editing
		cw.editing = nil
		t := cw.editor.Text()
		cw.editor.SetText("")
		cw.u.be.EditMessage(m, convertSmileys(t))
		return
	}
	t := cw.editor.Text()
	if strings.TrimSpace(t) == "" && len(cw.pending) == 0 {
		return
	}
	cw.editor.SetText("")
	cw.list.Position.BeforeEnd = false // torna in fondo
	text, reply, pending, jid, be := convertSmileys(t), cw.reply, cw.pending, cw.jid, cw.u.be
	names := cw.mention.names
	withPreview := cw.sendPreview(text)
	cw.reply, cw.pending, cw.mention.names = nil, nil, nil
	if len(pending) == 0 && withPreview != nil {
		safeGo("invio con anteprima", func() { be.SendTextFull(jid, text, names, reply, withPreview()) })
		return
	}
	if len(pending) == 0 {
		if len(names) > 0 {
			be.SendTextMentions(jid, text, names, reply)
		} else {
			be.SendText(jid, text, reply)
		}
		return
	}
	// allegati incollati: il testo diventa la didascalia della prima foto
	safeGo("invio allegati", func() {
		captionUsed := false
		for i, a := range pending {
			r := reply
			if i > 0 {
				r = nil
			}
			switch a.kind {
			case "image":
				caption := ""
				if !captionUsed {
					caption, captionUsed = text, true
				}
				if err := be.SendImage(jid, a.path, caption, r); err != nil {
					cw.showNotice("Foto non inviata: " + err.Error())
				}
			case "gif":
				if err := be.SendGIFFile(jid, a.path, r); err != nil {
					cw.showNotice("GIF non inviata: " + err.Error())
				}
			default:
				be.SendFile(jid, a.path, r)
			}
		}
		if !captionUsed && strings.TrimSpace(text) != "" {
			be.SendText(jid, text, nil)
		}
	})
}

type pendingAtt struct {
	path   string
	name   string
	kind   string // image, gif, file
	op     paint.ImageOp
	size   image.Point
	remove widget.Clickable
}

// paste gestisce Ctrl+V: immagini e file diventano allegati, il testo va nel campo.
func (cw *chatWin) paste() {
	c, err := ReadClipboard(cw.owner())
	if err != nil {
		cw.showNotice("Impossibile leggere gli appunti")
		return
	}
	switch {
	case len(c.Files) > 0:
		for _, f := range c.Files {
			cw.addPending(f)
		}
	case c.Image != nil:
		dir := filepath.Join(cw.u.be.dir, "media")
		os.MkdirAll(dir, 0o755)
		p := filepath.Join(dir, "incollata_"+time.Now().Format("20060102_150405.000")+".png")
		if f, err := os.Create(p); err == nil {
			png.Encode(f, c.Image)
			f.Close()
			cw.addPendingImage(p, c.Image)
		}
	case c.Text != "":
		cw.editor.Insert(strings.ReplaceAll(c.Text, "\r\n", "\n"))
	default:
		cw.gioPaste = true // niente immagini né file: incolla il testo come fa di solito il campo
	}
}

func (cw *chatWin) addPending(path string) {
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		return
	}
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".gif"):
		a := &pendingAtt{path: path, name: filepath.Base(path), kind: "gif"}
		if img := loadImage(path); img != nil {
			t := fitThumb(img, 160)
			a.op, a.size = paint.NewImageOp(t), t.Bounds().Size()
		}
		cw.pending = append(cw.pending, a)
	case strings.HasSuffix(lower, ".jpg") || strings.HasSuffix(lower, ".jpeg") || strings.HasSuffix(lower, ".png") ||
		strings.HasSuffix(lower, ".webp") || strings.HasSuffix(lower, ".bmp"):
		if img := loadImage(path); img != nil {
			cw.addPendingImage(path, img)
			return
		}
		fallthrough
	default:
		cw.pending = append(cw.pending, &pendingAtt{path: path, name: filepath.Base(path), kind: "file"})
	}
}

func (cw *chatWin) addPendingImage(path string, img image.Image) {
	t := fitThumb(img, 160)
	a := &pendingAtt{path: path, name: filepath.Base(path), kind: "image", size: t.Bounds().Size()}
	a.op = paint.NewImageOp(t)
	a.op.Filter = paint.FilterLinear
	cw.pending = append(cw.pending, a)
}

// pendingStrip mostra gli allegati incollati in attesa di invio.
func (cw *chatWin) pendingStrip(gtx C) D {
	if len(cw.pending) == 0 {
		return D{}
	}
	th, p := cw.th, cw.pal
	mc := op.Record(gtx.Ops)
	d := layout.Inset{Left: 12, Right: 12, Top: 8, Bottom: 4}.Layout(gtx, func(gtx C) D {
		var items []layout.FlexChild
		for _, a := range cw.pending {
			a := a
			items = append(items, layout.Rigid(func(gtx C) D {
				s := dp(gtx, 64)
				box := image.Pt(s, s)
				r := image.Rectangle{Max: box}
				st := clip.UniformRRect(r, dp(gtx, 8)).Push(gtx.Ops)
				paint.Fill(gtx.Ops, subtleBg(p))
				if a.size.X > 0 {
					drawImage(gtx, a.op, a.size, box)
				} else {
					g := gtx
					g.Constraints = layout.Exact(box)
					layout.Center.Layout(g, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(icon(th, 18, p.Accent, icDoc).Layout),
							layout.Rigid(func(gtx C) D {
								gtx.Constraints.Max.X = s - dp(gtx, 8)
								return one(lbl(th, 9.5, p.Muted, a.name)).Layout(gtx)
							}),
						)
					})
				}
				st.Pop()
				// pulsante per togliere l'allegato
				bs := dp(gtx, 20)
				o := op.Offset(image.Pt(s-bs-dp(gtx, 3), dp(gtx, 3))).Push(gtx.Ops)
				a.remove.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					rr := image.Rectangle{Max: image.Pt(bs, bs)}
					fillCircle(gtx, argb(0x000000, 0xA0), rr)
					g := gtx
					g.Constraints = layout.Exact(rr.Max)
					layout.Center.Layout(g, bold(lbl(th, 11, rgb(0xFFFFFF), "✕")).Layout)
					return D{Size: rr.Max}
				})
				o.Pop()
				return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D { return D{Size: box} })
			}))
		}
		items = append(items, layout.Flexed(1, func(gtx C) D {
			return layout.Inset{Left: 4, Top: 4}.Layout(gtx, lbl(th, 11.5, p.Faint, "Invio per mandare · il testo diventa la didascalia").Layout)
		}))
		return layout.Flex{}.Layout(gtx, items...)
	})
	call := mc.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
	call.Add(gtx.Ops)
	return d
}

func (cw *chatWin) header(gtx C) D {
	p, be := cw.pal, cw.u.be
	c, _ := be.ChatByJID(cw.jid)
	if c.Name == "" {
		c.Name = cw.title
	}
	pres := be.PresenceOf(cw.jid)
	sub, subCol := "", p.Muted
	switch {
	case isTyping(pres):
		sub, subCol = "sta scrivendo…", p.Accent
		if c.IsGroup && pres.TypingBy != "" {
			sub = pres.TypingBy + " sta scrivendo…"
		}
	case c.IsGroup:
		sub = "Gruppo"
	default:
		sub = lastSeenText(pres)
		if pres.Online {
			subCol = p.Online
		}
		if sub == "" && strings.HasSuffix(cw.jid, "@s.whatsapp.net") {
			sub = "+" + strings.TrimSuffix(cw.jid, "@s.whatsapp.net")
		}
	}
	m := op.Record(gtx.Ops)
	name := c.Name
	d := cw.click("hdr", func() { cw.showProfile(cw.jid, name) }).Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		return cw.headerContent(gtx, c, pres, sub, subCol)
	})
	call := m.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, d.Size.Y-1, d.Size.X, d.Size.Y))
	call.Add(gtx.Ops)
	return d
}

func (cw *chatWin) headerContent(gtx C, c Chat, pres Presence, sub string, subCol color.NRGBA) D {
	th, p, be := cw.th, cw.pal, cw.u.be
	return layout.Inset{Left: 14, Right: 14, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return avatar(gtx, th, p, be, cw.jid, c.Name, 38, c.IsGroup, pres.Online && !c.IsGroup, p.Surface)
			}),
			layout.Rigid(layout.Spacer{Width: 11}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(one(bold(lbl(th, 14.5, p.Text, c.Name))).Layout),
					layout.Rigid(func(gtx C) D {
						if sub == "" {
							return D{}
						}
						l := one(lbl(th, 11.5, subCol, sub))
						if isTyping(pres) {
							l.Font.Style = font.Italic
						}
						return layout.Inset{Top: 1}.Layout(gtx, l.Layout)
					}),
				)
			}),
		)
	})
}

func (cw *chatWin) messages(gtx C) D {
	th, p := cw.th, cw.pal
	if len(cw.rows) == 0 {
		return layout.Center.Layout(gtx, lbl(th, 12.5, p.Faint, "Nessun messaggio qui. Scrivi qualcosa!").Layout)
	}
	ls := material.List(th, &cw.list)
	ls.AnchorStrategy = material.Overlay
	ls.Indicator.Color = argb(0x888888, 0x50)
	return ls.Layout(gtx, len(cw.rows)+1, func(gtx C, i int) D {
		if i == len(cw.rows) {
			return layout.Spacer{Height: 10}.Layout(gtx)
		}
		r := cw.rows[i]
		if r.day != "" {
			return layout.Inset{Top: 12, Bottom: 6}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Center.Layout(gtx, func(gtx C) D {
					return rounded(gtx, p.Field, 8, 10, 3, lbl(th, 11, p.Muted, r.day).Layout)
				})
			})
		}
		return cw.bubbleRow(gtx, r)
	})
}

func ticks(gtx C, th *material.Theme, p Pal, st int) D {
	switch st {
	case StPending:
		return icon(th, 9.5, p.Faint, icClock).Layout(gtx)
	case StFailed:
		return icon(th, 10, p.Danger, icError).Layout(gtx)
	case StSent:
		return icon(th, 10, p.Faint, icCheck).Layout(gtx)
	}
	c := p.Faint
	if st == StRead {
		c = p.Read
	}
	// doppia spunta: due segni sovrapposti
	d := icon(th, 10, c, icCheck).Layout(gtx)
	st2 := op.Offset(image.Pt(dp(gtx, 4), 0)).Push(gtx.Ops)
	icon(th, 10, c, icCheck).Layout(gtx)
	st2.Pop()
	d.Size.X += dp(gtx, 4)
	return d
}

func (cw *chatWin) composer(gtx C) D {
	th, p := cw.th, cw.pal
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: 8, Right: 10, Top: 9, Bottom: 10}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.End}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 2}.Layout(gtx, func(gtx C) D {
					return iconButton(gtx, th, p, &cw.btnAttach, icAttach, false, p.Muted)
				})
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Bottom: 2}.Layout(gtx, func(gtx C) D {
					return cw.btnEmoji.Layout(gtx, func(gtx C) D {
						pointer.CursorPointer.Add(gtx.Ops)
						sz := dp(gtx, 32)
						r := image.Rectangle{Max: image.Pt(sz, sz)}
						if cw.btnEmoji.Hovered() || cw.emoji.open {
							fillRRect(gtx, p.Hover, r, dp(gtx, 8))
						}
						g := gtx
						g.Constraints = layout.Exact(r.Max)
						layout.Center.Layout(g, lbl(th, 16, p.Text, "🙂").Layout)
						return D{Size: r.Max}
					})
				})
			}),
			layout.Rigid(layout.Spacer{Width: 4}.Layout),
			layout.Flexed(1, func(gtx C) D {
				return rounded(gtx, p.Field, 12, 12, 9, func(gtx C) D {
					gtx.Constraints.Max.Y = dp(gtx, 120)
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					e := material.Editor(th, &cw.editor, "Scrivi un messaggio")
					e.TextSize = unit.Sp(13.5)
					e.Color = p.Text
					e.HintColor = p.Faint
					e.SelectionColor = argb(0x1F9D7A, 0x55)
					return e.Layout(gtx)
				})
			}),
			layout.Rigid(layout.Spacer{Width: 8}.Layout),
			layout.Rigid(func(gtx C) D {
				return cw.btnSend.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					sz := dp(gtx, 36)
					r := image.Rectangle{Max: image.Pt(sz, sz)}
					col := p.Accent
					if cw.editor.Len() == 0 {
						col = p.Field
					}
					fillCircle(gtx, col, r)
					g := gtx
					g.Constraints = layout.Exact(r.Max)
					tc := p.AccentTxt
					if cw.editor.Len() == 0 {
						tc = p.Faint
					}
					layout.Center.Layout(g, icon(th, 14, tc, icSend).Layout)
					return D{Size: r.Max}
				})
			}),
		)
	})
	call := m.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
	call.Add(gtx.Ops)
	return d
}

func (cw *chatWin) showNotice(s string) { cw.notify(s, false) }

// notify mostra un avviso sopra il campo di scrittura (rosso se è un errore, verde se no).
func (cw *chatWin) notify(s string, ok bool) {
	cw.noticeMu.Lock()
	cw.noticeOK = ok
	cw.notice, cw.noticeUntil = s, time.Now().Add(5*time.Second)
	cw.noticeMu.Unlock()
	cw.invalidate()
}

// startReply prepara la risposta a un messaggio.
func (cw *chatWin) startReply(m *Message) {
	c := *m
	cw.reply, cw.editing = &c, nil
	cw.reactFor, cw.deleteFor = "", ""
}

// startEdit mette il testo del messaggio nel campo di scrittura per modificarlo.
func (cw *chatWin) startEdit(m *Message) {
	c := *m
	cw.editing, cw.reply = &c, nil
	cw.reactFor, cw.deleteFor = "", ""
	t := cleanEdited(m.Text)
	cw.editor.SetText(t)
	n := len([]rune(t))
	cw.editor.SetCaret(n, n)
	cw.focusEditor = true
}

// replyStrip: "Rispondi a …" sopra il campo di scrittura (o un avviso temporaneo).
func (cw *chatWin) replyStrip(gtx C) D {
	th, p := cw.th, cw.pal
	cw.noticeMu.Lock()
	notice, until, okNotice := cw.notice, cw.noticeUntil, cw.noticeOK
	cw.noticeMu.Unlock()
	if notice != "" && time.Now().Before(until) {
		gtx.Execute(op.InvalidateCmd{At: until})
		col := p.Danger
		if okNotice {
			col = p.Accent
		}
		return layout.Inset{Left: 14, Right: 14, Top: 6, Bottom: 6}.Layout(gtx, lbl(th, 12, col, notice).Layout)
	}
	r := cw.reply
	editing := cw.editing != nil
	if editing {
		r = cw.editing
	}
	if r == nil {
		return D{}
	}
	who := "Tu"
	if !r.FromMe {
		who = r.SenderName
		if who == "" {
			c, _ := cw.u.be.ChatByJID(cw.jid)
			who = c.Name
		}
	}
	text := r.Text
	if r.Kind != KText {
		text = previewMsg(r)
	}
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: 12, Right: 10, Top: 8, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				mc := op.Record(gtx.Ops)
				dd := layout.Inset{Left: 10, Top: 4, Bottom: 4, Right: 6}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							title := "Rispondi a " + who
							if editing {
								title = "Modifica messaggio · Esc per annullare"
							}
							return one(bold(lbl(th, 11.5, p.Accent, title))).Layout(gtx)
						}),
						layout.Rigid(one(lbl(th, 12, p.Muted, strings.Join(strings.Fields(text), " "))).Layout),
					)
				})
				call := mc.Stop()
				st := clip.UniformRRect(image.Rectangle{Max: dd.Size}, dp(gtx, 6)).Push(gtx.Ops)
				paint.Fill(gtx.Ops, subtleBg(p))
				fillRect(gtx, p.Accent, image.Rect(0, 0, dp(gtx, 3), dd.Size.Y))
				st.Pop()
				call.Add(gtx.Ops)
				return dd
			}),
			layout.Rigid(layout.Spacer{Width: 6}.Layout),
			layout.Rigid(func(gtx C) D { return iconButton(gtx, th, p, &cw.btnNoReply, "\uE711", false, p.Muted) }),
		)
	})
	call := m.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
	call.Add(gtx.Ops)
	return d
}

// onDrop riceve i file trascinati (sul thread della finestra): li accoda e chiede un ridisegno.
func (cw *chatWin) onDrop(paths []string) {
	cw.dropMu.Lock()
	cw.dropped = append(cw.dropped, paths...)
	cw.dropMu.Unlock()
	cw.invalidate()
}

// newEmbeddedChat crea una chat da mostrare dentro la finestra principale (modalità finestra unica).
func newEmbeddedChat(u *UI, host *mainWin, jid string) *chatWin {
	cw := &chatWin{u: u, jid: jid, host: host, th: host.th,
		media: map[string]*widget.Clickable{}, actions: map[string]func(){}, sel: map[string]*widget.Selectable{},
		hover: map[string]*widget.Clickable{}, rowTags: map[string]*rowTagT{}}
	cw.list.Axis = layout.Vertical
	cw.list.ScrollToEnd = true
	cw.mention.dismissed = -1
	cw.editor.Submit = true
	c, _ := u.be.ChatByJID(jid)
	cw.title = c.Name
	if cw.title == "" {
		cw.title = jid
	}
	return cw
}

// invalidate chiede di ridisegnare la finestra che mostra la chat.
func (cw *chatWin) invalidate() {
	if cw.host != nil {
		cw.host.w.Invalidate()
	} else if cw.w != nil {
		cw.w.Invalidate()
	}
}

// owner: la finestra di sistema a cui agganciare dialoghi e appunti.
func (cw *chatWin) owner() uintptr {
	if cw.host != nil {
		return cw.host.hwnd
	}
	return cw.hwnd
}
