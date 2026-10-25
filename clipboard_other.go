//go:build !windows

package main

import "image"

type ClipboardContent struct {
	Image image.Image
	Files []string
	Text  string
}

func ReadClipboard(owner uintptr) (ClipboardContent, error) { return ClipboardContent{}, nil }
