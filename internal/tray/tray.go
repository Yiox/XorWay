// Package tray affiche l'état des fenêtres dans une icône StatusNotifierItem.
package tray

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"fyne.io/systray"

	"xorway/internal/backend"
)

const maxLabel = 70 // longueur maximale (en runes) d'une entrée du menu

// entry est une ligne du menu ; une entrée vide est un séparateur.
type entry struct {
	title    string
	tooltip  string
	disabled bool
}

var separator = entry{}

// Tray pilote l'icône et le menu. Ses méthodes doivent être appelées après
// le démarrage de systray (callback onReady).
type Tray struct {
	backendName string
	lastState   iconState
	lastMenu    []entry
	lastTooltip string
}

// New crée le tray. backendName est affiché en bas du menu.
func New(backendName string) *Tray {
	return &Tray{backendName: backendName, lastState: -1}
}

// Init pose l'icône et un menu d'attente.
func (t *Tray) Init() {
	systray.SetTitle("XorWay")
	t.setIcon(stateNative)
	t.setTooltip("XorWay — chargement…")
	t.setMenu([]entry{{title: "Chargement…", disabled: true}})
}

// Update reflète un nouvel état (ou une erreur du backend).
func (t *Tray) Update(snap backend.Snapshot, err error) {
	if errors.Is(err, backend.ErrPending) {
		return // l'état « Chargement… » posé par Init reste affiché
	}
	if err != nil {
		t.setIcon(stateError)
		t.setTooltip("XorWay — " + err.Error())
		t.setMenu([]entry{{title: "⚠ " + err.Error(), disabled: true}})
		return
	}

	nX := snap.Count(backend.XWayland)
	nW := snap.Count(backend.Wayland)

	if nX > 0 {
		t.setIcon(stateXWayland)
	} else {
		t.setIcon(stateNative)
	}

	if snap.NativeKnown {
		t.setTooltip(fmt.Sprintf("XorWay — %d XWayland, %d Wayland natif", nX, nW))
	} else {
		t.setTooltip(fmt.Sprintf("XorWay — %d fenêtre(s) XWayland", nX))
	}

	var menu []entry
	if snap.NativeKnown {
		menu = append(menu, entry{title: fmt.Sprintf("Wayland natif (%d)", nW), disabled: true})
		menu = append(menu, windowEntries(snap.Windows, backend.Wayland)...)
		menu = append(menu, separator)
	}
	menu = append(menu, entry{title: fmt.Sprintf("XWayland (%d)", nX), disabled: true})
	menu = append(menu, windowEntries(snap.Windows, backend.XWayland)...)
	if snap.Notice != "" {
		menu = append(menu, separator, entry{title: "ℹ " + snap.Notice, disabled: true})
	}
	t.setMenu(menu)
}

func windowEntries(ws []backend.Window, typ backend.ClientType) []entry {
	var sel []backend.Window
	for _, w := range ws {
		if w.Type == typ {
			sel = append(sel, w)
		}
	}
	slices.SortFunc(sel, func(a, b backend.Window) int {
		return cmp.Or(
			cmp.Compare(strings.ToLower(a.AppID), strings.ToLower(b.AppID)),
			cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)),
		)
	})

	if len(sel) == 0 {
		return []entry{{title: "    (aucune)", disabled: true}}
	}
	out := make([]entry, 0, len(sel))
	for _, w := range sel {
		title := w.Title
		if title == "" {
			title = "(sans titre)"
		}
		label := title
		if w.AppID != "" {
			label = title + " — " + w.AppID
		}
		tip := fmt.Sprintf("%s\napp : %s\nPID : %d", title, w.AppID, w.PID)
		out = append(out, entry{title: "    " + truncate(label, maxLabel), tooltip: tip})
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// escape protège les « _ », interprétés comme mnémoniques par dbusmenu.
func escape(s string) string { return strings.ReplaceAll(s, "_", "__") }

func (t *Tray) setIcon(st iconState) {
	if st == t.lastState {
		return
	}
	t.lastState = st
	systray.SetIcon(icons[st])
}

func (t *Tray) setTooltip(s string) {
	if s == t.lastTooltip {
		return
	}
	t.lastTooltip = s
	systray.SetTooltip(s)
}

// setMenu reconstruit le menu complet si son contenu a changé.
func (t *Tray) setMenu(menu []entry) {
	menu = append(menu,
		separator,
		entry{title: "Backend : " + t.backendName, disabled: true},
		entry{title: "Quitter"},
	)
	if slices.Equal(menu, t.lastMenu) {
		return
	}
	t.lastMenu = menu

	systray.ResetMenu()
	for i, e := range menu {
		if e == separator {
			systray.AddSeparator()
			continue
		}
		item := systray.AddMenuItem(escape(e.title), e.tooltip)
		if e.disabled {
			item.Disable()
		}
		if i == len(menu)-1 { // « Quitter »
			// Le canal est fermé par ResetMenu : ok vaut alors false.
			go func(ch <-chan struct{}) {
				if _, ok := <-ch; ok {
					systray.Quit()
				}
			}(item.ClickedCh)
		}
	}
}
