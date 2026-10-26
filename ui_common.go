package main

import (
	"bytes"
	_ "embed"
	"hash/fnv"
	"image"
	"image/color"
	_ "image/png"
	"strings"
	"sync"
	"time"
	"unicode"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/font/opentype"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	xdraw "golang.org/x/image/draw"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

type Pal struct {
	Dark      bool
	Bg        color.NRGBA // fondo finestra
	Surface   color.NRGBA // intestazioni, bolle altrui
	Field     color.NRGBA // campi di testo
	Hover     color.NRGBA
	Text      color.NRGBA
	Muted     color.NRGBA
	Faint     color.NRGBA
	Divider   color.NRGBA
	Accent    color.NRGBA
	AccentTxt color.NRGBA
	Mine      color.NRGBA // bolle mie
	Online    color.NRGBA
	Read      color.NRGBA // spunte blu
	Danger    color.NRGBA
}

func rgb(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

func argb(c uint32, a uint8) color.NRGBA { x := rgb(c); x.A = a; return x }

var lightPal = Pal{
	Bg: rgb(0xF4F5F7), Surface: rgb(0xFFFFFF), Field: rgb(0xECEEF1), Hover: rgb(0xEEF0F3),
	Text: rgb(0x1C2026), Muted: rgb(0x6B7280), Faint: rgb(0x9CA3AF), Divider: rgb(0xE6E8EC),
	Accent: rgb(0x1F9D7A), AccentTxt: rgb(0xFFFFFF), Mine: rgb(0xD9F2E8),
	Online: rgb(0x2DB36F), Read: rgb(0x2F8CE6), Danger: rgb(0xD64545),
}

var darkPal = Pal{
	Dark: true,
	Bg:   rgb(0x15171B), Surface: rgb(0x1E2126), Field: rgb(0x262A30), Hover: rgb(0x23272D),
	Text: rgb(0xE6E8EB), Muted: rgb(0x9AA1AB), Faint: rgb(0x6B7280), Divider: rgb(0x2A2E35),
	Accent: rgb(0x3CC49B), AccentTxt: rgb(0x0E1A16), Mine: rgb(0x1D4338),
	Online: rgb(0x3CC97E), Read: rgb(0x53A7F5), Danger: rgb(0xF06A6A),
}

// Caratteri delle icone (Segoe Fluent Icons su Windows 11, Segoe MDL2 Assets su Windows 10).
const (
	icSearch  = ""
	icAttach  = ""
	icSend    = ""
	icPhoto   = ""
	icList    = ""
	icVolume  = ""
	icMute    = ""
	icPower   = ""
	icGroup   = ""
	icDoc     = ""
	icChevron = ""
	icChevUp  = ""
	icPin     = "\uE718"
	emojiFace = font.Typeface("Ping Emoji")
)

//go:embed fonts/PingEmoji.ttf
var emojiTTF []byte

// newTheme usa i font di sistema per il testo e un font emoji incorporato (o scaricato, vedi
// emoji.go): il font emoji di Windows è vettoriale a colori e Gio lo mostrerebbe in bianco e nero.
func newTheme() *material.Theme {
	th := material.NewTheme()
	th.Shaper, _ = newShaper()
	th.Face = textFace
	return th
}

// newShaper crea il motore del testo con il font emoji attuale; restituisce anche la sua versione.
func newShaper() (*text.Shaper, int) {
	data, ver := currentEmojiFont()
	var faces []font.FontFace // un'unica raccolta: più WithCollection non si sommano
	if em, err := opentype.ParseCollection(data); err == nil {
		for i := range em {
			em[i].Font.Typeface = emojiFace
		}
		faces = append(faces, em...)
	}
	faces = append(faces, platformFaces()...)
	var opts []text.ShaperOption
	if len(faces) > 0 {
		opts = append(opts, text.WithCollection(faces))
	}
	return text.NewShaper(opts...), ver
}

// refreshEmoji ricrea il motore del testo della finestra se nel frattempo è cambiato lo stile delle emoji.
func refreshEmoji(th *material.Theme, ver *int) {
	if v := emojiFontVersion(); v != *ver {
		th.Shaper, *ver = newShaper()
	}
}

func dp(gtx C, v float32) int { return gtx.Dp(unit.Dp(v)) }

func fillRect(gtx C, c color.NRGBA, r image.Rectangle) {
	paint.FillShape(gtx.Ops, c, clip.Rect(r).Op())
}

func fillRRect(gtx C, c color.NRGBA, r image.Rectangle, rad int) {
	paint.FillShape(gtx.Ops, c, clip.UniformRRect(r, rad).Op(gtx.Ops))
}

func fillCircle(gtx C, c color.NRGBA, r image.Rectangle) {
	paint.FillShape(gtx.Ops, c, clip.Ellipse(r).Op(gtx.Ops))
}

// lbl crea un'etichetta di testo con dimensione, colore e peso.
func lbl(th *material.Theme, size float32, c color.NRGBA, s string) material.LabelStyle {
	l := material.Label(th, unit.Sp(size), s)
	l.Color = c
	return l
}

func bold(l material.LabelStyle) material.LabelStyle { l.Font.Weight = font.SemiBold; return l }
func one(l material.LabelStyle) material.LabelStyle  { l.MaxLines = 1; return l }
func icon(th *material.Theme, size float32, c color.NRGBA, glyph string) material.LabelStyle {
	l := lbl(th, size, c, glyph)
	l.Font.Typeface = iconFace
	return l
}

// rounded disegna lo sfondo arrotondato attorno a un contenuto con margine interno.
func rounded(gtx C, bg color.NRGBA, rad, padH, padV float32, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	d := layout.Inset{Left: unit.Dp(padH), Right: unit.Dp(padH), Top: unit.Dp(padV), Bottom: unit.Dp(padV)}.Layout(gtx, w)
	call := m.Stop()
	fillRRect(gtx, bg, image.Rectangle{Max: d.Size}, dp(gtx, rad))
	call.Add(gtx.Ops)
	return d
}

// iconButton: piccolo pulsante rotondo con un'icona; evidenziato al passaggio del mouse.
func iconButton(gtx C, th *material.Theme, p Pal, cl *widget.Clickable, glyph string, active bool, tint color.NRGBA) D {
	return cl.Layout(gtx, func(gtx C) D {
		sz := dp(gtx, 32)
		r := image.Rectangle{Max: image.Pt(sz, sz)}
		if cl.Hovered() || active {
			fillRRect(gtx, p.Hover, r, dp(gtx, 8))
		}
		pointer.CursorPointer.Add(gtx.Ops)
		gtx.Constraints = layout.Exact(r.Max)
		return layout.Center.Layout(gtx, icon(th, 14, tint, glyph).Layout)
	})
}

// ---- avatar ----

var avatarColors = []uint32{0x4E8DF5, 0xE5735B, 0x36B37E, 0x9B6BDF, 0xF0A33A, 0x2BB3C0, 0xE0609A, 0x6C7A89}

func hashColor(s string) color.NRGBA {
	h := fnv.New32a()
	h.Write([]byte(s))
	return rgb(avatarColors[h.Sum32()%uint32(len(avatarColors))])
}

func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		r := []rune(w)
		if len(r) == 0 {
			continue
		}
		if unicode.IsLetter(r[0]) || unicode.IsDigit(r[0]) {
			out = append(out, unicode.ToUpper(r[0]))
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	if strings.HasPrefix(name, "+") {
		return "#"
	}
	return string(out)
}

type avKey struct {
	src  image.Image
	size int
}

var (
	avCacheMu sync.Mutex
	avCache   = map[avKey]paint.ImageOp{}
	thumbMu   sync.Mutex
	thumbs    = map[string]thumbEntry{}
)

type thumbEntry struct {
	op   paint.ImageOp
	size image.Point
	ok   bool
}

// scaledOp ritaglia al centro e ridimensiona l'immagine alla misura esatta in pixel.
func scaledOp(src image.Image, size int) paint.ImageOp {
	avCacheMu.Lock()
	defer avCacheMu.Unlock()
	k := avKey{src, size}
	if op, ok := avCache[k]; ok {
		return op
	}
	b := src.Bounds()
	s := min(b.Dx(), b.Dy())
	crop := image.Rect(b.Min.X+(b.Dx()-s)/2, b.Min.Y+(b.Dy()-s)/2, 0, 0)
	crop.Max = crop.Min.Add(image.Pt(s, s))
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, xdraw.Src, nil)
	op := paint.NewImageOp(dst)
	avCache[k] = op
	return op
}

// avatar disegna foto o iniziali in un quadrato arrotondato (stile Messenger), con il pallino di stato.
func avatar(gtx C, th *material.Theme, p Pal, be *Backend, jid, name string, size float32, isGroup bool, online bool, ring color.NRGBA) D {
	px := dp(gtx, size)
	r := image.Rectangle{Max: image.Pt(px, px)}
	rad := px * 30 / 100
	img, known := be.Avatar(jid)
	if !known {
		be.RequestAvatar(jid)
	}
	if img != nil {
		st := clip.UniformRRect(r, rad).Push(gtx.Ops)
		op := scaledOp(img, px)
		op.Filter = paint.FilterLinear
		op.Add(gtx.Ops)
		paint.PaintOp{}.Add(gtx.Ops)
		st.Pop()
	} else {
		bg := hashColor(jid)
		fillRRect(gtx, bg, r, rad)
		gtx2 := gtx
		gtx2.Constraints = layout.Exact(r.Max)
		if isGroup {
			layout.Center.Layout(gtx2, icon(th, size*0.42, rgb(0xFFFFFF), icGroup).Layout)
		} else {
			l := bold(lbl(th, size*0.38, rgb(0xFFFFFF), initials(name)))
			layout.Center.Layout(gtx2, l.Layout)
		}
	}
	if online {
		d := px * 30 / 100
		ringW := max(2, dp(gtx, 2))
		o := image.Pt(px-d+ringW/2, px-d+ringW/2)
		fillCircle(gtx, ring, image.Rectangle{Min: o.Sub(image.Pt(ringW, ringW)), Max: o.Add(image.Pt(d, d))})
		fillCircle(gtx, p.Online, image.Rectangle{Min: o, Max: o.Add(image.Pt(d-ringW, d-ringW))})
	}
	return D{Size: r.Max}
}

// thumbOp decodifica (una volta) l'anteprima JPEG di un messaggio.
func thumbOp(key string, data []byte) (paint.ImageOp, image.Point, bool) {
	thumbMu.Lock()
	defer thumbMu.Unlock()
	if t, ok := thumbs[key]; ok {
		return t.op, t.size, t.ok
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		thumbs[key] = thumbEntry{}
		return paint.ImageOp{}, image.Point{}, false
	}
	t := thumbEntry{op: paint.NewImageOp(img), size: img.Bounds().Size(), ok: true}
	t.op.Filter = paint.FilterLinear
	thumbs[key] = t
	return t.op, t.size, true
}

// ---- date e ore in italiano ----

var giorni = []string{"dom", "lun", "mar", "mer", "gio", "ven", "sab"}
var giorniLunghi = []string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"}
var mesi = []string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func listTime(t time.Time) string {
	if t.IsZero() || t.Unix() <= 0 {
		return ""
	}
	t = t.Local()
	today := dayStart(time.Now())
	switch {
	case !t.Before(today):
		return t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "ieri"
	case !t.Before(today.AddDate(0, 0, -6)):
		return giorni[t.Weekday()]
	}
	return t.Format("02/01/06")
}

func dayLabel(t time.Time) string {
	t = t.Local()
	today := dayStart(time.Now())
	switch {
	case !t.Before(today):
		return "Oggi"
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Ieri"
	case !t.Before(today.AddDate(0, 0, -6)):
		s := giorniLunghi[t.Weekday()]
		return strings.ToUpper(s[:1]) + s[1:]
	}
	s := t.Format("2") + " " + mesi[t.Month()-1]
	if t.Year() != time.Now().Year() {
		s += " " + t.Format("2006")
	}
	return s
}

func lastSeenText(p Presence) string {
	if p.Online {
		return "online"
	}
	if p.LastSeen.IsZero() {
		return ""
	}
	t := p.LastSeen.Local()
	today := dayStart(time.Now())
	switch {
	case !t.Before(today):
		return "ultimo accesso oggi alle " + t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "ultimo accesso ieri alle " + t.Format("15:04")
	}
	return "ultimo accesso il " + t.Format("02/01") + " alle " + t.Format("15:04")
}

func isTyping(p Presence) bool { return !p.Typing.IsZero() && time.Since(p.Typing) < 12*time.Second }

func f32Scale(s float32) f32.Affine2D { return f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(s, s)) }

func f32pt(x, y float32) f32.Point { return f32.Pt(x, y) }

//go:embed winres/icon256.png
var appIconPNG []byte

var appIcon = func() image.Image {
	img, _, err := image.Decode(bytes.NewReader(appIconPNG))
	if err != nil {
		return nil
	}
	return img
}()
