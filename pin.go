package main

import (
	"context"
	"errors"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

// WhatsApp permette al massimo 3 chat fissate in alto.
const maxPinned = 3

var errTooManyPinned = errors.New("puoi fissare al massimo 3 chat (come su WhatsApp)")

// SetPin fissa o sblocca una chat, qui e sul telefono.
func (b *Backend) SetPin(jid string, pin bool) error {
	if pin {
		cs, _ := b.Snapshot()
		n := 0
		for _, c := range cs {
			if c.Pinned > 0 && c.JID != jid {
				n++
			}
		}
		if n >= maxPinned {
			return errTooManyPinned
		}
	}
	var ts int64
	if pin {
		ts = time.Now().Unix()
	}
	b.db.SetPinned(jid, ts)
	b.reloadChats("")
	if b.cli == nil || !b.cli.IsLoggedIn() {
		return nil
	}
	j, err := types.ParseJID(jid)
	if err != nil {
		return nil
	}
	safeGo("chat fissata", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := b.cli.SendAppState(ctx, appstate.BuildPin(j, pin)); err != nil {
			logHandled("fissa chat "+jid, err)
		}
	})
	return nil
}
