package main

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestForwardContext(t *testing.T) {
	orig := &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption:     proto.String("foto"),
		ContextInfo: &waE2E.ContextInfo{ForwardingScore: proto.Uint32(2), StanzaID: proto.String("citato")},
	}}
	fwd := proto.Clone(orig).(*waE2E.Message)
	if !setContext(fwd, forwardInfo(contextOf(orig))) {
		t.Fatal("contesto non applicato")
	}
	ci := fwd.GetImageMessage().GetContextInfo()
	if !ci.GetIsForwarded() || ci.GetForwardingScore() != 3 || ci.GetStanzaID() != "" {
		t.Fatalf("contesto sbagliato: %v", ci)
	}
	if orig.GetImageMessage().GetContextInfo().GetIsForwarded() {
		t.Fatal("l'originale non deve cambiare")
	}
	if fwd.GetImageMessage().GetCaption() != "foto" {
		t.Fatal("didascalia persa")
	}
}
