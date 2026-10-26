package main

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// pollDraft: il sondaggio che si sta scrivendo.
type pollDraft struct {
	question widget.Editor
	options  []*widget.Editor
	multi    bool
	toggle   widget.Clickable
	send     widget.Clickable
	cancel   widget.Clickable
	list     widget.List
	focus    bool
}

func (cw *chatWin) startPoll() {
	d := &pollDraft{multi: false, focus: true}
	d.question.SingleLine = true
	d.question.Submit = true
	d.list.Axis = layout.Vertical
	for i := 0; i < 2; i++ {
		d.addOption()
	}
	cw.pollNew = d
	cw.emoji.open = false
}

func (d *pollDraft) addOption() {
	e := new(widget.Editor)
	e.SingleLine = true
	e.Submit = true
	d.options = append(d.options, e)
}

// values: domanda e opzioni non vuote e senza doppioni.
func (d *pollDraft) values() (string, []string) {
	q := strings.TrimSpace(d.question.Text())
	seen := map[string]bool{}
	var opts []string
	for _, e := range d.options {
		o := strings.TrimSpace(e.Text())
		if o != "" && !seen[strings.ToLower(o)] {
			seen[strings.ToLower(o)] = true
			opts = append(opts, o)
		}
	}
	return q, opts
}

// attachStrip: il menu della graffetta, sopra il campo di testo.
func (cw *chatWin) attachStrip(gtx C) D {
	if !cw.attachMenu {
		return D{}
	}
	th, p := cw.th, cw.pal
	chip := func(cl *widget.Clickable, glyph, label string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Right: 8}.Layout(gtx, func(gtx C) D {
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					bg := p.Field
					if cl.Hovered() {
						bg = p.Hover
					}
					return rounded(gtx, bg, 10, 12, 8, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(icon(th, 12, p.Accent, glyph).Layout),
							layout.Rigid(layout.Spacer{Width: 7}.Layout),
							layout.Rigid(lbl(th, 12.5, p.Text, label).Layout),
						)
					})
				})
			})
		})
	}
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: 12, Right: 12, Top: 8, Bottom: 2}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{}.Layout(gtx,
			chip(&cw.attFile, icAttach, "File o foto"),
			chip(&cw.attPoll, icList, "Sondaggio"),
		)
	})
	call := m.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
	call.Add(gtx.Ops)
	return d
}

func (cw *chatWin) pollView(gtx C) D {
	th, p, d := cw.th, cw.pal, cw.pollNew
	if d.cancel.Clicked(gtx) {
		cw.pollNew = nil
		gtx.Execute(op.InvalidateCmd{})
		return D{}
	}
	if d.toggle.Clicked(gtx) {
		d.multi = !d.multi
	}
	q, opts := d.values()
	ready := q != "" && len(opts) >= 2
	if d.send.Clicked(gtx) && ready {
		jid, be, multi := cw.jid, cw.u.be, d.multi
		cw.pollNew = nil
		cw.list.Position.BeforeEnd = false
		gtx.Execute(op.InvalidateCmd{})
		go be.SendPoll(jid, q, opts, multi)
		return D{}
	}
	// Invio sulla domanda: si passa alla prima opzione
	for {
		ev, ok := d.question.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			gtx.Execute(keyFocus(d.options[0]))
		}
	}
	// Invio su un'opzione: passa alla successiva
	for i, e := range d.options {
		for {
			ev, ok := e.Update(gtx)
			if !ok {
				break
			}
			if _, ok := ev.(widget.SubmitEvent); ok && i+1 < len(d.options) {
				gtx.Execute(keyFocus(d.options[i+1]))
			}
		}
	}
	// c'è sempre un'opzione vuota in fondo, fino al massimo consentito
	if last := d.options[len(d.options)-1]; strings.TrimSpace(last.Text()) != "" && len(d.options) < maxPollOptions {
		d.addOption()
	}
	if d.focus {
		d.focus = false
		gtx.Execute(keyFocus(&d.question))
	}

	field := func(e *widget.Editor, hint string, size float32) layout.Widget {
		return func(gtx C) D {
			return rounded(gtx, p.Field, 10, 12, 9, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				ed := material.Editor(th, e, hint)
				ed.TextSize = unit.Sp(size)
				ed.Color, ed.HintColor, ed.SelectionColor = p.Text, p.Faint, selColor(p)
				return ed.Layout(gtx)
			})
		}
	}
	var items []layout.Widget
	items = append(items,
		func(gtx C) D { return bold(lbl(th, 14, p.Text, "Nuovo sondaggio")).Layout(gtx) },
		func(gtx C) D { return layout.Spacer{Height: 10}.Layout(gtx) },
		field(&d.question, "Fai una domanda", 14),
		func(gtx C) D {
			return layout.Inset{Top: 14, Bottom: 6}.Layout(gtx, bold(lbl(th, 11.5, p.Accent, "OPZIONI")).Layout)
		},
	)
	for i, e := range d.options {
		e, i := e, i
		items = append(items, func(gtx C) D {
			return layout.Inset{Bottom: 6}.Layout(gtx, field(e, "Opzione "+strconv.Itoa(i+1), 13))
		})
	}
	items = append(items, func(gtx C) D {
		return layout.Inset{Top: 8}.Layout(gtx, func(gtx C) D {
			return d.toggle.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(lbl(th, 13, p.Text, "Più risposte consentite").Layout),
							layout.Rigid(lbl(th, 11.5, p.Faint, "Ognuno può scegliere più opzioni").Layout),
						)
					}),
					layout.Rigid(func(gtx C) D { return switchWidget(gtx, p, d.multi) }),
				)
			})
		})
	})
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D {
			fillRect(gtx, p.Bg, image.Rectangle{Max: gtx.Constraints.Max})
			ls := material.List(th, &d.list)
			ls.AnchorStrategy = material.Overlay
			return ls.Layout(gtx, len(items), func(gtx C, i int) D {
				return layout.Inset{Left: 16, Right: 16, Top: unit.Dp(map[bool]float32{true: 14, false: 0}[i == 0])}.Layout(gtx, items[i])
			})
		}),
		layout.Rigid(func(gtx C) D {
			mc := op.Record(gtx.Ops)
			dd := layout.Inset{Left: 12, Right: 12, Top: 10, Bottom: 10}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						t := strconv.Itoa(len(opts)) + " opzioni"
						if !ready {
							t = "Servono una domanda e almeno 2 opzioni"
						}
						return lbl(th, 12, p.Muted, t).Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						return d.cancel.Layout(gtx, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							return rounded(gtx, p.Field, 10, 14, 8, bold(lbl(th, 12.5, p.Muted, "Annulla")).Layout)
						})
					}),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						return d.send.Layout(gtx, func(gtx C) D {
							bg, fg := p.Field, p.Faint
							if ready {
								pointer.CursorPointer.Add(gtx.Ops)
								bg, fg = p.Accent, p.AccentTxt
							}
							return rounded(gtx, bg, 10, 14, 8, bold(lbl(th, 12.5, fg, "Invia")).Layout)
						})
					}),
				)
			})
			call := mc.Stop()
			fillRect(gtx, p.Surface, image.Rectangle{Max: dd.Size})
			fillRect(gtx, p.Divider, image.Rect(0, 0, dd.Size.X, 1))
			call.Add(gtx.Ops)
			return dd
		}),
	)
}

func keyFocus(tag any) key.FocusCmd { return key.FocusCmd{Tag: tag} }
