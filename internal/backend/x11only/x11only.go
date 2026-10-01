// Package x11only est le backend de repli : il ne voit que les fenêtres
// XWayland, via _NET_CLIENT_LIST du serveur Xwayland.
package x11only

import (
	"context"
	"strconv"

	"xorway/internal/backend"
	"xorway/internal/x11"
)

// Backend implémente backend.Backend à partir du seul serveur X.
type Backend struct {
	mon *x11.Monitor
}

// New crée le backend pour le DISPLAY donné.
func New(display string) *Backend {
	return &Backend{mon: x11.NewMonitor(display)}
}

func (b *Backend) Name() string { return "X11 (repli)" }

func (b *Backend) Start(ctx context.Context) error {
	go b.mon.Run(ctx)
	return nil
}

func (b *Backend) Changes() <-chan struct{} { return b.mon.Changes() }

func (b *Backend) Close() error { return nil }

func (b *Backend) Snapshot() (backend.Snapshot, error) {
	clients, err := b.mon.Clients()
	if err != nil {
		return backend.Snapshot{}, err
	}
	snap := backend.Snapshot{
		Notice: "Backend de repli : seules les fenêtres XWayland sont visibles",
	}
	for _, c := range clients {
		if c.SkipTaskbar {
			continue
		}
		snap.Windows = append(snap.Windows, FromClient(c))
	}
	return snap, nil
}

// FromClient convertit un client X11 en backend.Window de type XWayland.
func FromClient(c x11.Client) backend.Window {
	app := c.Class
	if app == "" {
		app = c.Instance
	}
	return backend.Window{
		ID:    "x11:" + strconv.FormatUint(uint64(c.ID), 16),
		Title: c.Title,
		AppID: app,
		PID:   c.PID,
		Type:  backend.XWayland,
	}
}
