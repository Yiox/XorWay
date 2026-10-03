// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tray shows the window state in a StatusNotifierItem icon.
package tray

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"fyne.io/systray"

	"xorway/internal/backend"
	"xorway/internal/game"
	"xorway/internal/i18n"
)

const (
	maxLabel = 70 // maximum length (in runes) of a menu entry
	IconSize = 32 // size of application icons in the menu
)

// Actions of clickable entries.
const (
	actionNone = iota
	actionQuit
	actionToggleNotify
)

// entry is a menu line; the zero value is a separator.
type entry struct {
	title    string
	tooltip  string
	disabled bool
	icon     string // app id whose icon is shown ("" for none)
	action   int
	checkbox bool
	checked  bool
}

var separator = entry{}

// sameShape reports whether an entry can be updated in place.
func sameShape(a, b entry) bool {
	return (a == separator) == (b == separator) && a.action == b.action && a.checkbox == b.checkbox
}

// Options configures the tray.
type Options struct {
	BackendName string
	// Icons returns the PNG icon of an application (nil if not found).
	Icons func(appID string) []byte
	// GenericIcon replaces an icon that cannot be found.
	GenericIcon []byte
	// NotifyGames returns the state of the "Notify…" checkbox; nil hides it.
	NotifyGames func() bool
	// OnToggleNotify is called when the user toggles the checkbox.
	OnToggleNotify func()
}

// Tray drives the icon and the menu. Init then Update must be called from a
// single goroutine, after systray has started.
type Tray struct {
	opts        Options
	lastState   iconState
	lastTooltip string
	lastMenu    []entry
	items       []*systray.MenuItem // nil for separators
}

// New creates the tray.
func New(opts Options) *Tray {
	return &Tray{opts: opts, lastState: -1}
}

// Init sets the icon and a placeholder menu.
func (t *Tray) Init() {
	systray.SetTitle("XorWay")
	t.setIcon(stateNative)
	t.setTooltip("XorWay — " + i18n.T("status.loading"))
	t.setMenu([]entry{{title: i18n.T("status.loading"), disabled: true}})
}

// Update reflects a new state (or a backend error).
func (t *Tray) Update(snap backend.Snapshot, err error) {
	if errors.Is(err, backend.ErrPending) {
		return // the "Loading…" state set by Init stays visible
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
		t.setTooltip("XorWay — " + i18n.T("tooltip.counts", "XWayland", nX, "Native", nW))
	} else {
		t.setTooltip("XorWay — " + i18n.N("tooltip.xwayland_only", nX))
	}

	var menu []entry
	if snap.NativeKnown {
		menu = append(menu, entry{title: i18n.T("menu.native_header", "Count", nW), disabled: true})
		menu = append(menu, windowEntries(snap.Windows, backend.Wayland)...)
		menu = append(menu, separator)
	}
	menu = append(menu, entry{title: i18n.T("menu.xwayland_header", "Count", nX), disabled: true})
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
		return []entry{{title: i18n.T("menu.none"), disabled: true}}
	}
	out := make([]entry, 0, len(sel))
	for _, w := range sel {
		title := w.Title
		if title == "" {
			title = i18n.T("menu.untitled")
		}
		label := title
		if w.AppID != "" {
			label = title + " — " + w.AppID
		}
		tip := title + "\n" + i18n.T("tooltip.window", "App", w.AppID, "PID", w.PID)
		if w.Game != game.None {
			label = "🎮 " + label
			tip += "\n" + i18n.T("tooltip.game", "Source", w.Game.String())
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

// escape protects "_", which dbusmenu treats as a mnemonic marker.
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

// footer appends the fixed entries at the bottom of the menu.
func (t *Tray) footer(menu []entry) []entry {
	menu = append(menu, separator)
	if t.opts.NotifyGames != nil {
		menu = append(menu, entry{
			title:    i18n.T("menu.notify_games"),
			action:   actionToggleNotify,
			checkbox: true,
			checked:  t.opts.NotifyGames(),
		})
	}
	return append(menu,
		entry{title: i18n.T("menu.backend", "Name", t.opts.BackendName), disabled: true},
		entry{title: i18n.T("menu.quit"), action: actionQuit},
	)
}

// setMenu updates the menu: in place when its structure is unchanged (the
// common case of a changing title), otherwise by rebuilding it.
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

// listen runs the action on every click. ResetMenu closes the channel, which
// ends the goroutine.
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
