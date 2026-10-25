package main

import (
	"errors"
	"os"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// Inoltro dei messaggi: si riusa il contenuto originale (anche i file restano quelli già
// caricati sui server WhatsApp) con l'etichetta "Inoltrato", come fa l'app ufficiale.

const maxForwardChats = 5 // stesso limite di WhatsApp

// CanForward: testo e contenuti multimediali (con il messaggio originale o il file su disco).
func (b *Backend) CanForward(m *Message) bool {
	switch m.Kind {
	case KText:
		return strings.TrimSpace(m.Text) != ""
	case KImage, KVideo, KAudio, KDoc, KSticker:
		return m.Media != nil || m.MediaPath != ""
	}
	return false
}

// setContext applica la ContextInfo al contenuto del messaggio, qualunque tipo sia.
func setContext(m *waE2E.Message, ci *waE2E.ContextInfo) bool {
	switch {
	case m.ExtendedTextMessage != nil:
		m.ExtendedTextMessage.ContextInfo = ci
	case m.ImageMessage != nil:
		m.ImageMessage.ContextInfo = ci
	case m.VideoMessage != nil:
		m.VideoMessage.ContextInfo = ci
	case m.PtvMessage != nil:
		m.PtvMessage.ContextInfo = ci
	case m.AudioMessage != nil:
		m.AudioMessage.ContextInfo = ci
	case m.DocumentMessage != nil:
		m.DocumentMessage.ContextInfo = ci
	case m.StickerMessage != nil:
		m.StickerMessage.ContextInfo = ci
	default:
		return false
	}
	return true
}

func forwardInfo(prev *waE2E.ContextInfo) *waE2E.ContextInfo {
	score := uint32(1)
	if prev != nil {
		score = prev.GetForwardingScore() + 1
	}
	return &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(score)}
}

// Forward inoltra il messaggio m nella chat "to".
func (b *Backend) Forward(m *Message, to string) error {
	if m.Kind == KText {
		text := cleanEdited(m.Text)
		lm := &Message{Kind: KText, Text: text, Extra: MsgExtra{Forwarded: true}}
		b.sendPrepared(to, lm, nil, func(_ *waE2E.ContextInfo) (*waE2E.Message, error) {
			return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text: proto.String(text), ContextInfo: forwardInfo(nil)}}, nil
		})
		return nil
	}
	// contenuto originale disponibile: lo si riusa
	if raw := b.db.MediaOf(m.Chat, m.ID); raw != nil {
		var orig waE2E.Message
		if proto.Unmarshal(raw, &orig) == nil {
			fwd := proto.Clone(&orig).(*waE2E.Message)
			if setContext(fwd, forwardInfo(contextOf(&orig))) {
				extra := m.Extra
				extra.Forwarded = true
				lm := &Message{Kind: m.Kind, Text: m.Text, Thumb: m.Thumb, FileName: m.FileName,
					MediaPath: b.db.MediaPath(m.Chat, m.ID), Extra: extra}
				b.sendPrepared(to, lm, nil, func(_ *waE2E.ContextInfo) (*waE2E.Message, error) { return fwd, nil })
				return nil
			}
		}
	}
	// altrimenti si ricarica il file dal disco
	path := m.MediaPath
	if path == "" {
		return errors.New("contenuto non disponibile")
	}
	if _, err := os.Stat(path); err != nil {
		return errors.New("file non più presente sul PC")
	}
	switch m.Kind {
	case KImage:
		return b.SendImage(to, path, m.Text, nil)
	case KSticker:
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.SendStickerData(to, data, isAnimatedWebP(data), nil)
	case KVideo:
		b.sendVideo(to, path, m.Extra.GIF, nil)
	default:
		b.SendFile(to, path, nil)
	}
	return nil
}
