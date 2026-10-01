// Package config persiste les préférences de l'utilisateur dans
// $XDG_CONFIG_HOME/xorway/config.json.
package config

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config regroupe les préférences modifiables depuis le menu.
type Config struct {
	NotifyGames bool `json:"notifyGames"`
}

// Default est la configuration initiale.
var Default = Config{NotifyGames: true}

func path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "xorway", "config.json"), nil
}

// Load lit la configuration ; un fichier absent donne Default.
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

// Save écrit la configuration.
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
