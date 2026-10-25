package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"os"
	"strings"
	"time"

	"github.com/HugoSmits86/nativewebp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	xdraw "golang.org/x/image/draw"
	"google.golang.org/protobuf/proto"
)

// ---- risposte ----

// replyContext prepara la citazione del messaggio a cui si risponde.
func (b *Backend) replyContext(reply *Message) *waE2E.ContextInfo {
	if reply == nil || b.cli == nil {
		return nil
	}
	var quoted *waE2E.Message
	if raw := b.db.MediaOf(reply.Chat, reply.ID); raw != nil {
		quoted = new(waE2E.Message)
		if proto.Unmarshal(raw, quoted) != nil {
			quoted = nil
		}
	}
	if quoted == nil {
		t := reply.Text
		if reply.Kind != KText {
			t = previewMsg(reply)
		}
		quoted = &waE2E.Message{Conversation: proto.String(t)}
	}
	participant := reply.Sender
	if reply.FromMe && b.cli.Store.ID != nil {
		participant = b.cli.Store.ID.ToNonAD().String()
	}
	return &waE2E.ContextInfo{
		StanzaID:      proto.String(reply.ID),
		Participant:   proto.String(participant),
		QuotedMessage: quoted,
	}
}

// applyQuote copia nel messaggio locale le informazioni della citazione (per mostrarla subito).
func (b *Backend) applyQuote(m *Message, reply *Message) {
	if reply == nil {
		return
	}
	m.QuoteID = reply.ID
	m.QuoteText = previewMsg(reply)
	if reply.Kind == KText {
		m.QuoteText = reply.Text
	}
	switch {
	case reply.FromMe:
		m.QuoteSender = "Tu"
	case reply.SenderName != "":
		m.QuoteSender = reply.SenderName
	default:
		c, _ := b.ChatByJID(reply.Chat)
		m.QuoteSender = c.Name
	}
}

// sendPrepared salva subito il messaggio (in attesa) e lo spedisce in background.
// build riceve la citazione (o nil) e costruisce il messaggio WhatsApp; può caricare file.
func (b *Backend) sendPrepared(jid string, m *Message, reply *Message,
	build func(ci *waE2E.ContextInfo) (*waE2E.Message, error)) {
	if b.cli == nil {
		return
	}
	j, err := types.ParseJID(jid)
	if err != nil {
		return
	}
	id := b.cli.GenerateMessageID()
	me, _ := b.Me()
	m.Chat, m.ID, m.FromMe, m.Sender, m.Time, m.Status = jid, id, true, me, time.Now(), StPending
	b.applyQuote(m, reply)
	b.db.EnsureChat(jid, b.chatNameOr(jid, j), j.Server == types.GroupServer)
	b.db.AddMessage(m)
	b.reloadChats(jid)
	b.mu.Lock()
	delete(b.lastComposing, jid)
	b.mu.Unlock()
	ci := b.replyContext(reply)
	go func() {
		msg, err := build(ci)
		if err == nil {
			if raw, e := proto.Marshal(msg); e == nil && msg.Conversation == nil && msg.ExtendedTextMessage == nil {
				b.db.SetMedia(jid, id, raw)
			}
			var resp whatsmeow.SendResponse
			resp, err = b.cli.SendMessage(context.Background(), j, msg, whatsmeow.SendRequestExtra{ID: id})
			if err == nil {
				b.db.SetSentTime(jid, id, resp.Timestamp)
			}
		}
		if err != nil {
			b.db.SetStatus(jid, []string{id}, StFailed)
		} else {
			b.db.SetStatus(jid, []string{id}, StSent)
		}
		b.changed(jid)
	}()
}

// ---- reazioni ----

// React aggiunge una reazione; se è la stessa già messa la toglie.
func (b *Backend) React(m *Message, emoji string) {
	for _, r := range m.Reactions {
		if r.Sender == "me" && r.Emoji == emoji {
			emoji = "" // stessa reazione: la tolgo
		}
	}
	b.db.SetReaction(m.Chat, m.ID, "me", emoji, time.Now())
	b.changed(m.Chat)
	if b.cli == nil {
		return
	}
	chat, err := types.ParseJID(m.Chat)
	if err != nil {
		return
	}
	var sender types.JID
	if m.FromMe {
		if b.cli.Store.ID != nil {
			sender = b.cli.Store.ID.ToNonAD()
		}
	} else {
		sender, _ = types.ParseJID(m.Sender)
	}
	msg := b.cli.BuildReaction(chat, sender, m.ID, emoji)
	go b.cli.SendMessage(context.Background(), chat, msg)
}

// ---- sticker ----

func isAnimatedWebP(data []byte) bool {
	return len(data) > 30 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" && bytes.Contains(data[:min(len(data), 64)], []byte("ANIM"))
}

// stickerFromFile converte un'immagine (png, jpg, webp, gif) in uno sticker WebP 512×512.
// Le GIF animate diventano sticker animati.
func stickerFromFile(path string) (data []byte, animated bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	// animazioni (GIF o WebP animati) -> sticker animato
	if isGIF(raw) {
		if g, e := gif.DecodeAll(bytes.NewReader(raw)); e == nil && len(g.Image) > 1 {
			frames, delays := compositeGIF(g)
			return encodeAnimSticker(frames, delays)
		}
	}
	if isWebP(raw) {
		if a, e := decodeAnimWebP(raw, 0); e == nil {
			if len(a.frames) > 1 {
				return encodeAnimSticker(a.frames, a.delays)
			}
			if b := a.frames[0].Bounds(); b.Dx() == 512 && b.Dy() == 512 && len(raw) <= 100*1024 {
				return raw, false, nil // già uno sticker pronto
			}
			return encodeStaticSticker(a.frames[0])
		}
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false, errors.New("formato immagine non riconosciuto")
	}
	return encodeStaticSticker(img)
}

func encodeStaticSticker(img image.Image) ([]byte, bool, error) {
	for _, side := range []int{512, 448, 384, 320, 256} {
		var buf bytes.Buffer
		if err := nativewebp.Encode(&buf, fitSquare(img, side), &nativewebp.Options{UseExtendedFormat: true}); err != nil {
			return nil, false, err
		}
		if buf.Len() <= 100*1024 || side == 256 {
			return buf.Bytes(), false, nil
		}
	}
	return nil, false, errors.New("immagine troppo complessa")
}

// encodeAnimSticker crea uno sticker animato 512×512 (max 500 kB, come richiede WhatsApp).
func encodeAnimSticker(frames []image.Image, delays []time.Duration) ([]byte, bool, error) {
	// al massimo ~50 fotogrammi: se sono di più se ne tiene uno ogni n, sommando le durate
	step := max(1, (len(frames)+49)/50)
	for _, side := range []int{512, 384, 320, 256, 192} {
		var imgs []image.Image
		var durs, disp []uint
		for i := 0; i < len(frames); i += step {
			var d time.Duration
			for k := i; k < min(i+step, len(frames)); k++ {
				d += max(delays[k], 20*time.Millisecond)
			}
			imgs = append(imgs, fitSquare(frames[i], side))
			durs = append(durs, uint(d.Milliseconds()))
			disp = append(disp, 0)
		}
		var buf bytes.Buffer
		ani := &nativewebp.Animation{Images: imgs, Durations: durs, Disposals: disp}
		if err := nativewebp.EncodeAll(&buf, ani, nil); err != nil {
			return nil, false, err
		}
		if buf.Len() <= 500*1024 || side == 192 {
			return buf.Bytes(), true, nil
		}
	}
	return nil, false, errors.New("animazione troppo pesante")
}

// fitSquare centra l'immagine in un quadrato 512×512 trasparente, ridotta al lato indicato.
func fitSquare(src image.Image, side int) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	r := src.Bounds()
	w, h := side, r.Dy()*side/max(1, r.Dx())
	if r.Dy() > r.Dx() {
		w, h = r.Dx()*side/max(1, r.Dy()), side
	}
	off := image.Pt((512-w)/2, (512-h)/2)
	xdraw.CatmullRom.Scale(dst, image.Rectangle{Min: off, Max: off.Add(image.Pt(w, h))}, src, r, draw.Over, nil)
	return dst
}

// SendStickerData invia uno sticker già in formato WebP.
func (b *Backend) SendStickerData(jid string, data []byte, animated bool, reply *Message) {
	path := b.saveOutgoing(data, ".webp")
	m := &Message{Kind: KSticker, MediaPath: path, Extra: MsgExtra{Animated: animated}}
	b.sendPrepared(jid, m, reply, func(ci *waE2E.ContextInfo) (*waE2E.Message, error) {
		up, err := b.cli.Upload(context.Background(), data, whatsmeow.MediaImage)
		if err != nil {
			return nil, err
		}
		return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String("image/webp"), Width: proto.Uint32(512), Height: proto.Uint32(512),
			IsAnimated: proto.Bool(animated), ContextInfo: ci,
		}}, nil
	})
}

// SendStickerFile crea uno sticker da un'immagine del PC e lo invia.
func (b *Backend) SendStickerFile(jid, path string, reply *Message) error {
	data, animated, err := stickerFromFile(path)
	if err != nil {
		return err
	}
	b.SendStickerData(jid, data, animated, reply)
	return nil
}

// ResendSticker reinvia uno sticker già ricevuto o inviato.
func (b *Backend) ResendSticker(jid, fromChat, fromID string, reply *Message) {
	go func() {
		path, err := b.ensureMediaFile(fromChat, fromID)
		if err != nil {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		b.SendStickerData(jid, data, isAnimatedWebP(data), reply)
	}()
}

func (b *Backend) saveOutgoing(data []byte, ext string) string {
	dir := b.dir + string(os.PathSeparator) + "media"
	os.MkdirAll(dir, 0o755)
	p := dir + string(os.PathSeparator) + "out_" + time.Now().Format("20060102_150405.000") + ext
	os.WriteFile(p, data, 0o644)
	return p
}

// ---- GIF ----

// SendGIFFile invia una GIF: i file .gif vengono convertiti in MP4 (come fa WhatsApp).
func (b *Backend) SendGIFFile(jid, path string, reply *Message) error {
	mp4 := path
	if strings.HasSuffix(strings.ToLower(path), ".gif") {
		out := b.dir + string(os.PathSeparator) + "media" + string(os.PathSeparator) + "out_" + time.Now().Format("20060102_150405") + ".mp4"
		os.MkdirAll(b.dir+string(os.PathSeparator)+"media", 0o755)
		if err := gifToMP4(path, out); err != nil {
			return err
		}
		mp4 = out
	}
	b.sendVideo(jid, mp4, true, reply)
	return nil
}

// ResendGIF reinvia una GIF già ricevuta o inviata.
func (b *Backend) ResendGIF(jid, fromChat, fromID string, reply *Message) {
	go func() {
		if path, err := b.ensureMediaFile(fromChat, fromID); err == nil {
			b.sendVideo(jid, path, true, reply)
		}
	}()
}

func (b *Backend) sendVideo(jid, path string, asGIF bool, reply *Message) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	info := probeVideo(path) // prima immagine, durata e dimensioni
	var thumb []byte
	if info.first != nil {
		var buf bytes.Buffer
		jpeg.Encode(&buf, fitThumb(info.first, 200), &jpeg.Options{Quality: 75})
		thumb = buf.Bytes()
	}
	m := &Message{Kind: KVideo, MediaPath: path, Thumb: thumb,
		Extra: MsgExtra{GIF: asGIF, Seconds: info.seconds, W: uint32(info.w), H: uint32(info.h)}}
	b.sendPrepared(jid, m, reply, func(ci *waE2E.ContextInfo) (*waE2E.Message, error) {
		up, err := b.cli.Upload(context.Background(), data, whatsmeow.MediaVideo)
		if err != nil {
			return nil, err
		}
		v := &waE2E.VideoMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String("video/mp4"), JPEGThumbnail: thumb, Seconds: proto.Uint32(info.seconds),
			GifPlayback: proto.Bool(asGIF), ContextInfo: ci,
		}
		if info.w > 0 {
			v.Width, v.Height = proto.Uint32(uint32(info.w)), proto.Uint32(uint32(info.h))
		}
		return &waE2E.Message{VideoMessage: v}, nil
	})
}

func fitThumb(img image.Image, side int) image.Image {
	r := img.Bounds()
	w, h := side, r.Dy()*side/max(1, r.Dx())
	if r.Dy() > r.Dx() {
		w, h = r.Dx()*side/max(1, r.Dy()), side
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, r, xdraw.Src, nil)
	return dst
}

// ---- raccolte per il pannello ----

type mediaRef struct {
	Chat, ID string
	Thumb    []byte
}

// RecentStickers: sticker visti di recente, senza doppioni.
func (b *Backend) RecentStickers(limit int) []mediaRef {
	return b.db.recentMedia(`kind='sticker'`, limit, func(m *waE2E.Message) []byte {
		return m.GetStickerMessage().GetFileSHA256()
	})
}

// RecentGIFs: GIF viste di recente, senza doppioni.
func (b *Backend) RecentGIFs(limit int) []mediaRef {
	return b.db.recentMedia(`kind='video' AND extra LIKE '%"g":true%'`, limit, func(m *waE2E.Message) []byte {
		return m.GetVideoMessage().GetFileSHA256()
	})
}

func (s *Store) recentMedia(where string, limit int, key func(*waE2E.Message) []byte) []mediaRef {
	s.mu.Lock()
	rows, err := s.db.Query(`SELECT chat, id, thumb, media, media_path FROM messages
		WHERE (media IS NOT NULL OR media_path<>'') AND `+where+` ORDER BY ts DESC LIMIT ?`, limit*4)
	if err != nil {
		s.mu.Unlock()
		return nil
	}
	type row struct {
		r    mediaRef
		raw  []byte
		path string
	}
	var all []row
	for rows.Next() {
		var x row
		rows.Scan(&x.r.Chat, &x.r.ID, &x.r.Thumb, &x.raw, &x.path)
		all = append(all, x)
	}
	rows.Close()
	s.mu.Unlock()
	seen := map[string]bool{}
	var out []mediaRef
	for _, x := range all {
		k := x.path
		if x.raw != nil {
			var m waE2E.Message
			if proto.Unmarshal(x.raw, &m) == nil {
				if h := key(&m); len(h) > 0 {
					k = string(h)
				}
			}
		}
		if k == "" {
			k = x.r.Chat + x.r.ID
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, x.r)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// SendImage invia una foto (jpg/png/webp…) con didascalia facoltativa.
func (b *Backend) SendImage(jid, path, caption string, reply *Message) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return errors.New("immagine non riconosciuta")
	}
	data := raw
	if !bytes.HasPrefix(raw, []byte{0xFF, 0xD8}) { // WhatsApp vuole JPEG per le foto
		var buf bytes.Buffer
		flat := image.NewRGBA(img.Bounds())
		draw.Draw(flat, flat.Bounds(), image.White, image.Point{}, draw.Src) // trasparenza su bianco
		draw.Draw(flat, flat.Bounds(), img, img.Bounds().Min, draw.Over)
		if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: 88}); err != nil {
			return err
		}
		data = buf.Bytes()
	}
	thumb := makeThumb(data)
	sz := img.Bounds().Size()
	m := &Message{Kind: KImage, Text: caption, MediaPath: path, Thumb: thumb,
		Extra: MsgExtra{W: uint32(sz.X), H: uint32(sz.Y)}}
	b.sendPrepared(jid, m, reply, func(ci *waE2E.ContextInfo) (*waE2E.Message, error) {
		up, err := b.cli.Upload(context.Background(), data, whatsmeow.MediaImage)
		if err != nil {
			return nil, err
		}
		im := &waE2E.ImageMessage{
			URL: proto.String(up.URL), DirectPath: proto.String(up.DirectPath), MediaKey: up.MediaKey,
			FileEncSHA256: up.FileEncSHA256, FileSHA256: up.FileSHA256, FileLength: proto.Uint64(up.FileLength),
			Mimetype: proto.String("image/jpeg"), JPEGThumbnail: thumb, ContextInfo: ci,
			Width: proto.Uint32(uint32(sz.X)), Height: proto.Uint32(uint32(sz.Y)),
		}
		if strings.TrimSpace(caption) != "" {
			im.Caption = proto.String(caption)
		}
		return &waE2E.Message{ImageMessage: im}, nil
	})
	return nil
}

// ---- modifica e cancellazione ----

const (
	editWindow   = 15 * time.Minute // come l'app ufficiale
	revokeWindow = 60 * time.Hour   // "elimina per tutti" vale per circa due giorni e mezzo
)

func cleanEdited(t string) string { return strings.TrimSuffix(t, "  ✎") }

// CanEdit: solo i propri messaggi di testo, entro 15 minuti dall'invio.
func CanEdit(m *Message) bool {
	return m.FromMe && m.Kind == KText && m.Status != StPending && m.Status != StFailed &&
		time.Since(m.Time) < editWindow && !strings.HasPrefix(m.ID, "demo")
}

// CanRevoke: solo i propri messaggi, entro il limite di WhatsApp.
func CanRevoke(m *Message) bool {
	return m.FromMe && m.Kind != KDeleted && m.Kind != KCall && time.Since(m.Time) < revokeWindow
}

func (b *Backend) myJID() types.JID {
	if b.cli != nil && b.cli.Store.ID != nil {
		return b.cli.Store.ID.ToNonAD()
	}
	return types.JID{}
}

// EditMessage modifica il testo di un proprio messaggio per tutti.
func (b *Backend) EditMessage(m *Message, text string) {
	text = strings.TrimRight(text, " \n\r\t")
	if strings.TrimSpace(text) == "" || text == cleanEdited(m.Text) {
		return
	}
	b.db.UpdateText(m.Chat, m.ID, text+"  ✎", KText)
	b.changed(m.Chat)
	if b.cli == nil {
		return
	}
	chat, err := types.ParseJID(m.Chat)
	if err != nil {
		return
	}
	id := m.ID
	safeGo("modifica messaggio", func() {
		msg := b.cli.BuildEdit(chat, id, &waE2E.Message{Conversation: proto.String(text)})
		if _, err := b.cli.SendMessage(context.Background(), chat, msg); err != nil {
			b.db.SetStatus(chat.String(), []string{id}, StFailed)
			b.changed(chat.String())
		}
	})
}

// DeleteForEveryone cancella un proprio messaggio per tutti.
func (b *Backend) DeleteForEveryone(m *Message) {
	b.db.UpdateText(m.Chat, m.ID, "", KDeleted)
	b.db.refreshChatPreview(m.Chat)
	b.reloadChats(m.Chat)
	if b.cli == nil {
		return
	}
	chat, err := types.ParseJID(m.Chat)
	if err != nil {
		return
	}
	id, me := m.ID, b.myJID()
	safeGo("eliminazione messaggio", func() {
		b.cli.SendMessage(context.Background(), chat, b.cli.BuildRevoke(chat, me, id))
	})
}

// DeleteForMe toglie il messaggio solo da questo PC.
func (b *Backend) DeleteForMe(m *Message) {
	b.db.DeleteMessage(m.Chat, m.ID)
	b.db.refreshChatPreview(m.Chat)
	b.reloadChats(m.Chat)
}

func (s *Store) DeleteMessage(chat, id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db.Exec(`DELETE FROM messages WHERE chat=? AND id=?`, chat, id)
	s.db.Exec(`DELETE FROM reactions WHERE chat=? AND msg_id=?`, chat, id)
}

// refreshChatPreview ricalcola l'anteprima della chat dall'ultimo messaggio rimasto.
func (s *Store) refreshChatPreview(chat string) {
	ms := s.Messages(chat, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(ms) == 0 {
		s.db.Exec(`UPDATE chats SET last_text='' WHERE jid=?`, chat)
		return
	}
	m := ms[0]
	s.db.Exec(`UPDATE chats SET last_text=?, last_ts=?, last_me=? WHERE jid=?`, preview(&m), m.Time.Unix(), b2i(m.FromMe), chat)
}
