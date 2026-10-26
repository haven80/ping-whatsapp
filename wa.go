package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	xdraw "golang.org/x/image/draw"
	"google.golang.org/protobuf/proto"
)

type ConnState int

const (
	StateStarting ConnState = iota
	StateQR
	StateQRExpired
	StateSyncing
	StateReady
	StateOffline
	StateError
)

type Presence struct {
	Online   bool
	LastSeen time.Time
	Typing   time.Time // ultimo "sta scrivendo"
	TypingBy string    // nei gruppi: chi scrive
}

type Backend struct {
	dir       string
	db        *Store
	container *sqlstore.Container
	cli       *whatsmeow.Client

	mu       sync.RWMutex
	state    ConnState
	errText  string
	qr       string
	chats    []Chat
	presence map[string]*Presence
	version  uint64            // cambia a ogni modifica della lista
	chatVer  map[string]uint64 // cambia a ogni modifica dei messaggi della chat
	names    map[string]string

	avMu      sync.Mutex
	avatars   map[string]image.Image // nil = senza foto
	avPending map[string]bool
	avQueue   chan string

	lastComposing map[string]time.Time
	active        bool
	demoMe        string
	media         *mediaCache
	anims         *animCache

	// callback impostate dalla UI
	OnChange   func()
	OnIncoming func(chat Chat, m *Message)
	IsChatOpen func(jid string) bool // chat aperta e in primo piano
}

func NewBackend(dir string) (*Backend, error) {
	os.MkdirAll(filepath.Join(dir, "avatars"), 0o755)
	db, err := OpenStore(filepath.Join(dir, "ping.db"))
	if err != nil {
		return nil, err
	}
	b := &Backend{
		dir: dir, db: db,
		presence:      map[string]*Presence{},
		chatVer:       map[string]uint64{},
		names:         map[string]string{},
		avatars:       map[string]image.Image{},
		avPending:     map[string]bool{},
		avQueue:       make(chan string, 4096),
		lastComposing: map[string]time.Time{},
	}
	b.chats = db.Chats()
	b.initMedia()
	b.initAnims()
	go supervise("foto profilo", b.avatarWorker)
	return b, nil
}

func (b *Backend) changed(chat string) {
	b.mu.Lock()
	b.version++
	if chat != "" {
		b.chatVer[chat]++
	}
	b.mu.Unlock()
	if b.OnChange != nil {
		b.OnChange()
	}
}

func (b *Backend) reloadChats(chat string) {
	cs := b.db.Chats()
	b.mu.Lock()
	b.chats = cs
	b.mu.Unlock()
	b.changed(chat)
}

func (b *Backend) setState(s ConnState) {
	b.mu.Lock()
	b.state = s
	b.mu.Unlock()
	b.changed("")
}

// ---- lettura stato per la UI ----

func (b *Backend) State() (ConnState, string, string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.state, b.qr, b.errText
}

func (b *Backend) Snapshot() ([]Chat, uint64) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.chats, b.version
}

func (b *Backend) ChatVersion(jid string) uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.chatVer[jid]
}

func (b *Backend) ChatByJID(jid string) (Chat, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, c := range b.chats {
		if c.JID == jid {
			return c, true
		}
	}
	return Chat{JID: jid, Name: b.nameLocked(jid)}, false
}

func (b *Backend) PresenceOf(jid string) Presence {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if p := b.presence[jid]; p != nil {
		return *p
	}
	return Presence{}
}

func (b *Backend) Me() (jid, name string) {
	if b.cli == nil || b.cli.Store.ID == nil {
		return "me", b.demoMe
	}
	j := b.cli.Store.ID.ToNonAD().String()
	name = b.cli.Store.PushName
	if name == "" {
		name = "+" + b.cli.Store.ID.User
	}
	return j, name
}

// Contacts restituisce la rubrica (contatti salvati) ordinata per nome.
func (b *Backend) Contacts() []Chat {
	if b.cli == nil {
		return nil
	}
	all, err := b.cli.Store.Contacts.GetAllContacts(context.Background())
	if err != nil {
		return nil
	}
	out := make([]Chat, 0, len(all))
	for j, c := range all {
		if j.Server != types.DefaultUserServer || c.FullName == "" {
			continue
		}
		out = append(out, Chat{JID: j.String(), Name: c.FullName})
	}
	sort.Slice(out, func(i, k int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[k].Name) })
	return out
}

// ---- avvio e connessione ----

func (b *Backend) Start() {
	ctx := context.Background()
	store.SetOSInfo("Ping (Windows)", [3]uint32{1, 0, 0})
	store.DeviceProps.PlatformType = waCompanionReg.DeviceProps_DESKTOP.Enum()
	store.DeviceProps.RequireFullSync = proto.Bool(false)

	var err error
	dsn := "file:" + filepath.Join(b.dir, "session.db") + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	b.container, err = sqlstore.New(ctx, "sqlite", dsn, waLog.Noop)
	if err != nil {
		b.fail(err)
		return
	}
	b.connect()
}

func (b *Backend) fail(err error) {
	b.mu.Lock()
	b.state, b.errText = StateError, err.Error()
	b.mu.Unlock()
	b.changed("")
}

func (b *Backend) connect() {
	ctx := context.Background()
	dev, err := b.container.GetFirstDevice(ctx)
	if err != nil {
		b.fail(err)
		return
	}
	b.cli = whatsmeow.NewClient(dev, waLog.Noop)
	b.cli.EnableAutoReconnect = true
	b.cli.AddEventHandler(func(e any) {
		defer func() {
			if r := recover(); r != nil {
				logHandled(fmt.Sprintf("evento %T", e), r)
			}
		}()
		b.handle(e)
	})
	if b.cli.Store.ID == nil {
		b.startQR()
		return
	}
	b.setState(StateSyncing)
	if err := b.cli.Connect(); err != nil {
		b.setState(StateOffline)
	}
}

func (b *Backend) startQR() {
	ch, err := b.cli.GetQRChannel(context.Background())
	if err != nil {
		b.fail(err)
		return
	}
	if err := b.cli.Connect(); err != nil {
		b.fail(err)
		return
	}
	b.setState(StateQR)
	go func() {
		for it := range ch {
			switch it.Event {
			case "code":
				b.mu.Lock()
				b.qr, b.state = it.Code, StateQR
				b.mu.Unlock()
				b.changed("")
			case "success":
				b.setState(StateSyncing)
			case "timeout":
				b.cli.Disconnect()
				b.setState(StateQRExpired)
			default:
				if it.Error != nil {
					b.fail(it.Error)
				}
			}
		}
	}()
}

// RetryQR genera un nuovo codice dopo la scadenza.
func (b *Backend) RetryQR() {
	go b.connect()
}

func (b *Backend) Logout() {
	go func() {
		if b.cli != nil {
			b.cli.Logout(context.Background())
			b.cli.Disconnect()
		}
		b.db.Wipe()
		b.reloadChats("")
		b.connect()
	}()
}

func (b *Backend) Close() {
	if b.cli != nil {
		b.cli.Disconnect()
	}
}

// SetActive segnala a WhatsApp se l'utente è davanti al client
// (serve per ricevere gli stati online; da inattivi il telefono riprende le notifiche).
func (b *Backend) SetActive(active bool) {
	b.mu.Lock()
	changed := b.active != active
	b.active = active
	b.mu.Unlock()
	if !changed || b.cli == nil || !b.cli.IsLoggedIn() {
		return
	}
	go func() {
		p := types.PresenceUnavailable
		if active {
			p = types.PresenceAvailable
		}
		b.cli.SendPresence(context.Background(), p)
	}()
}

// ---- normalizzazione JID e nomi ----

func (b *Backend) norm(j types.JID) types.JID {
	j = j.ToNonAD()
	if j.Server == types.HiddenUserServer && b.cli != nil {
		if pn, err := b.cli.Store.LIDs.GetPNForLID(context.Background(), j); err == nil && !pn.IsEmpty() {
			return pn.ToNonAD()
		}
	}
	return j
}

func (b *Backend) nameLocked(jid string) string {
	if n := b.names[jid]; n != "" {
		return n
	}
	return ""
}

func (b *Backend) contactName(j types.JID) string {
	if b.cli == nil {
		return ""
	}
	c, err := b.cli.Store.Contacts.GetContact(context.Background(), j)
	if err == nil && c.Found {
		switch {
		case c.FullName != "":
			return c.FullName
		case c.PushName != "":
			return c.PushName
		case c.BusinessName != "":
			return c.BusinessName
		}
	}
	return ""
}

func prettyNumber(j types.JID) string {
	if j.Server == types.DefaultUserServer {
		return "+" + j.User
	}
	return j.User
}

func (b *Backend) displayName(j types.JID) string {
	if n := b.contactName(j); n != "" {
		return n
	}
	return prettyNumber(j)
}

// ---- eventi ----

func (b *Backend) handle(evt any) {
	switch e := evt.(type) {
	case *events.Connected:
		b.setState(StateReady)
		go b.afterConnect()
	case *events.Disconnected:
		b.setState(StateOffline)
	case *events.LoggedOut:
		b.db.Wipe()
		b.reloadChats("")
		go b.connect()
	case *events.StreamReplaced:
		b.fail(errors.New("Sessione aperta su un altro client"))
	case *events.Message:
		b.onMessage(e, true)
	case *events.CallOffer:
		b.onCall(e.BasicCallMeta, "")
	case *events.CallOfferNotice:
		b.onCall(e.BasicCallMeta, e.Media)
	case *events.HistorySync:
		b.onHistory(e)
	case *events.Receipt:
		b.onReceipt(e)
	case *events.ChatPresence:
		chat := b.norm(e.Chat).String()
		b.mu.Lock()
		p := b.presence[chat]
		if p == nil {
			p = &Presence{}
			b.presence[chat] = p
		}
		if e.State == types.ChatPresenceComposing {
			p.Typing = time.Now()
			if e.IsGroup {
				p.TypingBy = b.displayName(b.norm(e.Sender))
			}
		} else {
			p.Typing = time.Time{}
		}
		b.mu.Unlock()
		b.changed(chat)
	case *events.Presence:
		jid := b.norm(e.From).String()
		b.mu.Lock()
		p := b.presence[jid]
		if p == nil {
			p = &Presence{}
			b.presence[jid] = p
		}
		p.Online = !e.Unavailable
		if !e.LastSeen.IsZero() {
			p.LastSeen = e.LastSeen
		}
		b.mu.Unlock()
		b.changed(jid)
	case *events.PushName:
		jid := b.norm(e.JID)
		if e.NewPushName != "" && b.contactNameFull(jid) == "" {
			b.db.SetChatName(jid.String(), e.NewPushName)
			b.reloadChats("")
		}
	case *events.Contact:
		jid := b.norm(e.JID)
		b.db.SetChatName(jid.String(), b.displayName(jid))
		b.reloadChats("")
	case *events.GroupInfo:
		if e.Name != nil {
			b.db.SetChatName(e.JID.String(), e.Name.Name)
			b.reloadChats("")
		}
	case *events.JoinedGroup:
		b.db.EnsureChat(e.JID.String(), e.Name, true)
		b.reloadChats("")
	case *events.Archive:
		if e.Action != nil {
			b.db.SetArchived(b.norm(e.JID).String(), e.Action.GetArchived())
			b.reloadChats("")
		}
	case *events.Pin: // fissata o sbloccata dal telefono
		if e.Action != nil {
			var ts int64
			if e.Action.GetPinned() {
				ts = e.Timestamp.Unix()
			}
			b.db.SetPinned(b.norm(e.JID).String(), ts)
			b.reloadChats("")
		}
	case *events.MarkChatAsRead:
		if e.Action != nil && e.Action.GetRead() {
			b.db.MarkRead(b.norm(e.JID).String())
			b.reloadChats("")
		}
	}
}

func (b *Backend) contactNameFull(j types.JID) string {
	c, err := b.cli.Store.Contacts.GetContact(context.Background(), j)
	if err == nil {
		return c.FullName
	}
	return ""
}

func (b *Backend) afterConnect() {
	ctx := context.Background()
	b.mu.RLock()
	active := b.active
	b.mu.RUnlock()
	if active {
		b.cli.SendPresence(ctx, types.PresenceAvailable)
	}
	// aggiorna i nomi delle chat e dei gruppi
	if groups, err := b.cli.GetJoinedGroups(ctx); err == nil {
		for _, g := range groups {
			b.db.EnsureChat(g.JID.String(), g.Name, true)
		}
	}
	for _, c := range b.db.Chats() {
		if c.IsGroup {
			continue
		}
		j, err := types.ParseJID(c.JID)
		if err == nil {
			if n := b.contactName(j); n != "" && n != c.Name {
				b.db.SetChatName(c.JID, n)
			}
		}
	}
	b.reloadChats("")
	// stato online per le chat più recenti
	b.mu.RLock()
	cs := b.chats
	b.mu.RUnlock()
	n := 0
	for _, c := range cs {
		if c.IsGroup || n >= 40 {
			continue
		}
		if j, err := types.ParseJID(c.JID); err == nil {
			b.cli.SubscribePresence(ctx, j)
			n++
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func pick(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (b *Backend) onMessage(e *events.Message, live bool) {
	info := e.Info
	if info.Chat.Server == types.BroadcastServer || info.Chat.Server == types.NewsletterServer {
		return
	}
	chat := b.norm(info.Chat)
	if chat.Server == types.HiddenUserServer && !info.IsGroup {
		// prova con l'indirizzo alternativo (numero di telefono)
		if info.IsFromMe && !info.RecipientAlt.IsEmpty() {
			chat = info.RecipientAlt.ToNonAD()
		} else if !info.IsFromMe && !info.SenderAlt.IsEmpty() {
			chat = info.SenderAlt.ToNonAD()
		}
	}
	chatID := chat.String()
	msg := e.Message

	sender := b.norm(info.Sender)

	// modifiche ed eliminazioni
	if pm := msg.GetProtocolMessage(); pm != nil {
		target := pm.GetKey().GetID()
		switch pm.GetType() {
		case waE2E.ProtocolMessage_REVOKE:
			b.db.UpdateText(chatID, target, "", KDeleted)
			b.changed(chatID)
		case waE2E.ProtocolMessage_MESSAGE_EDIT:
			if t := parseContent(pm.GetEditedMessage()).Text; t != "" {
				b.db.UpdateText(chatID, target, t+"  ✎", KText)
				b.changed(chatID)
			}
		}
		return
	}
	if em := msg.GetEditedMessage().GetMessage(); em != nil {
		if pm := em.GetProtocolMessage(); pm != nil {
			if t := parseContent(pm.GetEditedMessage()).Text; t != "" {
				b.db.UpdateText(chatID, pm.GetKey().GetID(), t+"  ✎", KText)
				b.changed(chatID)
			}
		}
		return
	}
	// reazioni
	if rm := msg.GetReactionMessage(); rm != nil {
		who := sender.String()
		if info.IsFromMe {
			who = "me"
		}
		b.db.SetReaction(chatID, rm.GetKey().GetID(), who, rm.GetText(), info.Timestamp)
		b.changed(chatID)
		return
	}

	pc := parseContent(msg)
	if pc.Kind == "" {
		return // messaggi tecnici senza contenuto visibile
	}
	if ci := contextOf(msg); ci != nil && len(ci.GetMentionedJID()) > 0 {
		pc.Text = b.renderMentions(pc.Text, ci.GetMentionedJID())
	}
	m := &Message{
		Chat: chatID, ID: info.ID, FromMe: info.IsFromMe, Time: info.Timestamp,
		Text: pc.Text, Kind: pc.Kind, Thumb: pc.Thumb, FileName: pc.FileName, Status: StSent,
		QuoteID: pc.QuoteID, QuoteText: pc.QuoteText, Extra: pc.Extra,
	}
	m.Extra.ViewOnce = m.Extra.ViewOnce || e.IsViewOnce || e.IsViewOnceV2
	if pc.QuoteSender != "" {
		if qj, err := types.ParseJID(pc.QuoteSender); err == nil {
			qj = b.norm(qj)
			if me := b.cli.Store.ID; me != nil && qj.User == me.User {
				m.QuoteSender = "Tu"
			} else {
				m.QuoteSender = b.displayName(qj)
			}
		}
	} else if pc.QuoteID != "" && !info.IsGroup {
		m.QuoteSender = b.displayName(chat)
	}
	if pc.Media {
		m.Media, _ = proto.Marshal(msg)
	}
	m.Sender = sender.String()
	if info.IsGroup && !info.IsFromMe {
		m.SenderName = b.contactName(sender)
		if m.SenderName == "" {
			m.SenderName = pick(info.PushName, prettyNumber(sender))
		}
	}
	name := ""
	if info.IsGroup {
		if !b.hasChatName(chatID) {
			if gi, err := b.cli.GetGroupInfo(context.Background(), chat); err == nil {
				name = gi.Name
			}
		}
	} else {
		name = b.contactName(chat)
		if name == "" && !info.IsFromMe && info.PushName != "" {
			name = info.PushName
		}
		if name == "" && !b.hasChatName(chatID) {
			name = prettyNumber(chat)
		}
	}
	b.db.EnsureChat(chatID, name, info.IsGroup)

	open := b.IsChatOpen != nil && b.IsChatOpen(chatID)
	m.Unread = live && !info.IsFromMe && !open
	if !b.addWithPreview(m) {
		return
	}
	if live && !info.IsFromMe {
		b.mu.Lock()
		if p := b.presence[chatID]; p != nil {
			p.Typing = time.Time{}
		}
		b.mu.Unlock()
		if open {
			go b.cli.MarkRead(context.Background(), []types.MessageID{info.ID}, info.Timestamp, info.Chat, info.Sender)
		}
	}
	if !live {
		return // durante la sincronizzazione la lista si aggiorna alla fine
	}
	b.reloadChats(chatID)
	if !info.IsFromMe && b.OnIncoming != nil {
		c, _ := b.ChatByJID(chatID)
		b.OnIncoming(c, m)
	}
	if live {
		b.requestAvatar(chatID)
	}
}

// PrivateJID prepara una chat privata con jid (numero o LID) e ne restituisce l'indirizzo da usare.
func (b *Backend) PrivateJID(jid, name string) string {
	if jid == "" || jid == "me" {
		return ""
	}
	j, err := types.ParseJID(jid)
	if err != nil || j.Server == types.GroupServer {
		return ""
	}
	if b.cli != nil {
		j = b.norm(j)
		if me := b.cli.Store.ID; me != nil && j.User == me.User {
			return "" // sono io
		}
		if n := b.contactName(j); n != "" {
			name = n
		}
	}
	if name == "" {
		name = prettyNumber(j)
	}
	id := j.String()
	b.db.EnsureChat(id, name, false)
	b.reloadChats(id)
	return id
}

func (b *Backend) hasChatName(jid string) bool {
	c, ok := b.ChatByJID(jid)
	return ok && c.Name != ""
}

// addWithPreview salva il messaggio (l'anteprima per la lista la calcola lo store).
func (b *Backend) addWithPreview(m *Message) bool { return b.db.AddMessage(m) }

func (b *Backend) onHistory(e *events.HistorySync) {
	for _, conv := range e.Data.GetConversations() {
		jid, err := types.ParseJID(conv.GetID())
		if err != nil || jid.Server == types.BroadcastServer || jid.Server == types.NewsletterServer {
			continue
		}
		chat := b.norm(jid)
		isGroup := chat.Server == types.GroupServer
		name := conv.GetName()
		if !isGroup {
			if n := b.contactName(chat); n != "" {
				name = n
			} else if name == "" {
				name = prettyNumber(chat)
			}
		}
		b.db.EnsureChat(chat.String(), name, isGroup)
		msgs := conv.GetMessages()
		if len(msgs) > 60 {
			msgs = msgs[:60] // i più recenti arrivano per primi
		}
		for _, hm := range msgs {
			wm := hm.GetMessage()
			if wm == nil {
				continue
			}
			ev, err := b.cli.ParseWebMessage(jid, wm)
			if err != nil {
				continue
			}
			b.onMessage(ev, false)
			b.applyHistoryStatus(chat.String(), wm)
		}
		if conv.GetArchived() {
			b.db.SetArchived(chat.String(), true)
		}
		if p := conv.GetPinned(); p > 0 {
			b.db.SetPinned(chat.String(), int64(p))
		}
		if u := int(conv.GetUnreadCount()); u > 0 {
			b.db.MarkLastUnread(chat.String(), u)
		}
	}
	b.reloadChats("")
	go b.prefetchAvatars()
}

func (b *Backend) applyHistoryStatus(chat string, wm *waWeb.WebMessageInfo) {
	if !wm.GetKey().GetFromMe() {
		return
	}
	st := StSent
	switch wm.GetStatus() {
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		st = StDelivered
	case waWeb.WebMessageInfo_READ, waWeb.WebMessageInfo_PLAYED:
		st = StRead
	}
	b.db.SetStatus(chat, []string{wm.GetKey().GetID()}, st)
}

func (b *Backend) onReceipt(e *events.Receipt) {
	chat := b.norm(e.Chat).String()
	switch e.Type {
	case types.ReceiptTypeDelivered:
		if e.IsFromMe {
			return
		}
		b.db.SetStatus(chat, e.MessageIDs, StDelivered)
	case types.ReceiptTypeRead, types.ReceiptTypePlayed:
		if e.IsFromMe {
			return
		}
		b.db.SetStatus(chat, e.MessageIDs, StRead)
	case types.ReceiptTypeReadSelf:
		// letto dal telefono
		b.db.MarkRead(chat)
		b.reloadChats(chat)
		return
	default:
		return
	}
	b.changed(chat)
}

// ---- azioni dell'utente ----

// MarkRead azzera i non letti e invia le conferme di lettura.
func (b *Backend) MarkRead(jid string) {
	refs := b.db.MarkRead(jid)
	if len(refs) == 0 {
		return
	}
	b.reloadChats(jid)
	chat, err := types.ParseJID(jid)
	if err != nil || b.cli == nil {
		return
	}
	go func() {
		// raggruppa per mittente (nei gruppi serve indicarlo)
		bySender := map[string][]unreadRef{}
		for _, r := range refs {
			bySender[r.Sender] = append(bySender[r.Sender], r)
		}
		for s, rs := range bySender {
			ids := make([]types.MessageID, len(rs))
			var last time.Time
			for i, r := range rs {
				ids[i] = r.ID
				if r.Time.After(last) {
					last = r.Time
				}
			}
			var sender types.JID
			if chat.Server == types.GroupServer {
				sender, _ = types.ParseJID(s)
			}
			b.cli.MarkRead(context.Background(), ids, last, chat, sender)
		}
	}()
}

func (b *Backend) Typing(jid string) {
	if b.cli == nil || !b.cli.IsConnected() {
		return
	}
	b.mu.Lock()
	if time.Since(b.lastComposing[jid]) < 4*time.Second {
		b.mu.Unlock()
		return
	}
	b.lastComposing[jid] = time.Now()
	b.mu.Unlock()
	if j, err := types.ParseJID(jid); err == nil {
		go b.cli.SendChatPresence(context.Background(), j, types.ChatPresenceComposing, types.ChatPresenceMediaText)
	}
}

func (b *Backend) SendText(jid, text string, reply *Message) {
	b.SendTextFull(jid, text, nil, reply, nil)
}

func (b *Backend) chatNameOr(jid string, j types.JID) string {
	if c, ok := b.ChatByJID(jid); ok && c.Name != "" {
		return c.Name
	}
	return b.displayName(j)
}

// SendFile invia una foto (jpg/png) o un documento qualsiasi.
func (b *Backend) SendFile(jid, path string, reply *Message) {
	if b.cli == nil {
		return
	}
	j, err := types.ParseJID(jid)
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	mt := mime.TypeByExtension(ext)
	if mt == "" {
		mt = "application/octet-stream"
	}
	isImage := ext == ".jpg" || ext == ".jpeg" || ext == ".png"
	id := b.cli.GenerateMessageID()
	me, _ := b.Me()
	m := &Message{Chat: jid, ID: id, FromMe: true, Sender: me, Time: time.Now(), Status: StPending, FileName: name, MediaPath: path}
	var thumb []byte
	if isImage {
		m.Kind = KImage
		thumb = makeThumb(data)
		m.Thumb = thumb
	} else {
		m.Kind = KDoc
	}
	b.db.EnsureChat(jid, b.chatNameOr(jid, j), j.Server == types.GroupServer)
	b.applyQuote(m, reply)
	b.addWithPreview(m)
	b.reloadChats(jid)

	go func() {
		ctx := context.Background()
		var msg *waE2E.Message
		if isImage {
			if ext == ".png" { // WhatsApp vuole JPEG per le foto
				if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
					var buf bytes.Buffer
					jpeg.Encode(&buf, img, &jpeg.Options{Quality: 88})
					data, mt = buf.Bytes(), "image/jpeg"
				}
			}
			up, err := b.cli.Upload(ctx, data, whatsmeow.MediaImage)
			if err == nil {
				msg = &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
					URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
					FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
					Mimetype: proto.String("image/jpeg"), JPEGThumbnail: thumb,
				}}
			}
		} else {
			up, err := b.cli.Upload(ctx, data, whatsmeow.MediaDocument)
			if err == nil {
				msg = &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{
					URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
					FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
					Mimetype: proto.String(mt), FileName: proto.String(name), Title: proto.String(name),
				}}
			}
		}
		if msg == nil {
			b.db.SetStatus(jid, []string{id}, StFailed)
			b.changed(jid)
			return
		}
		if ci := b.replyContext(reply); ci != nil {
			if msg.ImageMessage != nil {
				msg.ImageMessage.ContextInfo = ci
			} else if msg.DocumentMessage != nil {
				msg.DocumentMessage.ContextInfo = ci
			}
		}
		raw, _ := proto.Marshal(msg)
		b.db.SetMedia(jid, id, raw)
		b.db.SetMediaPath(jid, id, path)
		resp, err := b.cli.SendMessage(ctx, j, msg, whatsmeow.SendRequestExtra{ID: id})
		if err != nil {
			b.db.SetStatus(jid, []string{id}, StFailed)
		} else {
			b.db.SetStatus(jid, []string{id}, StSent)
			b.db.SetSentTime(jid, id, resp.Timestamp)
		}
		b.changed(jid)
	}()
}

func makeThumb(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	r := img.Bounds()
	w, h := 96, 96*r.Dy()/max(1, r.Dx())
	if r.Dy() > r.Dx() {
		h, w = 96, 96*r.Dx()/max(1, r.Dy())
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, r, xdraw.Src, nil)
	var buf bytes.Buffer
	jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 70})
	return buf.Bytes()
}

// OpenMedia scarica (se serve) il file del messaggio e lo apre con il programma predefinito.
func (b *Backend) OpenMedia(chat, id string) {
	go func() {
		if path, err := b.ensureMediaFile(chat, id); err == nil {
			openPath(path)
		}
	}()
}

func extFor(mt, def string) string {
	if exts, _ := mime.ExtensionsByType(mt); len(exts) > 0 {
		for _, e := range exts {
			if e == ".jpg" || e == ".mp4" || e == ".pdf" || e == ".ogg" {
				return e
			}
		}
		return exts[0]
	}
	return def
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 32 {
			return '_'
		}
		return r
	}, s)
}

// ---- foto profilo ----

func (b *Backend) Avatar(jid string) (image.Image, bool) {
	b.avMu.Lock()
	defer b.avMu.Unlock()
	img, ok := b.avatars[jid]
	return img, ok
}

func (b *Backend) requestAvatar(jid string) {
	b.avMu.Lock()
	if _, ok := b.avatars[jid]; ok || b.avPending[jid] {
		b.avMu.Unlock()
		return
	}
	b.avPending[jid] = true
	b.avMu.Unlock()
	select {
	case b.avQueue <- jid:
	default:
	}
}

// RequestAvatar è chiamata dalla UI per le righe visibili.
func (b *Backend) RequestAvatar(jid string) { b.requestAvatar(jid) }

func (b *Backend) prefetchAvatars() {
	cs, _ := b.Snapshot()
	for i, c := range cs {
		if i >= 60 {
			break
		}
		b.requestAvatar(c.JID)
	}
}

func (b *Backend) avatarFile(jid string) string {
	return filepath.Join(b.dir, "avatars", safeName(jid)+".jpg")
}

func (b *Backend) avatarWorker() {
	httpc := &http.Client{Timeout: 20 * time.Second}
	for jid := range b.avQueue {
		path := b.avatarFile(jid)
		none := path + ".none"
		var img image.Image
		fresh := func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && time.Since(st.ModTime()) < 72*time.Hour
		}
		if fresh(path) {
			img = loadImage(path)
		} else if fresh(none) {
			// nessuna foto, controllato di recente
		} else if b.cli != nil && b.cli.IsLoggedIn() {
			j, err := types.ParseJID(jid)
			if err == nil {
				info, err := b.cli.GetProfilePictureInfo(context.Background(), j, &whatsmeow.GetProfilePictureParams{Preview: true})
				switch {
				case err == nil && info != nil && info.URL != "":
					if resp, err := httpc.Get(info.URL); err == nil {
						var buf bytes.Buffer
						buf.ReadFrom(resp.Body)
						resp.Body.Close()
						if resp.StatusCode == 200 {
							os.WriteFile(path, buf.Bytes(), 0o644)
							os.Remove(none)
							img = loadImage(path)
						}
					}
				case err == nil, errors.Is(err, whatsmeow.ErrProfilePictureNotSet), errors.Is(err, whatsmeow.ErrProfilePictureUnauthorized):
					os.WriteFile(none, nil, 0o644)
				default:
					// errore temporaneo: usa la copia vecchia se c'è
					if _, e := os.Stat(path); e == nil {
						img = loadImage(path)
					}
				}
				time.Sleep(250 * time.Millisecond) // gentili con i server
			}
		} else {
			if _, e := os.Stat(path); e == nil {
				img = loadImage(path)
			}
			b.avMu.Lock()
			delete(b.avPending, jid) // riprova quando saremo connessi
			b.avMu.Unlock()
			if img == nil {
				continue
			}
		}
		b.avMu.Lock()
		b.avatars[jid] = img
		delete(b.avPending, jid)
		b.avMu.Unlock()
		b.changed(jid)
	}
}

func loadImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}
