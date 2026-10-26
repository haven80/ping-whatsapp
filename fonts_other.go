//go:build !windows

package main

import (
	_ "embed"

	"gioui.org/font"
	"gioui.org/font/opentype"
)

// Fuori da Windows non c'è Segoe Fluent Icons: le stesse icone (stessi codici) vengono da un
// piccolo font incorporato, ricavato da Fluent UI System Icons di Microsoft (licenza MIT).
//
//go:embed fonts/PingIcons.ttf
var iconTTF []byte

const (
	iconFace = font.Typeface("Ping Icons")
	textFace = font.Typeface("Ping Emoji,Inter,Noto Sans,Cantarell,Ubuntu,DejaVu Sans,sans-serif") // emoji per primo: altrimenti ❤ e le faccine arrivano in bianco e nero dai font di testo
)

func platformFaces() []font.FontFace {
	faces, err := opentype.ParseCollection(iconTTF)
	if err != nil {
		return nil
	}
	for i := range faces {
		faces[i].Font.Typeface = iconFace
	}
	return faces
}
