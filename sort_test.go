package main

import (
	"strings"
	"testing"
)

func TestSortChatRows(t *testing.T) {
	mk := func() []rowItem {
		// già in ordine di ultimo messaggio
		return []rowItem{
			{chat: Chat{Name: "A"}},
			{chat: Chat{Name: "B", Unread: 2}},
			{chat: Chat{Name: "C", Pinned: 100}},
			{chat: Chat{Name: "D"}},
			{chat: Chat{Name: "E", Unread: 1}},
			{chat: Chat{Name: "F", Pinned: 200}},
		}
	}
	names := func(r []rowItem) string {
		var s []string
		for _, x := range r {
			s = append(s, x.chat.Name)
		}
		return strings.Join(s, "")
	}
	r := mk()
	sortChatRows(r, false)
	if got := names(r); got != "FCABDE" {
		t.Fatalf("solo fissate: %s", got)
	}
	r = mk()
	sortChatRows(r, true)
	if got := names(r); got != "FCBEAD" {
		t.Fatalf("non lette in alto: %s", got)
	}
}
