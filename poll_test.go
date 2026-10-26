package main

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
)

func TestOptionsFromHashes(t *testing.T) {
	opts := []string{"Venerdì 18", "Sabato 19", "Domenica 20"}
	got := optionsFromHashes(opts, whatsmeow.HashPollOptions([]string{"Domenica 20", "Venerdì 18"}))
	if strings.Join(got, "|") != "Venerdì 18|Domenica 20" {
		t.Fatalf("opzioni: %v", got)
	}
	if len(optionsFromHashes(opts, nil)) != 0 {
		t.Fatal("voto ritirato")
	}
}
