// SPDX-License-Identifier: AGPL-3.0-or-later

// Package game guesses whether a window belongs to a video game.
//
// Clues, from most to least reliable:
//   - window class steam_app_<id> (Steam games, Proton in particular);
//   - SteamAppId / SteamGameId in the process environment;
//   - Wine/Proton process (wine* executable, or a command ending in .exe);
//   - "Game" category in the application's .desktop file.
package game

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"xorway/internal/desktop"
	"xorway/internal/i18n"
)

// Source tells how a game was recognized.
type Source int

const (
	None    Source = iota
	Steam          // native Linux Steam game
	Proton         // Steam game through Proton
	Wine           // Windows application through Wine (Lutris, Heroic, Bottles…)
	Catalog        // Game category of the .desktop file
)

// Key returns a stable, untranslated identifier (for logs).
func (s Source) Key() string {
	switch s {
	case Steam:
		return "steam"
	case Proton:
		return "proton"
	case Wine:
		return "wine"
	case Catalog:
		return "catalog"
	default:
		return "none"
	}
}

// String returns the translated name of the source.
func (s Source) String() string {
	switch s {
	case Steam:
		return "Steam"
	case Proton:
		return "Steam (Proton)"
	case Wine:
		return "Wine"
	case Catalog:
		return i18n.T("game.source.catalog")
	default:
		return ""
	}
}

// Hint suggests a way to run the game as a native Wayland client.
func (s Source) Hint() string {
	switch s {
	case Proton:
		return i18n.T("game.hint.proton")
	case Wine:
		return i18n.T("game.hint.wine")
	case Steam, Catalog:
		return i18n.T("game.hint.sdl")
	default:
		return ""
	}
}

// Steam clients and launchers that are not games.
var notGames = map[string]bool{
	"steam":                       true,
	"steamwebhelper":              true,
	"lutris":                      true,
	"heroic":                      true,
	"net.lutris.lutris":           true,
	"com.heroicgameslauncher.hgl": true,
}

type cacheKey struct {
	pid   int
	appID string
}

// Detector caches detection results by (PID, app id).
type Detector struct {
	idx *desktop.Index

	mu    sync.Mutex
	cache map[cacheKey]Source
}

// NewDetector creates a detector backed by the .desktop index.
func NewDetector(idx *desktop.Index) *Detector {
	return &Detector{idx: idx, cache: map[cacheKey]Source{}}
}

// Detect returns how the game was detected, or None if it is not a game.
func (d *Detector) Detect(appID string, pid int) Source {
	k := cacheKey{pid, appID}
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.cache[k]; ok {
		return s
	}
	s := d.detect(appID, pid)
	d.cache[k] = s
	return s
}

// Prune forgets entries rejected by keep (reused PIDs, etc.).
func (d *Detector) Prune(keep func(pid int, appID string) bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for k := range d.cache {
		if !keep(k.pid, k.appID) {
			delete(d.cache, k)
		}
	}
}

func (d *Detector) detect(appID string, pid int) Source {
	lower := strings.ToLower(appID)
	if notGames[lower] {
		return None
	}
	env := environ(pid)
	wine := isWine(pid)

	if strings.HasPrefix(lower, "steam_app_") {
		if wine || env["STEAM_COMPAT_DATA_PATH"] != "" {
			return Proton
		}
		return Steam
	}
	if steamID(env) {
		if wine || env["STEAM_COMPAT_DATA_PATH"] != "" {
			return Proton
		}
		return Steam
	}
	if wine {
		return Wine
	}
	if e := d.idx.Lookup(appID); e != nil && e.HasCategory("Game") {
		return Catalog
	}
	return None
}

// steamID reports whether Steam launched the process for a game.
func steamID(env map[string]string) bool {
	for _, k := range []string{"SteamAppId", "SteamGameId"} {
		if v := env[k]; v != "" && v != "0" {
			return true
		}
	}
	return false
}

// environ reads /proc/<pid>/environ (readable for the user's own processes).
func environ(pid int) map[string]string {
	env := map[string]string{}
	if pid <= 0 {
		return env
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return env
	}
	for _, kv := range bytes.Split(data, []byte{0}) {
		if k, v, ok := strings.Cut(string(kv), "="); ok {
			env[k] = v
		}
	}
	return env
}

// isWine recognizes a Wine process: wine*/…-preloader executable, or a first
// argument ending in .exe.
func isWine(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	if exe, err := os.Readlink(filepath.Join(proc, "exe")); err == nil {
		base := filepath.Base(exe)
		if strings.HasPrefix(base, "wine") || strings.HasSuffix(base, "-preloader") {
			return true
		}
	}
	if cmd, err := os.ReadFile(filepath.Join(proc, "cmdline")); err == nil {
		arg0, _, _ := bytes.Cut(cmd, []byte{0})
		if strings.HasSuffix(strings.ToLower(string(arg0)), ".exe") {
			return true
		}
	}
	return false
}
