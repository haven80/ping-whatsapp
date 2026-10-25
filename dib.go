package main

import (
	"encoding/binary"
	"errors"
	"image"
	"image/color"
)

// decodeDIB interpreta una bitmap di Windows (intestazione + pixel) a 24 o 32 bit.
func decodeDIB(d []byte) (image.Image, error) {
	if len(d) < 40 {
		return nil, errors.New("bitmap corta")
	}
	hdr := binary.LittleEndian.Uint32(d[0:])
	w := int(int32(binary.LittleEndian.Uint32(d[4:])))
	h := int(int32(binary.LittleEndian.Uint32(d[8:])))
	bpp := int(binary.LittleEndian.Uint16(d[14:]))
	comp := binary.LittleEndian.Uint32(d[16:])
	clrUsed := int(binary.LittleEndian.Uint32(d[32:]))
	topDown := h < 0
	if topDown {
		h = -h
	}
	if w <= 0 || h <= 0 || w > 20000 || h > 20000 || (bpp != 24 && bpp != 32) || (comp != 0 && comp != 3) {
		return nil, errors.New("bitmap non supportata")
	}
	off := int(hdr)
	if comp == 3 && hdr == 40 {
		off += 12 // maschere dei colori dopo l'intestazione classica
	}
	off += clrUsed * 4
	stride := ((w*bpp + 31) / 32) * 4
	if off+stride*h > len(d) {
		return nil, errors.New("bitmap incompleta")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	hasAlpha := false
	for y := 0; y < h; y++ {
		sy := h - 1 - y
		if topDown {
			sy = y
		}
		row := d[off+sy*stride:]
		for x := 0; x < w; x++ {
			var c color.NRGBA
			if bpp == 32 {
				p := row[x*4:]
				c = color.NRGBA{p[2], p[1], p[0], p[3]}
				if p[3] != 0 {
					hasAlpha = true
				}
			} else {
				p := row[x*3:]
				c = color.NRGBA{p[2], p[1], p[0], 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	if bpp == 32 && !hasAlpha { // molte app lasciano l'alfa a zero: immagine opaca
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	return img, nil
}
