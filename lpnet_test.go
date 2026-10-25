package main

import "testing"

func TestFetchPreviewNet(t *testing.T) {
	if testing.Short() {
		t.Skip("rete")
	}
	p, err := fetchPreview("https://github.com/haven80/ping-whatsapp")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("titolo=%q desc=%q thumb=%d byte", p.Title, p.Desc, len(p.Thumb))
	if p.Title == "" {
		t.Fatal("senza titolo")
	}
}
