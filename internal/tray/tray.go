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
	"xorway/internal/game"
)

const (
	maxLabel = 70 // longueur maximale (en runes) d'une entrée du menu
	IconSize = 32 // taille des icônes d'applications dans le menu
)

// Actions des entrées cliquables.
const (
	actionNone = iota
	actionQuit
	actionToggleNotify
)

// entry est une ligne du menu ; la valeur zéro est un séparateur.
type entry struct {
	title    string
	tooltip  string
	disabled bool
	icon     string // app id dont on affiche l'icône ("" : aucune)
	action   int
	checkbox bool
	checked  bool
}

var separator = entry{}

// sameShape indique si deux entrées peuvent être mises à jour sur place.
func sameShape(a, b entry) bool {
	return (a == separator) == (b == separator) && a.action == b.action && a.checkbox == b.checkbox
}

// Options configure le tray.
type Options struct {
	BackendName string
	// Icons renvoie l'icône PNG d'une application (nil si introuvable).
	Icons func(appID string) []byte
	// GenericIcon remplace une icône introuvable.
	GenericIcon []byte
	// NotifyGames renvoie l'état de la case « Notifier… » ; nil la masque.
	NotifyGames func() bool
	// OnToggleNotify est appelé quand l'utilisateur coche ou décoche la case.
	OnToggleNotify func()
}

// Tray pilote l'icône et le menu. Init puis Update doivent être appelés
// depuis une même goroutine, après le démarrage de systray.
type Tray struct {
	opts        Options
	lastState   iconState
	lastTooltip string
	lastMenu    []entry
	items       []*systray.MenuItem // nil pour les séparateurs
}

// New crée le tray.
func New(opts Options) *Tray {
	return &Tray{opts: opts, lastState: -1}
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
		return []entry{{title: "(aucune)", disabled: true}}
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
		if w.Game != game.None {
			label = "🎮 " + label
			tip += "\nJeu détecté : " + w.Game.String()
		}
		out = append(out, entry{title: truncate(label, maxLabel), tooltip: tip, icon: w.AppID})
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

// footer ajoute les entrées fixes en bas du menu.
func (t *Tray) footer(menu []entry) []entry {
	menu = append(menu, separator)
	if t.opts.NotifyGames != nil {
		menu = append(menu, entry{
			title:    "Notifier les jeux lancés sous XWayland",
			action:   actionToggleNotify,
			checkbox: true,
			checked:  t.opts.NotifyGames(),
		})
	}
	return append(menu,
		entry{title: "Backend : " + t.opts.BackendName, disabled: true},
		entry{title: "Quitter", action: actionQuit},
	)
}

// setMenu met le menu à jour : sur place si sa structure n'a pas changé
// (cas courant d'un titre qui change), sinon en le reconstruisant.
func (t *Tray) setMenu(menu []entry) {
	menu = t.footer(menu)
	if slices.Equal(menu, t.lastMenu) {
		return
	}
	if len(menu) == len(t.lastMenu) && slices.EqualFunc(menu, t.lastMenu, sameShape) {
		for i, e := range menu {
			if e != t.lastMenu[i] {
				t.updateItem(t.items[i], t.lastMenu[i], e)
			}
		}
		t.lastMenu = menu
		return
	}

	t.lastMenu = menu
	t.items = make([]*systray.MenuItem, len(menu))
	systray.ResetMenu()
	for i, e := range menu {
		if e == separator {
			systray.AddSeparator()
			continue
		}
		var item *systray.MenuItem
		if e.checkbox {
			item = systray.AddMenuItemCheckbox(escape(e.title), e.tooltip, e.checked)
		} else {
			item = systray.AddMenuItem(escape(e.title), e.tooltip)
		}
		if e.disabled {
			item.Disable()
		}
		if e.icon != "" {
			item.SetIcon(t.appIcon(e.icon))
		}
		t.items[i] = item
		t.listen(item, e.action)
	}
}

func (t *Tray) updateItem(item *systray.MenuItem, old, e entry) {
	if e.title != old.title {
		item.SetTitle(escape(e.title))
	}
	if e.tooltip != old.tooltip {
		item.SetTooltip(e.tooltip)
	}
	if e.disabled != old.disabled {
		if e.disabled {
			item.Disable()
		} else {
			item.Enable()
		}
	}
	if e.checked != old.checked {
		if e.checked {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
	if e.icon != old.icon {
		item.SetIcon(t.appIcon(e.icon))
	}
}

func (t *Tray) appIcon(appID string) []byte {
	if appID == "" || t.opts.Icons == nil {
		return nil
	}
	if png := t.opts.Icons(appID); png != nil {
		return png
	}
	return t.opts.GenericIcon
}

// listen exécute l'action à chaque clic. ResetMenu ferme le canal, ce qui
// termine la goroutine.
func (t *Tray) listen(item *systray.MenuItem, action int) {
	var f func()
	switch action {
	case actionQuit:
		f = systray.Quit
	case actionToggleNotify:
		f = t.opts.OnToggleNotify
	}
	if f == nil {
		return
	}
	go func(ch <-chan struct{}) {
		for range ch {
			f()
		}
	}(item.ClickedCh)
}
