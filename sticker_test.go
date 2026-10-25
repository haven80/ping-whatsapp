package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStickerFromFile(t *testing.T) {
	dir := t.TempDir()
	photo, _ := demoPhoto(dir)
	gifP, _, _ := demoAnim(dir)
	for _, p := range []string{photo, gifP, demoSticker(dir)} {
		data, anim, err := stickerFromFile(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		a, err := decodeAnimWebP(data, 0)
		if err != nil {
			t.Fatalf("rilettura %s: %v", filepath.Base(p), err)
		}
		sz := a.frames[0].Bounds().Size()
		t.Logf("%-16s -> %6d byte, animato=%v, fotogrammi=%d, %dx%d", filepath.Base(p), len(data), anim, len(a.frames), sz.X, sz.Y)
		if sz.X != 512 || sz.Y != 512 {
			t.Errorf("dimensioni sbagliate")
		}
		os.WriteFile(filepath.Join(dir, "x.webp"), data, 0o644)
		if _, err := loadAnim(filepath.Join(dir, "x.webp"), 300); err != nil {
			t.Errorf("loadAnim: %v", err)
		}
	}
}
