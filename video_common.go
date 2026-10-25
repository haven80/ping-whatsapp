package main

import (
	"image"
	"image/gif"
	"io"

	xdraw "golang.org/x/image/draw"
)

func decodeGIFFile(r io.Reader) (*gif.GIF, error) { return gif.DecodeAll(r) }

// fitExact ridimensiona l'immagine esattamente a w×h.
func fitExact(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

// videoFormat: misure dei fotogrammi in uscita dal decoder.
type videoFormat struct {
	w, h       int // dimensioni del buffer (possono essere arrotondate, es. 272 invece di 270)
	stride     int // byte per riga dichiarati (negativo = immagine capovolta)
	cx, cy     int // area visibile (senza i bordi di riempimento)
	offX, offY int
}

// pitch ricava i byte per riga REALI dalla dimensione del fotogramma ricevuto, senza fidarsi
// solo di quanto dichiarato: i decoder aggiungono spesso riempimento a fine riga o righe in più.
func (vf *videoFormat) pitch(bufLen int) (pitch, rows int, ok bool) {
	minRow := vf.w * 4
	abs := vf.stride
	if abs < 0 {
		abs = -abs
	}
	// 1) valore dichiarato, solo se divide esattamente il buffer (con al massimo 32 righe in più)
	if abs >= minRow && bufLen%abs == 0 {
		if r := bufLen / abs; r >= vf.h && r <= vf.h+32 {
			return abs, r, true
		}
	}
	align := func(v, a int) int { return (v + a - 1) / a * a }
	// 2) righe effettive (eventualmente arrotondate) che dividono esattamente il buffer
	for _, r := range []int{vf.h, align(vf.h, 2), align(vf.h, 16), align(vf.h, 32)} {
		if r > 0 && bufLen%r == 0 {
			if p := bufLen / r; p >= minRow && p < minRow+512 {
				return p, r, true
			}
		}
	}
	// 3) ultime risorse: valore dichiarato o righe senza riempimento, se ci stanno
	if abs >= minRow && abs*vf.h <= bufLen {
		return abs, bufLen / abs, true
	}
	for _, p := range []int{minRow, align(minRow, 64), align(minRow, 128), align(minRow, 256)} {
		if p*vf.h <= bufLen {
			return p, bufLen / p, true
		}
	}
	return 0, 0, false
}
