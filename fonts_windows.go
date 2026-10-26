package main

import "gioui.org/font"

// Su Windows icone e testo usano i font di sistema.
const (
	iconFace = font.Typeface("Segoe Fluent Icons,Segoe MDL2 Assets")
	textFace = font.Typeface("Ping Emoji,Segoe UI Variable Text,Segoe UI,sans-serif") // emoji per primo: contiene solo emoji (più spazio, © e ®)
)

func platformFaces() []font.FontFace { return nil }
