package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Stili delle emoji. Quello di Google è incorporato nel programma; gli altri (circa 10 MB
// l'uno) si scaricano dal repository solo se l'utente li sceglie, e si controlla che il file
// sia esattamente quello previsto (impronta SHA-256). Sono costruiti con tools/emoji/build.py.

type EmojiSet struct {
	ID, Name, Desc, Credit string
	File, SHA              string
	Size                   int64
	Preview                []byte
}

//go:embed fonts/emoji-preview-google.png
var previewGoogle []byte

//go:embed fonts/emoji-preview-fluent3d.png
var previewFluent3D []byte

//go:embed fonts/emoji-preview-fluentflat.png
var previewFluentFlat []byte

//go:embed fonts/emoji-preview-twemoji.png
var previewTwemoji []byte

const emojiBaseURL = "https://raw.githubusercontent.com/haven80/ping-whatsapp/main/fonts/emoji/v1/"

var emojiSets = []EmojiSet{
	{ID: "google", Name: "Google", Desc: "Lo stile di Android. Incluso in Ping",
		Credit: "Noto Color Emoji, Google · OFL", Preview: previewGoogle},
	{ID: "fluent3d", Name: "Fluent 3D", Desc: "Lo stile di Windows 11 e Teams",
		Credit: "Fluent Emoji, Microsoft · MIT", File: "fluent3d.ttf",
		SHA: "34a016caaaa35d9e9a1d8b2e90d7fc6ceab3760482436860fbae14e7517ac17f", Size: 10743628, Preview: previewFluent3D},
	{ID: "fluentflat", Name: "Fluent piatte", Desc: "Lo stesso disegno, piatto e pulito",
		Credit: "Fluent Emoji, Microsoft · MIT", File: "fluentflat.ttf",
		SHA: "bbe5b475bcfd26b85695c9361ef04a5562f81cd3728214a024cd29498ede3dd6", Size: 9026100, Preview: previewFluentFlat},
	{ID: "twemoji", Name: "Twemoji", Desc: "Lo stile di X/Twitter e Discord",
		Credit: "Twemoji, Twitter e collaboratori · CC-BY 4.0", File: "twemoji.ttf",
		SHA: "4a2f63dae242143a37f0e7a2284712297425a6cfcc168124bd09a6669a35bf16", Size: 8679260, Preview: previewTwemoji},
}

func emojiSetByID(id string) EmojiSet {
	for _, s := range emojiSets {
		if s.ID == id {
			return s
		}
	}
	return emojiSets[0]
}

var emojiState struct {
	mu       sync.Mutex
	data     []byte // font in uso (nil = quello incorporato)
	ver      int    // cambia a ogni cambio di stile: le finestre ricreano il motore del testo
	progress map[string]float64
	errs     map[string]string
}

// currentEmojiFont: il font emoji da usare adesso.
func currentEmojiFont() ([]byte, int) {
	emojiState.mu.Lock()
	defer emojiState.mu.Unlock()
	if emojiState.data != nil {
		return emojiState.data, emojiState.ver
	}
	return emojiTTF, emojiState.ver
}

func emojiFontVersion() int {
	emojiState.mu.Lock()
	defer emojiState.mu.Unlock()
	return emojiState.ver
}

// EmojiDownload dice se lo stile è in download (0..1) o se il download è fallito.
func EmojiDownload(id string) (progress float64, downloading bool, errText string) {
	emojiState.mu.Lock()
	defer emojiState.mu.Unlock()
	p, ok := emojiState.progress[id]
	return p, ok, emojiState.errs[id]
}

func (u *UI) emojiPath(s EmojiSet) string { return filepath.Join(u.dir, "emoji", s.File) }

// EmojiReady: lo stile è già sul PC (o è quello incorporato)?
func (u *UI) EmojiReady(id string) bool {
	s := emojiSetByID(id)
	if s.File == "" {
		return true
	}
	st, err := os.Stat(u.emojiPath(s))
	return err == nil && st.Size() == s.Size
}

func applyEmojiFont(data []byte) {
	emojiState.mu.Lock()
	emojiState.data = data
	emojiState.ver++
	emojiState.mu.Unlock()
}

// loadEmojiSet usa lo stile scelto: lo carica dal disco o lo scarica. Quando è pronto ridisegna tutto.
func (u *UI) loadEmojiSet(id string) {
	s := emojiSetByID(id)
	if s.File == "" {
		applyEmojiFont(nil)
		u.invalidateAll()
		return
	}
	if data, err := readVerified(u.emojiPath(s), s.SHA); err == nil {
		applyEmojiFont(data)
		u.invalidateAll()
		return
	}
	emojiState.mu.Lock()
	if emojiState.progress == nil {
		emojiState.progress, emojiState.errs = map[string]float64{}, map[string]string{}
	}
	if _, busy := emojiState.progress[id]; busy {
		emojiState.mu.Unlock()
		return
	}
	emojiState.progress[id] = 0
	delete(emojiState.errs, id)
	emojiState.mu.Unlock()
	safeGo("download emoji", func() {
		data, err := u.downloadEmoji(s)
		emojiState.mu.Lock()
		delete(emojiState.progress, id)
		if err != nil {
			emojiState.errs[id] = err.Error()
		}
		emojiState.mu.Unlock()
		// si applica solo se nel frattempo l'utente non ha scelto altro
		if err == nil && u.Settings().EmojiSet == id {
			applyEmojiFont(data)
		}
		u.invalidateAll()
	})
}

func readVerified(path, want string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return nil, errors.New("file diverso da quello previsto")
	}
	return data, nil
}

func (u *UI) downloadEmoji(s EmojiSet) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	base := emojiBaseURL
	if b := os.Getenv("PING_EMOJI_URL"); b != "" { // per le prove
		base = b
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", base+s.File, nil)
	req.Header.Set("User-Agent", "Ping/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("download non riuscito, controlla la connessione")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download non riuscito (%s)", resp.Status)
	}
	buf := make([]byte, 0, s.Size)
	chunk := make([]byte, 64<<10)
	last := time.Now()
	for {
		n, err := resp.Body.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if int64(len(buf)) > s.Size+1024 {
			return nil, errors.New("file più grande del previsto")
		}
		if time.Since(last) > 150*time.Millisecond {
			last = time.Now()
			emojiState.mu.Lock()
			emojiState.progress[s.ID] = float64(len(buf)) / float64(s.Size)
			emojiState.mu.Unlock()
			u.invalidateAll()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("download interrotto")
		}
	}
	sum := sha256.Sum256(buf)
	if hex.EncodeToString(sum[:]) != s.SHA {
		return nil, errors.New("il file scaricato non è quello previsto")
	}
	path := u.emojiPath(s)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path+".tmp", buf, 0o644); err == nil {
		os.Rename(path+".tmp", path)
	}
	return buf, nil
}
