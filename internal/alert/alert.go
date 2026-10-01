// SPDX-License-Identifier: AGPL-3.0-or-later

// Package alert notifie l'utilisateur quand un jeu s'ouvre sous XWayland.
package alert

import (
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"

	"xorway/internal/appicon"
	"xorway/internal/backend"
	"xorway/internal/desktop"
	"xorway/internal/game"
	"xorway/internal/notify"
)

const imageSize = 64 // taille de l'icône du jeu dans la notification

// Alerter envoie une notification par jeu (et non par fenêtre) tant que
// celui-ci reste ouvert.
type Alerter struct {
	client  *notify.Client
	icons   *appicon.Resolver
	idx     *desktop.Index
	enabled atomic.Bool
	seen    map[string]bool // jeux déjà signalés et toujours ouverts
}

// New crée un alerteur. client peut être nil (notifications indisponibles).
func New(client *notify.Client, icons *appicon.Resolver, idx *desktop.Index, enabled bool) *Alerter {
	a := &Alerter{client: client, icons: icons, idx: idx, seen: map[string]bool{}}
	a.enabled.Store(enabled)
	return a
}

func (a *Alerter) Enabled() bool     { return a.enabled.Load() }
func (a *Alerter) SetEnabled(v bool) { a.enabled.Store(v) }

// key identifie un jeu : son PID, à défaut l'identifiant de la fenêtre.
func key(w backend.Window) string {
	if w.PID > 0 {
		return "pid:" + strconv.Itoa(w.PID)
	}
	return "win:" + w.ID
}

// Process doit être appelé à chaque nouvel état. Il n'est pas sûr pour un
// usage concurrent.
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
		// Un jeu déjà ouvert quand on active l'option n'est pas signalé.
		if !a.seen[k] && a.Enabled() && a.client != nil {
			a.send(w)
		}
	}
	a.seen = present
}

func (a *Alerter) send(w backend.Window) {
	name := w.Title
	if e := a.idx.Lookup(w.AppID); e != nil && e.Name != "" {
		name = e.Name
	}
	if name == "" {
		name = w.AppID
	}

	body := fmt.Sprintf("« %s » (%s) tourne sous XWayland au lieu de Wayland natif.", name, w.Game)
	if h := w.Game.Hint(); h != "" {
		body += "\n" + h
	}
	n := notify.Notification{
		Summary: "Jeu lancé sous X11",
		Body:    body,
		Icon:    "applications-games",
		Image:   a.icons.Image(w.AppID, imageSize),
	}
	if err := a.client.Send(n); err != nil {
		slog.Warn("notification", "err", err)
		return
	}
	slog.Info("jeu sous XWayland signalé", "app", w.AppID, "pid", w.PID, "source", w.Game)
}
