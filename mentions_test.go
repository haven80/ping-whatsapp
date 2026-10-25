package main

import "testing"

func TestMentionWire(t *testing.T) {
	names := map[string]string{
		"Marco":       "393401111222@s.whatsapp.net",
		"Marco Rossi": "393409999888@s.whatsapp.net",
		"Luca":        "123456789@lid",
		"Tolto":       "390000000000@s.whatsapp.net",
	}
	wire, ment := mentionWire("Ciao @Marco Rossi e @Marco, @Luca vieni?", names)
	want := "Ciao @393409999888 e @393401111222, @123456789 vieni?"
	if wire != want {
		t.Fatalf("testo:\n got %q\nwant %q", wire, want)
	}
	if len(ment) != 3 {
		t.Fatalf("menzionati: %v", ment)
	}
	for _, m := range ment {
		if m == "390000000000@s.whatsapp.net" {
			t.Fatal("una menzione tolta dal testo non va inviata")
		}
	}
}
