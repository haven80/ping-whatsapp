package main

import (
	"image"
	"image/color"
	"runtime"
	"strings"
	"time"

	"gioui.org/font"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
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
	chatRight  widget.Clickable
	chatLeft   widget.Clickable
}

// applyTheme sceglie la palette in base all'impostazione e la applica a tutte le finestre.
func (u *UI) applyTheme() {
	u.setMu.Lock()
	dark := u.settings.Theme == "dark" || (u.settings.Theme != "light" && SystemDarkMode())
	if dark {
		u.pal = darkPal
	} else {
		u.pal = lightPal
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
		section("AVVISI"),
		toggleRow(&o.sound, "Suono per i nuovi messaggi", "", u.settings.Sound),
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
