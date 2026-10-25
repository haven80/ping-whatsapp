package main

import (
	"context"
	"errors"
	"image"
	_ "image/gif"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"gioui.org/op/paint"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"google.golang.org/protobuf/proto"
)

// Stato delle immagini mostrate nelle chat.
const (
	MediaNone = iota
	MediaLoading
	MediaReady
	MediaFailed
)

type mediaImg struct {
	op    paint.ImageOp
	size  image.Point
	state int
	used  time.Time
}

type mediaCache struct {
	mu    sync.Mutex
	items map[string]*mediaImg
	queue chan [2]string
}

const maxCachedImages = 40 // limita la RAM: le immagini lontane si ricaricano dal disco

func (b *Backend) initMedia() {
	b.media = &mediaCache{items: map[string]*mediaImg{}, queue: make(chan [2]string, 1024)}
	for i := 0; i < 2; i++ {
		go supervise("download immagini", b.mediaWorker)
	}
}

// MediaImage restituisce la foto/sticker a piena qualità, avviando il download se serve.
func (b *Backend) MediaImage(chat, id string) (paint.ImageOp, image.Point, int) {
	k := chat + "/" + id
	c := b.media
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.items[k]
	if e == nil {
		e = &mediaImg{state: MediaLoading}
		c.items[k] = e
		select {
		case c.queue <- [2]string{chat, id}:
		default:
			e.state = MediaNone
			delete(c.items, k)
		}
	}
	e.used = time.Now()
	return e.op, e.size, e.state
}

func (b *Backend) mediaWorker() {
	for job := range b.media.queue {
		chat, id := job[0], job[1]
		k := chat + "/" + id
		var img image.Image
		path, err := b.ensureMediaFile(chat, id)
		if err == nil {
			img = loadScaled(path, 640)
		}
		c := b.media
		c.mu.Lock()
		e := c.items[k]
		if e != nil {
			if img != nil {
				e.op = paint.NewImageOp(img)
				e.op.Filter = paint.FilterLinear
				e.size, e.state = img.Bounds().Size(), MediaReady
			} else {
				e.state = MediaFailed
			}
		}
		c.evictLocked()
		c.mu.Unlock()
		b.changed(chat)
	}
}

func (c *mediaCache) evictLocked() {
	var ready []string
	for k, e := range c.items {
		if e.state == MediaReady {
			ready = append(ready, k)
		}
	}
	if len(ready) <= maxCachedImages {
		return
	}
	sort.Slice(ready, func(i, j int) bool { return c.items[ready[i]].used.Before(c.items[ready[j]].used) })
	for _, k := range ready[:len(ready)-maxCachedImages] {
		delete(c.items, k)
	}
}

// loadScaled apre un'immagine e la riduce (lato lungo max px) per risparmiare memoria.
func loadScaled(path string, maxPx int) image.Image {
	img := loadImage(path)
	if img == nil {
		// WebP animato: il primo fotogramma
		if data, err := os.ReadFile(path); err == nil && isAnimatedWebP(data) {
			if a, err := decodeAnimWebP(data, maxPx); err == nil {
				return a.frames[0]
			}
		}
		return nil
	}
	r := img.Bounds()
	if r.Dx() <= maxPx && r.Dy() <= maxPx {
		return img
	}
	w, h := maxPx, r.Dy()*maxPx/max(1, r.Dx())
	if r.Dy() > r.Dx() {
		w, h = r.Dx()*maxPx/max(1, r.Dy()), maxPx
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, r, xdraw.Src, nil)
	return dst
}

// ensureMediaFile scarica il file del messaggio (una volta sola) e ne restituisce il percorso.
func (b *Backend) ensureMediaFile(chat, id string) (string, error) {
	if p := b.db.MediaPath(chat, id); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	raw := b.db.MediaOf(chat, id)
	if raw == nil {
		return "", errors.New("nessun file")
	}
	if b.cli == nil {
		return "", errors.New("non connesso")
	}
	var msg waE2E.Message
	if err := proto.Unmarshal(raw, &msg); err != nil {
		return "", err
	}
	ext, name := ".bin", ""
	switch {
	case msg.ImageMessage != nil:
		ext = extFor(msg.GetImageMessage().GetMimetype(), ".jpg")
	case msg.VideoMessage != nil:
		ext = extFor(msg.GetVideoMessage().GetMimetype(), ".mp4")
	case msg.PtvMessage != nil:
		ext = ".mp4"
	case msg.AudioMessage != nil:
		ext = ".ogg"
		if mt := msg.GetAudioMessage().GetMimetype(); mt != "" && !isOgg(mt) {
			ext = extFor(mt, ".m4a")
		}
	case msg.StickerMessage != nil:
		ext = ".webp"
	case msg.DocumentMessage != nil:
		name = msg.GetDocumentMessage().GetFileName()
		ext = extFor(msg.GetDocumentMessage().GetMimetype(), ".bin")
	}
	dir := filepath.Join(b.dir, "media")
	os.MkdirAll(dir, 0o755)
	if name == "" {
		name = safeName(id) + ext
	} else {
		name = safeName(id[:min(6, len(id))]) + "_" + safeName(name)
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		data, err := b.cli.DownloadAny(ctx, &msg)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return "", err
		}
	}
	b.db.SetMediaPath(chat, id, path)
	return path, nil
}

func isOgg(mt string) bool { return len(mt) >= 9 && mt[:9] == "audio/ogg" }

// onCall registra una chiamata in arrivo nella chat (le chiamate si rispondono dal telefono).
func (b *Backend) onCall(meta types.BasicCallMeta, media string) {
	from := b.norm(meta.From)
	if !meta.CallCreatorAlt.IsEmpty() && from.Server == types.HiddenUserServer {
		from = meta.CallCreatorAlt.ToNonAD()
	}
	chat := from
	if !meta.GroupJID.IsEmpty() {
		chat = meta.GroupJID
	}
	chatID := chat.String()
	text := "Chiamata vocale in arrivo"
	if media == "video" {
		text = "Videochiamata in arrivo"
	}
	ts := meta.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	m := &Message{Chat: chatID, ID: "call-" + meta.CallID, Sender: from.String(), Time: ts,
		Text: text, Kind: KCall, Status: StSent, Unread: true}
	b.db.EnsureChat(chatID, b.displayName(chat), chat.Server == types.GroupServer)
	if !b.db.AddMessage(m) {
		return
	}
	b.reloadChats(chatID)
	if b.OnIncoming != nil {
		c, _ := b.ChatByJID(chatID)
		b.OnIncoming(c, m)
	}
}
