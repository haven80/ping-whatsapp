package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"os"
	"sort"
	"sync"
	"time"

	"gioui.org/op/paint"
	"golang.org/x/image/webp"
)

// Anim è un'animazione pronta da disegnare (sticker animati, GIF).
type Anim struct {
	frames []paint.ImageOp
	delays []time.Duration
	size   image.Point
	total  time.Duration
}

// Frame restituisce il fotogramma da mostrare ora e quando passare al successivo.
func (a *Anim) Frame(now time.Time) (paint.ImageOp, time.Time) {
	if len(a.frames) == 1 || a.total <= 0 {
		return a.frames[0], time.Time{}
	}
	t := time.Duration(now.UnixNano()) % a.total
	for i, d := range a.delays {
		if t < d {
			return a.frames[i], now.Add(d - t)
		}
		t -= d
	}
	return a.frames[0], now.Add(a.delays[0])
}

type animEntry struct {
	a     *Anim
	state int
	used  time.Time
}

type animCache struct {
	mu    sync.Mutex
	items map[string]*animEntry
	queue chan [2]string
}

const maxCachedAnims = 6

func (b *Backend) initAnims() {
	b.anims = &animCache{items: map[string]*animEntry{}, queue: make(chan [2]string, 256)}
	go supervise("animazioni", b.animWorker)
}

// AnimOf restituisce l'animazione del messaggio, avviandone il caricamento se serve.
func (b *Backend) AnimOf(chat, id string) (*Anim, int) {
	c, k := b.anims, chat+"/"+id
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.items[k]
	if e == nil {
		e = &animEntry{state: MediaLoading}
		c.items[k] = e
		select {
		case c.queue <- [2]string{chat, id}:
		default:
			delete(c.items, k)
			return nil, MediaNone
		}
	}
	e.used = time.Now()
	return e.a, e.state
}

func (b *Backend) animWorker() {
	for job := range b.anims.queue {
		chat, id := job[0], job[1]
		var a *Anim
		if path, err := b.ensureMediaFile(chat, id); err == nil {
			a, _ = loadAnim(path, 300)
		}
		c := b.anims
		c.mu.Lock()
		if e := c.items[chat+"/"+id]; e != nil {
			if a != nil {
				e.a, e.state = a, MediaReady
			} else {
				e.state = MediaFailed
			}
		}
		// libera le animazioni non viste da più tempo
		var ready []string
		for k, e := range c.items {
			if e.state == MediaReady {
				ready = append(ready, k)
			}
		}
		if len(ready) > maxCachedAnims {
			sort.Slice(ready, func(i, j int) bool { return c.items[ready[i]].used.Before(c.items[ready[j]].used) })
			for _, k := range ready[:len(ready)-maxCachedAnims] {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
		b.changed(chat)
	}
}

// loadAnim decodifica sticker WebP (animati o no), GIF o video MP4 (le "GIF" di WhatsApp).
func loadAnim(path string, maxPx int) (*Anim, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isWebP(data) || isGIF(data) {
		return loadAnimData(data, maxPx, 200)
	}
	frames, delays, err := decodeVideoFrames(path, maxPx, 150)
	if err != nil {
		return nil, err
	}
	return makeAnim(frames, delays)
}

func isWebP(d []byte) bool {
	return len(d) > 12 && string(d[0:4]) == "RIFF" && string(d[8:12]) == "WEBP"
}
func isGIF(d []byte) bool { return len(d) > 6 && string(d[0:4]) == "GIF8" }

// loadAnimData decodifica WebP o GIF già in memoria.
func loadAnimData(data []byte, maxPx, maxFrames int) (*Anim, error) {
	var frames []image.Image
	var delays []time.Duration
	switch {
	case isWebP(data):
		r, err := decodeAnimWebP(data, maxPx)
		if err != nil {
			return nil, err
		}
		frames, delays = r.frames, r.delays
	case isGIF(data):
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		fr, dl := compositeGIF(g)
		for i := range fr {
			frames = append(frames, fitThumb(fr[i], maxPx))
		}
		delays = dl
	default:
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		frames, delays = []image.Image{fitThumb(img, maxPx)}, []time.Duration{0}
	}
	if len(frames) > maxFrames {
		frames, delays = frames[:maxFrames], delays[:maxFrames]
	}
	return makeAnim(frames, delays)
}

func makeAnim(frames []image.Image, delays []time.Duration) (*Anim, error) {
	if len(frames) == 0 {
		return nil, errors.New("nessun fotogramma")
	}
	a := &Anim{size: frames[0].Bounds().Size()}
	for i, f := range frames {
		op := paint.NewImageOp(f)
		op.Filter = paint.FilterLinear
		a.frames = append(a.frames, op)
		d := delays[i]
		if d < 20*time.Millisecond {
			d = 100 * time.Millisecond
		}
		a.delays = append(a.delays, d)
		a.total += d
	}
	return a, nil
}

// compositeGIF ricompone i fotogrammi di una GIF (che spesso contengono solo le differenze).
func compositeGIF(g *gif.GIF) ([]image.Image, []time.Duration) {
	w, h := g.Config.Width, g.Config.Height
	if w == 0 || h == 0 {
		b := g.Image[0].Bounds()
		w, h = b.Dx(), b.Dy()
	}
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var frames []image.Image
	var delays []time.Duration
	for i, f := range g.Image {
		if i >= 300 {
			break
		}
		var prev *image.RGBA
		disp := byte(0)
		if i < len(g.Disposal) {
			disp = g.Disposal[i]
		}
		if disp == gif.DisposalPrevious {
			prev = image.NewRGBA(canvas.Bounds())
			copy(prev.Pix, canvas.Pix)
		}
		draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
		snap := image.NewRGBA(canvas.Bounds())
		copy(snap.Pix, canvas.Pix)
		frames = append(frames, snap)
		d := 100 * time.Millisecond
		if i < len(g.Delay) && g.Delay[i] > 1 {
			d = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		delays = append(delays, d)
		switch disp {
		case gif.DisposalBackground:
			draw.Draw(canvas, f.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, prev.Pix)
		}
	}
	return frames, delays
}

type webpAnim struct {
	frames []image.Image
	delays []time.Duration
}

func u24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// decodeAnimWebP decodifica un WebP, anche animato: ogni fotogramma viene reimpacchettato
// come immagine WebP singola, decodificato e ricomposto sulla tela.
func decodeAnimWebP(data []byte, maxPx int) (*webpAnim, error) {
	if len(data) < 16 {
		return nil, errors.New("webp troppo corto")
	}
	var cw, ch int
	type frame struct {
		x, y, w, h int
		dur        time.Duration
		noBlend    bool
		dispose    bool
		alph, bits []byte
		lossless   bool
	}
	var frames []frame
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4:]))
		body := pos + 8
		if body+size > len(data) {
			break
		}
		p := data[body : body+size]
		switch id {
		case "VP8X":
			if len(p) >= 10 {
				cw, ch = u24(p[4:])+1, u24(p[7:])+1
			}
		case "ANMF":
			if len(p) < 16 {
				break
			}
			f := frame{x: u24(p[0:]) * 2, y: u24(p[3:]) * 2, w: u24(p[6:]) + 1, h: u24(p[9:]) + 1,
				dur: time.Duration(u24(p[12:])) * time.Millisecond, noBlend: p[15]&2 != 0, dispose: p[15]&1 != 0}
			q := 16
			for q+8 <= len(p) {
				sid := string(p[q : q+4])
				ss := int(binary.LittleEndian.Uint32(p[q+4:]))
				if q+8+ss > len(p) {
					break
				}
				switch sid {
				case "ALPH":
					f.alph = p[q : q+8+ss]
				case "VP8 ":
					f.bits = p[q : q+8+ss]
				case "VP8L":
					f.bits, f.lossless = p[q:q+8+ss], true
				}
				q += 8 + ss + ss&1
			}
			frames = append(frames, f)
		}
		pos = body + size + size&1
	}

	if len(frames) == 0 { // immagine statica
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if maxPx > 0 {
			img = fitThumb(img, maxPx)
		}
		return &webpAnim{frames: []image.Image{img}, delays: []time.Duration{0}}, nil
	}
	if cw == 0 || ch == 0 {
		cw, ch = frames[0].w, frames[0].h
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, cw, ch))
	out := &webpAnim{}
	for i, f := range frames {
		if i >= 200 || f.bits == nil {
			break
		}
		img, err := webp.Decode(bytes.NewReader(wrapWebPFrame(f.w, f.h, f.alph, f.bits, f.lossless)))
		if err != nil {
			return nil, err
		}
		r := image.Rect(f.x, f.y, f.x+f.w, f.y+f.h)
		op := draw.Over
		if f.noBlend {
			op = draw.Src
		}
		draw.Draw(canvas, r, img, img.Bounds().Min, op)
		var snap image.Image
		if maxPx > 0 && (cw > maxPx || ch > maxPx) {
			snap = fitThumb(canvas, maxPx)
		} else {
			c := image.NewNRGBA(canvas.Bounds())
			copy(c.Pix, canvas.Pix)
			snap = c
		}
		out.frames = append(out.frames, snap)
		out.delays = append(out.delays, f.dur)
		if f.dispose {
			draw.Draw(canvas, r, image.Transparent, image.Point{}, draw.Src)
		}
	}
	if len(out.frames) == 0 {
		return nil, errors.New("webp senza fotogrammi")
	}
	return out, nil
}

// wrapWebPFrame crea un file WebP autonomo con un solo fotogramma.
func wrapWebPFrame(w, h int, alph, bits []byte, lossless bool) []byte {
	var body bytes.Buffer
	if alph != nil && !lossless {
		vp8x := make([]byte, 18)
		copy(vp8x, "VP8X")
		binary.LittleEndian.PutUint32(vp8x[4:], 10)
		vp8x[8] = 0x10 // ha la trasparenza
		put24 := func(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }
		put24(vp8x[12:], w-1)
		put24(vp8x[15:], h-1)
		body.Write(vp8x)
		body.Write(alph)
		if len(alph)&1 == 1 {
			body.WriteByte(0)
		}
	}
	body.Write(bits)
	if len(bits)&1 == 1 {
		body.WriteByte(0)
	}
	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.Write(&out, binary.LittleEndian, uint32(4+body.Len()))
	out.WriteString("WEBP")
	out.Write(body.Bytes())
	return out.Bytes()
}
