package main

import (
	"encoding/binary"
	"testing"
)

func TestDecodeDIB(t *testing.T) {
	// bitmap 3×2 a 32 bit, dal basso verso l'alto, alfa a zero (come fanno molte app)
	w, h := 3, 2
	d := make([]byte, 40+w*h*4)
	binary.LittleEndian.PutUint32(d[0:], 40)
	binary.LittleEndian.PutUint32(d[4:], uint32(w))
	binary.LittleEndian.PutUint32(d[8:], uint32(h))
	binary.LittleEndian.PutUint16(d[12:], 1)
	binary.LittleEndian.PutUint16(d[14:], 32)
	// riga in basso (y=1) rossa, riga in alto (y=0) blu — in BGRA
	for x := 0; x < w; x++ {
		copy(d[40+x*4:], []byte{0, 0, 255, 0})     // prima riga nel file = in basso
		copy(d[40+w*4+x*4:], []byte{255, 0, 0, 0}) // seconda riga nel file = in alto
	}
	img, err := decodeDIB(d)
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if b>>8 != 255 || r != 0 || g != 0 || a>>8 != 255 {
		t.Fatalf("in alto atteso blu opaco, trovato %v %v %v %v", r>>8, g>>8, b>>8, a>>8)
	}
	r, _, _, _ = img.At(2, 1).RGBA()
	if r>>8 != 255 {
		t.Fatalf("in basso atteso rosso")
	}
	// 24 bit con riempimento di riga
	d24 := make([]byte, 40+4*2) // larghezza 1 → riga di 3 byte arrotondata a 4
	binary.LittleEndian.PutUint32(d24[0:], 40)
	binary.LittleEndian.PutUint32(d24[4:], 1)
	binary.LittleEndian.PutUint32(d24[8:], uint32(0xFFFFFFFE)) // -2: dall'alto verso il basso
	binary.LittleEndian.PutUint16(d24[14:], 24)
	copy(d24[40:], []byte{0, 255, 0})
	if img, err := decodeDIB(d24); err != nil {
		t.Fatal(err)
	} else if _, g, _, _ := img.At(0, 0).RGBA(); g>>8 != 255 {
		t.Fatal("verde atteso in alto")
	}
}
