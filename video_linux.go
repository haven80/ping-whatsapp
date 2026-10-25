//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Su Linux i video (le "GIF" di WhatsApp sono MP4) passano da ffmpeg, se è installato.

type videoInfo struct {
	first   image.Image
	seconds uint32
	w, h    int
}

var errNoFFmpeg = errors.New("per le GIF animate serve ffmpeg")

type probeResult struct {
	w, h int
	fps  float64
	dur  float64
}

func ffprobe(path string) (probeResult, error) {
	var r probeResult
	if !hasCmd("ffprobe") {
		return r, errNoFFmpeg
	}
	ctx, cancel := withTimeout(10 * time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=width,height,avg_frame_rate,r_frame_rate:format=duration",
		"-of", "default=noprint_wrappers=1", path).Output()
	if err != nil {
		return r, err
	}
	for _, l := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if !ok {
			continue
		}
		switch k {
		case "width":
			r.w, _ = strconv.Atoi(v)
		case "height":
			r.h, _ = strconv.Atoi(v)
		case "avg_frame_rate", "r_frame_rate":
			if r.fps == 0 {
				if a, b, ok := strings.Cut(v, "/"); ok {
					n, _ := strconv.ParseFloat(a, 64)
					d, _ := strconv.ParseFloat(b, 64)
					if d > 0 && n > 0 {
						r.fps = n / d
					}
				}
			}
		case "duration":
			r.dur, _ = strconv.ParseFloat(v, 64)
		}
	}
	if r.w <= 0 || r.h <= 0 {
		return r, errors.New("video non leggibile")
	}
	return r, nil
}

// decodeVideoFrames estrae i fotogrammi (lato lungo al massimo maxPx, 0 = originale).
func decodeVideoFrames(path string, maxPx, maxFrames int) ([]image.Image, []time.Duration, error) {
	if !hasCmd("ffmpeg") {
		return nil, nil, errNoFFmpeg
	}
	pr, err := ffprobe(path)
	if err != nil {
		return nil, nil, err
	}
	w, h := pr.w, pr.h
	if maxPx > 0 && (w > maxPx || h > maxPx) {
		if w >= h {
			w, h = maxPx, max(1, maxPx*h/w)
		} else {
			w, h = max(1, maxPx*w/h), maxPx
		}
	}
	fps := pr.fps
	if fps <= 0 || fps > 25 {
		fps = 25
	}
	ctx, cancel := withTimeout(60 * time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path,
		"-vf", fmt.Sprintf("fps=%g,scale=%d:%d:flags=bilinear", fps, w, h),
		"-frames:v", strconv.Itoa(max(1, maxFrames)), "-f", "rawvideo", "-pix_fmt", "rgba", "-")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	var frames []image.Image
	var delays []time.Duration
	step := time.Duration(float64(time.Second) / fps)
	for {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		if _, err := io.ReadFull(out, img.Pix); err != nil {
			break
		}
		frames = append(frames, img)
		delays = append(delays, step)
	}
	cmd.Wait()
	if len(frames) == 0 {
		return nil, nil, errors.New("nessun fotogramma")
	}
	return frames, delays, nil
}

func probeVideo(path string) (vi videoInfo) {
	pr, err := ffprobe(path)
	if err != nil {
		return
	}
	vi.w, vi.h, vi.seconds = pr.w, pr.h, uint32(pr.dur+0.5)
	ctx, cancel := withTimeout(10 * time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-v", "error", "-i", path, "-frames:v", "1",
		"-f", "image2pipe", "-vcodec", "png", "-").Output(); err == nil {
		vi.first, _ = png.Decode(bytes.NewReader(out))
	}
	return
}

// gifToMP4 converte una GIF in MP4 H.264 (come fa WhatsApp). Prova gli encoder H.264
// disponibili: Fedora/Bazzite possono avere libx264 oppure solo libopenh264.
func gifToMP4(in, out string) error {
	if !hasCmd("ffmpeg") {
		return errNoFFmpeg
	}
	var last error
	for _, enc := range [][]string{
		{"-c:v", "libx264", "-preset", "veryfast", "-crf", "23"},
		{"-c:v", "libopenh264", "-b:v", "1M"},
		{"-c:v", "h264"},
	} {
		args := []string{"-y", "-v", "error", "-i", in, "-movflags", "+faststart", "-pix_fmt", "yuv420p",
			"-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2", "-an"}
		args = append(args, enc...)
		args = append(args, out)
		ctx, cancel := withTimeout(2 * time.Minute)
		msg, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
		cancel()
		if err == nil {
			return nil
		}
		last = fmt.Errorf("%v: %s", err, strings.TrimSpace(string(msg)))
	}
	return last
}
