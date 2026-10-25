package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// ProfileInfo: i dati della scheda di un contatto o di un gruppo.
type ProfileInfo struct {
	JID      string
	IsGroup  bool
	Name     string // nome in rubrica (o del gruppo)
	Phone    string // +39…
	PushName string // nome che la persona si è data su WhatsApp
	About    string // "Info"
	Business string // nome verificato dell'account business
	Topic    string // descrizione del gruppo
	Created  time.Time
	Count    int    // partecipanti
	Photo    string // file della foto grande ("" se non c'è o non ancora scaricata)
	Loading  bool
	Err      string
}

var profiles = struct {
	mu sync.Mutex
	m  map[string]*ProfileInfo
	at map[string]time.Time
}{m: map[string]*ProfileInfo{}, at: map[string]time.Time{}}

// Profile restituisce la scheda (subito, con quello che si sa) e la aggiorna in sottofondo.
func (b *Backend) Profile(jid string) ProfileInfo {
	profiles.mu.Lock()
	defer profiles.mu.Unlock()
	p := profiles.m[jid]
	if p == nil || (!p.Loading && time.Since(profiles.at[jid]) > 5*time.Minute) {
		base := b.localProfile(jid)
		if p != nil { // tiene i dati già scaricati finché arrivano i nuovi
			base.About, base.Business, base.Topic, base.Created, base.Count, base.Photo =
				p.About, p.Business, p.Topic, p.Created, p.Count, p.Photo
		}
		base.Loading = true
		p = &base
		profiles.m[jid] = p
		profiles.at[jid] = time.Now()
		safeGo("scheda contatto", func() { b.fetchProfile(jid) })
	}
	return *p
}

// localProfile: quello che si sa senza chiedere a WhatsApp.
func (b *Backend) localProfile(jid string) ProfileInfo {
	p := ProfileInfo{JID: jid}
	c, _ := b.ChatByJID(jid)
	p.Name = c.Name
	j, err := types.ParseJID(jid)
	if err != nil {
		return p
	}
	p.IsGroup = j.Server == types.GroupServer
	if p.IsGroup {
		return p
	}
	if b.cli != nil {
		j = b.norm(j)
		if ci, err := b.cli.Store.Contacts.GetContact(context.Background(), j); err == nil && ci.Found {
			p.PushName = ci.PushName
			if p.Name == "" {
				p.Name = pick(ci.FullName, pick(ci.PushName, ci.BusinessName))
			}
			if j.Server != types.DefaultUserServer && ci.RedactedPhone != "" {
				p.Phone = ci.RedactedPhone
			}
		}
	}
	if j.Server == types.DefaultUserServer {
		p.Phone = prettyNumber(j)
	}
	if p.Name == "" {
		p.Name = p.PushName // vuoto: la scheda usa il nome visto nel gruppo o il numero
	}
	return p
}

func (b *Backend) fetchProfile(jid string) {
	p := b.localProfile(jid)
	profiles.mu.Lock()
	if old := profiles.m[jid]; old != nil {
		p.Photo = old.Photo
	}
	profiles.mu.Unlock()
	defer func() {
		p.Loading = false
		profiles.mu.Lock()
		profiles.m[jid] = &p
		profiles.mu.Unlock()
		b.changed(jid)
	}()
	if b.cli == nil { // modalità dimostrativa
		if p.IsGroup {
			p.Topic, p.Count, p.Created = "Partita ogni giovedì alle 21 al campo di via Roma. Portate la pettorina!", 12, time.Now().AddDate(-2, -3, 0)
		} else {
			p.About = "Disponibile 🙂"
		}
		return
	}
	if !b.cli.IsLoggedIn() {
		p.Err = "non collegato"
		return
	}
	j, err := types.ParseJID(jid)
	if err != nil {
		p.Err = "indirizzo non valido"
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if p.IsGroup {
		if info, err := b.cli.GetGroupInfo(ctx, j); err == nil {
			p.Name = pick(info.Name, p.Name)
			p.Topic, p.Created, p.Count = info.Topic, info.GroupCreated, len(info.Participants)
		} else {
			p.Err = "dettagli del gruppo non disponibili"
		}
		b.GroupMembers(jid) // aggiorna anche l'elenco dei partecipanti
	} else {
		j = b.norm(j)
		if infos, err := b.cli.GetUserInfo(ctx, []types.JID{j}); err == nil {
			if u, ok := infos[j]; ok {
				p.About = u.Status
				if u.VerifiedName != nil && u.VerifiedName.Details != nil {
					p.Business = u.VerifiedName.Details.GetVerifiedName()
				}
			}
		}
	}
	// foto a piena risoluzione
	info, err := b.cli.GetProfilePictureInfo(ctx, j, &whatsmeow.GetProfilePictureParams{Preview: false})
	switch {
	case err == nil && info != nil && info.URL != "":
		path := filepath.Join(b.dir, "avatars", "big-"+safeName(jid)+".jpg")
		if downloadTo(ctx, info.URL, path) == nil {
			p.Photo = path
		}
	case err == nil, errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
		p.Photo = ""
	}
}

func downloadTo(ctx context.Context, url, path string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New(resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
