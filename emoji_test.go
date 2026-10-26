package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"gioui.org/font/opentype"
)

// I font nel repository devono essere proprio quelli che Ping si aspetta di scaricare.
func TestEmojiFontsMatch(t *testing.T) {
	for _, s := range emojiSets {
		if s.File == "" {
			continue
		}
		data, err := os.ReadFile("fonts/emoji/v1/" + s.File)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != s.SHA || int64(len(data)) != s.Size {
			t.Errorf("%s: impronta o dimensione diverse da emoji.go", s.File)
		}
		if _, err := opentype.ParseCollection(data); err != nil {
			t.Errorf("%s: font non leggibile: %v", s.File, err)
		}
	}
}
