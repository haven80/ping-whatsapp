package main

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// forwardPicker: scelta delle chat a cui inoltrare un messaggio.
type forwardPicker struct {
	msg    *Message
	sel    []string // jid scelti, nell'ordine
	search widget.Editor
	list   widget.List
	rows   map[string]*widget.Clickable
	send   widget.Clickable
	cancel widget.Clickable
	init   bool
}

func (cw *chatWin) startForward(m *Message) {
	c := *m
	cw.fwd = &forwardPicker{msg: &c, rows: map[string]*widget.Clickable{}}
	cw.reactFor, cw.deleteFor, cw.menuFor = "", "", ""
	cw.emoji.open = false
}

func (fp *forwardPicker) isSel(jid string) bool {
	for _, s := range fp.sel {
		if s == jid {
			return true
		}
	}
	return false
}

func (fp *forwardPicker) toggle(jid string) {
	for i, s := range fp.sel {
		if s == jid {
			fp.sel = append(fp.sel[:i], fp.sel[i+1:]...)
			return
		}
	}
	if len(fp.sel) < maxForwardChats {
		fp.sel = append(fp.sel, jid)
	}
}

func (cw *chatWin) forwardView(gtx C) D {
	th, p, be, fp := cw.th, cw.pal, cw.u.be, cw.fwd
	if !fp.init {
		fp.search.SingleLine = true
		fp.list.Axis = layout.Vertical
		fp.init = true
	}
	if fp.cancel.Clicked(gtx) {
		cw.fwd = nil
		return D{}
	}
	if fp.send.Clicked(gtx) && len(fp.sel) > 0 {
		msg, targets := fp.msg, append([]string(nil), fp.sel...)
		cw.fwd = nil
		n := len(targets)
		safeGo("inoltro", func() {
			failed := 0
			for _, to := range targets {
				if err := be.Forward(msg, to); err != nil {
					failed++
				}
			}
			switch {
			case failed == n:
				cw.showNotice("Inoltro non riuscito: contenuto non disponibile")
			case n == 1:
				cw.notify("Messaggio inoltrato", true)
			default:
				cw.notify("Inoltrato a "+strconv.Itoa(n-failed)+" chat", true)
			}
		})
		return D{}
	}

	// elenco: chat (anche archiviate), poi rubrica, senza doppioni
	q := strings.ToLower(strings.TrimSpace(fp.search.Text()))
	chats, _ := be.Snapshot()
	seen := map[string]bool{}
	var items []Chat
	add := func(c Chat) {
		if seen[c.JID] || c.Name == "" {
			return
		}
		if q != "" && !strings.Contains(strings.ToLower(c.Name), q) && !strings.Contains(c.JID, q) {
			return
		}
		seen[c.JID] = true
		items = append(items, c)
	}
	for _, c := range chats {
		add(c)
	}
	if q != "" {
		for _, c := range be.Contacts() {
			add(c)
		}
	}
	for _, it := range items {
		cl := fp.rows[it.JID]
		if cl == nil {
			cl = new(widget.Clickable)
			fp.rows[it.JID] = cl
		}
		if cl.Clicked(gtx) {
			fp.toggle(it.JID)
		}
	}

	fillRect(gtx, p.Bg, image.Rectangle{Max: gtx.Constraints.Max})
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// intestazione con anteprima del messaggio
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 14, Right: 14, Top: 12, Bottom: 6}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(bold(lbl(th, 14, p.Text, "Inoltra a…")).Layout),
					layout.Rigid(layout.Spacer{Height: 4}.Layout),
					layout.Rigid(func(gtx C) D {
						t := strings.Join(strings.Fields(previewMsg(fp.msg)), " ")
						return one(lbl(th, 12, p.Muted, "↪ "+t)).Layout(gtx)
					}),
				)
			})
		}),
		// ricerca
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 12, Right: 12, Bottom: 8}.Layout(gtx, func(gtx C) D {
				return rounded(gtx, p.Field, 10, 10, 8, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(icon(th, 12, p.Faint, icSearch).Layout),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Flexed(1, func(gtx C) D {
							e := material.Editor(th, &fp.search, "Cerca chat o contatto")
							e.TextSize = unit.Sp(13)
							e.Color, e.HintColor = p.Text, p.Faint
							return e.Layout(gtx)
						}),
					)
				})
			})
		}),
		// elenco
		layout.Flexed(1, func(gtx C) D {
			ls := material.List(th, &fp.list)
			ls.AnchorStrategy = material.Overlay
			return ls.Layout(gtx, len(items), func(gtx C, i int) D {
				c := items[i]
				cl := fp.rows[c.JID]
				sel := fp.isSel(c.JID)
				full := len(fp.sel) >= maxForwardChats && !sel
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					mc := op.Record(gtx.Ops)
					d := layout.Inset{Left: 14, Right: 16, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return avatar(gtx, th, p, be, c.JID, c.Name, 34, c.IsGroup, false, p.Bg)
							}),
							layout.Rigid(layout.Spacer{Width: 10}.Layout),
							layout.Flexed(1, func(gtx C) D {
								col := p.Text
								if full {
									col = p.Faint
								}
								l := one(lbl(th, 13.5, col, c.Name))
								if sel {
									l.Font.Weight = font.SemiBold
								}
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx C) D { return checkCircle(gtx, th, p, sel) }),
						)
					})
					call := mc.Stop()
					if cl.Hovered() || sel {
						fillRRect(gtx, p.Hover, image.Rect(dp(gtx, 4), 0, d.Size.X-dp(gtx, 4), d.Size.Y), dp(gtx, 8))
					}
					call.Add(gtx.Ops)
					return d
				})
			})
		}),
		// pulsanti
		layout.Rigid(func(gtx C) D {
			mc := op.Record(gtx.Ops)
			d := layout.Inset{Left: 12, Right: 12, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						hint := "Scegli fino a " + strconv.Itoa(maxForwardChats) + " chat"
						if len(fp.sel) > 0 {
							hint = strconv.Itoa(len(fp.sel)) + " di " + strconv.Itoa(maxForwardChats) + " scelte"
						}
						return lbl(th, 12, p.Muted, hint).Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						return fp.cancel.Layout(gtx, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							return rounded(gtx, p.Field, 10, 14, 8, bold(lbl(th, 12.5, p.Muted, "Annulla")).Layout)
						})
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						return fp.send.Layout(gtx, func(gtx C) D {
							bg, fg := p.Field, p.Faint
							if len(fp.sel) > 0 {
								pointer.CursorPointer.Add(gtx.Ops)
								bg, fg = p.Accent, p.AccentTxt
							}
							return rounded(gtx, bg, 10, 14, 8, bold(lbl(th, 12.5, fg, "Inoltra")).Layout)
						})
					}),
				)
			})
			call := mc.Stop()
			fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
			fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
			call.Add(gtx.Ops)
			return d
		}),
	)
}

// checkCircle: cerchio di selezione (pieno con ✓ se scelto).
func checkCircle(gtx C, th *material.Theme, p Pal, on bool) D {
	s := dp(gtx, 20)
	r := image.Rectangle{Max: image.Pt(s, s)}
	if on {
		fillCircle(gtx, p.Accent, r)
		g := gtx
		g.Constraints = layout.Exact(r.Max)
		layout.Center.Layout(g, bold(lbl(th, 11, p.AccentTxt, "✓")).Layout)
	} else {
		fillCircle(gtx, p.Divider, r)
		fillCircle(gtx, p.Bg, r.Inset(dp(gtx, 2)))
	}
	return D{Size: r.Max}
}
