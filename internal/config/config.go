// SPDX-License-Identifier: AGPL-3.0-or-later

// Package config persists user preferences in
// $XDG_CONFIG_HOME/xorway/config.json.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config holds the user preferences.
type Config struct {
	NotifyGames bool `json:"notifyGames"`
	// Language is a language code ("en", "fr"…); empty means automatic
	// detection from the environment.
	Language string `json:"language,omitempty"`
}

// Default is the initial configuration.
var Default = Config{NotifyGames: true}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "xorway", "config.json"), nil
}

// Load reads the configuration; a missing file yields Default.
func Load() (Config, error) {
	c := Default
	p, err := path()
	if err != nil {
		return c, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(data, &c)
	return c, err
}

// Save writes the configuration.
func Save(c Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(data, '\n'), 0o644)
}
