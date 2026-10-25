package main

import (
	"strings"
	"testing"
)

func TestParseMeta(t *testing.T) {
	page := `<html><head><title>Titolo &amp; pagina</title>
<meta name="description" content="descrizione semplice">
<meta property="og:title" content="Titolo OG">
<meta property="og:image" content="/img/a.jpg">
</head><body><meta property="og:title" content="non questo"></body></html>`
	m := parseMeta(strings.NewReader(page))
	if m["og:title"] != "Titolo OG" || m["title"] != "Titolo & pagina" || m["description"] != "descrizione semplice" || m["og:image"] != "/img/a.jpg" {
		t.Fatalf("meta sbagliati: %#v", m)
	}
}

func TestLinkSpans(t *testing.T) {
	s := linkSpans("guarda www.esempio.it/a, e https://x.com/b?c=1.")
	var got []string
	for _, p := range s {
		got = append(got, p.text+"|"+p.url)
	}
	want := []string{"guarda |", "www.esempio.it/a|https://www.esempio.it/a", ", e |", "https://x.com/b?c=1|https://x.com/b?c=1", ".|"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("pezzi:\n%s", strings.Join(got, "\n"))
	}
	if linkSpans("niente link qui") != nil {
		t.Fatal("testo senza link")
	}
}

func TestMatchedIn(t *testing.T) {
	if matchedIn("vai su www.a.it ora", "https://www.a.it") != "www.a.it" {
		t.Fatal("matched")
	}
}
