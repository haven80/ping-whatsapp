package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/HugoSmits86/nativewebp"
)

// demoPhoto disegna un "tramonto sul mare" finto per provare le foto.
func demoPhoto(dir string) (string, []byte) {
	w, h := 800, 560
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			t := float64(y) / float64(h)
			var c color.RGBA
			if y < h*58/100 {
				c = color.RGBA{uint8(250 - 60*t), uint8(140 + 60*t), uint8(90 + 120*t), 255}
				dx, dy := float64(x-w/2), float64(y-h*50/100)
				if d := math.Hypot(dx, dy); d < 70 {
					c = color.RGBA{255, 225, 140, 255}
				}
			} else {
				k := 0.5 + 0.5*math.Sin(float64(x)/18+float64(y)/4)
				c = color.RGBA{uint8(30 + 40*k), uint8(80 + 40*k), uint8(130 + 30*k), 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85})
	p := filepath.Join(dir, "demo-foto.jpg")
	os.WriteFile(p, buf.Bytes(), 0o644)
	return p, makeThumb(buf.Bytes())
}

func demoSticker(dir string) string {
	s := 256
	img := image.NewNRGBA(image.Rect(0, 0, s, s))
	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			dx, dy := float64(x-s/2), float64(y-s/2)
			d := math.Hypot(dx, dy)
			switch {
			case d < 100 && dy > 20 && dy < 45 && math.Abs(dx) < 50:
				img.Set(x, y, color.NRGBA{90, 50, 20, 255}) // bocca
			case d < 100 && dy > -40 && dy < -15 && (math.Abs(dx-35) < 12 || math.Abs(dx+35) < 12):
				img.Set(x, y, color.NRGBA{60, 40, 20, 255}) // occhi
			case d < 100:
				img.Set(x, y, color.NRGBA{255, 205, 60, 255})
			case d < 110:
				img.Set(x, y, color.NRGBA{255, 255, 255, 255})
			}
		}
	}
	p := filepath.Join(dir, "demo-sticker.png")
	f, _ := os.Create(p)
	png.Encode(f, img)
	f.Close()
	return p
}

// demoAnim crea una GIF (pallina che rimbalza) e uno sticker WebP animato.
func demoAnim(dir string) (gifPath, webpPath string, thumb []byte) {
	pal := color.Palette{color.RGBA{40, 44, 52, 255}, color.RGBA{255, 120, 80, 255}, color.RGBA{80, 200, 160, 255}}
	g := &gif.GIF{}
	var frames []image.Image
	var durs, disp []uint
	for i := 0; i < 16; i++ {
		img := image.NewPaletted(image.Rect(0, 0, 240, 160), pal)
		y := 120 - int(90*math.Abs(math.Sin(float64(i)*math.Pi/16)))
		for py := 0; py < 160; py++ {
			for px := 0; px < 240; px++ {
				if math.Hypot(float64(px-120), float64(py-y)) < 18 {
					img.SetColorIndex(px, py, 1)
				} else if py > 140 {
					img.SetColorIndex(px, py, 2)
				}
			}
		}
		g.Image = append(g.Image, img)
		g.Delay = append(g.Delay, 5)
		// sticker: faccina che ruota colore
		st := image.NewNRGBA(image.Rect(0, 0, 256, 256))
		hue := uint8(i * 15)
		for py := 0; py < 256; py++ {
			for px := 0; px < 256; px++ {
				d := math.Hypot(float64(px-128), float64(py-128))
				if d < 100 {
					st.Set(px, py, color.NRGBA{255, 180 + hue/4, 40 + hue, 255})
				}
			}
		}
		frames = append(frames, st)
		durs = append(durs, 80)
		disp = append(disp, 0)
	}
	gifPath = filepath.Join(dir, "demo.gif")
	f, _ := os.Create(gifPath)
	gif.EncodeAll(f, g)
	f.Close()
	var buf bytes.Buffer
	nativewebp.EncodeAll(&buf, &nativewebp.Animation{Images: frames, Durations: durs, Disposals: disp}, nil)
	webpPath = filepath.Join(dir, "demo-anim.webp")
	os.WriteFile(webpPath, buf.Bytes(), 0o644)
	var tb bytes.Buffer
	jpeg.Encode(&tb, g.Image[0], nil)
	return gifPath, webpPath, tb.Bytes()
}

// fillDemo riempie il client con conversazioni di esempio (opzione -demo).
func fillDemo(b *Backend, u *UI) {
	now := time.Now()
	type dm struct {
		jid, name string
		group     bool
		msgs      []Message
		unread    int
		online    bool
	}
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	data := []dm{
		{jid: "393331112233@s.whatsapp.net", name: "Giulia Rossi", online: true, unread: 2, msgs: []Message{
			{Text: "Ciao! Stasera ci sei per la pizza?", Time: ago(26 * time.Hour)},
			{Text: "Certo, alle 21 va bene?", FromMe: true, Time: ago(25 * time.Hour), Status: StRead},
			{Text: "Perfetto 🍕", Time: ago(25 * time.Hour)},
			{Text: "Ho prenotato da Gino, tavolo per quattro", Time: ago(12 * time.Minute)},
			{Text: "Porti tu il dolce?", Time: ago(11 * time.Minute)},
		}},
		{jid: "120363000000001@g.us", name: "Calcetto del giovedì", group: true, unread: 5, msgs: []Message{
			{Text: "Chi c'è giovedì?", Sender: "393401111222@s.whatsapp.net", SenderName: "Marco", Time: ago(3 * time.Hour)},
			{Text: "Io sì 💪", Sender: "393402333444@s.whatsapp.net", SenderName: "Luca B.", Time: ago(170 * time.Minute)},
			{Text: "Presente", FromMe: true, Time: ago(160 * time.Minute), Status: StDelivered},
			{Text: "Ci manca il portiere come al solito", Sender: "393401111222@s.whatsapp.net", SenderName: "Marco", Time: ago(40 * time.Minute)},
		}},
		{jid: "393479998877@s.whatsapp.net", name: "Mamma", online: false, msgs: []Message{
			{Text: "Domenica pranzo da noi?", Time: ago(50 * time.Hour)},
			{Text: "Sì, arrivo verso l'una ❤️", FromMe: true, Time: ago(49 * time.Hour), Status: StRead},
		}},
		{jid: "393285554433@s.whatsapp.net", name: "Andrea Bianchi", online: true, msgs: []Message{
			{Text: "Ti mando il preventivo per il sito", Time: ago(4 * time.Hour)},
			{Kind: KDoc, FileName: "Preventivo_sito_CORIME.pdf", Time: ago(4 * time.Hour)},
			{Text: "Grazie, lo guardo stasera", FromMe: true, Time: ago(3 * time.Hour), Status: StRead},
		}},
		{jid: "393201234567@s.whatsapp.net", name: "Sara", msgs: []Message{
			{Kind: KAudio, Text: "Messaggio vocale · 0:42", Time: ago(5 * 24 * time.Hour)},
		}},
		{jid: "390612345678@s.whatsapp.net", name: "Officina Ferri", msgs: []Message{
			{Text: "La macchina è pronta, può passare quando vuole.", Time: ago(9 * 24 * time.Hour)},
		}},
		{jid: "120363000000002@g.us", name: "Famiglia", group: true, msgs: []Message{
			{Text: "Buon compleanno nonna!! 🎉", Sender: "393475556666@s.whatsapp.net", SenderName: "Zia Anna", Time: ago(30 * time.Hour)},
		}},
		{jid: "393387776655@s.whatsapp.net", name: "Paolo Conti", msgs: []Message{
			{Text: "Ok, ci sentiamo lunedì", FromMe: true, Time: ago(3 * 24 * time.Hour), Status: StRead},
		}},
	}
	photo, photoThumb := demoPhoto(b.dir)
	photoData, _ := os.ReadFile(photo)
	linkThumb := previewThumb(photoData) // anteprima link grande
	sticker := demoSticker(b.dir)
	gifP, animSticker, gifThumb := demoAnim(b.dir)
	data = append(data, dm{jid: "393409876543@s.whatsapp.net", name: "Chiara Neri", online: true, unread: 3, msgs: []Message{
		{Text: "Guarda che tramonto stasera!", Time: ago(95 * time.Minute)},
		{Kind: KImage, Text: "Sperlonga 🌅", Thumb: photoThumb, MediaPath: photo, Time: ago(94 * time.Minute)},
		{Text: "Stupendo 😍", FromMe: true, Time: ago(90 * time.Minute), Status: StRead,
			QuoteSender: "Chiara Neri", QuoteText: "📷 Sperlonga 🌅",
			Reactions: []Reaction{{Sender: "x", Emoji: "❤️"}}},
		{Kind: KSticker, MediaPath: sticker, Time: ago(88 * time.Minute)},
		{Text: "😂😂😂", FromMe: true, Time: ago(80 * time.Minute), Status: StRead},
		{Kind: KVideo, Thumb: photoThumb, Time: ago(70 * time.Minute), Extra: MsgExtra{Seconds: 37, W: 1280, H: 720}},
		{Kind: KAudio, Text: "Messaggio vocale", Time: ago(60 * time.Minute), Extra: MsgExtra{Seconds: 42}},
		{Kind: KLocation, Time: ago(50 * time.Minute), Extra: MsgExtra{Title: "Ristorante La Risacca", Desc: "Via Fontana 12, Sperlonga", URL: "https://www.google.com/maps?q=41.26,13.43"}},
		{Kind: KPoll, Time: ago(40 * time.Minute), Extra: MsgExtra{Title: "Domani dove andiamo?", Options: []string{"Mare", "Montagna", "Divano"}}},
		{Kind: KContact, Time: ago(30 * time.Minute), Extra: MsgExtra{Title: "Luca Ferri", Options: []string{"+39 333 123 4567"}}},
		{Text: "Leggi qui: https://www.repubblica.it/tecnologia", FromMe: true, Time: ago(20 * time.Minute), Status: StDelivered,
			Extra: MsgExtra{URL: "https://www.repubblica.it/tecnologia", Title: "Tecnologia - la Repubblica", Desc: "Le ultime notizie su tecnologia, internet e innovazione"}, Thumb: linkThumb},
		{Kind: KCall, Text: "Chiamata vocale in arrivo", Time: ago(10 * time.Minute)},
		{Kind: KDoc, FileName: "Biglietti_treno.pdf", Time: ago(5 * time.Minute), Extra: MsgExtra{Size: 245_000, Pages: 2, Forwarded: true}},
		{Kind: KVideo, MediaPath: gifP, Thumb: gifThumb, Time: ago(4 * time.Minute), Extra: MsgExtra{GIF: true, W: 240, H: 160}},
		{Kind: KSticker, MediaPath: animSticker, Time: ago(3 * time.Minute), Extra: MsgExtra{Animated: true}},
	}})
	for _, c := range data {
		b.db.EnsureChat(c.jid, c.name, c.group)
		for i, m := range c.msgs {
			m.Chat, m.ID = c.jid, fmt.Sprintf("demo%d", i)
			if m.Kind == "" {
				m.Kind = KText
			}
			if !m.FromMe && i >= len(c.msgs)-c.unread {
				m.Unread = true
			}
			if m.FromMe {
				m.Sender = "me"
			}
			b.addWithPreview(&m)
			for _, r := range m.Reactions {
				b.db.SetReaction(c.jid, m.ID, r.Sender, r.Emoji, m.Time)
			}
		}
		if c.unread > 0 {
			b.db.SetUnread(c.jid, c.unread)
		}
		b.mu.Lock()
		b.presence[c.jid] = &Presence{Online: c.online, LastSeen: ago(37 * time.Minute)}
		b.avatars[c.jid] = nil
		b.mu.Unlock()
	}
	b.mu.Lock()
	b.presence["393331112233@s.whatsapp.net"].Typing = now.Add(time.Hour)
	b.avatars["me"] = nil
	b.state = StateReady
	b.demoMe = "Max"
	b.mu.Unlock()
	b.reloadChats("")
}
