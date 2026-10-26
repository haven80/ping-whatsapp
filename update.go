package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Version è la versione di questo eseguibile: la imposta la build di GitHub
// (-ldflags "-X main.Version=v1.2.3"); "dev" nelle compilazioni fatte a mano.
var Version = "dev"

const releasesAPI = "https://api.github.com/repos/haven80/ping-whatsapp/releases?per_page=10"

// UpdateInfo: una versione più recente pubblicata su GitHub.
type UpdateInfo struct {
	Tag string
	URL string // pagina della release
}

var updates struct {
	mu     sync.Mutex
	avail  *UpdateInfo
	told   string // versione già segnalata con una notifica
	lastOK time.Time
}

// AvailableUpdate restituisce la versione nuova, se c'è.
func AvailableUpdate() *UpdateInfo {
	updates.mu.Lock()
	defer updates.mu.Unlock()
	return updates.avail
}

// startUpdateChecks controlla le release poco dopo l'avvio e poi ogni 6 ore.
func (u *UI) startUpdateChecks() {
	if parseVersion(Version) == nil {
		return // compilazione di sviluppo: niente avvisi
	}
	safeGo("controllo aggiornamenti", func() {
		time.Sleep(20 * time.Second)
		for {
			if u.Settings().CheckUpdates {
				u.checkUpdate()
			}
			time.Sleep(6 * time.Hour)
		}
	})
}

func (u *UI) checkUpdate() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Ping/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return
	}
	var rels []struct {
		Tag   string `json:"tag_name"`
		URL   string `json:"html_url"`
		Draft bool   `json:"draft"`
	}
	if json.NewDecoder(resp.Body).Decode(&rels) != nil {
		return
	}
	var best *UpdateInfo
	for _, r := range rels {
		if r.Draft || parseVersion(r.Tag) == nil {
			continue
		}
		if best == nil || newerVersion(r.Tag, best.Tag) {
			best = &UpdateInfo{Tag: r.Tag, URL: r.URL}
		}
	}
	if best == nil || !newerVersion(best.Tag, Version) {
		best = nil
	}
	updates.mu.Lock()
	updates.avail, updates.lastOK = best, time.Now()
	notify := best != nil && updates.told != best.Tag && u.Settings().SkipVersion != best.Tag
	if notify {
		updates.told = best.Tag
	}
	updates.mu.Unlock()
	if notify && u.Settings().Notify {
		Notify("È disponibile Ping "+best.Tag, "Aprilo dalla lista delle chat per scaricarlo.")
	}
	u.invalidateAll()
}

// parseVersion legge "v1.2.3" (anche "v1.2.3-4-gabc" delle build intermedie) come [1 2 3].
func parseVersion(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return nil
	}
	var out []int
	for _, p := range strings.Split(s, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// newerVersion: a è più recente di b? Una build intermedia "v1.2.3-4-g…" conta come v1.2.3.
func newerVersion(a, b string) bool {
	va, vb := parseVersion(a), parseVersion(b)
	if va == nil {
		return false
	}
	if vb == nil {
		return true
	}
	for i := 0; i < max(len(va), len(vb)); i++ {
		x, y := 0, 0
		if i < len(va) {
			x = va[i]
		}
		if i < len(vb) {
			y = vb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}
