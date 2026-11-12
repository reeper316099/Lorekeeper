package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Window size limits and defaults (same as the Electron build).
const (
	MinWidth, MinHeight         = 1000, 680
	DefaultWidth, DefaultHeight = 1320, 840
)

// WindowState is the remembered geometry (window-state.json in the user-data folder, the same
// file and keys as the Electron build).
type WindowState struct {
	X, Y        int
	HasPosition bool
	Width       int
	Height      int
	Maximized   bool
}

type stateFile struct {
	X         *float64 `json:"x,omitempty"`
	Y         *float64 `json:"y,omitempty"`
	Width     float64  `json:"width"`
	Height    float64  `json:"height"`
	Maximized bool     `json:"maximized"`
}

// LoadWindowState reads the saved geometry. visible tells whether a window at that position is
// still on a screen; a position that is not (a monitor was unplugged) is dropped so the OS places the window.
func LoadWindowState(userData string, visible func(x, y, w, h int) bool) WindowState {
	st := WindowState{Width: DefaultWidth, Height: DefaultHeight}
	b, err := os.ReadFile(filepath.Join(userData, "window-state.json"))
	if err != nil {
		return st
	}
	var f stateFile
	if json.Unmarshal(b, &f) != nil {
		return st
	}
	if w := int(f.Width); w > 0 {
		st.Width = max(MinWidth, w)
	}
	if h := int(f.Height); h > 0 {
		st.Height = max(MinHeight, h)
	}
	st.Maximized = f.Maximized
	if f.X != nil && f.Y != nil && (visible == nil || visible(int(*f.X), int(*f.Y), st.Width, st.Height)) {
		st.X, st.Y, st.HasPosition = int(*f.X), int(*f.Y), true
	}
	return st
}

// SaveWindowState writes the geometry. While maximized only the flag changes, so the restored size
// and position survive.
func SaveWindowState(userData string, st WindowState) error {
	file := filepath.Join(userData, "window-state.json")
	f := stateFile{Width: float64(st.Width), Height: float64(st.Height), Maximized: st.Maximized}
	x, y := float64(st.X), float64(st.Y)
	if st.HasPosition {
		f.X, f.Y = &x, &y
	}
	if st.Maximized {
		if b, err := os.ReadFile(file); err == nil {
			var old stateFile
			if json.Unmarshal(b, &old) == nil {
				f.X, f.Y, f.Width, f.Height = old.X, old.Y, old.Width, old.Height
			}
		}
	}
	if err := os.MkdirAll(userData, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(file, b, 0o644)
}
