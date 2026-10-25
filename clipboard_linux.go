//go:build linux

package main

import (
	"bytes"
	"image"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "golang.org/x/image/bmp"
)

type ClipboardContent struct {
	Image image.Image
	Files []string
	Text  string // su Linux resta vuoto: il testo lo incolla Gio
}

// ReadClipboard legge immagini e file copiati, con wl-paste (Wayland) o xclip (X11).
// Se mancano entrambi restituisce un contenuto vuoto e si incolla solo il testo.
func ReadClipboard(owner uintptr) (ClipboardContent, error) {
	var c ClipboardContent
	types := clipTypes()
	has := func(t string) bool {
		for _, x := range types {
			if x == t {
				return true
			}
		}
		return false
	}
	if has("text/uri-list") {
		for _, l := range strings.Split(string(clipGet("text/uri-list")), "\n") {
			l = strings.TrimSpace(l)
			if u, err := url.Parse(l); err == nil && u.Scheme == "file" {
				if st, err := os.Stat(u.Path); err == nil && !st.IsDir() {
					c.Files = append(c.Files, u.Path)
				}
			}
		}
		if len(c.Files) > 0 {
			return c, nil
		}
	}
	for _, t := range []string{"image/png", "image/jpeg", "image/bmp", "image/gif", "image/webp"} {
		if has(t) {
			if img, _, err := image.Decode(bytes.NewReader(clipGet(t))); err == nil {
				c.Image = img
				return c, nil
			}
		}
	}
	return c, nil
}

func clipTool() string {
	if os.Getenv("WAYLAND_DISPLAY") != "" && hasCmd("wl-paste") {
		return "wl-paste"
	}
	if os.Getenv("DISPLAY") != "" && hasCmd("xclip") {
		return "xclip"
	}
	return ""
}

func clipTypes() []string {
	var out []byte
	switch clipTool() {
	case "wl-paste":
		out = runQuick("wl-paste", "--list-types")
	case "xclip":
		out = runQuick("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
	}
	return strings.Fields(string(out))
}

func clipGet(t string) []byte {
	switch clipTool() {
	case "wl-paste":
		return runQuick("wl-paste", "--no-newline", "--type", t)
	case "xclip":
		return runQuick("xclip", "-selection", "clipboard", "-t", t, "-o")
	}
	return nil
}

func runQuick(name string, args ...string) []byte {
	ctx, cancel := withTimeout(2 * time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, name, args...).Output()
	return out
}
