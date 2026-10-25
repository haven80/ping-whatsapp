//go:build !windows

package main

func EnableFileDrop(hwnd uintptr, fn func([]string)) {}

func WatchPlace(hwnd uintptr, fn func(WinPlace)) {}
