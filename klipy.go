package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Ricerca di GIF e sticker online con KLIPY (il servizio usato anche da WhatsApp dopo la chiusura di Tenor).
// Serve una chiave gratuita personale: https://partner.klipy.com

const klipyBase = "https://api.klipy.com/api/v1/"

var httpGet = &http.Client{Timeout: 25 * time.Second}

type klipyFile struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Size   int64  `json:"size"`
}

type klipyItem struct {
	ID    json.RawMessage                 `json:"id"`
	Title string                          `json:"title"`
	Type  string                          `json:"type"`
	File  map[string]map[string]klipyFile `json:"file"`
}

// OnlineItem è un risultato di ricerca pronto per la griglia.
type OnlineItem struct {
	ID      string
	Title   string
	Preview string // GIF/WebP piccola per l'anteprima animata
	Send    string // file da inviare (mp4 per le GIF, webp per gli sticker)
	W, H    int
	anim    atomic.Pointer[Anim]
	loading atomic.Bool
	failed  atomic.Bool
}

func (it *OnlineItem) Anim() *Anim { return it.anim.Load() }

func pickFile(f map[string]map[string]klipyFile, sizes []string, formats []string) klipyFile {
	for _, s := range sizes {
		for _, fm := range formats {
			if x, ok := f[s][fm]; ok && x.URL != "" {
				return x
			}
		}
	}
	return klipyFile{}
}

// KlipySearch cerca GIF ("gifs") o sticker ("stickers"); con q vuota restituisce le tendenze.
func KlipySearch(key, customer, kind, q string) ([]*OnlineItem, error) {
	if key == "" {
		return nil, errors.New("manca la chiave KLIPY")
	}
	v := url.Values{}
	v.Set("per_page", "30")
	v.Set("customer_id", customer)
	v.Set("locale", "it")
	path := kind + "/trending"
	if q != "" {
		path = kind + "/search"
		v.Set("q", q)
	}
	resp, err := httpGet.Get(klipyBase + url.PathEscape(key) + "/" + path + "?" + v.Encode())
	if err != nil {
		return nil, errors.New("KLIPY non raggiungibile")
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var env struct {
		Result bool `json:"result"`
		Data   struct {
			Data []json.RawMessage `json:"data"`
		} `json:"data"`
		Errors struct {
			Message []string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("KLIPY ha risposto %d", resp.StatusCode)
	}
	if !env.Result {
		msg := strings.Join(env.Errors.Message, " ")
		if strings.Contains(strings.ToLower(msg), "key") || resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, errors.New("chiave KLIPY non valida")
		}
		if msg == "" {
			msg = fmt.Sprintf("errore %d", resp.StatusCode)
		}
		return nil, errors.New("KLIPY: " + msg)
	}
	var out []*OnlineItem
	for _, raw := range env.Data.Data {
		var it klipyItem
		if json.Unmarshal(raw, &it) != nil || it.Type == "ad" || it.File == nil {
			continue
		}
		o := &OnlineItem{ID: strings.Trim(string(it.ID), `"`), Title: it.Title}
		if kind == "gifs" {
			pv := pickFile(it.File, []string{"xs", "sm", "md"}, []string{"gif", "webp"})
			snd := pickFile(it.File, []string{"md", "sm", "hd"}, []string{"mp4"})
			if snd.URL == "" {
				snd = pickFile(it.File, []string{"md", "sm", "hd"}, []string{"gif"})
			}
			o.Preview, o.Send, o.W, o.H = pv.URL, snd.URL, pv.Width, pv.Height
		} else {
			pv := pickFile(it.File, []string{"xs", "sm", "md"}, []string{"webp", "gif", "png"})
			snd := pickFile(it.File, []string{"hd", "md", "sm"}, []string{"webp", "png", "gif"})
			o.Preview, o.Send, o.W, o.H = pv.URL, snd.URL, pv.Width, pv.Height
		}
		if o.Preview != "" && o.Send != "" {
			out = append(out, o)
		}
	}
	return out, nil
}

func download(u string, limit int64) ([]byte, error) {
	resp, err := httpGet.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download: %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// previewSem limita i download contemporanei delle anteprime.
var previewSem = make(chan struct{}, 4)

// LoadPreview scarica e decodifica l'anteprima animata (una volta sola).
func (it *OnlineItem) LoadPreview(onDone func()) {
	if it.anim.Load() != nil || it.failed.Load() || !it.loading.CompareAndSwap(false, true) {
		return
	}
	go func() {
		previewSem <- struct{}{}
		defer func() { <-previewSem }()
		data, err := download(it.Preview, 8<<20)
		if err == nil {
			if a, err := loadAnimData(data, 180, 40); err == nil {
				it.anim.Store(a)
			} else {
				it.failed.Store(true)
			}
		} else {
			it.failed.Store(true)
		}
		if onDone != nil {
			onDone()
		}
	}()
}

// SendOnline scarica il file scelto e lo invia come GIF o sticker.
func (b *Backend) SendOnline(jid string, it *OnlineItem, sticker bool, reply *Message) error {
	data, err := download(it.Send, 30<<20)
	if err != nil {
		return errors.New("download non riuscito")
	}
	dir := filepath.Join(b.dir, "media")
	os.MkdirAll(dir, 0o755)
	if sticker {
		ext := ".webp"
		if bytes.HasPrefix(data, []byte("GIF8")) {
			ext = ".gif"
		} else if bytes.HasPrefix(data, []byte("\x89PNG")) {
			ext = ".png"
		}
		p := filepath.Join(dir, "klipy_"+safeName(it.ID)+ext)
		os.WriteFile(p, data, 0o644)
		return b.SendStickerFile(jid, p, reply)
	}
	if bytes.HasPrefix(data, []byte("GIF8")) {
		p := filepath.Join(dir, "klipy_"+safeName(it.ID)+".gif")
		os.WriteFile(p, data, 0o644)
		return b.SendGIFFile(jid, p, reply)
	}
	p := filepath.Join(dir, "klipy_"+safeName(it.ID)+".mp4")
	os.WriteFile(p, data, 0o644)
	b.sendVideo(jid, p, true, reply)
	return nil
}

// onlineSearch gestisce una ricerca con ritardo (si aspetta che l'utente smetta di scrivere).
type onlineSearch struct {
	mu      sync.Mutex
	kind    string
	query   string
	key     string
	done    bool // l'ultima ricerca è terminata (con risultati o con errore)
	results []*OnlineItem
	err     string
	loading bool
	seq     int
}

func (s *onlineSearch) snapshot() ([]*OnlineItem, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.results, s.err, s.loading
}

// run avvia la ricerca dopo 400 ms; le ricerche superate vengono scartate.
func (s *onlineSearch) run(key, customer, kind, q string, done func()) {
	s.mu.Lock()
	if s.kind == kind && s.query == q && s.key == key && (s.done || s.loading) {
		s.mu.Unlock()
		return
	}
	s.seq++
	seq := s.seq
	s.kind, s.query, s.key, s.loading, s.done, s.err = kind, q, key, true, false, ""
	s.mu.Unlock()
	go func() {
		time.Sleep(400 * time.Millisecond)
		s.mu.Lock()
		stale := seq != s.seq
		s.mu.Unlock()
		if stale {
			return
		}
		res, err := KlipySearch(key, customer, kind, q)
		s.mu.Lock()
		if seq == s.seq {
			s.results, s.loading, s.done = res, false, true
			if err != nil {
				s.err = err.Error()
			} else if len(res) == 0 {
				s.err = "Nessun risultato"
			}
		}
		s.mu.Unlock()
		done()
	}()
}
