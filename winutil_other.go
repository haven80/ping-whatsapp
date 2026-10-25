//go:build !windows

package main

import "os/exec"

func SingleInstance() bool                                { return true }
func StartTray(onOpen, onQuit, onClick func())            {}
func RemoveTray()                                         {}
func Notify(title, text string)                           {}
func SetTrayTip(s string)                                 {}
func FlashWindow(hwnd uintptr)                            {}
func ShowHWND(hwnd uintptr)                               {}
func HideHWND(hwnd uintptr)                               {}
func IsVisibleHWND(hwnd uintptr) bool                     { return true }
func SetDarkTitleBar(hwnd uintptr, dark bool)             {}
func SystemDarkMode() bool                                { return false }
func PlayChime()                                          {}
func PickFile(owner uintptr, title, filter string) string { return "" }
func openPath(path string)                                { exec.Command("xdg-open", path).Start() }

func GetPlace(hwnd uintptr) (WinPlace, bool) { return WinPlace{}, false }
func SetPlace(hwnd uintptr, p WinPlace)      {}
