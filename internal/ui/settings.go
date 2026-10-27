// Package ui serves the control panel: a one-page web app that starts and
// stops the bridge, shows its state and lets the user change settings.
package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Settings is what serve needs to know, as the panel and the config file see it.
type Settings struct {
	Port   string `json:"port"`   // serial port path; empty means auto-detect
	Mount  string `json:"mount"`  // mount profile name
	Baud   int    `json:"baud"`   // 0 means the profile's default
	Listen string `json:"listen"` // TCP address SkySafari connects to
	Verify bool   `json:"verify"` // probe the mount after every open
}

// DefaultSettings are used when nothing is saved and no flags are given.
var DefaultSettings = Settings{Mount: "celestron-nexstar", Listen: "127.0.0.1:4030"}

// ConfigPath is where settings are saved: the user's config directory.
func ConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skybridge", "settings.json"), nil
}

// Load reads settings from path. A missing file yields DefaultSettings.
func Load(path string) (Settings, error) {
	s := DefaultSettings
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return DefaultSettings, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Save writes settings to path, creating the directory if needed.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
