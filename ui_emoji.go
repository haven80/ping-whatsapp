package main

import (
	"image"
	"strings"
	"time"

	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type emojiCat struct {
	name   string
	emojis []string
}

func splitE(s string) []string { return strings.Fields(s) }

var emojiCats = []emojiCat{
	{"Faccine", splitE(`😀 😃 😄 😁 😆 😅 🤣 😂 🙂 🙃 🫠 😉 😊 😇 🥰 😍 🤩 😘 😗 ☺️ 😚 😙 🥲 😋 😛 😜 🤪 😝 🤑 🤗 🤭 🫢 🤫 🤔 🫡 🤐 🤨 😐 😑 😶 🫥 😏 😒 🙄 😬 😮‍💨 🤥 😌 😔 😪 🤤 😴 😷 🤒 🤕 🤢 🤮 🤧 🥵 🥶 🥴 😵 😵‍💫 🤯 🤠 🥳 🥸 😎 🤓 🧐 😕 🫤 😟 🙁 ☹️ 😮 😯 😲 😳 🥺 🥹 😦 😧 😨 😰 😥 😢 😭 😱 😖 😣 😞 😓 😩 😫 🥱 😤 😡 😠 🤬 😈 👿 💀 ☠️ 💩 🤡 👹 👺 👻 👽 👾 🤖 😺 😸 😹 😻 😼 😽 🙀 😿 😾 🙈 🙉 🙊`)},
	{"Gesti", splitE(`👋 🤚 🖐️ ✋ 🖖 🫱 🫲 👌 🤌 🤏 ✌️ 🤞 🫰 🤟 🤘 🤙 👈 👉 👆 🖕 👇 ☝️ 🫵 👍 👎 ✊ 👊 🤛 🤜 👏 🙌 🫶 👐 🤲 🤝 🙏 ✍️ 💅 🤳 💪 🦾 🦵 🦶 👂 👃 🧠 👀 👁️ 👅 👄 🫦 👶 🧒 👦 👧 🧑 👨 👩 🧓 👴 👵 🙋 🙋‍♂️ 🙋‍♀️ 🤷 🤷‍♂️ 🤷‍♀️ 🤦 🤦‍♂️ 🤦‍♀️ 🙇 💁 🙅 🙆 🧏 🏃 🚶 💃 🕺 👯 🧘 👫 👬 👭 💏 💑 👪`)},
	{"Cuori", splitE(`❤️ 🧡 💛 💚 💙 🩵 💜 🤎 🖤 🩶 🤍 🩷 💔 ❤️‍🔥 ❤️‍🩹 ❣️ 💕 💞 💓 💗 💖 💘 💝 💟 💌 💋 💯 💢 💥 💫 💦 💨 🕳️ 💬 💭 💤 ✨ 🌟 ⭐ 🔥 🎉 🎊`)},
	{"Natura", splitE(`🐶 🐱 🐭 🐹 🐰 🦊 🐻 🐼 🐨 🐯 🦁 🐮 🐷 🐸 🐵 🐔 🐧 🐦 🐤 🦆 🦅 🦉 🦇 🐺 🐗 🐴 🦄 🐝 🪲 🐛 🦋 🐌 🐞 🐜 🕷️ 🐢 🐍 🦎 🐙 🦑 🦐 🦀 🐡 🐠 🐟 🐬 🐳 🐋 🦈 🐊 🐅 🐆 🦓 🦍 🐘 🦒 🐪 🐄 🐎 🐖 🐑 🐐 🦌 🐕 🐈 🐓 🦃 🕊️ 🐇 🦔 🐾 🌵 🎄 🌲 🌳 🌴 🌱 🌿 ☘️ 🍀 🍁 🍂 🍃 🍄 🌾 💐 🌷 🌹 🥀 🌺 🌸 🌼 🌻 🌞 🌝 🌛 🌚 🌙 🌎 🌍 🪐 ⭐ ⚡ ☄️ 🌈 ☀️ 🌤️ ⛅ 🌦️ 🌧️ ⛈️ 🌩️ ❄️ ☃️ ⛄ 🌬️ 💧 🌊`)},
	{"Cibo", splitE(`🍏 🍎 🍐 🍊 🍋 🍌 🍉 🍇 🍓 🫐 🍈 🍒 🍑 🥭 🍍 🥥 🥝 🍅 🍆 🥑 🥦 🥬 🥒 🌶️ 🌽 🥕 🧄 🧅 🥔 🍠 🥐 🥯 🍞 🥖 🥨 🧀 🥚 🍳 🥞 🧇 🥓 🥩 🍗 🍖 🌭 🍔 🍟 🍕 🥪 🌮 🌯 🥗 🥘 🍝 🍜 🍲 🍛 🍣 🍱 🥟 🍤 🍙 🍚 🍘 🍥 🍢 🍡 🍧 🍨 🍦 🥧 🧁 🍰 🎂 🍮 🍭 🍬 🍫 🍿 🍩 🍪 🌰 🥜 🍯 🥛 ☕ 🫖 🍵 🧃 🥤 🧋 🍶 🍺 🍻 🥂 🍷 🥃 🍸 🍹 🧉 🍾 🧊`)},
	{"Attività", splitE(`⚽ 🏀 🏈 ⚾ 🥎 🎾 🏐 🏉 🥏 🎱 🏓 🏸 🏒 🥍 🏏 ⛳ 🏹 🎣 🥊 🥋 🛹 ⛸️ 🎿 ⛷️ 🏂 🏋️ 🤸 ⛹️ 🤺 🤾 🏌️ 🏇 🧘 🏄 🏊 🚴 🏆 🥇 🥈 🥉 🏅 🎖️ 🎗️ 🎫 🎟️ 🎪 🎭 🎨 🎬 🎤 🎧 🎼 🎹 🥁 🎷 🎺 🎸 🪕 🎻 🎲 ♟️ 🎯 🎳 🎮 🎰 🧩`)},
	{"Viaggi", splitE(`🚗 🚕 🚙 🚌 🚎 🏎️ 🚓 🚑 🚒 🚐 🛻 🚚 🚛 🚜 🛵 🏍️ 🚲 🛴 🚨 🚔 🚍 🚘 🚖 🚡 🚠 🚟 🚃 🚋 🚝 🚄 🚅 🚈 🚂 🚆 🚇 🚊 ✈️ 🛫 🛬 🛩️ 💺 🚀 🛸 🚁 🛶 ⛵ 🚤 🛥️ 🛳️ ⛴️ 🚢 ⚓ ⛽ 🚧 🚦 🚥 🗺️ 🗿 🗽 🗼 🏰 🏯 🏟️ 🎡 🎢 🎠 ⛲ ⛱️ 🏖️ 🏝️ 🏜️ 🌋 ⛰️ 🏔️ 🏕️ ⛺ 🏠 🏡 🏘️ 🏗️ 🏭 🏢 🏬 🏣 🏥 🏦 🏨 🏪 🏫 🏩 💒 🏛️ ⛪ 🕌 🕍 🌅 🌄 🌠 🎇 🎆 🌇 🌆 🏙️ 🌃 🌌 🌉 🌁 🇮🇹 🇪🇺 🏳️‍🌈`)},
	{"Oggetti", splitE(`⌚ 📱 💻 ⌨️ 🖥️ 🖨️ 🖱️ 💾 💿 📷 📸 📹 🎥 📞 ☎️ 📺 📻 🎙️ ⏰ ⌛ ⏳ 📡 🔋 🔌 💡 🔦 🕯️ 🧯 💸 💵 💶 💰 💳 💎 ⚖️ 🧰 🔧 🔨 ⚒️ 🛠️ 🔩 ⚙️ 🧱 🔫 💣 🔪 🛡️ 🔮 💈 🔭 🔬 💊 💉 🩹 🧬 🌡️ 🧹 🧺 🧻 🚽 🚿 🛁 🔑 🗝️ 🚪 🛋️ 🛏️ 🧸 🖼️ 🛍️ 🛒 🎁 🎈 🎀 🪄 ✉️ 📩 📦 📮 📝 📁 📂 📅 📆 📌 📍 📎 🖇️ 📏 📐 ✂️ 🗑️ 🔒 🔓 🖊️ ✏️ 🔍 🔎 📚 📖 🔖 🏷️`)},
	{"Simboli", splitE(`✅ ☑️ ✔️ ❌ ❎ ➕ ➖ ➗ ✖️ ♾️ ‼️ ⁉️ ❓ ❔ ❕ ❗ 〰️ ⚠️ 🚸 ⛔ 🚫 🚳 🚭 🚯 🚱 🚷 📵 🔞 ☢️ ☣️ ⬆️ ↗️ ➡️ ↘️ ⬇️ ↙️ ⬅️ ↖️ ↕️ ↔️ ↩️ ↪️ 🔄 🔙 🔚 🔛 🔜 🔝 🛐 ⚛️ 🕉️ ✡️ ☸️ ☯️ ✝️ ☦️ ☪️ ☮️ 🕎 🔯 ♈ ♉ ♊ ♋ ♌ ♍ ♎ ♏ ♐ ♑ ♒ ♓ ⛎ 🔀 🔁 🔂 ▶️ ⏩ ⏭️ ⏯️ ◀️ ⏪ ⏮️ 🔼 ⏫ 🔽 ⏬ ⏸️ ⏹️ ⏺️ 🎦 🔅 🔆 📶 📳 📴 ♻️ 🔱 📛 🔰 ⭕ 🆗 🆒 🆕 🆓 🆙 🆘 🔴 🟠 🟡 🟢 🔵 🟣 🟤 ⚫ ⚪ 🟥 🟧 🟨 🟩 🟦 🟪 🟫 ⬛ ⬜ 🔶 🔷 🔸 🔹 🔺 🔻 💠 🔘 🔳 🔲 🏁 🚩 🎌 🏴 🏳️`)},
}

const (
	tabRecent  = -1
	tabSticker = -2
	tabGIF     = -3
)

type emojiPanel struct {
	open     bool
	tab      int // >=0 categoria, oppure tabRecent / tabSticker / tabGIF
	tabs     []widget.Clickable
	recentT  widget.Clickable
	stickerT widget.Clickable
	gifT     widget.Clickable
	cells    []widget.Clickable
	list     widget.List
	mlist    widget.List
	addBtn   widget.Clickable
	media    []mediaRef
	mediaTab int
	mediaAt  time.Time
	search   widget.Editor
	onlineG  onlineSearch
	onlineS  onlineSearch
}

func (cw *chatWin) toggleEmoji() {
	cw.emoji.open = !cw.emoji.open
	if cw.emoji.tabs == nil {
		cw.emoji.tabs = make([]widget.Clickable, len(emojiCats))
		cw.emoji.list.Axis = layout.Vertical
		if len(cw.set.Recent) > 0 {
			cw.emoji.tab = -1
		}
	}
}

func (cw *chatWin) pickEmoji(e string) {
	if cw.reactPick != nil { // il pannello sta scegliendo una reazione
		cw.u.be.React(cw.reactPick, e)
		cw.reactPick = nil
		cw.emoji.open = false
	} else {
		cw.editor.Insert(e)
	}
	cw.u.Update(func(s *Settings) {
		r := []string{e}
		for _, x := range s.Recent {
			if x != e && len(r) < 32 {
				r = append(r, x)
			}
		}
		s.Recent = r
	})
}

func (cw *chatWin) emojiPanel(gtx C) D {
	if !cw.emoji.open {
		cw.reactPick = nil
		return D{}
	}
	th, p, ep := cw.th, cw.pal, &cw.emoji
	if ep.stickerT.Clicked(gtx) {
		ep.tab, ep.mlist.Position = tabSticker, layout.Position{}
	}
	if ep.gifT.Clicked(gtx) {
		ep.tab, ep.mlist.Position = tabGIF, layout.Position{}
	}
	if cw.reactPick != nil && ep.tab < tabRecent {
		ep.tab = 0 // per le reazioni servono le emoji
	}
	var items []string
	switch {
	case ep.tab == tabRecent:
		items = cw.set.Recent
	case ep.tab >= 0:
		items = emojiCats[ep.tab].emojis
	}
	if len(ep.cells) < len(items) {
		ep.cells = make([]widget.Clickable, len(items)+16)
	}
	for i := range items {
		if ep.cells[i].Clicked(gtx) {
			cw.pickEmoji(items[i])
			gtx.Execute(key.FocusCmd{Tag: &cw.editor})
		}
	}
	if ep.recentT.Clicked(gtx) {
		ep.tab = tabRecent
		ep.list.Position = layout.Position{}
	}
	for i := range ep.tabs {
		if ep.tabs[i].Clicked(gtx) {
			ep.tab = i
			ep.list.Position = layout.Position{}
		}
	}

	height := dp(gtx, 230)
	gtx.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, height))
	fillRect(gtx, p.Surface, image.Rectangle{Max: gtx.Constraints.Max})
	fillRect(gtx, p.Divider, image.Rect(0, 0, gtx.Constraints.Max.X, 1))

	tabBtn := func(cl *widget.Clickable, glyph string, active bool) layout.FlexChild {
		return layout.Rigid(func(gtx C) D {
			return cl.Layout(gtx, func(gtx C) D {
				pointer.CursorPointer.Add(gtx.Ops)
				s := dp(gtx, 32)
				r := image.Rectangle{Max: image.Pt(s, s)}
				if active || cl.Hovered() {
					fillRRect(gtx, p.Hover, r, dp(gtx, 8))
				}
				g := gtx
				g.Constraints = layout.Exact(r.Max)
				layout.Center.Layout(g, lbl(th, 16, p.Text, glyph).Layout)
				if active {
					fillRRect(gtx, p.Accent, image.Rect(dp(gtx, 10), s-dp(gtx, 2), s-dp(gtx, 10), s), dp(gtx, 1))
				}
				return D{Size: r.Max}
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 8, Right: 8, Top: 6, Bottom: 2}.Layout(gtx, func(gtx C) D {
				tabs := []layout.FlexChild{tabBtn(&ep.recentT, "🕘", ep.tab == tabRecent)}
				for i, c := range emojiCats {
					tabs = append(tabs, tabBtn(&ep.tabs[i], c.emojis[0], ep.tab == i))
				}
				if cw.reactPick == nil {
					tabs = append(tabs, layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
						cw.textTab(&ep.stickerT, "Sticker", ep.tab == tabSticker),
						cw.textTab(&ep.gifT, "GIF", ep.tab == tabGIF))
				} else {
					tabs = append(tabs, layout.Flexed(1, func(gtx C) D { return D{Size: image.Pt(gtx.Constraints.Min.X, 0)} }),
						layout.Rigid(func(gtx C) D {
							return layout.Inset{Top: 8, Right: 4}.Layout(gtx, lbl(th, 11.5, p.Accent, "Scegli la reazione").Layout)
						}))
				}
				return layout.Flex{}.Layout(gtx, tabs...)
			})
		}),
		layout.Flexed(1, func(gtx C) D {
			if ep.tab == tabSticker || ep.tab == tabGIF {
				return cw.mediaGrid(gtx)
			}
			if len(items) == 0 {
				return layout.Center.Layout(gtx, lbl(th, 12, p.Faint, "Le emoji che usi compariranno qui").Layout)
			}
			cell := dp(gtx, 36)
			pad := dp(gtx, 8)
			cols := max(1, (gtx.Constraints.Max.X-2*pad)/cell)
			rows := (len(items) + cols - 1) / cols
			ls := material.List(th, &ep.list)
			ls.AnchorStrategy = material.Overlay
			return ls.Layout(gtx, rows, func(gtx C, row int) D {
				st := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
				for c := 0; c < cols; c++ {
					i := row*cols + c
					if i >= len(items) {
						break
					}
					o := op.Offset(image.Pt(c*cell, 0)).Push(gtx.Ops)
					g := gtx
					g.Constraints = layout.Exact(image.Pt(cell, cell))
					cl := &ep.cells[i]
					e := items[i]
					cl.Layout(g, func(gtx C) D {
						pointer.CursorPointer.Add(gtx.Ops)
						if cl.Hovered() {
							fillRRect(gtx, p.Hover, image.Rectangle{Max: gtx.Constraints.Max}, dp(gtx, 8))
						}
						return layout.Center.Layout(gtx, lbl(th, 21, p.Text, e).Layout)
					})
					o.Pop()
				}
				st.Pop()
				return D{Size: image.Pt(gtx.Constraints.Max.X, cell)}
			})
		}),
	)
}

func (cw *chatWin) textTab(cl *widget.Clickable, label string, active bool) layout.FlexChild {
	th, p := cw.th, cw.pal
	return layout.Rigid(func(gtx C) D {
		return cl.Layout(gtx, func(gtx C) D {
			pointer.CursorPointer.Add(gtx.Ops)
			mc := op.Record(gtx.Ops)
			c := p.Muted
			if active {
				c = p.Text
			}
			d := layout.Inset{Left: 10, Right: 10, Top: 7, Bottom: 8}.Layout(gtx, bold(lbl(th, 12, c, label)).Layout)
			call := mc.Stop()
			if active || cl.Hovered() {
				fillRRect(gtx, p.Hover, image.Rectangle{Max: d.Size}, dp(gtx, 8))
			}
			if active {
				fillRRect(gtx, p.Accent, image.Rect(dp(gtx, 10), d.Size.Y-dp(gtx, 2), d.Size.X-dp(gtx, 10), d.Size.Y), dp(gtx, 1))
			}
			call.Add(gtx.Ops)
			return d
		})
	})
}

// mediaGrid: ricerca online (KLIPY), sticker/GIF recenti da reinviare e pulsante per sceglierne uno dal PC.
func (cw *chatWin) mediaGrid(gtx C) D {
	th, p, ep, be, u := cw.th, cw.pal, &cw.emoji, cw.u.be, cw.u
	isGIF := ep.tab == tabGIF
	kind, srch := "stickers", &ep.onlineS
	if isGIF {
		kind, srch = "gifs", &ep.onlineG
	}
	if ep.mediaTab != ep.tab || time.Since(ep.mediaAt) > 5*time.Second {
		if isGIF {
			ep.media = be.RecentGIFs(24)
		} else {
			ep.media = be.RecentStickers(36)
		}
		if ep.mediaTab != ep.tab {
			ep.search.SetText("")
		}
		ep.mediaTab, ep.mediaAt = ep.tab, time.Now()
		ep.mlist.Axis = layout.Vertical
		ep.search.SingleLine = true
	}
	for {
		if _, ok := ep.search.Update(gtx); !ok {
			break
		}
		ep.mlist.Position = layout.Position{}
	}
	q := strings.TrimSpace(ep.search.Text())
	key := u.settings.KlipyKey
	if key != "" {
		w := cw.w
		srch.run(key, u.settings.CustomerID, kind, q, w.Invalidate)
	}
	online, onlineErr, loading := srch.snapshot()

	if ep.addBtn.Clicked(gtx) {
		owner, jid, reply := cw.hwnd, cw.jid, cw.reply
		cw.reply = nil
		go func() {
			if isGIF {
				path := PickFile(owner, "Scegli una GIF", "GIF e video brevi|*.gif;*.mp4|Tutti i file|*.*")
				if path != "" {
					if err := be.SendGIFFile(jid, path, reply); err != nil {
						cw.showNotice("GIF non inviata: " + err.Error())
					}
				}
			} else {
				path := PickFile(owner, "Crea uno sticker da un'immagine", "Immagini|*.png;*.jpg;*.jpeg;*.webp;*.gif|Tutti i file|*.*")
				if path != "" {
					if err := be.SendStickerFile(jid, path, reply); err != nil {
						cw.showNotice("Sticker non creato: " + err.Error())
					}
				}
			}
		}()
		ep.open = false
	}

	// celle: 0 = "dal PC", poi i recenti (solo senza ricerca), poi i risultati online
	var local []mediaRef
	if q == "" {
		local = ep.media
	}
	n := 1 + len(local) + len(online)
	cellW, cellH := dp(gtx, 76), dp(gtx, 76)
	if isGIF {
		cellW, cellH = dp(gtx, 120), dp(gtx, 86)
	}
	pad := dp(gtx, 8)
	var next time.Time

	header := func(gtx C) D {
		return layout.Inset{Left: 10, Right: 10, Top: 4, Bottom: 6}.Layout(gtx, func(gtx C) D {
			if key == "" {
				return lbl(th, 11.5, p.Faint, "Per cercare GIF e sticker online, inserisci la chiave KLIPY gratuita in Opzioni.").Layout(gtx)
			}
			return rounded(gtx, p.Field, 8, 10, 6, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(icon(th, 11, p.Faint, icSearch).Layout),
					layout.Rigid(layout.Spacer{Width: 8}.Layout),
					layout.Flexed(1, func(gtx C) D {
						hint := "Cerca sticker"
						if isGIF {
							hint = "Cerca GIF"
						}
						e := material.Editor(th, &ep.search, hint)
						e.TextSize = 12.5
						e.Color, e.HintColor = p.Text, p.Faint
						return e.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						switch {
						case loading:
							return lbl(th, 11, p.Faint, "…").Layout(gtx)
						case onlineErr != "":
							return lbl(th, 11, p.Faint, onlineErr).Layout(gtx)
						}
						return lbl(th, 10, p.Faint, "KLIPY").Layout(gtx)
					}),
				)
			})
		})
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(header),
		layout.Flexed(1, func(gtx C) D {
			cols := max(1, (gtx.Constraints.Max.X-2*pad)/cellW)
			rows := (n + cols - 1) / cols
			ls := material.List(th, &ep.mlist)
			ls.AnchorStrategy = material.Overlay
			d := ls.Layout(gtx, rows, func(gtx C, row int) D {
				st := op.Offset(image.Pt(pad, 0)).Push(gtx.Ops)
				for c := 0; c < cols; c++ {
					i := row*cols + c
					if i >= n {
						break
					}
					o := op.Offset(image.Pt(c*cellW, 0)).Push(gtx.Ops)
					g := gtx
					g.Constraints = layout.Exact(image.Pt(cellW, cellH))
					switch {
					case i == 0:
						label := "＋ Crea sticker\nda immagine"
						if isGIF {
							label = "＋ GIF dal PC"
						}
						ep.addBtn.Layout(g, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							r := image.Rectangle{Max: gtx.Constraints.Max}.Inset(dp(gtx, 3))
							bg := subtleBg(p)
							if ep.addBtn.Hovered() {
								bg = p.Hover
							}
							fillRRect(gtx, bg, r, dp(gtx, 10))
							l := lbl(th, 11.5, p.Accent, label)
							l.Alignment = 2
							return layout.Center.Layout(gtx, l.Layout)
						})
					case i-1 < len(local):
						ref := local[i-1]
						var cl *widget.Clickable
						if isGIF {
							cl = cw.click("pg:"+ref.Chat+ref.ID, func() {
								be.ResendGIF(cw.jid, ref.Chat, ref.ID, cw.reply)
								cw.reply, ep.open = nil, false
							})
						} else {
							cl = cw.click("ps:"+ref.Chat+ref.ID, func() {
								be.ResendSticker(cw.jid, ref.Chat, ref.ID, cw.reply)
								cw.reply, ep.open = nil, false
							})
						}
						cl.Layout(g, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							r := image.Rectangle{Max: gtx.Constraints.Max}.Inset(dp(gtx, 3))
							if cl.Hovered() {
								fillRRect(gtx, p.Hover, r, dp(gtx, 10))
							}
							inner := r.Inset(dp(gtx, 4))
							st := op.Offset(inner.Min).Push(gtx.Ops)
							cw.mediaThumb(gtx, ref, inner.Size(), isGIF)
							st.Pop()
							return D{Size: gtx.Constraints.Max}
						})
					default:
						it := online[i-1-len(local)]
						it.LoadPreview(cw.w.Invalidate)
						jid := cw.jid
						cl := cw.click("ol:"+kind+it.ID, func() {
							reply := cw.reply
							cw.reply, ep.open = nil, false
							go func() {
								if err := be.SendOnline(jid, it, !isGIF, reply); err != nil {
									cw.showNotice("Invio non riuscito: " + err.Error())
								}
							}()
						})
						cl.Layout(g, func(gtx C) D {
							pointer.CursorPointer.Add(gtx.Ops)
							r := image.Rectangle{Max: gtx.Constraints.Max}.Inset(dp(gtx, 3))
							if cl.Hovered() {
								fillRRect(gtx, p.Hover, r, dp(gtx, 10))
							}
							inner := r.Inset(dp(gtx, 3))
							st := op.Offset(inner.Min).Push(gtx.Ops)
							box := inner.Size()
							if a := it.Anim(); a != nil {
								fr, nx := a.Frame(gtx.Now)
								if !nx.IsZero() && (next.IsZero() || nx.Before(next)) {
									next = nx
								}
								if isGIF {
									cs := clip.UniformRRect(image.Rectangle{Max: box}, dp(gtx, 6)).Push(gtx.Ops)
									drawImage(gtx, fr, a.size, box)
									cs.Pop()
								} else {
									sc := min(float32(box.X)/float32(a.size.X), float32(box.Y)/float32(a.size.Y))
									w, h := int(float32(a.size.X)*sc), int(float32(a.size.Y)*sc)
									s2 := op.Offset(image.Pt((box.X-w)/2, (box.Y-h)/2)).Push(gtx.Ops)
									tr := op.Affine(f32Scale(sc)).Push(gtx.Ops)
									fr.Add(gtx.Ops)
									paint.PaintOp{}.Add(gtx.Ops)
									tr.Pop()
									s2.Pop()
								}
							} else {
								fillRRect(gtx, subtleBg(p), image.Rectangle{Max: box}, dp(gtx, 6))
							}
							st.Pop()
							return D{Size: gtx.Constraints.Max}
						})
					}
					o.Pop()
				}
				st.Pop()
				return D{Size: image.Pt(gtx.Constraints.Max.X, cellH)}
			})
			if !next.IsZero() {
				gtx.Execute(op.InvalidateCmd{At: next})
			}
			return d
		}),
	)
}

// mediaThumb disegna l'anteprima di uno sticker (immagine vera) o di una GIF (miniatura).
func (cw *chatWin) mediaThumb(gtx C, ref mediaRef, box image.Point, isGIF bool) {
	p := cw.pal
	if !isGIF {
		if op_, size, st := cw.u.be.MediaImage(ref.Chat, ref.ID); st == MediaReady {
			sc := min(float32(box.X)/float32(size.X), float32(box.Y)/float32(size.Y))
			w, h := int(float32(size.X)*sc), int(float32(size.Y)*sc)
			s := op.Offset(image.Pt((box.X-w)/2, (box.Y-h)/2)).Push(gtx.Ops)
			tr := op.Affine(f32Scale(sc)).Push(gtx.Ops)
			op_.Add(gtx.Ops)
			paint.PaintOp{}.Add(gtx.Ops)
			tr.Pop()
			s.Pop()
			return
		}
	}
	if len(ref.Thumb) > 0 {
		if tOp, tSize, ok := thumbOp(ref.Chat+"/"+ref.ID, ref.Thumb); ok {
			st := clip.UniformRRect(image.Rectangle{Max: box}, dp(gtx, 6)).Push(gtx.Ops)
			drawImage(gtx, tOp, tSize, box)
			st.Pop()
			return
		}
	}
	fillRRect(gtx, subtleBg(p), image.Rectangle{Max: box}, dp(gtx, 6))
}
