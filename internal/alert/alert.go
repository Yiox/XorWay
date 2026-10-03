// SPDX-License-Identifier: AGPL-3.0-or-later

// Package alert notifies the user when a game opens under XWayland.
package alert

import (
	"log/slog"
	"strconv"
	"sync/atomic"

	"xorway/internal/appicon"
	"xorway/internal/backend"
	"xorway/internal/desktop"
	"xorway/internal/game"
	"xorway/internal/i18n"
	"xorway/internal/notify"
)

const imageSize = 64 // size of the game icon in the notification

// Alerter sends one notification per game (not per window) for as long as
// the game stays open.
type Alerter struct {
	client  *notify.Client
	icons   *appicon.Resolver
	idx     *desktop.Index
	enabled atomic.Bool
	seen    map[string]bool // games already reported and still open
}

// New creates an alerter. client may be nil (notifications unavailable).
func New(client *notify.Client, icons *appicon.Resolver, idx *desktop.Index, enabled bool) *Alerter {
	a := &Alerter{client: client, icons: icons, idx: idx, seen: map[string]bool{}}
	a.enabled.Store(enabled)
	return a
}

func (a *Alerter) Enabled() bool     { return a.enabled.Load() }
func (a *Alerter) SetEnabled(v bool) { a.enabled.Store(v) }

// key identifies a game: its PID, or the window ID when unknown.
func key(w backend.Window) string {
	if w.PID > 0 {
		return "pid:" + strconv.Itoa(w.PID)
	}
	return "win:" + w.ID
}

// Process must be called on every new state. It is not safe for concurrent
// use.
func (a *Alerter) Process(snap backend.Snapshot) {
	present := map[string]bool{}
	for _, w := range snap.Windows {
		if w.Type != backend.XWayland || w.Game == game.None {
			continue
		}
		k := key(w)
		if present[k] {
			continue
		}
		present[k] = true
		// A game already open when the option is enabled is not reported.
		if !a.seen[k] && a.Enabled() && a.client != nil {
			a.send(w)
		}
	}
	a.seen = present
}

func (a *Alerter) send(w backend.Window) {
	name := w.Title
	if e := a.idx.Lookup(w.AppID); e != nil && e.Name != "" {
		name = e.LocalName(i18n.Language())
	}
	if name == "" {
		name = w.AppID
	}

	body := i18n.T("notify.game.body", "Name", name, "Source", w.Game.String())
	if h := w.Game.Hint(); h != "" {
		body += "\n" + h
	}
	n := notify.Notification{
		Summary: i18n.T("notify.game.summary"),
		Body:    body,
		Icon:    "applications-games",
		Image:   a.icons.Image(w.AppID, imageSize),
	}
	if err := a.client.Send(n); err != nil {
		slog.Warn("notification", "err", err)
		return
	}
	slog.Info("game under XWayland reported", "app", w.AppID, "pid", w.PID, "source", w.Game.Key())
}
