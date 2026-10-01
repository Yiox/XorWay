// Package game devine si une fenêtre appartient à un jeu vidéo.
//
// Les indices utilisés, du plus au moins fiable :
//   - classe de fenêtre steam_app_<id> (jeux Steam, en particulier Proton) ;
//   - variable SteamAppId / SteamGameId dans l'environnement du processus ;
//   - processus Wine/Proton (exécutable wine*, ou commande en .exe) ;
//   - catégorie « Game » dans le fichier .desktop de l'application.
package game

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"xorway/internal/desktop"
)

// Source indique comment un jeu a été reconnu.
type Source int

const (
	None    Source = iota
	Steam          // jeu Steam natif Linux
	Proton         // jeu Steam via Proton
	Wine           // application Windows via Wine (Lutris, Heroic, Bottles…)
	Catalog        // catégorie Game du fichier .desktop
)

func (s Source) String() string {
	switch s {
	case Steam:
		return "Steam"
	case Proton:
		return "Steam (Proton)"
	case Wine:
		return "Wine"
	case Catalog:
		return "catégorie Game"
	default:
		return ""
	}
}

// Hint propose un moyen de lancer le jeu en Wayland natif.
func (s Source) Hint() string {
	switch s {
	case Proton:
		return "Proton ≥ 10 : ajoutez « PROTON_ENABLE_WAYLAND=1 %command% » aux options de lancement Steam."
	case Wine:
		return "Wine ≥ 9 peut utiliser son pilote Wayland (winewayland) : vérifiez la configuration du lanceur."
	case Steam, Catalog:
		return "Si le jeu utilise SDL, essayez SDL_VIDEODRIVER=wayland."
	default:
		return ""
	}
}

// Clients Steam et utilitaires qui ne sont pas des jeux.
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

// Detector met en cache la détection par (PID, app id).
type Detector struct {
	idx *desktop.Index

	mu    sync.Mutex
	cache map[cacheKey]Source
}

// NewDetector crée un détecteur s'appuyant sur l'index .desktop.
func NewDetector(idx *desktop.Index) *Detector {
	return &Detector{idx: idx, cache: map[cacheKey]Source{}}
}

// Detect renvoie la source de détection, ou None si ce n'est pas un jeu.
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

// Prune oublie les entrées absentes de keep (PID réutilisés, etc.).
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

// steamID indique si le processus a été lancé par Steam pour un jeu.
func steamID(env map[string]string) bool {
	for _, k := range []string{"SteamAppId", "SteamGameId"} {
		if v := env[k]; v != "" && v != "0" {
			return true
		}
	}
	return false
}

// environ lit /proc/<pid>/environ (lisible pour les processus de l'utilisateur).
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

// isWine reconnaît un processus Wine : exécutable wine*/…-preloader, ou
// premier argument se terminant par .exe.
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
