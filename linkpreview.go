package main

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	_ "image/gif"
	_ "image/png"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

// LinkPreview: titolo, descrizione e immagine di una pagina web, come le mostra WhatsApp.
type LinkPreview struct {
	URL   string // indirizzo come scritto nel messaggio (con https:// davanti se mancava)
	Title string
	Desc  string
	Thumb []byte // JPEG piccolo
}

const previewUA = "Mozilla/5.0 (compatible; Ping/1.0; +https://github.com/haven80/ping-whatsapp) facebookexternalhit/1.1"

var (
	previewHTTP = &http.Client{Timeout: 8 * time.Second}
	previewMu   sync.Mutex
	previewMap  = map[string]*previewEntry{}
)

type previewEntry struct {
	done chan struct{}
	p    *LinkPreview
	err  error
}

// PreviewAsync avvia (una volta sola per indirizzo) il caricamento dell'anteprima e chiama
// onDone quando è pronta. Restituisce subito l'anteprima se è già stata caricata.
func PreviewAsync(u string, onDone func()) (p *LinkPreview, ready bool, err error) {
	previewMu.Lock()
	e := previewMap[u]
	if e == nil {
		e = &previewEntry{done: make(chan struct{})}
		previewMap[u] = e
		previewMu.Unlock()
		safeGo("anteprima link", func() {
			e.p, e.err = fetchPreview(u)
			close(e.done)
			if onDone != nil {
				onDone()
			}
		})
		return nil, false, nil
	}
	previewMu.Unlock()
	select {
	case <-e.done:
		return e.p, true, e.err
	default:
		if onDone != nil {
			go func() { <-e.done; onDone() }()
		}
		return nil, false, nil
	}
}

// PreviewWait aspetta l'anteprima (al massimo d) e la restituisce se c'è.
func PreviewWait(u string, d time.Duration) *LinkPreview {
	PreviewAsync(u, nil)
	previewMu.Lock()
	e := previewMap[u]
	previewMu.Unlock()
	select {
	case <-e.done:
		return e.p
	case <-time.After(d):
		return nil
	}
}

func fetchPreview(raw string) (*LinkPreview, error) {
	pu, err := url.Parse(raw)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return nil, errors.New("indirizzo non valido")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", raw, nil)
	req.Header.Set("User-Agent", previewUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	req.Header.Set("Accept-Language", "it-IT,it;q=0.9,en;q=0.6")
	resp, err := previewHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errors.New(resp.Status)
	}
	ct := resp.Header.Get("Content-Type")
	p := &LinkPreview{URL: raw}
	base := resp.Request.URL
	if strings.HasPrefix(ct, "image/") { // link diretto a un'immagine
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		p.Thumb = previewThumb(data)
		p.Title = pathName(base)
		return p, nil
	}
	if ct != "" && !strings.Contains(ct, "html") {
		return nil, errors.New("non è una pagina web")
	}
	body, err := charset.NewReader(io.LimitReader(resp.Body, 768<<10), ct)
	if err != nil {
		body = io.LimitReader(resp.Body, 768<<10)
	}
	meta := parseMeta(body)
	p.Title = firstNonEmpty(meta["og:title"], meta["twitter:title"], meta["title"])
	p.Desc = firstNonEmpty(meta["og:description"], meta["twitter:description"], meta["description"])
	p.Title, p.Desc = clip1(p.Title, 140), clip1(p.Desc, 240)
	if img := firstNonEmpty(meta["og:image:secure_url"], meta["og:image"], meta["twitter:image"], meta["twitter:image:src"]); img != "" {
		if iu, err := base.Parse(strings.TrimSpace(img)); err == nil && (iu.Scheme == "http" || iu.Scheme == "https") {
			p.Thumb = fetchThumb(ctx, iu.String())
		}
	}
	if p.Title == "" && p.Desc == "" && len(p.Thumb) == 0 {
		return nil, errors.New("nessuna anteprima")
	}
	return p, nil
}

// parseMeta legge dal <head> i meta tag og:, twitter:, description e il <title>.
func parseMeta(r io.Reader) map[string]string {
	out := map[string]string{}
	z := html.NewTokenizer(r)
	inTitle := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "body":
				return out // i meta stanno nell'intestazione
			case "title":
				inTitle = true
			case "meta":
				var key, content string
				for hasAttr {
					var k, v []byte
					k, v, hasAttr = z.TagAttr()
					switch strings.ToLower(string(k)) {
					case "property", "name", "itemprop":
						if key == "" || strings.HasPrefix(strings.ToLower(string(v)), "og:") {
							key = strings.ToLower(string(v))
						}
					case "content":
						content = string(v)
					}
				}
				if key != "" && content != "" && out[key] == "" {
					out[key] = html.UnescapeString(content)
				}
			}
		case html.TextToken:
			if inTitle && out["title"] == "" {
				out["title"] = strings.TrimSpace(html.UnescapeString(string(z.Text())))
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "title" {
				inTitle = false
			}
		}
	}
}

func fetchThumb(ctx context.Context, u string) []byte {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", previewUA)
	resp, err := previewHTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return previewThumb(data)
}

// previewThumb riduce l'immagine a una miniatura JPEG (lato lungo 300 px).
func previewThumb(data []byte) []byte {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	r := img.Bounds()
	if r.Dx() < 16 || r.Dy() < 16 {
		return nil // icone minuscole o pixel di tracciamento
	}
	const side = 300
	w, h := r.Dx(), r.Dy()
	if w > side || h > side {
		if w >= h {
			w, h = side, side*h/w
		} else {
			w, h = side*w/h, side
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(1, w), max(1, h)))
	xdraw.Draw(dst, dst.Bounds(), image.White, image.Point{}, xdraw.Src) // sfondo per le PNG trasparenti
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, r, xdraw.Over, nil)
	var buf bytes.Buffer
	if jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 75}) != nil {
		return nil
	}
	return buf.Bytes()
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// clip1 mette tutto su una riga e taglia i testi troppo lunghi.
func clip1(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:n-1])) + "…"
	}
	return s
}

func pathName(u *url.URL) string {
	p := strings.TrimRight(u.Path, "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		return u.Host
	}
	return p
}
