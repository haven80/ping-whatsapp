package main

import (
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// profileView: la scheda di un contatto o di un gruppo, mostrata al posto della conversazione.
type profileView struct {
	jid, name string
	list      widget.List
	back      widget.Clickable
	write     widget.Clickable
	copyNum   widget.Clickable
	photo     widget.Clickable
	members   map[string]*widget.Clickable

	photoPath string
	photoOp   paint.ImageOp
	photoSize image.Point
}

// showProfile apre la scheda; dalla scheda di un gruppo si può aprire quella di un partecipante
// e tornare indietro.
func (cw *chatWin) showProfile(jid, name string) {
	if jid == "" || jid == "me" {
		return
	}
	if n := len(cw.profiles); n > 0 && cw.profiles[n-1].jid == jid {
		return
	}
	pv := &profileView{jid: jid, name: name, members: map[string]*widget.Clickable{}}
	pv.list.Axis = layout.Vertical
	cw.profiles = append(cw.profiles, pv)
	cw.reactFor, cw.deleteFor, cw.menuFor = "", "", ""
	cw.emoji.open = false
	cw.invalidate()
}

func (cw *chatWin) closeProfile() {
	if n := len(cw.profiles); n > 0 {
		cw.profiles = cw.profiles[:n-1]
	}
}

// RequestProfile: chiesta da un'altra finestra (clic sulla foto nella lista). Sicura da qualsiasi goroutine.
func (cw *chatWin) RequestProfile() {
	cw.noticeMu.Lock()
	cw.wantProfile = true
	cw.noticeMu.Unlock()
	cw.invalidate()
}

func (cw *chatWin) profileView(gtx C) D {
	th, p, be := cw.th, cw.pal, cw.u.be
	pv := cw.profiles[len(cw.profiles)-1]
	info := be.Profile(pv.jid)
	if info.Name == "" {
		info.Name = pick(pv.name, info.Phone)
	}
	isMine := pv.jid == cw.jid // la scheda della chat che è già aperta qui

	if pv.back.Clicked(gtx) {
		cw.closeProfile()
		gtx.Execute(op.InvalidateCmd{})
		return D{}
	}
	if pv.write.Clicked(gtx) {
		cw.closeProfile()
		gtx.Execute(op.InvalidateCmd{})
		if !isMine {
			jid, name := pv.jid, info.Name
			go cw.u.OpenChatWith(jid, name)
		}
		return D{}
	}
	if pv.copyNum.Clicked(gtx) && info.Phone != "" {
		cw.copyText = strings.ReplaceAll(info.Phone, " ", "")
	}
	if pv.photo.Clicked(gtx) && info.Photo != "" {
		go openPath(info.Photo)
	}
	if info.Photo != "" && info.Photo != pv.photoPath {
		if img := loadImage(info.Photo); img != nil {
			pv.photoOp = paint.NewImageOp(img)
			pv.photoOp.Filter = paint.FilterLinear
			pv.photoSize = img.Bounds().Size()
			pv.photoPath = info.Photo
		}
	}
	var members []Member
	if info.IsGroup {
		members = be.GroupMembers(pv.jid)
		for _, mb := range members {
			cl := pv.members[mb.JID]
			if cl == nil {
				cl = new(widget.Clickable)
				pv.members[mb.JID] = cl
			}
			if cl.Clicked(gtx) {
				cw.showProfile(mb.JID, mb.Name)
				gtx.Execute(op.InvalidateCmd{})
			}
		}
	}
	pres := be.PresenceOf(pv.jid)

	fillRect(gtx, p.Bg, image.Rectangle{Max: gtx.Constraints.Max})
	center := func(w layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return layout.N.Layout(gtx, w)
		})
	}
	centerText := func(size float32, col color.NRGBA, s string, boldText bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			l := lbl(th, size, col, s)
			l.Alignment = 2
			if boldText {
				l.Font.Weight = font.SemiBold
			}
			return layout.Inset{Left: 20, Right: 20}.Layout(gtx, l.Layout)
		})
	}
	section := func(title string, body layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 14, Right: 14, Top: 10}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return rounded(gtx, p.Surface, 12, 14, 11, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(bold(lbl(th, 11.5, p.Accent, title)).Layout),
						layout.Rigid(layout.Spacer{Height: 5}.Layout),
						layout.Rigid(body),
					)
				})
			})
		})
	}
	text := func(s string) layout.Widget {
		return func(gtx C) D {
			if spans := linkSpans(s); spans != nil {
				return cw.richText(gtx, "profile:"+pv.jid, spans, 13.5)
			}
			return lbl(th, 13.5, p.Text, s).Layout(gtx)
		}
	}

	var items []layout.FlexChild
	// foto grande
	items = append(items, layout.Rigid(layout.Spacer{Height: 18}.Layout), center(func(gtx C) D {
		s := min(gtx.Constraints.Max.X-dp(gtx, 48), dp(gtx, 260))
		if pv.photoPath == "" {
			pt := pxToDp(gtx, s)
			return avatar(gtx, th, p, be, pv.jid, info.Name, pt, info.IsGroup, false, p.Bg)
		}
		return pv.photo.Layout(gtx, func(gtx C) D {
			pointer.CursorPointer.Add(gtx.Ops)
			r := image.Rectangle{Max: image.Pt(s, s)}
			st := clip.UniformRRect(r, dp(gtx, 18)).Push(gtx.Ops)
			drawImage(gtx, pv.photoOp, pv.photoSize, r.Max)
			st.Pop()
			return D{Size: r.Max}
		})
	}))
	items = append(items, layout.Rigid(layout.Spacer{Height: 14}.Layout), centerText(19, p.Text, info.Name, true))
	// sotto il nome: numero o numero di partecipanti, poi lo stato
	sub := info.Phone
	if info.IsGroup {
		n := info.Count
		if n == 0 {
			n = len(members) + 1
		}
		sub = "Gruppo · " + strconv.Itoa(n) + " partecipanti"
	}
	if sub != "" && sub != info.Name {
		items = append(items, layout.Rigid(layout.Spacer{Height: 3}.Layout), centerText(13, p.Muted, sub, false))
	}
	if !info.IsGroup {
		if s := lastSeenText(pres); s != "" {
			col := p.Faint
			if pres.Online {
				col = p.Online
			}
			items = append(items, layout.Rigid(layout.Spacer{Height: 2}.Layout), centerText(12, col, s, false))
		}
	}
	// pulsanti
	items = append(items, layout.Rigid(layout.Spacer{Height: 14}.Layout), center(func(gtx C) D {
		btn := func(cl *widget.Clickable, glyph, label string) layout.FlexChild {
			return layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 4, Right: 4}.Layout(gtx, func(gtx C) D {
					return cl.Layout(gtx, func(gtx C) D {
						pointer.CursorPointer.Add(gtx.Ops)
						bg := p.Surface
						if cl.Hovered() {
							bg = p.Hover
						}
						return rounded(gtx, bg, 12, 16, 9, func(gtx C) D {
							return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(icon(th, 15, p.Accent, glyph).Layout),
								layout.Rigid(layout.Spacer{Height: 4}.Layout),
								layout.Rigid(lbl(th, 11.5, p.Text, label).Layout),
							)
						})
					})
				})
			})
		}
		var bs []layout.FlexChild
		if isMine {
			bs = append(bs, btn(&pv.write, icSend, "Torna alla chat"))
		} else {
			bs = append(bs, btn(&pv.write, icSend, "Scrivi"))
		}
		if info.Phone != "" && !strings.Contains(info.Phone, "∙") {
			bs = append(bs, btn(&pv.copyNum, icPhone, "Copia numero"))
		}
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return layout.Flex{Spacing: layout.SpaceSides}.Layout(gtx, bs...)
	}))
	items = append(items, layout.Rigid(layout.Spacer{Height: 6}.Layout))
	// dettagli
	if info.Business != "" {
		items = append(items, section("ACCOUNT BUSINESS", text("✔ "+info.Business)))
	}
	if !info.IsGroup {
		about := info.About
		if about == "" && info.Loading {
			about = "…"
		}
		if about != "" {
			items = append(items, section("INFO", text(about)))
		}
		if info.PushName != "" && info.PushName != info.Name {
			items = append(items, section("NOME SU WHATSAPP", text("~"+info.PushName)))
		}
	} else {
		if info.Topic != "" {
			items = append(items, section("DESCRIZIONE", text(info.Topic)))
		}
		if !info.Created.IsZero() {
			items = append(items, section("CREATO", text(strconv.Itoa(info.Created.Day())+" "+mesi[info.Created.Month()-1]+" "+strconv.Itoa(info.Created.Year()))))
		}
		if len(members) > 0 {
			items = append(items, section(strconv.Itoa(len(members))+" PARTECIPANTI OLTRE A TE", func(gtx C) D {
				var rows []layout.FlexChild
				for _, mb := range members {
					mb := mb
					cl := pv.members[mb.JID]
					rows = append(rows, layout.Rigid(func(gtx C) D {
						return cl.Layout(gtx, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							mc := op.Record(gtx.Ops)
							d := layout.Inset{Top: 5, Bottom: 5, Left: 4, Right: 4}.Layout(gtx, func(gtx C) D {
								gtx.Constraints.Min.X = gtx.Constraints.Max.X
								return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										return avatar(gtx, th, p, be, mb.JID, mb.Name, 32, false, false, p.Surface)
									}),
									layout.Rigid(layout.Spacer{Width: 10}.Layout),
									layout.Flexed(1, one(lbl(th, 13, p.Text, mb.Name)).Layout),
									layout.Rigid(func(gtx C) D {
										if !mb.Admin {
											return D{}
										}
										return rounded(gtx, subtleBg(p), 6, 6, 2, lbl(th, 10.5, p.Accent, "Admin").Layout)
									}),
								)
							})
							call := mc.Stop()
							if cl.Hovered() {
								fillRRect(gtx, p.Hover, image.Rectangle{Max: d.Size}, dp(gtx, 8))
							}
							call.Add(gtx.Ops)
							return d
						})
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
			}))
		}
	}
	if info.Loading {
		items = append(items, layout.Rigid(func(gtx C) D {
			return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.N.Layout(gtx, func(gtx C) D {
					gtx.Constraints = layout.Exact(image.Pt(dp(gtx, 18), dp(gtx, 18)))
					l := material.Loader(th)
					l.Color = p.Faint
					return l.Layout(gtx)
				})
			})
		}))
	} else if info.Err != "" {
		items = append(items, layout.Rigid(layout.Spacer{Height: 10}.Layout), centerText(11.5, p.Faint, "Alcuni dettagli non sono disponibili ("+info.Err+")", false))
	}
	items = append(items, layout.Rigid(layout.Spacer{Height: 20}.Layout))

	title := "Info contatto"
	if info.IsGroup {
		title = "Info gruppo"
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			mc := op.Record(gtx.Ops)
			d := pv.back.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Inset{Left: 14, Right: 14, Top: 13, Bottom: 13}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(icon(th, 12, p.Muted, icBack).Layout),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Rigid(bold(lbl(th, 14.5, p.Text, title)).Layout),
					)
				})
			})
			call := mc.Stop()
			fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
			fillRect(gtx, p.Divider, image.Rect(0, d.Size.Y-1, d.Size.X, d.Size.Y))
			call.Add(gtx.Ops)
			return d
		}),
		layout.Flexed(1, func(gtx C) D {
			ls := material.List(th, &pv.list)
			ls.AnchorStrategy = material.Overlay
			ls.Indicator.Color = argb(0x888888, 0x50)
			return ls.Layout(gtx, len(items), func(gtx C, i int) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, items[i])
			})
		}),
	)
}

const icBack = "\uE72B"

func pxToDp(gtx C, px int) float32 {
	return float32(px) / gtx.Metric.PxPerDp
}
