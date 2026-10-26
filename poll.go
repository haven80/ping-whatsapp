package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// Sondaggi. I voti viaggiano cifrati con una chiave legata al sondaggio (whatsmeow la conserva
// quando il sondaggio arriva o viene inviato) e contengono solo le impronte SHA-256 delle
// opzioni scelte: qui si risale ai nomi confrontandole con le opzioni salvate.

const maxPollOptions = 12

// optionsFromHashes traduce le impronte nelle opzioni del sondaggio.
func optionsFromHashes(options []string, hashes [][]byte) []string {
	var out []string
	for _, o := range options {
		h := sha256.Sum256([]byte(o))
		for _, x := range hashes {
			if string(x) == string(h[:]) {
				out = append(out, o)
				break
			}
		}
	}
	return out
}

// onPollVote: è arrivato un voto (anche mio, dal telefono).
func (b *Backend) onPollVote(e *events.Message, chat string, sender types.JID) {
	pu := e.Message.GetPollUpdateMessage()
	pollID := pu.GetPollCreationMessageKey().GetID()
	opts := b.db.PollOptions(chat, pollID)
	if len(opts) == 0 || b.cli == nil {
		return // sondaggio che non abbiamo
	}
	vote, err := b.cli.DecryptPollVote(context.Background(), e)
	if err != nil {
		logHandled("voto del sondaggio "+pollID, err)
		return
	}
	voter, name := sender.String(), b.displayName(sender)
	if e.Info.IsFromMe {
		voter, name = "me", ""
	}
	b.db.SetPollVote(chat, pollID, voter, name, optionsFromHashes(opts, vote.GetSelectedOptions()), e.Info.Timestamp)
	b.changed(chat)
}

// historyPollVotes: nella cronologia i voti arrivano già decifrati, insieme al sondaggio.
func (b *Backend) historyPollVotes(chat types.JID, isGroup bool, pollID string, updates []*waWeb.PollUpdate) {
	chatID := chat.String()
	opts := b.db.PollOptions(chatID, pollID)
	if len(opts) == 0 {
		return
	}
	for _, pu := range updates {
		key := pu.GetPollUpdateMessageKey()
		voter, name := "me", ""
		if !key.GetFromMe() {
			j := chat
			if isGroup {
				pj, err := types.ParseJID(key.GetParticipant())
				if err != nil {
					continue
				}
				j = pj
			}
			j = b.norm(j)
			voter, name = j.String(), b.displayName(j)
		}
		ts := time.UnixMilli(pu.GetSenderTimestampMS())
		b.db.SetPollVote(chatID, pollID, voter, name, optionsFromHashes(opts, pu.GetVote().GetSelectedOptions()), ts)
	}
}

// VotePoll invia il mio voto (choices vuoto = ritiro il voto).
func (b *Backend) VotePoll(m *Message, choices []string) error {
	b.db.SetPollVote(m.Chat, m.ID, "me", "", choices, time.Now())
	b.changed(m.Chat)
	if b.cli == nil || !b.cli.IsLoggedIn() {
		return nil
	}
	chat, err := types.ParseJID(m.Chat)
	if err != nil {
		return err
	}
	var sender types.JID
	if m.FromMe {
		if b.cli.Store.ID == nil {
			return errors.New("non collegato")
		}
		sender = b.cli.Store.ID.ToNonAD()
	} else if sender, err = types.ParseJID(m.Sender); err != nil || m.Sender == "" {
		sender = chat // chat privata: chi ha creato il sondaggio è l'altra persona
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// la chiave del sondaggio è salvata con il mittente nella forma in cui è arrivato (numero o LID)
	if _, real, err := b.cli.Store.MsgSecrets.GetMessageSecret(ctx, chat, sender, m.ID); err == nil && !real.IsEmpty() {
		sender = real
	}
	info := &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsFromMe: m.FromMe, IsGroup: chat.Server == types.GroupServer},
		ID:            m.ID,
	}
	msg, err := b.cli.BuildPollVote(ctx, info, choices)
	if err != nil {
		if strings.Contains(err.Error(), "secret") {
			return errors.New("questo sondaggio è arrivato prima che Ping fosse collegato: vota dal telefono")
		}
		return err
	}
	_, err = b.cli.SendMessage(ctx, chat, msg)
	return err
}

// SendPoll crea un sondaggio. multi = si possono scegliere più opzioni.
func (b *Backend) SendPoll(jid, question string, options []string, multi bool) {
	sel := 1
	if multi {
		sel = 0 // nessun limite
	}
	m := &Message{Kind: KPoll, Extra: MsgExtra{Title: question, Options: options, Max: uint32(sel)}}
	b.sendPrepared(jid, m, nil, func(ci *waE2E.ContextInfo) (*waE2E.Message, error) {
		return b.cli.BuildPollCreation(question, options, sel), nil
	})
}
