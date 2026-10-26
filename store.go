package main

import (
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Stato di consegna dei messaggi inviati.
const (
	StPending   = 0
	StSent      = 1
	StDelivered = 2
	StRead      = 3
	StFailed    = -1
)

// Tipi di contenuto.
const (
	KText    = "text"
	KImage   = "image"
	KVideo   = "video"
	KAudio   = "audio"
	KDoc     = "doc"
	KSticker = "sticker"
	KOther   = "other"
	KDeleted = "deleted"
	KSystem  = "system"
)

type Chat struct {
	JID        string
	Name       string
	IsGroup    bool
	LastText   string
	LastTime   time.Time
	LastFromMe bool
	Unread     int
	Archived   bool
	Pinned     int64 // momento in cui è stata fissata in alto (0 = non fissata)
}

type Message struct {
	Chat       string
	ID         string
	Sender     string
	SenderName string
	FromMe     bool
	Time       time.Time
	Text       string
	Kind       string
	Status     int
	Thumb      []byte
	Media      []byte // proto serializzato per scaricare il file
	FileName   string
	Unread     bool

	QuoteID     string
	QuoteSender string // nome già risolto di chi ha scritto il messaggio citato
	QuoteText   string
	Extra       MsgExtra
	MediaPath   string     // file scaricato, se presente
	Reactions   []Reaction // reazioni ricevute
	Deleted     bool       // eliminato da chi l'ha scritto: il contenuto resta qui, nascosto se non richiesto
	Votes       []PollVote // sondaggi: i voti ricevuti (uno per persona)
}

// PollVote: le opzioni scelte da una persona in un sondaggio ("me" = io).
type PollVote struct {
	Voter, Name string
	Options     []string
}

type Reaction struct {
	Sender string
	Emoji  string
}

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS chats(
  jid TEXT PRIMARY KEY, name TEXT NOT NULL DEFAULT '', is_group INTEGER NOT NULL DEFAULT 0,
  last_text TEXT NOT NULL DEFAULT '', last_ts INTEGER NOT NULL DEFAULT 0, last_me INTEGER NOT NULL DEFAULT 0,
  unread INTEGER NOT NULL DEFAULT 0, archived INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS messages(
  chat TEXT NOT NULL, id TEXT NOT NULL, sender TEXT NOT NULL DEFAULT '', sender_name TEXT NOT NULL DEFAULT '',
  from_me INTEGER NOT NULL DEFAULT 0, ts INTEGER NOT NULL, text TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL DEFAULT 'text', status INTEGER NOT NULL DEFAULT 0, thumb BLOB, media BLOB,
  file_name TEXT NOT NULL DEFAULT '', unread INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(chat, id));
CREATE INDEX IF NOT EXISTS msg_ts ON messages(chat, ts);
CREATE TABLE IF NOT EXISTS reactions(
  chat TEXT NOT NULL, msg_id TEXT NOT NULL, sender TEXT NOT NULL, emoji TEXT NOT NULL, ts INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(chat, msg_id, sender));`)
	if err != nil {
		return nil, err
	}
	// colonne aggiunte dopo la prima versione (l'errore "già esiste" si ignora)
	for _, c := range []string{"quote_id TEXT NOT NULL DEFAULT ''", "quote_sender TEXT NOT NULL DEFAULT ''",
		"quote_text TEXT NOT NULL DEFAULT ''", "extra TEXT NOT NULL DEFAULT ''", "media_path TEXT NOT NULL DEFAULT ''",
		"deleted INTEGER NOT NULL DEFAULT 0"} {
		db.Exec("ALTER TABLE messages ADD COLUMN " + c)
	}
	db.Exec(`CREATE TABLE IF NOT EXISTS poll_votes(chat TEXT NOT NULL, poll_id TEXT NOT NULL, voter TEXT NOT NULL,
  voter_name TEXT NOT NULL DEFAULT '', options TEXT NOT NULL DEFAULT '[]', ts INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(chat, poll_id, voter))`)
	db.Exec("ALTER TABLE chats ADD COLUMN pinned INTEGER NOT NULL DEFAULT 0") // quando è stata fissata (0 = no)
	return &Store{db: db}, nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// EnsureChat crea la chat se manca e aggiorna il nome se ne arriva uno non vuoto.
func (s *Store) EnsureChat(jid, name string, isGroup bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`INSERT INTO chats(jid,name,is_group) VALUES(?,?,?)
		ON CONFLICT(jid) DO UPDATE SET name=CASE WHEN excluded.name<>'' THEN excluded.name ELSE chats.name END`,
		jid, name, b2i(isGroup))
}

func (s *Store) SetChatName(jid, name string) {
	if name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE chats SET name=? WHERE jid=?`, name, jid)
}

// SetPinned fissa (ts > 0) o sblocca (ts = 0) la chat.
func (s *Store) SetPinned(jid string, ts int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE chats SET pinned=? WHERE jid=?`, ts, jid)
}

func (s *Store) SetArchived(jid string, a bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE chats SET archived=? WHERE jid=?`, b2i(a), jid)
}

// AddMessage salva il messaggio; restituisce false se era già presente.
func (s *Store) AddMessage(m *Message) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, err := s.db.Exec(`INSERT OR IGNORE INTO messages(chat,id,sender,sender_name,from_me,ts,text,kind,status,thumb,media,file_name,unread,
		quote_id,quote_sender,quote_text,extra,media_path)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		m.Chat, m.ID, m.Sender, m.SenderName, b2i(m.FromMe), m.Time.Unix(), m.Text, m.Kind, m.Status,
		m.Thumb, m.Media, m.FileName, b2i(m.Unread), m.QuoteID, m.QuoteSender, m.QuoteText, m.Extra.encode(), m.MediaPath)
	if err != nil {
		return false
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false
	}
	s.db.Exec(`UPDATE chats SET last_text=?, last_ts=?, last_me=?, archived=CASE WHEN ? THEN 0 ELSE archived END
		WHERE jid=? AND last_ts<=?`,
		preview(m), m.Time.Unix(), b2i(m.FromMe), b2i(!m.FromMe && m.Unread), m.Chat, m.Time.Unix())
	if m.Unread && !m.FromMe {
		s.db.Exec(`UPDATE chats SET unread=unread+1 WHERE jid=?`, m.Chat)
	}
	return true
}

func preview(m *Message) string {
	t := strings.Join(strings.Fields(previewMsg(m)), " ")
	if len([]rune(t)) > 120 {
		t = string([]rune(t)[:120])
	}
	return t
}

// UpdateText modifica il testo (messaggio modificato o eliminato).
func (s *Store) UpdateText(chat, id, text, kind string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE messages SET text=?, kind=?, thumb=CASE WHEN ?='deleted' THEN NULL ELSE thumb END,
		media=CASE WHEN ?='deleted' THEN NULL ELSE media END WHERE chat=? AND id=?`, text, kind, kind, kind, chat, id)
}

// MarkRevoked segna come eliminato un messaggio senza cancellarne il contenuto.
func (s *Store) MarkRevoked(chat, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE messages SET deleted=1 WHERE chat=? AND id=?`, chat, id)
	// se era l'ultimo messaggio, nella lista delle chat non si vede più il testo
	s.db.Exec(`UPDATE chats SET last_text='🚫 Messaggio eliminato'
		WHERE jid=? AND last_ts=(SELECT ts FROM messages WHERE chat=? AND id=?)`, chat, chat, id)
}

func (s *Store) SetStatus(chat string, ids []string, st int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		s.db.Exec(`UPDATE messages SET status=? WHERE chat=? AND id=? AND (status<? OR ?<0)`, st, chat, id, st, st)
	}
}

func (s *Store) SetSentTime(chat, id string, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE messages SET ts=? WHERE chat=? AND id=?`, t.Unix(), chat, id)
}

func (s *Store) SetUnread(chat string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE chats SET unread=? WHERE jid=?`, n, chat)
}

// MarkLastUnread segna come non letti gli ultimi n messaggi ricevuti (dallo storico).
func (s *Store) MarkLastUnread(chat string, n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE messages SET unread=1 WHERE chat=? AND from_me=0 AND id IN
		(SELECT id FROM messages WHERE chat=? AND from_me=0 ORDER BY ts DESC LIMIT ?)`, chat, chat, n)
	s.db.Exec(`UPDATE chats SET unread=? WHERE jid=?`, n, chat)
}

type unreadRef struct {
	ID     string
	Sender string
	Time   time.Time
}

// MarkRead azzera i non letti e restituisce i messaggi da confermare come letti.
func (s *Store) MarkRead(chat string) []unreadRef {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id, sender, ts FROM messages WHERE chat=? AND unread=1 AND from_me=0`, chat)
	var out []unreadRef
	if err == nil {
		for rows.Next() {
			var r unreadRef
			var ts int64
			rows.Scan(&r.ID, &r.Sender, &ts)
			r.Time = time.Unix(ts, 0)
			out = append(out, r)
		}
		rows.Close()
	}
	s.db.Exec(`UPDATE messages SET unread=0 WHERE chat=? AND unread=1`, chat)
	s.db.Exec(`UPDATE chats SET unread=0 WHERE jid=?`, chat)
	return out
}

func (s *Store) Chats() []Chat {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT jid,name,is_group,last_text,last_ts,last_me,unread,archived,pinned FROM chats ORDER BY last_ts DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Chat
	for rows.Next() {
		var c Chat
		var g, me, ar int
		var ts int64
		rows.Scan(&c.JID, &c.Name, &g, &c.LastText, &ts, &me, &c.Unread, &ar, &c.Pinned)
		c.IsGroup, c.LastFromMe, c.Archived = g == 1, me == 1, ar == 1
		c.LastTime = time.Unix(ts, 0)
		out = append(out, c)
	}
	return out
}

func (s *Store) Messages(chat string, limit int) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT id,sender,sender_name,from_me,ts,text,kind,status,thumb,file_name,unread,media IS NOT NULL,
		quote_id,quote_sender,quote_text,extra,media_path,deleted
		FROM messages WHERE chat=? ORDER BY ts DESC, rowid DESC LIMIT ?`, chat, limit)
	if err != nil {
		return nil
	}
	var out []Message
	idx := map[string]int{}
	for rows.Next() {
		m := Message{Chat: chat}
		var me, un, hasMedia int
		var ts int64
		var extra string
		var del int
		rows.Scan(&m.ID, &m.Sender, &m.SenderName, &me, &ts, &m.Text, &m.Kind, &m.Status, &m.Thumb, &m.FileName, &un, &hasMedia,
			&m.QuoteID, &m.QuoteSender, &m.QuoteText, &extra, &m.MediaPath, &del)
		m.FromMe, m.Unread, m.Deleted = me == 1, un == 1, del == 1
		if hasMedia == 1 {
			m.Media = []byte{1} // segnaposto: il proto vero si legge con MediaOf
		}
		m.Extra = decodeExtra(extra)
		m.Time = time.Unix(ts, 0)
		out = append(out, m)
	}
	rows.Close()
	for i := range out {
		if out[i].Kind == KPoll {
			out[i].Votes = s.pollVotesLocked(chat, out[i].ID)
		}
	}
	// ordine cronologico
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	for i := range out {
		idx[out[i].ID] = i
	}
	if rr, err := s.db.Query(`SELECT msg_id, sender, emoji FROM reactions WHERE chat=? AND emoji<>'' ORDER BY ts`, chat); err == nil {
		for rr.Next() {
			var id string
			var r Reaction
			rr.Scan(&id, &r.Sender, &r.Emoji)
			if i, ok := idx[id]; ok {
				out[i].Reactions = append(out[i].Reactions, r)
			}
		}
		rr.Close()
	}
	return out
}

func (s *Store) MediaOf(chat, id string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b []byte
	s.db.QueryRow(`SELECT media FROM messages WHERE chat=? AND id=?`, chat, id).Scan(&b)
	return b
}

func (s *Store) SetMedia(chat, id string, raw []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE messages SET media=? WHERE chat=? AND id=?`, raw, chat, id)
}

func (s *Store) MediaPath(chat, id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var p string
	s.db.QueryRow(`SELECT media_path FROM messages WHERE chat=? AND id=?`, chat, id).Scan(&p)
	return p
}

func (s *Store) SetMediaPath(chat, id, path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`UPDATE messages SET media_path=? WHERE chat=? AND id=?`, path, chat, id)
}

// SetReaction salva (o toglie, con emoji vuota) la reazione di qualcuno a un messaggio.
func (s *Store) SetReaction(chat, msgID, sender, emoji string, ts time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`INSERT INTO reactions(chat,msg_id,sender,emoji,ts) VALUES(?,?,?,?,?)
		ON CONFLICT(chat,msg_id,sender) DO UPDATE SET emoji=excluded.emoji, ts=excluded.ts`,
		chat, msgID, sender, emoji, ts.Unix())
}

func (s *Store) Wipe() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`DELETE FROM messages; DELETE FROM chats; DELETE FROM reactions;`)
}

// SetPollVote salva il voto (sostituisce quello precedente della stessa persona, se più vecchio).
func (s *Store) SetPollVote(chat, poll, voter, name string, options []string, ts time.Time) {
	b, _ := json.Marshal(options)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`INSERT INTO poll_votes(chat,poll_id,voter,voter_name,options,ts) VALUES(?,?,?,?,?,?)
		ON CONFLICT(chat,poll_id,voter) DO UPDATE SET options=excluded.options, ts=excluded.ts,
		voter_name=CASE WHEN excluded.voter_name<>'' THEN excluded.voter_name ELSE voter_name END
		WHERE excluded.ts>=poll_votes.ts`, chat, poll, voter, name, string(b), ts.UnixMilli())
}

func (s *Store) PollVotes(chat, poll string) []PollVote {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pollVotesLocked(chat, poll)
}

func (s *Store) pollVotesLocked(chat, poll string) []PollVote {
	rows, err := s.db.Query(`SELECT voter,voter_name,options FROM poll_votes WHERE chat=? AND poll_id=? ORDER BY ts`, chat, poll)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []PollVote
	for rows.Next() {
		var v PollVote
		var opts string
		rows.Scan(&v.Voter, &v.Name, &opts)
		json.Unmarshal([]byte(opts), &v.Options)
		if len(v.Options) > 0 { // voto ritirato: non conta
			out = append(out, v)
		}
	}
	return out
}

// PollOptions: le opzioni di un sondaggio salvato.
func (s *Store) PollOptions(chat, id string) []string {
	s.mu.Lock()
	var extra, kind string
	s.db.QueryRow(`SELECT extra, kind FROM messages WHERE chat=? AND id=?`, chat, id).Scan(&extra, &kind)
	s.mu.Unlock()
	if kind != KPoll {
		return nil
	}
	return decodeExtra(extra).Options
}
