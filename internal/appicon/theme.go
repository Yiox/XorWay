package appicon

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// subdir est un répertoire d'icônes déclaré dans index.theme.
type subdir struct {
	path               string
	size, min, max, th int
	typ                string // Fixed, Scalable ou Threshold
}

// distance suit DirectorySizeDistance de la spécification Icon Theme.
func (d subdir) distance(size int) int {
	switch d.typ {
	case "Fixed":
		return abs(size - d.size)
	case "Scalable":
		if size < d.min {
			return d.min - size
		}
		if size > d.max {
			return size - d.max
		}
		return 0
	default: // Threshold
		if size < d.size-d.th {
			return d.size - d.th - size
		}
		if size > d.size+d.th {
			return size - (d.size + d.th)
		}
		return 0
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// theme est un thème d'icônes, éventuellement réparti sur plusieurs bases.
type theme struct {
	name     string
	roots    []string
	dirs     []subdir
	inherits []string
}

// loadTheme lit index.theme ; renvoie nil si le thème n'existe pas.
func loadTheme(name string, bases []string) *theme {
	t := &theme{name: name}
	var index string
	for _, b := range bases {
		root := filepath.Join(b, name)
		if st, err := os.Stat(root); err == nil && st.IsDir() {
			t.roots = append(t.roots, root)
			if index == "" {
				if _, err := os.Stat(filepath.Join(root, "index.theme")); err == nil {
					index = filepath.Join(root, "index.theme")
				}
			}
		}
	}
	if index == "" {
		return nil
	}

	sections, err := parseINI(index)
	if err != nil {
		return nil
	}
	head := sections["Icon Theme"]
	for _, n := range splitList(head["Inherits"]) {
		t.inherits = append(t.inherits, n)
	}
	for _, p := range append(splitList(head["Directories"]), splitList(head["ScaledDirectories"])...) {
		s := sections[p]
		if s == nil || atoi(s["Scale"], 1) != 1 {
			continue // on vise une échelle 1, l'image est ensuite redimensionnée
		}
		size := atoi(s["Size"], 0)
		t.dirs = append(t.dirs, subdir{
			path: p,
			size: size,
			min:  atoi(s["MinSize"], size),
			max:  atoi(s["MaxSize"], size),
			th:   atoi(s["Threshold"], 2),
			typ:  s["Type"],
		})
	}
	return t
}

// find renvoie les fichiers candidats pour une icône, du plus proche de la
// taille demandée au plus éloigné.
func (t *theme) find(name string, size int) []string {
	type cand struct {
		path string
		dist int
	}
	var cs []cand
	for _, d := range t.dirs {
		for _, root := range t.roots {
			for _, ext := range []string{".png", ".svg"} {
				p := filepath.Join(root, d.path, name+ext)
				if _, err := os.Stat(p); err == nil {
					cs = append(cs, cand{p, d.distance(size)})
				}
			}
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].dist < cs[j].dist })
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.path
	}
	return out
}

// themeChain charge un thème, ses parents (Inherits) puis hicolor.
func themeChain(name string, bases []string) []*theme {
	var chain []*theme
	seen := map[string]bool{}
	var walk func(string)
	walk = func(n string) {
		if n == "" || seen[n] || n == "hicolor" {
			return
		}
		seen[n] = true
		t := loadTheme(n, bases)
		if t == nil {
			return
		}
		chain = append(chain, t)
		for _, p := range t.inherits {
			walk(p)
		}
	}
	walk(name)
	if t := loadTheme("hicolor", bases); t != nil {
		chain = append(chain, t)
	}
	return chain
}

// currentThemeName devine le thème d'icônes de l'utilisateur.
func currentThemeName() string {
	if n := os.Getenv("XORWAY_ICON_THEME"); n != "" {
		return n
	}
	desktop := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
	if strings.Contains(desktop, "KDE") {
		cfg, err := os.UserConfigDir()
		if err == nil {
			if s, err := parseINI(filepath.Join(cfg, "kdeglobals")); err == nil {
				if n := s["Icons"]["Theme"]; n != "" {
					return n
				}
			}
		}
		return "breeze" // valeur par défaut de Plasma
	}
	if strings.Contains(desktop, "GNOME") {
		out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "icon-theme").Output()
		if err == nil {
			if n := strings.Trim(strings.TrimSpace(string(out)), "'"); n != "" {
				return n
			}
		}
		return "Adwaita"
	}
	return "hicolor"
}

// parseINI lit un fichier de type .desktop / index.theme.
func parseINI(path string) (map[string]map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]map[string]string{}
	var cur map[string]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			cur = map[string]string{}
			out[line[1:len(line)-1]] = cur
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && cur != nil {
			cur[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out, sc.Err()
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
