package main

import "gioui.org/font"

// Su Windows icone e testo usano i font di sistema.
const (
	iconFace = font.Typeface("Segoe Fluent Icons,Segoe MDL2 Assets")
	textFace = font.Typeface("Segoe UI Variable Text,Segoe UI,Ping Emoji,sans-serif")
)

func platformFaces() []font.FontFace { return nil }
