//go:build !windows && !linux

package main

import (
	"errors"
	"image"
	"time"
)

type videoInfo struct {
	first   image.Image
	seconds uint32
	w, h    int
}

func decodeVideoFrames(path string, maxPx, maxFrames int) ([]image.Image, []time.Duration, error) {
	return nil, nil, errors.New("video non supportati su questo sistema")
}

func probeVideo(path string) videoInfo { return videoInfo{} }

func gifToMP4(in, out string) error {
	return errors.New("conversione non supportata su questo sistema")
}
