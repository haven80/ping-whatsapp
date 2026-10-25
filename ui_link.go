package main

import (
	"image"
	"net/url"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
)

// linkDraft: l'anteprima del link scritto nel campo di testo, prima dell'invio.
type linkDraft struct {
	cand      string    // link trovato nel testo
	since     time.Time // da quando è lì (si aspetta che l'utente finisca di scriverlo)
	dismissed string    // link di cui l'utente ha tolto l'anteprima
	close     widget.Clickable
}

const linkSettle = 700 * time.Millisecond

// draftURL: il link del testo che sta scrivendo, se l'anteprima va mostrata.
func (cw *chatWin) draftURL() string {
	if cw.editing != nil || len(cw.pending) > 0 {
		return "" // modifica o didascalia di un allegato: niente anteprima
	}
	u := firstURL(cw.editor.Text())
	if u == "" || u == cw.lp.dismissed {
		return ""
	}
	return u
}

// sendPreview: l'anteprima da allegare all'invio di text (nil se non serve).
// Se è ancora in caricamento aspetta poco, per non trattenere il messaggio.
func (cw *chatWin) sendPreview(text string) func() *LinkPreview {
	u := firstURL(text)
	if u == "" || u == cw.lp.dismissed || len(cw.pending) > 0 {
		return nil
	}
	cw.lp = linkDraft{}
	return func() *LinkPreview { return PreviewWait(u, 4*time.Second) }
}

func (cw *chatWin) linkStrip(gtx C) D {
	th, p := cw.th, cw.pal
	u := cw.draftURL()
	if u != cw.lp.cand {
		cw.lp.cand, cw.lp.since = u, time.Now()
	}
	if u == "" {
		return D{}
	}
	if cw.lp.close.Clicked(gtx) {
		cw.lp.dismissed = u
		gtx.Execute(op.InvalidateCmd{})
		return D{}
	}
	if wait := time.Until(cw.lp.since.Add(linkSettle)); wait > 0 {
		gtx.Execute(op.InvalidateCmd{At: cw.lp.since.Add(linkSettle)})
		return D{}
	}
	prev, ready, _ := PreviewAsync(u, cw.invalidate)
	if ready && prev == nil {
		return D{} // la pagina non ha un'anteprima
	}
	host := hostOf(u)
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: 12, Right: 10, Top: 8, Bottom: 2}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				mc := op.Record(gtx.Ops)
				dd := layout.Inset{Left: 10, Top: 6, Bottom: 6, Right: 6}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							if prev == nil || len(prev.Thumb) == 0 {
								return icon(th, 14, p.Accent, icLink).Layout(gtx)
							}
							return thumbSquare(gtx, "lp:"+u, prev.Thumb, 44)
						}),
						layout.Rigid(layout.Spacer{Width: 10}.Layout),
						layout.Flexed(1, func(gtx C) D {
							if prev == nil {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(one(lbl(th, 12, p.Muted, "Caricamento anteprima…")).Layout),
									layout.Rigid(one(lbl(th, 11.5, linkColor(p), host)).Layout),
								)
							}
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(one(bold(lbl(th, 12.5, p.Text, pick(prev.Title, host)))).Layout),
								layout.Rigid(func(gtx C) D {
									if prev.Desc == "" {
										return D{}
									}
									return one(lbl(th, 11.5, p.Muted, prev.Desc)).Layout(gtx)
								}),
								layout.Rigid(one(lbl(th, 11.5, linkColor(p), host)).Layout),
							)
						}),
					)
				})
				call := mc.Stop()
				st := clip.UniformRRect(image.Rectangle{Max: dd.Size}, dp(gtx, 6)).Push(gtx.Ops)
				paint.Fill(gtx.Ops, subtleBg(p))
				fillRect(gtx, linkColor(p), image.Rect(0, 0, dp(gtx, 3), dd.Size.Y))
				st.Pop()
				call.Add(gtx.Ops)
				return dd
			}),
			layout.Rigid(layout.Spacer{Width: 6}.Layout),
			layout.Rigid(func(gtx C) D { return iconButton(gtx, th, p, &cw.lp.close, "", false, p.Muted) }),
		)
	})
	call := m.Stop()
	fillRect(gtx, p.Surface, image.Rectangle{Max: d.Size})
	fillRect(gtx, p.Divider, image.Rect(0, 0, d.Size.X, 1))
	call.Add(gtx.Ops)
	return d
}

// thumbSquare: miniatura ritagliata in un quadrato con gli angoli arrotondati.
func thumbSquare(gtx C, key string, data []byte, size float32) D {
	s := dp(gtx, size)
	tOp, tSize, ok := thumbOp(key, data)
	if !ok {
		return D{Size: image.Pt(s, s)}
	}
	st := clip.UniformRRect(image.Rectangle{Max: image.Pt(s, s)}, dp(gtx, 6)).Push(gtx.Ops)
	drawImage(gtx, tOp, tSize, image.Pt(s, s))
	st.Pop()
	return D{Size: image.Pt(s, s)}
}

func hostOf(u string) string {
	if pu, err := url.Parse(u); err == nil && pu.Host != "" {
		return strings.TrimPrefix(pu.Host, "www.")
	}
	return u
}
