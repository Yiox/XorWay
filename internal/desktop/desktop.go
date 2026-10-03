// SPDX-License-Identifier: AGPL-3.0-or-later

// Package desktop indexes .desktop files (freedesktop Desktop Entry
// specification) to find the name, icon and categories of an application
// from its app id or window class.
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

// rescanDelay limits how often a failed lookup triggers a new scan.
const rescanDelay = 30 * time.Second

// Entry is the useful subset of a .desktop file.
type Entry struct {
	ID             string // desktop file ID, e.g. org.kde.dolphin
	Name           string
	Names          map[string]string // localized names: Name[fr], Name[pt_BR]…
	Icon           string            // theme icon name or absolute path
	Categories     []string
	StartupWMClass string
}

// LocalName returns the name in the given language ("fr", "pt_BR"…), falling
// back to the untranslated name.
func (e *Entry) LocalName(lang string) string {
	lang = strings.ReplaceAll(lang, "-", "_")
	if n := e.Names[lang]; n != "" {
		return n
	}
	if base, _, ok := strings.Cut(lang, "_"); ok {
		if n := e.Names[base]; n != "" {
			return n
		}
	}
	return e.Name
}

// HasCategory reports whether the entry belongs to the given category.
func (e *Entry) HasCategory(c string) bool {
	for _, x := range e.Categories {
		if strings.EqualFold(x, c) {
			return true
		}
	}
	return false
}

// Index is a lazy index, safe for concurrent use.
type Index struct {
	mu       sync.Mutex
	scanned  time.Time
	byID     map[string]*Entry // key: lowercase ID
	byClass  map[string]*Entry // key: lowercase StartupWMClass
	bySuffix map[string]*Entry // key: last component of the ID (org.kde.dolphin → dolphin)
}

// NewIndex creates an empty index; the first Lookup triggers the scan.
func NewIndex() *Index { return &Index{} }

// Lookup finds the entry matching an app id or a WM_CLASS class.
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
	// Application installed since the last scan?
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

// DataDirs returns $XDG_DATA_HOME then $XDG_DATA_DIRS, by priority.
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
			// The ID of a file in a subdirectory replaces "/" with "-".
			rel, _ := filepath.Rel(root, path)
			id := strings.ReplaceAll(strings.TrimSuffix(rel, ".desktop"), "/", "-")
			key := strings.ToLower(id)
			if _, seen := x.byID[key]; seen {
				return nil // a higher-priority directory already provided it
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

// parse reads the [Desktop Entry] group. It returns nil for hidden entries
// (Hidden=true) and for entries that are not applications.
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
				break // only the first group matters
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
		default:
			if lang, ok := strings.CutPrefix(strings.TrimSpace(k), "Name["); ok {
				if e.Names == nil {
					e.Names = map[string]string{}
				}
				e.Names[strings.TrimSuffix(lang, "]")] = v
			}
		}
	}
	if typ != "Application" {
		return nil, nil
	}
	return e, sc.Err()
}
