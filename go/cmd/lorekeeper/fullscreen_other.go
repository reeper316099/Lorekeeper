//go:build desktop && !darwin

package main

// Windows and Linux windows can already go full screen.
func enableFullscreen()       {}
func fullscreenCapable() bool { return true }
