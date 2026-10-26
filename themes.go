package main

import "image/color"

// Temi di colore: ognuno ha una versione chiara e una scura, scelte poi da Chiaro/Scuro/Sistema.
// I temi "pastello" colorano leggermente anche gli sfondi, gli altri cambiano solo gli accenti.

type ColorScheme struct {
	ID, Name string
	Pastel   bool
	Light    Pal
	Dark     Pal
}

type schemeSpec struct {
	id, name     string
	accL, accD   uint32 // colore principale (pulsanti, spunte, selezioni)
	mineL, mineD uint32 // le mie bolle
	tintL, tintD uint32 // colore degli sfondi (solo pastello)
	tint         float32
	pastel       bool
}

var colorSchemes = func() []ColorScheme {
	specs := []schemeSpec{
		{id: "ping", name: "Ping", accL: 0x1F9D7A, accD: 0x3CC49B, mineL: 0xD9F2E8, mineD: 0x1D4338},
		{id: "azzurro", name: "Azzurro", accL: 0x2B7BD6, accD: 0x5AA8F0, mineL: 0xDCEBFB, mineD: 0x1C3A5C},
		{id: "oceano", name: "Oceano", accL: 0x0E8FA3, accD: 0x39C2D6, mineL: 0xD6F1F5, mineD: 0x15404A},
		{id: "viola", name: "Viola", accL: 0x7C5CD6, accD: 0xA68CF2, mineL: 0xEAE4FB, mineD: 0x352A5C},
		{id: "arancio", name: "Arancio", accL: 0xDD7420, accD: 0xF2A15A, mineL: 0xFDEBD9, mineD: 0x4A321C},
		{id: "rosso", name: "Ciliegia", accL: 0xD2445A, accD: 0xF07A8C, mineL: 0xFBE1E5, mineD: 0x4E2329},
		{id: "grafite", name: "Grafite", accL: 0x4A5563, accD: 0xAEB6C2, mineL: 0xE4E7EB, mineD: 0x30363F},
		{id: "rosa", name: "Rosa", accL: 0xD06A8A, accD: 0xEE9BB4, mineL: 0xF9DCE5, mineD: 0x4A2A36,
			tintL: 0xF7E6EC, tintD: 0x2B1E24, tint: 0.65, pastel: true},
		{id: "cielo", name: "Cielo", accL: 0x4F91CF, accD: 0x8CC0EE, mineL: 0xD9EBFA, mineD: 0x23384C,
			tintL: 0xE4EFF9, tintD: 0x1B2530, tint: 0.65, pastel: true},
		{id: "menta", name: "Menta", accL: 0x3FA582, accD: 0x7FD4B2, mineL: 0xD5F2E5, mineD: 0x1F4034,
			tintL: 0xE3F4EC, tintD: 0x18261F, tint: 0.65, pastel: true},
	}
	out := make([]ColorScheme, 0, len(specs))
	for _, s := range specs {
		out = append(out, buildScheme(s))
	}
	return out
}()

func buildScheme(s schemeSpec) ColorScheme {
	l, d := lightPal, darkPal
	l.Accent, l.Mine = rgb(s.accL), rgb(s.mineL)
	d.Accent, d.Mine = rgb(s.accD), rgb(s.mineD)
	if s.tint > 0 {
		tl, td, k := rgb(s.tintL), rgb(s.tintD), s.tint
		l.Bg, l.Field, l.Hover = mixColor(l.Bg, tl, k), mixColor(l.Field, tl, k), mixColor(l.Hover, tl, k)
		l.Surface, l.Divider = mixColor(l.Surface, tl, k*0.35), mixColor(l.Divider, tl, k*0.8)
		d.Bg, d.Field, d.Hover = mixColor(d.Bg, td, k), mixColor(d.Field, td, k), mixColor(d.Hover, td, k)
		d.Surface, d.Divider = mixColor(d.Surface, td, k*0.7), mixColor(d.Divider, td, k*0.7)
	}
	return ColorScheme{ID: s.id, Name: s.name, Pastel: s.pastel, Light: l, Dark: d}
}

func schemeByID(id string) ColorScheme {
	for _, s := range colorSchemes {
		if s.ID == id {
			return s
		}
	}
	return colorSchemes[0]
}

func mixColor(a, b color.NRGBA, t float32) color.NRGBA {
	m := func(x, y uint8) uint8 { return uint8(float32(x)*(1-t) + float32(y)*t + 0.5) }
	return color.NRGBA{R: m(a.R, b.R), G: m(a.G, b.G), B: m(a.B, b.B), A: 0xFF}
}

// selColor: colore della selezione del testo, dal colore principale del tema.
func selColor(p Pal) color.NRGBA { return withAlpha(p.Accent, 0x55) }

func withAlpha(c color.NRGBA, a uint8) color.NRGBA { c.A = a; return c }
