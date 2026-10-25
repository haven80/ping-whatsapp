package main

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// Menzioni nei gruppi. Nel testo WhatsApp viaggia "@<numero o identificativo>" più l'elenco
// delle persone menzionate; ogni client sostituisce poi il numero con il nome.

type Member struct {
	JID   string
	Name  string
	Admin bool
}

type memberCache struct {
	mu      sync.Mutex
	members map[string][]Member
	fetched map[string]time.Time
	errs    map[string]string
	loading map[string]bool
}

var groupMembers = memberCache{members: map[string][]Member{}, fetched: map[string]time.Time{},
	errs: map[string]string{}, loading: map[string]bool{}}

// MembersStatus dice se la lista è in caricamento o se il caricamento è fallito.
func (b *Backend) MembersStatus(group string) (loading bool, errText string) {
	groupMembers.mu.Lock()
	defer groupMembers.mu.Unlock()
	return groupMembers.loading[group], groupMembers.errs[group]
}

// GroupMembers restituisce i partecipanti del gruppo (aggiornati in sottofondo ogni 10 minuti).
// Finché la lista non arriva, usa chi ha scritto nel gruppo.
func (b *Backend) GroupMembers(group string) []Member {
	c := &groupMembers
	c.mu.Lock()
	list, last := c.members[group], c.fetched[group]
	if time.Since(last) > 10*time.Minute && b.cli != nil {
		c.fetched[group] = time.Now() // evita richieste doppie
		c.loading[group] = true
		safeGo("partecipanti gruppo", func() { b.fetchMembers(group) })
	}
	c.mu.Unlock()
	if len(list) > 0 {
		return list
	}
	return b.db.knownSenders(group)
}

func (b *Backend) fetchMembers(group string) {
	fail := func(msg string) {
		groupMembers.mu.Lock()
		groupMembers.loading[group] = false
		groupMembers.errs[group] = msg
		groupMembers.fetched[group] = time.Now().Add(-9 * time.Minute) // riprova tra un minuto
		groupMembers.mu.Unlock()
		b.changed(group)
	}
	j, err := types.ParseJID(group)
	if err != nil || j.Server != types.GroupServer {
		fail("indirizzo del gruppo non valido")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	info, err := b.cli.GetGroupInfo(ctx, j)
	if err != nil {
		logHandled("partecipanti del gruppo "+group, err)
		fail("partecipanti non disponibili: " + err.Error())
		return
	}
	me := b.myJID()
	var out []Member
	for _, p := range info.Participants {
		if p.JID.User == me.User || (!p.PhoneNumber.IsEmpty() && p.PhoneNumber.User == me.User) {
			continue
		}
		name := ""
		if !p.PhoneNumber.IsEmpty() {
			name = b.contactName(p.PhoneNumber.ToNonAD())
		}
		if name == "" {
			name = b.contactName(b.norm(p.JID))
		}
		if name == "" && p.DisplayName != "" {
			name = p.DisplayName
		}
		if name == "" {
			if !p.PhoneNumber.IsEmpty() {
				name = prettyNumber(p.PhoneNumber)
			} else {
				name = prettyNumber(p.JID)
			}
		}
		out = append(out, Member{JID: p.JID.ToNonAD().String(), Name: name, Admin: p.IsAdmin || p.IsSuperAdmin})
	}
	sort.Slice(out, func(i, k int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name) })
	groupMembers.mu.Lock()
	groupMembers.members[group] = out
	groupMembers.loading[group] = false
	delete(groupMembers.errs, group)
	groupMembers.mu.Unlock()
	b.changed(group)
}

// knownSenders: chi ha scritto nel gruppo (ripiego finché non arriva la lista completa).
func (s *Store) knownSenders(group string) []Member {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT sender, MAX(sender_name) FROM messages
		WHERE chat=? AND from_me=0 AND sender<>'' AND sender_name<>'' GROUP BY sender ORDER BY MAX(sender_name)`, group)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		rows.Scan(&m.JID, &m.Name)
		out = append(out, m)
	}
	return out
}

// renderMentions sostituisce "@numero" con "@Nome" nei messaggi ricevuti.
func (b *Backend) renderMentions(text string, mentioned []string) string {
	for _, s := range mentioned {
		j, err := types.ParseJID(s)
		if err != nil || j.User == "" || !strings.Contains(text, "@"+j.User) {
			continue
		}
		var name string
		if me := b.myJID(); j.User == me.User {
			_, name = b.Me()
		} else {
			name = b.contactName(b.norm(j))
			if name == "" {
				// nei gruppi recenti l'indirizzo può essere un identificativo anonimo: cerco tra i partecipanti
				groupMembers.mu.Lock()
				for _, list := range groupMembers.members {
					for _, m := range list {
						if strings.HasPrefix(m.JID, j.User+"@") {
							name = m.Name
						}
					}
				}
				groupMembers.mu.Unlock()
			}
			if name == "" {
				name = prettyNumber(b.norm(j))
			}
		}
		text = strings.ReplaceAll(text, "@"+j.User, "@"+name)
	}
	return text
}

// SendTextMentions invia un testo con menzioni: names associa "Nome" (come scritto dopo @) al JID.
func (b *Backend) SendTextMentions(jid, text string, names map[string]string, reply *Message) {
	text = strings.TrimRight(text, " \n\r\t")
	if strings.TrimSpace(text) == "" {
		return
	}
	wire, mentioned := mentionWire(text, names)
	if len(mentioned) == 0 {
		b.SendText(jid, text, reply)
		return
	}
	m := &Message{Text: text, Kind: KText}
	b.sendPrepared(jid, m, reply, func(ci *waE2E.ContextInfo) (*waE2E.Message, error) {
		if ci == nil {
			ci = &waE2E.ContextInfo{}
		}
		ci.MentionedJID = mentioned
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(wire), ContextInfo: ci}}, nil
	})
}

// mentionWire converte "@Nome" nel formato di WhatsApp ("@numero") e restituisce i JID menzionati.
func mentionWire(text string, names map[string]string) (string, []string) {
	// prima i nomi più lunghi, così "@Marco Rossi" non viene spezzato da "@Marco"
	keys := make([]string, 0, len(names))
	for n := range names {
		keys = append(keys, n)
	}
	sort.Slice(keys, func(i, k int) bool { return len(keys[i]) > len(keys[k]) })
	wire := text
	var mentioned []string
	for _, n := range keys {
		if !strings.Contains(wire, "@"+n) {
			continue // menzione cancellata dal testo prima dell'invio
		}
		j, err := types.ParseJID(names[n])
		if err != nil || j.User == "" {
			continue
		}
		wire = strings.ReplaceAll(wire, "@"+n, "@"+j.User)
		mentioned = append(mentioned, j.String())
	}
	return wire, mentioned
}
