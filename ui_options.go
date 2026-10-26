package main

import (
	"bytes"
	"gioui.org/op/paint"
	"image"
	"image/color"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type optionsState struct {
	list       widget.List
	themeAuto  widget.Clickable
	themeLight widget.Clickable
	themeDark  widget.Clickable
	photos     widget.Clickable
	sound      widget.Clickable
	notify     widget.Clickable
	logout     widget.Clickable
	chatPhotos widget.Clickable
	keyEditor  widget.Editor
	keyInit    bool
	getKey     widget.Clickable
	back       widget.Clickable
	winSep     widget.Clickable
	winOne     widget.Clickable
	unreadTop  widget.Clickable
	colorBtns  map[string]*widget.Clickable
	emojiBtns  map[string]*widget.Clickable
	emojiPrev  map[string]paint.ImageOp
	showDel    widget.Clickable
	updates    widget.Clickable
	chatRight  widget.Clickable
	chatLeft   widget.Clickable
}

// applyTheme sceglie la palette in base all'impostazione e la applica a tutte le finestre.
func (u *UI) applyTheme() {
	u.setMu.Lock()
	dark := u.settings.Theme == "dark" || (u.settings.Theme != "light" && SystemDarkMode())
	sc := schemeByID(u.settings.Color)
	if dark {
		u.pal = sc.Dark
	} else {
		u.pal = sc.Light
	}
	u.setMu.Unlock()
	var hwnds []uintptr
	var wins []interface{ Invalidate() }
	if u.main != nil {
		hwnds = append(hwnds, u.main.hwnd)
		wins = append(wins, u.main.w)
	}
	u.mu.Lock()
	for _, c := range u.chats {
		hwnds = append(hwnds, c.hwnd)
		wins = append(wins, c.w)
	}
	u.mu.Unlock()
	go func() { // fuori dal disegno: sono chiamate verso altre finestre
		for _, h := range hwnds {
			SetDarkTitleBar(h, dark)
		}
	}()
	for _, w := range wins {
		w.Invalidate()
	}
}

func applyPalette(th *material.Theme, p Pal) {
	th.Palette.Fg = p.Text
	th.Palette.Bg = p.Bg
	th.Palette.ContrastBg = p.Accent
	th.Palette.ContrastFg = p.AccentTxt
}

func (m *mainWin) optionsView(gtx C) D {
	th, p, u, o := m.th, m.pal, m.u, &m.opt
	setTheme := func(t string) {
		u.setMu.Lock()
		u.settings.Theme = t
		u.setMu.Unlock()
		u.saveSettings()
		u.applyTheme()
	}
	if o.themeAuto.Clicked(gtx) {
		setTheme("auto")
	}
	if o.themeLight.Clicked(gtx) {
		setTheme("light")
	}
	if o.themeDark.Clicked(gtx) {
		setTheme("dark")
	}
	if o.photos.Clicked(gtx) {
		u.setMu.Lock()
		u.settings.ShowPhotos = !u.settings.ShowPhotos
		u.setMu.Unlock()
		u.saveSettings()
	}
	if o.sound.Clicked(gtx) {
		u.setMu.Lock()
		u.settings.Sound = !u.settings.Sound
		u.setMu.Unlock()
		u.saveSettings()
		if u.settings.Sound {
			PlayChime()
		}
	}
	if o.chatPhotos.Clicked(gtx) {
		u.setMu.Lock()
		u.settings.ChatPhotos = !u.settings.ChatPhotos
		u.setMu.Unlock()
		u.saveSettings()
		u.invalidateAll()
	}
	if !o.keyInit {
		o.keyEditor.SingleLine = true
		o.keyEditor.SetText(u.settings.KlipyKey)
		o.keyInit = true
	}
	for {
		ev, ok := o.keyEditor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			u.setMu.Lock()
			u.settings.KlipyKey = strings.TrimSpace(o.keyEditor.Text())
			u.setMu.Unlock()
			u.saveSettings()
		}
	}
	if o.getKey.Clicked(gtx) {
		go openPath("https://partner.klipy.com")
	}
	if o.winSep.Clicked(gtx) {
		m.setSingle(false)
		gtx.Execute(op.InvalidateCmd{})
	}
	if o.winOne.Clicked(gtx) {
		m.setSingle(true)
		gtx.Execute(op.InvalidateCmd{})
	}
	if o.colorBtns == nil {
		o.colorBtns = map[string]*widget.Clickable{}
		for _, sc := range colorSchemes {
			o.colorBtns[sc.ID] = new(widget.Clickable)
		}
	}
	for id, cl := range o.colorBtns {
		if cl.Clicked(gtx) {
			id := id
			u.Update(func(s *Settings) { s.Color = id })
			u.applyTheme()
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	if o.emojiBtns == nil {
		o.emojiBtns, o.emojiPrev = map[string]*widget.Clickable{}, map[string]paint.ImageOp{}
		for _, s := range emojiSets {
			o.emojiBtns[s.ID] = new(widget.Clickable)
			if img, _, err := image.Decode(bytes.NewReader(s.Preview)); err == nil {
				op := paint.NewImageOp(img)
				op.Filter = paint.FilterLinear
				o.emojiPrev[s.ID] = op
			}
		}
	}
	for id, cl := range o.emojiBtns {
		if cl.Clicked(gtx) {
			id := id
			u.Update(func(s *Settings) { s.EmojiSet = id })
			u.loadEmojiSet(id)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	if o.showDel.Clicked(gtx) {
		u.Update(func(s *Settings) { s.ShowDeleted = !s.ShowDeleted })
		u.invalidateAll() // le chat aperte si ricaricano
	}
	if o.updates.Clicked(gtx) {
		on := !u.Settings().CheckUpdates
		u.Update(func(s *Settings) { s.CheckUpdates = on })
		if on {
			go u.checkUpdate()
		}
	}
	if o.unreadTop.Clicked(gtx) {
		u.Update(func(s *Settings) { s.UnreadTop = !s.UnreadTop })
		gtx.Execute(op.InvalidateCmd{})
	}
	if o.chatRight.Clicked(gtx) {
		u.Update(func(s *Settings) { s.ChatLeft = false })
		gtx.Execute(op.InvalidateCmd{})
	}
	if o.chatLeft.Clicked(gtx) {
		u.Update(func(s *Settings) { s.ChatLeft = true })
		gtx.Execute(op.InvalidateCmd{})
	}
	if o.notify.Clicked(gtx) {
		u.setMu.Lock()
		u.settings.Notify = !u.settings.Notify
		u.setMu.Unlock()
		u.saveSettings()
	}
	if o.back.Clicked(gtx) {
		m.showOptions = false
		gtx.Execute(op.InvalidateCmd{}) // ridisegna subito la lista
	}
	confirming := time.Since(m.logoutAsk) < 4*time.Second
	if o.logout.Clicked(gtx) {
		if confirming {
			m.logoutAsk = time.Time{}
			m.showOptions = false
			gtx.Execute(op.InvalidateCmd{})
			u.be.Logout()
		} else {
			m.logoutAsk = time.Now()
			gtx.Execute(op.InvalidateCmd{At: m.logoutAsk.Add(4 * time.Second)})
		}
		confirming = !confirming
	}

	section := func(title string) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18, Top: 18, Bottom: 8}.Layout(gtx, bold(lbl(th, 11.5, p.Accent, title)).Layout)
		})
	}
	theme := u.settings.Theme
	if theme == "" {
		theme = "auto"
	}
	seg := func(cl *widget.Clickable, label string, active bool) layout.FlexChild {
		return layout.Flexed(1, func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				mc := op.Record(gtx.Ops)
				c := p.Muted
				if active {
					c = p.AccentTxt
				}
				l := lbl(th, 12.5, c, label)
				l.Alignment = 2
				if active {
					l.Font.Weight = font.SemiBold
				}
				d := layout.Inset{Top: 7, Bottom: 8}.Layout(gtx, l.Layout)
				call := mc.Stop()
				if active {
					fillRRect(gtx, p.Accent, image.Rectangle{Max: d.Size}, dp(gtx, 8))
				} else if cl.Hovered() {
					fillRRect(gtx, p.Hover, image.Rectangle{Max: d.Size}, dp(gtx, 8))
				}
				call.Add(gtx.Ops)
				return d
			})
		})
	}
	toggleRow := func(cl *widget.Clickable, title, sub string, on bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				mc := op.Record(gtx.Ops)
				d := layout.Inset{Left: 18, Right: 18, Top: 9, Bottom: 9}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(lbl(th, 13.5, p.Text, title).Layout),
								layout.Rigid(func(gtx C) D {
									if sub == "" {
										return D{}
									}
									return lbl(th, 11.5, p.Muted, sub).Layout(gtx)
								}),
							)
						}),
						layout.Rigid(func(gtx C) D { return switchWidget(gtx, p, on) }),
					)
				})
				call := mc.Stop()
				if cl.Hovered() {
					fillRect(gtx, p.Hover, image.Rectangle{Max: d.Size})
				}
				call.Add(gtx.Ops)
				return d
			})
		})
	}

	o.list.Axis = layout.Vertical
	children := []layout.FlexChild{
		layout.Rigid(func(gtx C) D {
			return o.back.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				return layout.Inset{Left: 14, Right: 14, Top: 12, Bottom: 4}.Layout(gtx, func(gtx C) D {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(icon(th, 12, p.Muted, "").Layout),
						layout.Rigid(layout.Spacer{Width: 8}.Layout),
						layout.Rigid(bold(lbl(th, 16, p.Text, "Opzioni")).Layout),
					)
				})
			})
		}),
		section("ASPETTO"),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18}.Layout(gtx, func(gtx C) D {
				return rounded(gtx, p.Field, 10, 3, 3, func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						seg(&o.themeAuto, "Sistema", theme == "auto"),
						seg(&o.themeLight, "Chiaro", theme == "light"),
						seg(&o.themeDark, "Scuro", theme == "dark"),
					)
				})
			})
		}),
		layout.Rigid(layout.Spacer{Height: 12}.Layout),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18}.Layout(gtx, func(gtx C) D { return m.colorGrid(gtx) })
		}),
		section("EMOJI"),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 12, Right: 12}.Layout(gtx, func(gtx C) D { return m.emojiChooser(gtx) })
		}),
		section("FINESTRE"),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18}.Layout(gtx, func(gtx C) D {
				return rounded(gtx, p.Field, 10, 3, 3, func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						seg(&o.winSep, "Separate", !m.single),
						seg(&o.winOne, "Unica", m.single),
					)
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			t := "Ogni chat si apre nella sua finestra, come su MSN"
			if m.single {
				t = "Elenco e chat affiancati in una sola finestra"
			}
			return layout.Inset{Left: 20, Right: 18, Top: 6}.Layout(gtx, lbl(th, 11.5, p.Faint, t).Layout)
		}),
		layout.Rigid(func(gtx C) D {
			if !m.single {
				return D{}
			}
			left := u.Settings().ChatLeft
			return layout.Inset{Left: 18, Right: 18, Top: 10}.Layout(gtx, func(gtx C) D {
				return rounded(gtx, p.Field, 10, 3, 3, func(gtx C) D {
					return layout.Flex{}.Layout(gtx,
						seg(&o.chatLeft, "Chat a sinistra", left),
						seg(&o.chatRight, "Chat a destra", !left),
					)
				})
			})
		}),
		section("FOTO"),
		toggleRow(&o.photos, "Foto profilo nella lista", "Spento: elenco compatto stile ICQ", u.settings.ShowPhotos),
		toggleRow(&o.chatPhotos, "Foto accanto ai messaggi", "Mostra chi scrive, come su MSN", u.settings.ChatPhotos),
		section("ELENCO DELLE CHAT"),
		toggleRow(&o.unreadTop, "Chat non lette in alto", "Subito sotto quelle fissate", u.Settings().UnreadTop),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18, Top: 2}.Layout(gtx,
				lbl(th, 11.5, p.Faint, "Per fissare una chat in alto: tasto destro sulla chat › Fissa in alto (al massimo 3, come su WhatsApp).").Layout)
		}),
		section("MESSAGGI"),
		toggleRow(&o.showDel, "Mostra i messaggi eliminati", "Se qualcuno elimina un messaggio, qui resta visibile con l'etichetta Eliminato", u.Settings().ShowDeleted),
		section("AVVISI"),
		toggleRow(&o.sound, "Suono per i nuovi messaggi", "", u.settings.Sound),
		toggleRow(&o.updates, "Avvisami delle nuove versioni", "Versione installata: "+Version, u.Settings().CheckUpdates),
		toggleRow(&o.notify, notifyLabel, "Quando Ping non è in primo piano", u.settings.Notify),
		section("GIF E STICKER ONLINE"),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(lbl(th, 12, p.Muted, "La ricerca usa KLIPY (gratuito). Crea una chiave e incollala qui.").Layout),
					layout.Rigid(layout.Spacer{Height: 8}.Layout),
					layout.Rigid(func(gtx C) D {
						return rounded(gtx, p.Field, 8, 10, 8, func(gtx C) D {
							gtx.Constraints.Min.X = gtx.Constraints.Max.X
							e := material.Editor(th, &o.keyEditor, "Chiave KLIPY")
							e.TextSize = 12.5
							e.Color = p.Text
							e.HintColor = p.Faint
							return e.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx C) D {
						return o.getKey.Layout(gtx, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							return layout.Inset{Top: 7, Bottom: 4}.Layout(gtx, lbl(th, 12, p.Accent, "Ottieni una chiave gratuita › partner.klipy.com").Layout)
						})
					}),
				)
			})
		}),
		section("ACCOUNT"),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18, Top: 2}.Layout(gtx, func(gtx C) D {
				return o.logout.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					label := "Disconnetti questo PC"
					bg := subtleBg(p)
					c := p.Danger
					if confirming {
						label, bg, c = "Clicca ancora per confermare", p.Danger, rgb(0xFFFFFF)
					}
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return rounded(gtx, bg, 10, 12, 9, func(gtx C) D {
						l := bold(lbl(th, 13, c, label))
						l.Alignment = 2
						return l.Layout(gtx)
					})
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18, Top: 8}.Layout(gtx,
				lbl(th, 11.5, p.Faint, "Il telefono resta collegato a WhatsApp; questo PC dovrà inquadrare di nuovo il QR.").Layout)
		}),
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18, Right: 18, Top: 24, Bottom: 16}.Layout(gtx,
				lbl(th, 11, p.Faint, "Ping 1.2 · di HaVeN80").Layout)
		}),
	}
	ls := material.List(th, &o.list)
	return ls.Layout(gtx, len(children), func(gtx C, i int) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children[i])
	})
}

// switchWidget: interruttore acceso/spento.
func switchWidget(gtx C, p Pal, on bool) D {
	w, h := dp(gtx, 36), dp(gtx, 20)
	r := image.Rectangle{Max: image.Pt(w, h)}
	bg := p.Divider
	if on {
		bg = p.Accent
	}
	fillRRect(gtx, bg, r, h/2)
	k := h - dp(gtx, 6)
	x := dp(gtx, 3)
	if on {
		x = w - k - dp(gtx, 3)
	}
	fillCircle(gtx, rgbWhite(), image.Rect(x, dp(gtx, 3), x+k, dp(gtx, 3)+k))
	return D{Size: r.Max}
}

func rgbWhite() color.NRGBA { return rgb(0xFFFFFF) }

var notifyLabel = func() string {
	if runtime.GOOS == "windows" {
		return "Notifiche di Windows"
	}
	return "Notifiche del sistema"
}()

// colorGrid: i temi di colore, 5 per riga, con un'anteprima di sfondo, bolle e colore principale.
func (m *mainWin) colorGrid(gtx C) D {
	th, p, o := m.th, m.pal, &m.opt
	cur := schemeByID(m.u.Settings().Color).ID
	const perRow = 5
	gap := dp(gtx, 8)
	w := (gtx.Constraints.Max.X - gap*(perRow-1)) / perRow
	h := dp(gtx, 42)
	var rows []layout.FlexChild
	for start := 0; start < len(colorSchemes); start += perRow {
		end := min(start+perRow, len(colorSchemes))
		var cells []layout.FlexChild
		for i := start; i < end; i++ {
			sc := colorSchemes[i]
			sp := sc.Light
			if p.Dark {
				sp = sc.Dark
			}
			if i > start {
				cells = append(cells, layout.Rigid(layout.Spacer{Width: unit.Dp(float32(gap) / gtx.Metric.PxPerDp)}.Layout))
			}
			cl, sel := o.colorBtns[sc.ID], sc.ID == cur
			cells = append(cells, layout.Rigid(func(gtx C) D {
				return cl.Layout(gtx, func(gtx C) D {
					pointer.CursorPointer.Add(gtx.Ops)
					gtx.Constraints = layout.Exact(image.Pt(w, gtx.Constraints.Max.Y))
					gtx.Constraints.Min.Y = 0
					return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							r := image.Rect(0, 0, w, h)
							rad := dp(gtx, 9)
							if sel {
								fillRRect(gtx, p.Accent, r, rad)
								r = r.Inset(dp(gtx, 2))
								fillRRect(gtx, p.Bg, r, rad-dp(gtx, 1))
								r = r.Inset(dp(gtx, 1))
							} else if cl.Hovered() {
								fillRRect(gtx, p.Faint, r, rad)
								r = r.Inset(dp(gtx, 1))
							} else {
								fillRRect(gtx, p.Divider, r, rad)
								r = r.Inset(dp(gtx, 1))
							}
							fillRRect(gtx, sp.Bg, r, rad-dp(gtx, 2))
							// bolla altrui, bolla mia, puntino del colore principale
							bw, bh := r.Dx()*45/100, dp(gtx, 8)
							fillRRect(gtx, sp.Surface, image.Rect(r.Min.X+dp(gtx, 5), r.Min.Y+dp(gtx, 6), r.Min.X+dp(gtx, 5)+bw, r.Min.Y+dp(gtx, 6)+bh), dp(gtx, 4))
							fillRRect(gtx, sp.Mine, image.Rect(r.Max.X-dp(gtx, 5)-bw, r.Min.Y+dp(gtx, 17), r.Max.X-dp(gtx, 5), r.Min.Y+dp(gtx, 17)+bh), dp(gtx, 4))
							d := dp(gtx, 9)
							fillCircle(gtx, sp.Accent, image.Rect(r.Min.X+dp(gtx, 5), r.Max.Y-dp(gtx, 5)-d, r.Min.X+dp(gtx, 5)+d, r.Max.Y-dp(gtx, 5)))
							return D{Size: image.Pt(w, h)}
						}),
						layout.Rigid(layout.Spacer{Height: 3}.Layout),
						layout.Rigid(func(gtx C) D {
							col := p.Muted
							if sel {
								col = p.Text
							}
							l := one(lbl(th, 10.5, col, sc.Name))
							if sel {
								l.Font.Weight = font.SemiBold
							}
							return l.Layout(gtx)
						}),
					)
				})
			}))
		}
		if start > 0 {
			rows = append(rows, layout.Rigid(layout.Spacer{Height: 8}.Layout))
		}
		rows = append(rows, layout.Rigid(func(gtx C) D { return layout.Flex{}.Layout(gtx, cells...) }))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}

// emojiChooser: gli stili delle emoji, con anteprima; quelli non ancora sul PC si scaricano alla scelta.
func (m *mainWin) emojiChooser(gtx C) D {
	th, p, o, u := m.th, m.pal, &m.opt, m.u
	cur := emojiSetByID(u.Settings().EmojiSet).ID
	var rows []layout.FlexChild
	for _, s := range emojiSets {
		s := s
		cl, sel := o.emojiBtns[s.ID], s.ID == cur
		prog, busy, errText := EmojiDownload(s.ID)
		ready := u.EmojiReady(s.ID)
		rows = append(rows, layout.Rigid(func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				mc := op.Record(gtx.Ops)
				d := layout.Inset{Left: 8, Right: 8, Top: 7, Bottom: 7}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx C) D {
									l := one(lbl(th, 13, p.Text, s.Name))
									if sel {
										l.Font.Weight = font.SemiBold
									}
									return l.Layout(gtx)
								}),
								layout.Rigid(func(gtx C) D {
									switch {
									case busy:
										return lbl(th, 11.5, p.Accent, "Download "+strconv.Itoa(int(prog*100))+"%").Layout(gtx)
									case sel && ready:
										return icon(th, 12, p.Accent, icCheck).Layout(gtx)
									case !ready:
										mb := float64(s.Size) / 1e6
										return lbl(th, 11, p.Faint, "da scaricare · "+strconv.FormatFloat(mb, 'f', 0, 64)+" MB").Layout(gtx)
									}
									return D{}
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: 5}.Layout),
						layout.Rigid(func(gtx C) D {
							pv, ok := o.emojiPrev[s.ID]
							if !ok {
								return D{}
							}
							sz := pv.Size()
							h := dp(gtx, 22)
							w := sz.X * h / max(1, sz.Y)
							st := op.Affine(f32Scale(float32(h) / float32(sz.Y))).Push(gtx.Ops)
							pv.Add(gtx.Ops)
							paint.PaintOp{}.Add(gtx.Ops)
							st.Pop()
							return D{Size: image.Pt(w, h)}
						}),
						layout.Rigid(layout.Spacer{Height: 4}.Layout),
						layout.Rigid(func(gtx C) D {
							t := s.Desc + " · " + s.Credit
							col := p.Faint
							if errText != "" {
								t, col = errText+" (clicca per riprovare)", p.Danger
							}
							return lbl(th, 11, col, t).Layout(gtx)
						}),
					)
				})
				call := mc.Stop()
				switch {
				case sel:
					fillRRect(gtx, withAlpha(p.Accent, 0x22), image.Rectangle{Max: d.Size}, dp(gtx, 10))
				case cl.Hovered():
					fillRRect(gtx, p.Hover, image.Rectangle{Max: d.Size}, dp(gtx, 10))
				}
				call.Add(gtx.Ops)
				return d
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, rows...)
}
