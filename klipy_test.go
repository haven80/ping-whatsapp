package main

import "testing"

func TestKlipyBadKey(t *testing.T) {
	if testing.Short() {
		t.Skip("usa la rete")
	}
	_, err := KlipySearch("chiave-finta", "x", "gifs", "ciao")
	t.Logf("errore atteso: %v", err)
	if err == nil || err.Error() != "chiave KLIPY non valida" {
		t.Fatalf("messaggio inatteso: %v", err)
	}
}

func TestSearchNoLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("usa la rete")
	}
	var s onlineSearch
	done := make(chan struct{}, 10)
	s.run("chiave-finta", "x", "gifs", "", func() { done <- struct{}{} })
	<-done
	seq := s.seq
	for i := 0; i < 5; i++ { // la UI chiama run a ogni fotogramma
		s.run("chiave-finta", "x", "gifs", "", func() { done <- struct{}{} })
	}
	_, err, loading := s.snapshot()
	t.Logf("errore mostrato: %q, in corso: %v", err, loading)
	if s.seq != seq || loading || err != "chiave KLIPY non valida" {
		t.Fatalf("la ricerca è ripartita o lo stato è sbagliato (seq %d→%d)", seq, s.seq)
	}
}
