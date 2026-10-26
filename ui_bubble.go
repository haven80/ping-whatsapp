package main

import (
	"gioui.org/x/richtext"
	"image"
	"image/color"
	"strings"
	"unicode"

	"gioui.org/font"
	"gioui.org/io/event"
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
	icPlay    = ""
	icContact = ""
	icMap     = ""
	icPhone   = ""
	icLink    = ""
	icForward = "\uE72A"
)

// click restituisce il pulsante associato a una chiave e registra l'azione da eseguire.
func (cw *chatWin) click(key string, action func()) *widget.Clickable {
	cl := cw.media[key]
	if cl == nil {
		cl = new(widget.Clickable)
		cw.media[key] = cl
	}
	cw.actions[key] = action
	return cl
}

func (cw *chatWin) openMedia(m *Message) func() {
	chat, id := m.Chat, m.ID
	return func() { cw.u.be.OpenMedia(chat, id) }
}

var quickReactions = []string{"👍", "❤️", "😂", "😮", "😢", "🙏"}

func (cw *chatWin) hoverOf(id string) *widget.Clickable {
	h := cw.hover[id]
	if h == nil {
		h = new(widget.Clickable)
		cw.hover[id] = h
	}
	return h
}

func (cw *chatWin) bubbleRow(gtx C, r chatRow) D {
	m := r.msg
	if m.Kind == KCall {
		return cw.callRow(gtx, m)
	}
	top := unit.Dp(2)
	if r.first {
		top = 7
	}
	canAct := m.Kind != KDeleted && !m.Deleted && !strings.HasPrefix(m.ID, "call-")
	tag := cw.rowTag(m.ID)
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok && pe.Buttons.Contain(pointer.ButtonSecondary) && canAct {
			cw.reactFor, cw.deleteFor = "", ""
			if cw.menuFor == m.ID {
				cw.menuFor = ""
			} else {
				cw.menuFor = m.ID
			}
		}
	}
	return layout.Inset{Top: top}.Layout(gtx, func(gtx C) D {
		hv := cw.hoverOf(m.ID)
		mc := op.Record(gtx.Ops)
		dd := hv.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D {
						full := gtx.Constraints.Max.X
						// foto del mittente accanto ai messaggi (opzione)
						gutter, avSize := 0, float32(30)
						if cw.set.ChatPhotos {
							gutter = dp(gtx, avSize+8)
						}
						maxW := (full - gutter) * 80 / 100
						g := gtx
						g.Constraints.Min = image.Point{}
						g.Constraints.Max.X = maxW
						mc := op.Record(gtx.Ops)
						d := cw.bubble(g, m, r.first)
						call := mc.Stop()
						x := gutter
						if m.FromMe {
							x = full - gutter - d.Size.X
						}
						st := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
						call.Add(gtx.Ops)
						st.Pop()
						if gutter > 0 && r.first {
							ax := 0
							if m.FromMe {
								ax = full - dp(gtx, avSize)
							}
							st := op.Offset(image.Pt(ax, 0)).Push(gtx.Ops)
							if !m.FromMe && m.SenderName != "" {
								sender, name, msg := m.Sender, m.SenderName, m
								cw.click("sav:"+m.ID, func() { cw.showProfile(sender, name) }).Layout(gtx, func(gtx C) D {
									pointer.CursorPointer.Add(gtx.Ops)
									return cw.senderAvatar(gtx, msg, avSize)
								})
							} else {
								cw.senderAvatar(gtx, m, avSize)
							}
							st.Pop()
						}
						// azioni al passaggio del mouse
						if canAct && (hv.Hovered() || cw.reactFor == m.ID || cw.deleteFor == m.ID || cw.menuFor == m.ID) {
							bw := dp(gtx, 28)
							gapX := dp(gtx, 6)
							n := cw.actionCount(m)
							total := n*bw + (n-1)*dp(gtx, 4)
							bx := x + d.Size.X + gapX
							if m.FromMe {
								bx = x - gapX - total
							}
							by := max(0, min(d.Size.Y-bw, dp(gtx, 4)))
							if bx < 0 || bx+total > full {
								// non c'è spazio di fianco (messaggio lungo): sopra l'angolo esterno della bolla
								by = -dp(gtx, 6)
								if m.FromMe {
									bx = x - dp(gtx, 4)
								} else {
									bx = x + d.Size.X - total + dp(gtx, 4)
								}
								bx = max(0, min(bx, full-total))
							}
							st := op.Offset(image.Pt(bx, by)).Push(gtx.Ops)
							cw.actionButtons(gtx, m)
							st.Pop()
						}
						return D{Size: image.Pt(full, d.Size.Y)}
					})
				}),
				layout.Rigid(func(gtx C) D {
					switch {
					case cw.reactFor == m.ID:
						return cw.reactionBar(gtx, m)
					case cw.deleteFor == m.ID:
						return cw.deleteBar(gtx, m)
					case cw.menuFor == m.ID:
						return cw.menuBar(gtx, m)
					}
					return D{}
				}),
			)
		})
		call := mc.Stop()
		// area sensibile al tasto destro, grande quanto la riga
		// i contenuti stanno DENTRO l'area: così riceve anche i clic fatti sopra la bolla
		st := clip.Rect{Max: dd.Size}.Push(gtx.Ops)
		event.Op(gtx.Ops, tag)
		call.Add(gtx.Ops)
		st.Pop()
		return dd
	})
}

type rowTagT struct{ id string }

func (cw *chatWin) rowTag(id string) *rowTagT {
	t := cw.rowTags[id]
	if t == nil {
		t = &rowTagT{id}
		cw.rowTags[id] = t
	}
	return t
}

// menuBar: azioni con il nome scritto (aperto col tasto destro sul messaggio).
func (cw *chatWin) menuBar(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	msg := m
	side := unit.Dp(12)
	if cw.set.ChatPhotos {
		side += 38
	}
	chip := func(key, label string, col color.NRGBA, action func()) layout.FlexChild {
		cl := cw.click(key+m.ID, func() {
			cw.menuFor = ""
			action()
		})
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 6, Bottom: 4}.Layout(gtx, func(gtx C) D {
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					bg := p.Surface
					if cl.Hovered() {
						bg = p.Hover
					}
					return rounded(gtx, bg, 14, 12, 6, bold(lbl(th, 12, col, label)).Layout)
				})
			})
		})
	}
	items := []layout.FlexChild{
		chip("m-reply:", "Rispondi", p.Text, func() { cw.startReply(msg) }),
		chip("m-react:", "Reagisci", p.Text, func() { cw.reactFor = msg.ID }),
	}
	if cw.u.be.CanForward(m) {
		items = append(items, chip("m-fwd:", "Inoltra", p.Text, func() { cw.startForward(msg) }))
	}
	if text := cleanEdited(msg.Text); strings.TrimSpace(text) != "" {
		items = append(items, chip("m-copy:", "Copia", p.Text, func() { cw.copyText = text }))
	}
	if CanEdit(m) {
		items = append(items, chip("m-edit:", "Modifica", p.Text, func() { cw.startEdit(msg) }))
	}
	items = append(items, chip("m-del:", "Elimina", p.Danger, func() { cw.deleteFor = msg.ID }))
	return layout.Inset{Left: side, Right: side, Top: 4, Bottom: 2}.Layout(gtx, func(gtx C) D {
		mc := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		d := layout.Flex{Alignment: layout.Middle}.Layout(g, items...)
		call := mc.Stop()
		x := 0
		if m.FromMe {
			x = max(0, gtx.Constraints.Max.X-d.Size.X)
		}
		st := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
	})
}

func roundBtn(gtx C, th *material.Theme, p Pal, cl *widget.Clickable, w layout.Widget) D {
	return cl.Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		s := dp(gtx, 28)
		r := image.Rectangle{Max: image.Pt(s, s)}
		bg := p.Surface
		if cl.Hovered() {
			bg = p.Hover
		}
		fillCircle(gtx, p.Divider, r.Inset(-1))
		fillCircle(gtx, bg, r)
		g := gtx
		g.Constraints = layout.Exact(r.Max)
		layout.Center.Layout(g, w)
		return D{Size: r.Max}
	})
}

func (cw *chatWin) actionButtons(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	msg := m
	var btns []layout.FlexChild
	addBtn := func(cl *widget.Clickable, w layout.Widget) {
		if len(btns) > 0 {
			btns = append(btns, layout.Rigid(layout.Spacer{Width: 4}.Layout))
		}
		btns = append(btns, layout.Rigid(func(gtx C) D { return roundBtn(gtx, th, p, cl, w) }))
	}
	addBtn(cw.click("reply:"+m.ID, func() { cw.startReply(msg) }), icon(th, 12, p.Muted, "\uE97A").Layout)
	addBtn(cw.click("react:"+m.ID, func() {
		cw.deleteFor = ""
		if cw.reactFor == msg.ID {
			cw.reactFor = ""
		} else {
			cw.reactFor = msg.ID
		}
	}), lbl(th, 13, p.Text, "🙂").Layout)
	if cw.u.be.CanForward(m) {
		addBtn(cw.click("fwd:"+m.ID, func() { cw.startForward(msg) }), icon(th, 12, p.Muted, icForward).Layout)
	}
	if CanEdit(m) {
		addBtn(cw.click("edit:"+m.ID, func() { cw.startEdit(msg) }), icon(th, 12, p.Muted, "\uE70F").Layout)
	}
	addBtn(cw.click("del:"+m.ID, func() {
		cw.reactFor = ""
		if cw.deleteFor == msg.ID {
			cw.deleteFor = ""
		} else {
			cw.deleteFor = msg.ID
		}
	}), icon(th, 12, p.Muted, "\uE74D").Layout)
	return layout.Flex{}.Layout(gtx, btns...)
}

// actionCount: quanti pulsanti mostra actionButtons (per calcolarne la larghezza).
func (cw *chatWin) actionCount(m *Message) int {
	n := 3
	if CanEdit(m) {
		n++
	}
	if cw.u.be.CanForward(m) {
		n++
	}
	return n
}

// deleteBar: conferma della cancellazione sotto il messaggio.
func (cw *chatWin) deleteBar(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	msg := m
	side := unit.Dp(12)
	if cw.set.ChatPhotos {
		side += 38
	}
	chip := func(cl *widget.Clickable, label string, col, bg color.NRGBA) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 6}.Layout(gtx, func(gtx C) D {
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					b := bg
					if cl.Hovered() {
						b = p.Hover
					}
					return rounded(gtx, b, 14, 12, 6, bold(lbl(th, 12, col, label)).Layout)
				})
			})
		})
	}
	var items []layout.FlexChild
	if CanRevoke(m) {
		items = append(items, chip(cw.click("delall:"+m.ID, func() {
			cw.u.be.DeleteForEveryone(msg)
			cw.deleteFor = ""
		}), "Elimina per tutti", p.Danger, p.Surface))
	}
	items = append(items,
		chip(cw.click("delme:"+m.ID, func() {
			cw.u.be.DeleteForMe(msg)
			cw.deleteFor = ""
		}), "Elimina per me", p.Danger, p.Surface),
		chip(cw.click("delno:"+m.ID, func() { cw.deleteFor = "" }), "Annulla", p.Muted, p.Surface),
	)
	return layout.Inset{Left: side, Right: side, Top: 4, Bottom: 2}.Layout(gtx, func(gtx C) D {
		mc := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		d := layout.Flex{Alignment: layout.Middle}.Layout(g, items...)
		call := mc.Stop()
		x := 0
		if m.FromMe {
			x = gtx.Constraints.Max.X - d.Size.X
		}
		st := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		call.Add(gtx.Ops)
		st.Pop()
		return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
	})
}

// reactionBar: emoji rapide sotto il messaggio, più "＋" per sceglierne un'altra.
func (cw *chatWin) reactionBar(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	msg := m
	mine := ""
	for _, r := range m.Reactions {
		if r.Sender == "me" {
			mine = r.Emoji
		}
	}
	side := unit.Dp(12)
	if cw.set.ChatPhotos {
		side += 38
	}
	return layout.Inset{Left: side, Right: side, Top: 4, Bottom: 2}.Layout(gtx, func(gtx C) D {
		mc := op.Record(gtx.Ops)
		g := gtx
		g.Constraints.Min = image.Point{}
		var items []layout.FlexChild
		for _, e := range quickReactions {
			e := e
			cl := cw.click("r:"+m.ID+e, func() {
				cw.u.be.React(msg, e)
				cw.reactFor = ""
			})
			sel := e == mine
			items = append(items, layout.Rigid(func(gtx C) D {
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					s := dp(gtx, 34)
					r := image.Rectangle{Max: image.Pt(s, s)}
					if sel {
						fillCircle(gtx, argb(0x1F9D7A, 0x40), r)
					} else if cl.Hovered() {
						fillCircle(gtx, p.Hover, r)
					}
					g := gtx
					g.Constraints = layout.Exact(r.Max)
					layout.Center.Layout(g, lbl(th, 19, p.Text, e).Layout)
					return D{Size: r.Max}
				})
			}))
		}
		more := cw.click("r+:"+m.ID, func() {
			cw.reactPick = msg
			cw.reactFor = ""
			if !cw.emoji.open {
				cw.toggleEmoji()
			}
		})
		items = append(items, layout.Rigid(func(gtx C) D {
			return more.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				s := dp(gtx, 34)
				r := image.Rectangle{Max: image.Pt(s, s)}
				if more.Hovered() {
					fillCircle(gtx, p.Hover, r)
				}
				g := gtx
				g.Constraints = layout.Exact(r.Max)
				layout.Center.Layout(g, bold(lbl(th, 18, p.Muted, "+")).Layout)
				return D{Size: r.Max}
			})
		}))
		d := layout.Inset{Left: 4, Right: 4, Top: 2, Bottom: 2}.Layout(g, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, items...)
		})
		call := mc.Stop()
		x := 0
		if m.FromMe {
			x = gtx.Constraints.Max.X - d.Size.X
		}
		st := op.Offset(image.Pt(x, 0)).Push(gtx.Ops)
		rr := image.Rectangle{Max: d.Size}
		fillRRect(gtx, p.Divider, rr.Inset(-1), d.Size.Y/2+1)
		fillRRect(gtx, p.Surface, rr, d.Size.Y/2)
		call.Add(gtx.Ops)
		st.Pop()
		return D{Size: image.Pt(gtx.Constraints.Max.X, d.Size.Y)}
	})
}

func (cw *chatWin) callRow(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	return layout.Inset{Top: 8, Bottom: 4}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Center.Layout(gtx, func(gtx C) D {
			return rounded(gtx, p.Surface, 10, 12, 6, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(icon(th, 12, p.Danger, icPhone).Layout),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(lbl(th, 12.5, p.Text, m.Text+" · "+m.Time.Local().Format("15:04")).Layout),
					layout.Rigid(layout.Spacer{Width: 6}.Layout),
					layout.Rigid(lbl(th, 11.5, p.Faint, "rispondi dal telefono").Layout),
				)
			})
		})
	})
}

func subtleBg(p Pal) color.NRGBA {
	if p.Dark {
		return argb(0xFFFFFF, 0x10)
	}
	return argb(0x000000, 0x0b)
}

func (cw *chatWin) bubble(gtx C, m *Message, first bool) D {
	th, p := cw.th, cw.pal

	// gli sticker non hanno la bolla
	if m.Kind == KSticker {
		return cw.withReactions(gtx, m, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
				layout.Rigid(func(gtx C) D { return cw.sticker(gtx, m) }),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 2}.Layout(gtx, func(gtx C) D {
						return rounded(gtx, subtleBg(p), 6, 6, 1, func(gtx C) D { return cw.meta(gtx, m) })
					})
				}),
			)
		})
	}

	bg := p.Surface
	if m.FromMe {
		bg = p.Mine
	}
	var parts []layout.FlexChild
	add := func(w layout.Widget) { parts = append(parts, layout.Rigid(w)) }
	gap := func(h unit.Dp) { add(layout.Spacer{Height: h}.Layout) }

	if m.Deleted { // eliminato da chi l'ha scritto, mostrato perché l'opzione è attiva
		add(func(gtx C) D {
			l := bold(lbl(th, 11, p.Danger, "Eliminato da chi l'ha scritto"))
			l.Font.Style = font.Italic
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(icon(th, 10, p.Danger, "\uE74D").Layout),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(l.Layout),
			)
		})
		gap(3)
	}
	if m.Extra.Forwarded {
		add(func(gtx C) D {
			l := lbl(th, 11, p.Faint, "Inoltrato")
			l.Font.Style = font.Italic
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(icon(th, 10, p.Faint, icForward).Layout),
				layout.Rigid(layout.Spacer{Width: 4}.Layout),
				layout.Rigid(l.Layout),
			)
		})
		gap(2)
	}
	if first && !m.FromMe && m.SenderName != "" {
		// clic sul nome: chat privata con quel partecipante
		sender, name := m.Sender, m.SenderName
		cl := cw.click("snd:"+m.ID, func() { cw.showProfile(sender, name) })
		add(func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				col := hashColor(sender)
				mc := op.Record(gtx.Ops)
				d := one(bold(lbl(th, 12, col, name))).Layout(gtx)
				call := mc.Stop()
				call.Add(gtx.Ops)
				if cl.Hovered() { // sottolineatura al passaggio del mouse
					fillRect(gtx, col, image.Rect(0, d.Size.Y-dp(gtx, 1), d.Size.X, d.Size.Y))
				}
				return d
			})
		})
		gap(2)
	}
	if m.QuoteText != "" {
		qid := m.QuoteID
		add(func(gtx C) D {
			return cw.click("q:"+m.ID, func() { cw.jumpTo(qid) }).Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				return cw.quote(gtx, m)
			})
		})
		gap(4)
	}
	if m.Extra.ViewOnce {
		add(lbl(th, 11, p.Faint, "① Visualizza una volta").Layout)
		gap(3)
	}

	switch m.Kind {
	case KImage, KVideo:
		add(func(gtx C) D { return cw.picture(gtx, m) })
	case KAudio:
		add(func(gtx C) D { return cw.audio(gtx, m) })
	case KDoc:
		add(func(gtx C) D { return cw.document(gtx, m) })
	case KLocation:
		add(func(gtx C) D { return cw.location(gtx, m) })
	case KContact:
		add(func(gtx C) D { return cw.contact(gtx, m) })
	case KPoll:
		add(func(gtx C) D { return cw.poll(gtx, m) })
	case KEvent:
		add(func(gtx C) D { return cw.event(gtx, m) })
	case KDeleted:
		add(func(gtx C) D {
			l := lbl(th, 13, p.Faint, "Messaggio eliminato")
			l.Font.Style = font.Italic
			return l.Layout(gtx)
		})
	}

	// testo / didascalia
	text := m.Text
	if m.Kind == KAudio || m.Kind == KDeleted || m.Kind == KEvent {
		text = ""
	}
	if strings.TrimSpace(text) != "" {
		if m.Kind != KText {
			gap(4)
		}
		sel := cw.sel[m.ID]
		if sel == nil {
			sel = new(widget.Selectable)
			cw.sel[m.ID] = sel
		}
		size := float32(13.5)
		if m.Kind == KText && emojiOnly(text) {
			size = 32
		}
		if spans := linkSpans(text); spans != nil {
			add(func(gtx C) D { return cw.richText(gtx, m.ID, spans, size) })
		} else {
			add(func(gtx C) D {
				l := lbl(th, size, p.Text, text)
				l.State = sel
				l.SelectionColor = argb(0x1F9D7A, 0x55)
				return l.Layout(gtx)
			})
		}
	}
	// anteprima del link
	if m.Kind == KText && m.Extra.URL != "" {
		gap(5)
		add(func(gtx C) D { return cw.linkCard(gtx, m) })
	}

	return cw.withReactions(gtx, m, func(gtx C) D {
		padX, padT, padB := dp(gtx, 10), dp(gtx, 6), dp(gtx, 5)
		inner := gtx
		inner.Constraints.Min = image.Point{}
		inner.Constraints.Max.X = max(0, gtx.Constraints.Max.X-2*padX)
		mc := op.Record(gtx.Ops)
		dc := layout.Flex{Axis: layout.Vertical}.Layout(inner, parts...)
		contentCall := mc.Stop()
		mm := op.Record(gtx.Ops)
		dm := cw.meta(inner, m)
		metaCall := mm.Stop()

		w := max(dc.Size.X, dm.Size.X) + 2*padX
		h := padT + dc.Size.Y + dp(gtx, 2) + dm.Size.Y + padB
		size := image.Pt(w, h)
		rad := dp(gtx, 12)
		rr := clip.RRect{Rect: image.Rectangle{Max: size}, NW: rad, NE: rad, SE: rad, SW: rad}
		if first {
			small := dp(gtx, 4)
			if m.FromMe {
				rr.NE = small
			} else {
				rr.NW = small
			}
		}
		paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
		if !m.FromMe && !p.Dark {
			paint.FillShape(gtx.Ops, argb(0x000000, 0x0d), clip.Stroke{Path: rr.Path(gtx.Ops), Width: 1}.Op())
		}
		st := op.Offset(image.Pt(padX, padT)).Push(gtx.Ops)
		contentCall.Add(gtx.Ops)
		st.Pop()
		st = op.Offset(image.Pt(w-padX-dm.Size.X, padT+dc.Size.Y+dp(gtx, 2))).Push(gtx.Ops)
		metaCall.Add(gtx.Ops)
		st.Pop()
		return D{Size: size}
	})
}

func (cw *chatWin) meta(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(lbl(th, 10.5, p.Faint, m.Time.Local().Format("15:04")).Layout),
		layout.Rigid(func(gtx C) D {
			if !m.FromMe {
				return D{}
			}
			return layout.Inset{Left: 4}.Layout(gtx, func(gtx C) D { return ticks(gtx, th, p, m.Status) })
		}),
	)
}

// withReactions disegna la bolla e, se ci sono reazioni, la pastiglia con le emoji sotto.
func (cw *chatWin) withReactions(gtx C, m *Message, w layout.Widget) D {
	if len(m.Reactions) == 0 {
		return w(gtx)
	}
	th, p := cw.th, cw.pal
	mb := op.Record(gtx.Ops)
	db := w(gtx)
	bubble := mb.Stop()

	var uniq []string
	seen := map[string]bool{}
	for _, r := range m.Reactions {
		if !seen[r.Emoji] {
			seen[r.Emoji] = true
			uniq = append(uniq, r.Emoji)
		}
	}
	label := strings.Join(uniq, "")
	if len(m.Reactions) > 1 {
		label += " " + itoa(len(m.Reactions))
	}
	mp := op.Record(gtx.Ops)
	g := gtx
	g.Constraints.Min = image.Point{}
	dpill := layout.Inset{Left: 6, Right: 6, Top: 1, Bottom: 2}.Layout(g, lbl(th, 12, p.Muted, label).Layout)
	pill := mp.Stop()

	overlap := dp(gtx, 5)
	total := image.Pt(max(db.Size.X, dpill.Size.X+dp(gtx, 16)), db.Size.Y+dpill.Size.Y-overlap)
	offX := 0
	if m.FromMe {
		offX = total.X - db.Size.X
	}
	st := op.Offset(image.Pt(offX, 0)).Push(gtx.Ops)
	bubble.Add(gtx.Ops)
	st.Pop()
	px := dp(gtx, 8)
	if m.FromMe {
		px = total.X - dpill.Size.X - dp(gtx, 8)
	}
	st = op.Offset(image.Pt(px, db.Size.Y-overlap)).Push(gtx.Ops)
	r := image.Rectangle{Max: dpill.Size}
	fillRRect(gtx, p.Bg, r.Inset(-dp(gtx, 2)), dpill.Size.Y/2+dp(gtx, 2))
	fillRRect(gtx, p.Surface, r, dpill.Size.Y/2)
	pill.Add(gtx.Ops)
	st.Pop()
	return D{Size: total}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}

// emojiOnly: messaggi fatti solo di 1-3 emoji si mostrano grandi, come su WhatsApp.
func emojiOnly(s string) bool {
	s = strings.TrimSpace(s)
	n := 0
	for _, r := range s {
		switch {
		case r == 0x200D || r == 0xFE0F || (r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F):
			// unione, variante, carnagione, bandiere regionali: non contano
		case r >= 0x1F1E6 && r <= 0x1F1FF:
			n++ // lettere delle bandiere: due per bandiera
		case unicode.IsSpace(r):
		case r >= 0x2190 && !unicode.IsLetter(r) && !unicode.IsDigit(r):
			n++
		default:
			return false
		}
	}
	return n > 0 && n <= 4
}

// ---- contenuti ----

func (cw *chatWin) quote(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	col := hashColor(m.QuoteSender)
	if m.QuoteSender == "Tu" {
		col = p.Accent
	}
	mc := op.Record(gtx.Ops)
	d := layout.Inset{Left: 10, Right: 8, Top: 4, Bottom: 5}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(one(bold(lbl(th, 11.5, col, pick(m.QuoteSender, "Messaggio")))).Layout),
			layout.Rigid(func(gtx C) D {
				l := lbl(th, 12, p.Muted, m.QuoteText)
				l.MaxLines = 2
				return l.Layout(gtx)
			}),
		)
	})
	call := mc.Stop()
	r := image.Rectangle{Max: d.Size}
	rad := dp(gtx, 6)
	st := clip.UniformRRect(r, rad).Push(gtx.Ops)
	paint.Fill(gtx.Ops, subtleBg(p))
	fillRect(gtx, col, image.Rect(0, 0, dp(gtx, 3), d.Size.Y))
	st.Pop()
	call.Add(gtx.Ops)
	return d
}

// boxFor calcola le dimensioni di un'immagine entro i limiti, rispettando le proporzioni.
func boxFor(gtx C, src image.Point, maxW, maxH int) image.Point {
	if src.X <= 0 || src.Y <= 0 {
		src = image.Pt(4, 3)
	}
	w := maxW
	h := w * src.Y / src.X
	if h > maxH {
		h = maxH
		w = h * src.X / src.Y
	}
	if w < dp(gtx, 80) {
		w = dp(gtx, 80)
	}
	return image.Pt(w, h)
}

// drawImage disegna un'immagine scalata nel riquadro (riempiendolo, ritagliando il superfluo).
func drawImage(gtx C, imgOp paint.ImageOp, src image.Point, box image.Point) {
	sc := max(float32(box.X)/float32(src.X), float32(box.Y)/float32(src.Y))
	off := image.Pt((box.X-int(float32(src.X)*sc))/2, (box.Y-int(float32(src.Y)*sc))/2)
	st := op.Offset(off).Push(gtx.Ops)
	tr := op.Affine(f32Scale(sc)).Push(gtx.Ops)
	imgOp.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	tr.Pop()
	st.Pop()
}

func (cw *chatWin) picture(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	var full paint.ImageOp
	var fullSize image.Point
	state := MediaNone
	if m.Kind == KImage && (m.Media != nil || m.MediaPath != "") {
		full, fullSize, state = cw.u.be.MediaImage(m.Chat, m.ID)
	}
	tOp, tSize, hasThumb := paint.ImageOp{}, image.Point{}, false
	if len(m.Thumb) > 0 {
		tOp, tSize, hasThumb = thumbOp(m.Chat+"/"+m.ID, m.Thumb)
	}
	// GIF: si animano direttamente nella chat
	var anim *Anim
	if m.Kind == KVideo && m.Extra.GIF && (m.Media != nil || m.MediaPath != "") {
		var ast int
		anim, ast = cw.u.be.AnimOf(m.Chat, m.ID)
		if ast == MediaLoading {
			state = MediaLoading
		}
	}
	src := image.Pt(int(m.Extra.W), int(m.Extra.H))
	switch {
	case anim != nil:
		src = anim.size
	case state == MediaReady:
		src = fullSize
	case src.X == 0 && hasThumb:
		src = tSize
	}
	box := boxFor(gtx, src, min(dp(gtx, 260), gtx.Constraints.Max.X), dp(gtx, 320))
	return cw.click("m:"+m.ID, cw.openMedia(m)).Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		r := image.Rectangle{Max: box}
		st := clip.UniformRRect(r, dp(gtx, 8)).Push(gtx.Ops)
		paint.Fill(gtx.Ops, subtleBg(p))
		switch {
		case anim != nil:
			fr, next := anim.Frame(gtx.Now)
			drawImage(gtx, fr, anim.size, box)
			if !next.IsZero() {
				gtx.Execute(op.InvalidateCmd{At: next})
			}
		case state == MediaReady:
			drawImage(gtx, full, fullSize, box)
		case hasThumb:
			drawImage(gtx, tOp, tSize, box)
		}
		if state == MediaLoading {
			s := dp(gtx, 26)
			g := gtx
			g.Constraints = layout.Exact(image.Pt(s, s))
			st2 := op.Offset(image.Pt(box.X/2-s/2, box.Y/2-s/2)).Push(gtx.Ops)
			l := material.Loader(th)
			l.Color = rgb(0xFFFFFF)
			l.Layout(g)
			st2.Pop()
		}
		if m.Kind == KVideo && anim != nil {
			// GIF in riproduzione: solo l'etichetta
			g := gtx
			g.Constraints.Min = image.Point{}
			mc := op.Record(gtx.Ops)
			d := layout.Inset{Left: 6, Right: 6, Top: 1, Bottom: 2}.Layout(g, bold(lbl(th, 10.5, rgb(0xFFFFFF), "GIF")).Layout)
			call := mc.Stop()
			st3 := op.Offset(image.Pt(dp(gtx, 6), box.Y-d.Size.Y-dp(gtx, 6))).Push(gtx.Ops)
			fillRRect(gtx, argb(0x000000, 0x80), image.Rectangle{Max: d.Size}, d.Size.Y/2)
			call.Add(gtx.Ops)
			st3.Pop()
		} else if m.Kind == KVideo {
			c := image.Pt(box.X/2, box.Y/2)
			rad := dp(gtx, 20)
			fillCircle(gtx, argb(0x000000, 0x90), image.Rect(c.X-rad, c.Y-rad, c.X+rad, c.Y+rad))
			var path clip.Path
			path.Begin(gtx.Ops)
			s := float32(dp(gtx, 8))
			path.MoveTo(f32pt(float32(c.X)-s*0.6, float32(c.Y)-s))
			path.LineTo(f32pt(float32(c.X)+s, float32(c.Y)))
			path.LineTo(f32pt(float32(c.X)-s*0.6, float32(c.Y)+s))
			path.Close()
			paint.FillShape(gtx.Ops, rgb(0xFFFFFF), clip.Outline{Path: path.End()}.Op())
			badge := dur(m.Extra.Seconds)
			if m.Extra.GIF {
				badge = "GIF"
			}
			if badge != "0:00" {
				g := gtx
				g.Constraints.Min = image.Point{}
				mc := op.Record(gtx.Ops)
				d := layout.Inset{Left: 6, Right: 6, Top: 1, Bottom: 2}.Layout(g, bold(lbl(th, 10.5, rgb(0xFFFFFF), badge)).Layout)
				call := mc.Stop()
				o := image.Pt(dp(gtx, 6), box.Y-d.Size.Y-dp(gtx, 6))
				st3 := op.Offset(o).Push(gtx.Ops)
				fillRRect(gtx, argb(0x000000, 0x90), image.Rectangle{Max: d.Size}, d.Size.Y/2)
				call.Add(gtx.Ops)
				st3.Pop()
			}
		}
		st.Pop()
		return D{Size: box}
	})
}

func (cw *chatWin) sticker(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	var full paint.ImageOp
	var fullSize image.Point
	state := MediaNone
	var anim *Anim
	if m.Extra.Animated {
		anim, state = cw.u.be.AnimOf(m.Chat, m.ID)
	} else {
		full, fullSize, state = cw.u.be.MediaImage(m.Chat, m.ID)
		if state == MediaFailed { // magari era animato senza dichiararlo
			anim, state = cw.u.be.AnimOf(m.Chat, m.ID)
		}
	}
	s := dp(gtx, 140)
	box := image.Pt(s, s)
	return cw.click("m:"+m.ID, cw.openMedia(m)).Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		switch {
		case anim != nil:
			fr, next := anim.Frame(gtx.Now)
			sc := min(float32(s)/float32(anim.size.X), float32(s)/float32(anim.size.Y))
			w, h := int(float32(anim.size.X)*sc), int(float32(anim.size.Y)*sc)
			st := op.Offset(image.Pt((s-w)/2, (s-h)/2)).Push(gtx.Ops)
			tr := op.Affine(f32Scale(sc)).Push(gtx.Ops)
			fr.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			st.Pop()
			if !next.IsZero() {
				gtx.Execute(op.InvalidateCmd{At: next})
			}
		case state == MediaReady:
			sc := min(float32(s)/float32(fullSize.X), float32(s)/float32(fullSize.Y))
			w, h := int(float32(fullSize.X)*sc), int(float32(fullSize.Y)*sc)
			st := op.Offset(image.Pt((s-w)/2, (s-h)/2)).Push(gtx.Ops)
			tr := op.Affine(f32Scale(sc)).Push(gtx.Ops)
			full.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			st.Pop()
		case len(m.Thumb) > 0:
			if tOp, tSize, ok := thumbOp(m.Chat+"/"+m.ID, m.Thumb); ok {
				sc := min(float32(s)/float32(tSize.X), float32(s)/float32(tSize.Y))
				tr := op.Affine(f32Scale(sc)).Push(gtx.Ops)
				tOp.Add(gtx.Ops)
				paint.PaintOp{}.Add(gtx.Ops)
				tr.Pop()
			}
		case state == MediaLoading:
			fillRRect(gtx, subtleBg(p), image.Rectangle{Max: box}, dp(gtx, 12))
		default:
			// sticker animato o non scaricabile: segnaposto
			fillRRect(gtx, subtleBg(p), image.Rectangle{Max: box}, dp(gtx, 12))
			g := gtx
			g.Constraints = layout.Exact(box)
			layout.Center.Layout(g, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(lbl(th, 34, p.Text, "💟").Layout),
					layout.Rigid(lbl(th, 11, p.Muted, "Sticker animato").Layout),
				)
			})
		}
		return D{Size: box}
	})
}

// chip: riga cliccabile con icona tonda, titolo e sottotitolo.
func (cw *chatWin) chip(gtx C, key string, action func(), glyph string, title, sub string) D {
	th, p := cw.th, cw.pal
	w := func(gtx C) D {
		return rounded(gtx, subtleBg(p), 8, 9, 7, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					s := dp(gtx, 34)
					r := image.Rectangle{Max: image.Pt(s, s)}
					fillCircle(gtx, p.Accent, r)
					g := gtx
					g.Constraints = layout.Exact(r.Max)
					layout.Center.Layout(g, icon(th, 14, p.AccentTxt, glyph).Layout)
					return D{Size: r.Max}
				}),
				layout.Rigid(layout.Spacer{Width: 10}.Layout),
				layout.Flexed(1, func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							l := lbl(th, 13, p.Text, title)
							l.MaxLines = 2
							l.Font.Weight = font.Medium
							return l.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							if sub == "" {
								return D{}
							}
							return one(lbl(th, 11.5, p.Muted, sub)).Layout(gtx)
						}),
					)
				}),
			)
		})
	}
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, dp(gtx, 280))
	if action == nil {
		return w(gtx)
	}
	return cw.click(key, action).Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		return w(gtx)
	})
}

func (cw *chatWin) audio(gtx C, m *Message) D {
	sub := dur(m.Extra.Seconds) + " · clic per ascoltare"
	return cw.chip(gtx, "m:"+m.ID, cw.openMedia(m), icPlay, pick(m.Text, "Messaggio vocale"), sub)
}

func (cw *chatWin) document(gtx C, m *Message) D {
	var info []string
	if ext := strings.TrimPrefix(strings.ToUpper(fileExt(m.FileName)), "."); ext != "" {
		info = append(info, ext)
	}
	if m.Extra.Pages > 0 {
		info = append(info, itoa(int(m.Extra.Pages))+" pagine")
	}
	if s := humanSize(m.Extra.Size); s != "" {
		info = append(info, s)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			if len(m.Thumb) == 0 {
				return D{}
			}
			tOp, tSize, ok := thumbOp(m.Chat+"/"+m.ID, m.Thumb)
			if !ok {
				return D{}
			}
			box := boxFor(gtx, tSize, min(dp(gtx, 260), gtx.Constraints.Max.X), dp(gtx, 140))
			return layout.Inset{Bottom: 4}.Layout(gtx, func(gtx C) D {
				st := clip.UniformRRect(image.Rectangle{Max: box}, dp(gtx, 6)).Push(gtx.Ops)
				drawImage(gtx, tOp, tSize, box)
				st.Pop()
				return D{Size: box}
			})
		}),
		layout.Rigid(func(gtx C) D {
			return cw.chip(gtx, "m:"+m.ID, cw.openMedia(m), icDoc, pick(m.FileName, "Documento"), strings.Join(info, " · "))
		}),
	)
}

func fileExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 && len(name)-i <= 6 {
		return name[i:]
	}
	return ""
}

func (cw *chatWin) location(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	u := m.Extra.URL
	open := func() { go openPath(u) }
	return cw.click("u:"+m.ID, open).Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				if len(m.Thumb) == 0 {
					return D{}
				}
				tOp, tSize, ok := thumbOp(m.Chat+"/"+m.ID, m.Thumb)
				if !ok {
					return D{}
				}
				box := boxFor(gtx, tSize, min(dp(gtx, 240), gtx.Constraints.Max.X), dp(gtx, 160))
				return layout.Inset{Bottom: 5}.Layout(gtx, func(gtx C) D {
					st := clip.UniformRRect(image.Rectangle{Max: box}, dp(gtx, 8)).Push(gtx.Ops)
					drawImage(gtx, tOp, tSize, box)
					c := image.Pt(box.X/2, box.Y/2)
					rr := dp(gtx, 7)
					fillCircle(gtx, rgb(0xFFFFFF), image.Rect(c.X-rr-2, c.Y-rr-2, c.X+rr+2, c.Y+rr+2))
					fillCircle(gtx, p.Danger, image.Rect(c.X-rr, c.Y-rr, c.X+rr, c.Y+rr))
					st.Pop()
					return D{Size: box}
				})
			}),
			layout.Rigid(func(gtx C) D {
				if m.Extra.Title == "" {
					return one(bold(lbl(th, 13, p.Text, "📍 Posizione"))).Layout(gtx)
				}
				return one(bold(lbl(th, 13, p.Text, "📍 "+m.Extra.Title))).Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if m.Extra.Desc == "" {
					return D{}
				}
				l := lbl(th, 12, p.Muted, m.Extra.Desc)
				l.MaxLines = 2
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Top: 2}.Layout(gtx, lbl(th, 12, p.Accent, "Apri in Google Maps").Layout)
			}),
		)
	})
}

func (cw *chatWin) contact(gtx C, m *Message) D {
	return cw.chip(gtx, "", nil, icContact, pick(m.Extra.Title, "Contatto"), strings.Join(m.Extra.Options, " · "))
}

func (cw *chatWin) poll(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	ch := []layout.FlexChild{
		layout.Rigid(bold(lbl(th, 13.5, p.Text, "📊 "+m.Extra.Title)).Layout),
		layout.Rigid(layout.Spacer{Height: 6}.Layout),
	}
	for _, o := range m.Extra.Options {
		o := o
		ch = append(ch, layout.Rigid(func(gtx C) D {
			return layout.Inset{Bottom: 5}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						s := dp(gtx, 14)
						r := image.Rectangle{Max: image.Pt(s, s)}
						paint.FillShape(gtx.Ops, p.Faint, clip.Stroke{Path: clip.Ellipse(r).Path(gtx.Ops), Width: float32(dp(gtx, 1.5))}.Op())
						return D{Size: r.Max}
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(lbl(th, 13, p.Text, o).Layout),
				)
			})
		}))
	}
	ch = append(ch, layout.Rigid(func(gtx C) D {
		l := lbl(th, 11, p.Faint, "Per votare usa il telefono")
		l.Font.Style = font.Italic
		return l.Layout(gtx)
	}))
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
}

func (cw *chatWin) event(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(bold(lbl(th, 13.5, p.Text, "📅 "+pick(m.Extra.Title, "Evento"))).Layout),
		layout.Rigid(func(gtx C) D {
			if m.Text == "" {
				return D{}
			}
			return lbl(th, 12, p.Accent, m.Text).Layout(gtx)
		}),
		layout.Rigid(func(gtx C) D {
			if m.Extra.Desc == "" {
				return D{}
			}
			return layout.Inset{Top: 3}.Layout(gtx, lbl(th, 12.5, p.Muted, m.Extra.Desc).Layout)
		}),
	)
}

func (cw *chatWin) linkCard(gtx C, m *Message) D {
	th, p := cw.th, cw.pal
	u := m.Extra.URL
	host := hostOf(u)
	title := m.Extra.Title
	gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, dp(gtx, 300))
	// immagine grande sopra il testo se la miniatura è larga e abbastanza nitida
	var big bool
	var tOp paint.ImageOp
	var tSize image.Point
	if len(m.Thumb) > 0 {
		var ok bool
		if tOp, tSize, ok = thumbOp(m.Chat+"/"+m.ID, m.Thumb); ok {
			big = tSize.X >= 200 && tSize.X*10 >= tSize.Y*13
		}
	}
	texts := func(gtx C) D {
		var ch []layout.FlexChild
		if title != "" {
			ch = append(ch, layout.Rigid(func(gtx C) D {
				l := bold(lbl(th, 12.5, p.Text, title))
				l.MaxLines = 2
				return l.Layout(gtx)
			}))
		}
		if m.Extra.Desc != "" {
			ch = append(ch, layout.Rigid(func(gtx C) D {
				l := lbl(th, 11.5, p.Muted, m.Extra.Desc)
				l.MaxLines = 2
				return l.Layout(gtx)
			}))
		}
		ch = append(ch, layout.Rigid(one(lbl(th, 11.5, linkColor(p), host)).Layout))
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, ch...)
	}
	return cw.click("u:"+m.ID, func() { go openPath(u) }).Layout(gtx, func(gtx C) D {
		pointer.CursorPointer.Add(gtx.Ops)
		if big {
			w := gtx.Constraints.Max.X
			h := min(w*tSize.Y/max(1, tSize.X), dp(gtx, 170))
			mc := op.Record(gtx.Ops)
			dd := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					defer clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops).Pop()
					drawImage(gtx, tOp, tSize, image.Pt(w, h))
					return D{Size: image.Pt(w, h)}
				}),
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = w
					return layout.Inset{Left: 9, Right: 9, Top: 6, Bottom: 7}.Layout(gtx, texts)
				}),
			)
			call := mc.Stop()
			st := clip.UniformRRect(image.Rectangle{Max: dd.Size}, dp(gtx, 8)).Push(gtx.Ops)
			paint.Fill(gtx.Ops, subtleBg(p))
			call.Add(gtx.Ops)
			st.Pop()
			return dd
		}
		return rounded(gtx, subtleBg(p), 8, 9, 7, func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					if len(m.Thumb) == 0 {
						return icon(th, 14, p.Accent, icLink).Layout(gtx)
					}
					return thumbSquare(gtx, m.Chat+"/"+m.ID, m.Thumb, 48)
				}),
				layout.Rigid(layout.Spacer{Width: 9}.Layout),
				layout.Flexed(1, texts),
			)
		})
	})
}

// jumpTo scorre la chat fino al messaggio indicato (clic su una citazione).
func (cw *chatWin) jumpTo(id string) {
	for i, r := range cw.rows {
		if r.msg != nil && r.msg.ID == id {
			cw.list.Position = layout.Position{First: max(0, i-1), BeforeEnd: true}
			return
		}
	}
}

// senderAvatar disegna la foto di chi ha scritto il messaggio.
func (cw *chatWin) senderAvatar(gtx C, m *Message, size float32) D {
	th, p, be := cw.th, cw.pal, cw.u.be
	if m.FromMe {
		jid, name := be.Me()
		return avatar(gtx, th, p, be, jid, name, size, false, false, p.Bg)
	}
	jid, name := m.Sender, m.SenderName
	c, _ := be.ChatByJID(cw.jid)
	if jid == "" || !c.IsGroup {
		jid = cw.jid // nelle chat singole il mittente è il contatto
	}
	if name == "" {
		name = c.Name
	}
	return avatar(gtx, th, p, be, jid, name, size, false, false, p.Bg)
}

// textSpan: un pezzo di testo; se url non è vuoto è un link cliccabile.
type textSpan struct{ text, url string }

// linkSpans divide il testo in pezzi normali e link. nil se non contiene link.
func linkSpans(s string) []textSpan {
	idx := reURL.FindAllStringIndex(s, -1)
	if len(idx) == 0 {
		return nil
	}
	var out []textSpan
	last := 0
	for _, r := range idx {
		start, end := r[0], r[1]
		raw := strings.TrimRight(s[start:end], ".,;:!?)»”'")
		end = start + len(raw)
		if start < last {
			continue
		}
		if start > last {
			out = append(out, textSpan{text: s[last:start]})
		}
		u := raw
		if !strings.HasPrefix(strings.ToLower(u), "http") {
			u = "https://" + u
		}
		out = append(out, textSpan{text: raw, url: u})
		last = end
	}
	if last < len(s) {
		out = append(out, textSpan{text: s[last:]})
	}
	return out
}

func linkColor(p Pal) color.NRGBA {
	if p.Dark {
		return rgb(0x53BDEB)
	}
	return rgb(0x027EB5)
}

// richText disegna un testo con i link cliccabili (si aprono nel browser).
func (cw *chatWin) richText(gtx C, id string, spans []textSpan, size float32) D {
	if cw.rich == nil {
		cw.rich = map[string]*richtext.InteractiveText{}
	}
	st := cw.rich[id]
	if st == nil {
		st = new(richtext.InteractiveText)
		cw.rich[id] = st
	}
	for {
		span, ev, ok := st.Update(gtx)
		if !ok {
			break
		}
		if ev.Type == richtext.Click {
			if u, _ := span.Get("url").(string); u != "" {
				go openPath(u)
			}
		}
	}
	p := cw.pal
	styles := make([]richtext.SpanStyle, 0, len(spans))
	for _, s := range spans {
		ss := richtext.SpanStyle{Font: font.Font{Typeface: cw.th.Face}, Size: unit.Sp(size), Color: p.Text, Content: s.text}
		if s.url != "" {
			ss.Color, ss.Interactive = linkColor(p), true
			ss.Set("url", s.url)
		}
		styles = append(styles, ss)
	}
	return richtext.Text(st, cw.th.Shaper, styles...).Layout(gtx)
}
