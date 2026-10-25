//go:build linux

package main

import (
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
)

func TestFFmpegGIF(t *testing.T) {
	if !hasCmd("ffmpeg") || !hasCmd("ffprobe") {
		t.Skip("ffmpeg non installato")
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "a.gif")
	g := &gif.GIF{}
	for i := 0; i < 10; i++ {
		p := image.NewPaletted(image.Rect(0, 0, 121, 81), color.Palette{color.White, color.RGBA{200, 30, 30, 255}})
		for y := 0; y < 81; y++ {
			for x := i * 10; x < i*10+20 && x < 121; x++ {
				p.SetColorIndex(x, y, 1)
			}
		}
		g.Image, g.Delay = append(g.Image, p), append(g.Delay, 10)
	}
	f, _ := os.Create(in)
	gif.EncodeAll(f, g)
	f.Close()
	out := filepath.Join(dir, "a.mp4")
	if err := gifToMP4(in, out); err != nil {
		t.Fatal(err)
	}
	vi := probeVideo(out)
	if vi.first == nil || vi.w != 120 || vi.h != 80 {
		t.Fatalf("probe: %+v", vi)
	}
	frames, delays, err := decodeVideoFrames(out, 60, 150)
	if err != nil || len(frames) < 8 || len(delays) != len(frames) {
		t.Fatalf("frames=%d err=%v", len(frames), err)
	}
	if b := frames[0].Bounds(); b.Dx() != 60 || b.Dy() != 40 {
		t.Fatalf("size %v", b)
	}
	t.Logf("%d fotogrammi, %v l'uno, %ds", len(frames), delays[0], vi.seconds)
}
