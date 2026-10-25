package main

import (
	"testing"

	"gioui.org/font"
	"gioui.org/text"
	"golang.org/x/image/math/fixed"
)

// Lo spazio dopo un'emoji deve avere la larghezza di uno spazio normale
// (prima era largo quanto un'emoji: "📷     Foto").
func TestEmojiSpace(t *testing.T) {
	th := newTheme()
	params := text.Parameters{Font: font.Font{Typeface: textFace}, PxPerEm: fixed.I(16), MaxWidth: 10000}
	th.Shaper.LayoutString(params, "💟 S")
	var adv []float64
	for {
		g, ok := th.Shaper.NextGlyph()
		if !ok {
			break
		}
		adv = append(adv, float64(g.Advance)/64)
	}
	if len(adv) < 2 || adv[1] > 8 {
		t.Fatalf("spazio dopo l'emoji troppo largo: %v", adv)
	}
}
