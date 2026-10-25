package main

import "testing"

func TestVideoPitch(t *testing.T) {
	cases := []struct {
		name        string
		vf          videoFormat
		buf         int
		pitch, rows int
	}{
		{"esatto", videoFormat{w: 480, h: 270, stride: 1920}, 1920 * 270, 1920, 270},
		{"righe arrotondate a 16", videoFormat{w: 480, h: 270, stride: 1920}, 1920 * 272, 1920, 272},
		{"riempimento a fine riga, stride dichiarato sbagliato", videoFormat{w: 498, h: 280, stride: 1992}, 2048 * 280, 2048, 280},
		{"stride dichiarato giusto con riempimento", videoFormat{w: 498, h: 280, stride: 2048}, 2048 * 288, 2048, 288},
		{"capovolto", videoFormat{w: 320, h: 240, stride: -1280}, 1280 * 240, 1280, 240},
	}
	for _, c := range cases {
		p, r, ok := c.vf.pitch(c.buf)
		if !ok || p != c.pitch || r != c.rows {
			t.Errorf("%s: pitch=%d righe=%d ok=%v, attesi %d/%d", c.name, p, r, ok, c.pitch, c.rows)
		}
	}
}
