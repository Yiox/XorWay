// SPDX-License-Identifier: AGPL-3.0-or-later

// Package desktop indexe les fichiers .desktop (spécification freedesktop
// Desktop Entry) pour retrouver le nom, l'icône et les catégories d'une
// application à partir de son app id ou de sa classe de fenêtre.
package desktop

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// rescanDelay limite la fréquence des réindexations déclenchées par un échec.
const rescanDelay = 30 * time.Second

// Entry est le sous-ensemble utile d'un fichier .desktop.
type Entry struct {
	ID             string // identifiant de fichier, ex. org.kde.dolphin
	Name           string
	Icon           string // nom d'icône du thème ou chemin absolu
	Categories     []string
	StartupWMClass string
}

// HasCategory indique si l'entrée appartient à la catégorie donnée.
func (e *Entry) HasCategory(c string) bool {
	for _, x := range e.Categories {
		if strings.EqualFold(x, c) {
			return true
		}
	}
	return false
}

// Index est un index paresseux, sûr pour un usage concurrent.
type Index struct {
	mu       sync.Mutex
	scanned  time.Time
	byID     map[string]*Entry // clé : ID en minuscules
	byClass  map[string]*Entry // clé : StartupWMClass en minuscules
	bySuffix map[string]*Entry // clé : dernier composant de l'ID (org.kde.dolphin → dolphin)
}

// NewIndex crée un index vide ; le premier Lookup déclenche le scan.
func NewIndex() *Index { return &Index{} }

// Lookup cherche l'entrée correspondant à un app id ou une classe WM_CLASS.
func (x *Index) Lookup(appID string) *Entry {
	if appID == "" {
		return nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()

	if x.byID == nil {
		x.scan()
	}
	if e := x.find(appID); e != nil {
		return e
	}
	// Application installée depuis le dernier scan ?
	if time.Since(x.scanned) > rescanDelay {
		x.scan()
		return x.find(appID)
	}
	return nil
}

func (x *Index) find(appID string) *Entry {
	k := strings.ToLower(strings.TrimSuffix(appID, ".desktop"))
	if e := x.byID[k]; e != nil {
		return e
	}
	if e := x.byClass[k]; e != nil {
		return e
	}
	return x.bySuffix[k]
}

// DataDirs renvoie $XDG_DATA_HOME puis $XDG_DATA_DIRS, par ordre de priorité.
func DataDirs() []string {
	home := os.Getenv("XDG_DATA_HOME")
	if home == "" {
		if h, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(h, ".local", "share")
		}
	}
	dirs := []string{home}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(sys, ":") {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

func (x *Index) scan() {
	x.scanned = time.Now()
	x.byID = map[string]*Entry{}
	x.byClass = map[string]*Entry{}
	x.bySuffix = map[string]*Entry{}

	for _, dir := range DataDirs() {
		root := filepath.Join(dir, "applications")
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			// L'ID d'un fichier en sous-dossier remplace « / » par « - ».
			rel, _ := filepath.Rel(root, path)
			id := strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), "/", "-")
			key := strings.ToLower(id)
			if _, seen := x.byID[key]; seen {
				return nil // un répertoire prioritaire l'a déjà fourni
			}
			e, err := parse(path)
			if err != nil || e == nil {
				return nil
			}
			e.ID = id
			x.byID[key] = e
			if e.StartupWMClass != "" {
				setIfAbsent(x.byClass, strings.ToLower(e.StartupWMClass), e)
			}
			if i := strings.LastIndexByte(key, '.'); i >= 0 {
				setIfAbsent(x.bySuffix, key[i+1:], e)
			}
			return nil
		})
	}
}

func setIfAbsent(m map[string]*Entry, k string, e *Entry) {
	if _, ok := m[k]; !ok {
		m[k] = e
	}
}

// parse lit le groupe [Desktop Entry]. Renvoie nil pour les entrées
// masquées (Hidden=true) ou qui ne sont pas des applications.
func parse(path string) (*Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	e := &Entry{}
	inGroup := false
	typ := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if inGroup {
				break // seul le premier groupe nous intéresse
			}
			inGroup = line == "[Desktop Entry]"
			continue
		}
		if !inGroup {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Type":
			typ = v
		case "Name":
			e.Name = v
		case "Icon":
			e.Icon = v
		case "StartupWMClass":
			e.StartupWMClass = v
		case "Categories":
			for _, c := range strings.Split(v, ";") {
				if c != "" {
					e.Categories = append(e.Categories, c)
				}
			}
		case "Hidden":
			if v == "true" {
				return nil, nil
			}
		}
	}
	if typ != "Application" {
		return nil, nil
	}
	return e, sc.Err()
}
