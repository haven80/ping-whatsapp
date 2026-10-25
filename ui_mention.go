package main

import (
	"image"
	"strings"
	"unicode"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
)

// mentionState: la tendina dei partecipanti che appare scrivendo "@" in un gruppo.
type mentionState struct {
	active    bool
	note      string // "Caricamento partecipanti…" o un errore, quando non ci sono nomi da mostrare
	start     int    // posizione (in caratteri) della @
	items     []Member
	sel       int
	clicks    map[string]*widget.Clickable
	names     map[string]string // "Nome" inserito → JID, usato all'invio
	dismissed int               // posizione della @ chiusa con Esc (-1 = nessuna)
}

func (cw *chatWin) isGroup() bool { return strings.HasSuffix(cw.jid, "@g.us") }

// mentionKeys gestisce frecce, Invio/Tab ed Esc quando la tendina è aperta.
// Va chiamata PRIMA di editor.Update, così i tasti non arrivano al campo di testo.
func (cw *chatWin) mentionKeys(gtx C) {
	ms := &cw.mention
	if !ms.active || len(ms.items) == 0 {
		return
	}
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: &cw.editor, Name: key.NameUpArrow},
			key.Filter{Focus: &cw.editor, Name: key.NameDownArrow},
			key.Filter{Focus: &cw.editor, Name: key.NameReturn},
			key.Filter{Focus: &cw.editor, Name: key.NameEnter},
			key.Filter{Focus: &cw.editor, Name: key.NameTab},
			key.Filter{Focus: &cw.editor, Name: key.NameEscape},
		)
		if !ok {
			break
		}
		ke, ok := ev.(key.Event)
		if !ok || ke.State != key.Press {
			continue
		}
		switch ke.Name {
		case key.NameUpArrow:
			ms.sel = (ms.sel - 1 + len(ms.items)) % len(ms.items)
		case key.NameDownArrow:
			ms.sel = (ms.sel + 1) % len(ms.items)
		case key.NameReturn, key.NameEnter, key.NameTab:
			cw.insertMention(ms.items[ms.sel])
		case key.NameEscape:
			ms.dismissed, ms.active = ms.start, false
		}
	}
}

// updateMention controlla se il cursore sta dopo una "@" e prepara i suggerimenti.
func (cw *chatWin) updateMention() {
	ms := &cw.mention
	ms.active = false
	if !cw.isGroup() || cw.editing != nil {
		return
	}
	text := []rune(cw.editor.Text())
	_, caret := cw.editor.Selection()
	if caret > len(text) {
		caret = len(text)
	}
	at := -1
	for i := caret - 1; i >= 0 && caret-i <= 32; i-- {
		if text[i] == '\n' {
			break
		}
		if text[i] == '@' {
			if i == 0 || unicode.IsSpace(text[i-1]) {
				at = i
			}
			break
		}
	}
	if at < 0 || at == ms.dismissed {
		if at < 0 {
			ms.dismissed = -1
		}
		return
	}
	q := strings.ToLower(string(text[at+1 : caret]))
	members := cw.u.be.GroupMembers(cw.jid)
	var prefix, other []Member
	for _, m := range members {
		n := strings.ToLower(m.Name)
		switch {
		case strings.HasPrefix(n, q):
			prefix = append(prefix, m)
		case q != "" && (strings.Contains(n, " "+q) || strings.Contains(strings.TrimPrefix(n, "+"), q)):
			other = append(other, m)
		}
	}
	items := append(prefix, other...)
	if len(items) > 6 {
		items = items[:6]
	}
	ms.note = ""
	if len(items) == 0 {
		if len(members) > 0 || q != "" && len(q) > 2 {
			return // nessun nome corrisponde: niente tendina
		}
		// lista vuota: dico perché invece di non mostrare nulla
		loading, errText := cw.u.be.MembersStatus(cw.jid)
		switch {
		case errText != "":
			ms.note = errText
		case loading:
			ms.note = "Caricamento partecipanti…"
		default:
			ms.note = "Nessun partecipante trovato"
		}
		ms.active, ms.start, ms.items = true, at, nil
		return
	}
	if ms.start != at {
		ms.sel = 0
	}
	ms.active, ms.start, ms.items = true, at, items
	if ms.sel >= len(items) {
		ms.sel = 0
	}
}

// insertMention sostituisce "@abc" con "@Nome " e ricorda a chi corrisponde.
func (cw *chatWin) insertMention(m Member) {
	ms := &cw.mention
	text := []rune(cw.editor.Text())
	_, caret := cw.editor.Selection()
	if ms.start < 0 || ms.start >= len(text) || caret < ms.start || caret > len(text) {
		return
	}
	ins := []rune("@" + m.Name + " ")
	out := append(append(append([]rune{}, text[:ms.start]...), ins...), text[caret:]...)
	cw.editor.SetText(string(out))
	pos := ms.start + len(ins)
	cw.editor.SetCaret(pos, pos)
	if ms.names == nil {
		ms.names = map[string]string{}
	}
	ms.names[m.Name] = m.JID
	ms.active = false
	cw.focusEditor = true
}

// mentionPopup disegna la tendina sopra il campo di scrittura.
func (cw *chatWin) mentionPopup(gtx C) D {
	ms := &cw.mention
	if !ms.active {
		return D{}
	}
	th, p, be := cw.th, cw.pal, cw.u.be
	if len(ms.items) == 0 {
		mc := op.Record(gtx.Ops)
		d := layout.Inset{Left: 14, Right: 14, Top: 9, Bottom: 9}.Layout(gtx, lbl(th, 12, p.Muted, ms.note).Layout)
		call := mc.Stop()
		fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
		fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
		call.Add(gtx.Ops)
		return d
	}
	if ms.clicks == nil {
		ms.clicks = map[string]*widget.Clickable{}
	}
	for _, m := range ms.items {
		cl := ms.clicks[m.JID]
		if cl == nil {
			cl = new(widget.Clickable)
			ms.clicks[m.JID] = cl
		}
		if cl.Clicked(gtx) {
			cw.insertMention(m)
			return D{}
		}
	}
	mc := op.Record(gtx.Ops)
	var rows []layout.FlexChild
	for i, m := range ms.items {
		i, m := i, m
		cl := ms.clicks[m.JID]
		rows = append(rows, layout.Rigid(func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				rm := op.Record(gtx.Ops)
				d := layout.Inset{Left: 12, Right: 12, Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D { return avatar(gtx, th, p, be, m.JID, m.Name, 26, false, false, p.Surface) }),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, one(lbl(th, 13, p.Text, m.Name)).Layout),
						layout.Rigid(func(gtx C) D {
							if !m.Admin {
								return D{}
							}
							return lbl(th, 10.5, p.Faint, "amministratore").Layout(gtx)
						}),
					)
				})
				call := rm.Stop()
				if i == ms.sel || cl.Hovered() {
					fillRRect(gtx, p.Hover, image.Rect(dp(gtx, 4), 0, d.Size.X-dp(gtx, 4), d.Size.Y), dp(gtx, 8))
				}
				call.Add(gtx.Ops)
				return d
			})
		}))
	}
	d := layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
	})
	call := mc.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
	call.Add(gtx.Ops)
	return d
}
