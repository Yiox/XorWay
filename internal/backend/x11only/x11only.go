// SPDX-License-Identifier: AGPL-3.0-or-later

// Package x11only is the fallback backend: it only sees XWayland windows,
// through _NET_CLIENT_LIST of the Xwayland server.
package x11only

import (
	"context"
	"errors"
	"strconv"

	"xorway/internal/backend"
	"xorway/internal/i18n"
	"xorway/internal/x11"
)

// Backend implements backend.Backend using the X server only.
type Backend struct {
	mon *x11.Monitor
}

// New creates the backend for the given DISPLAY.
func New(display string) *Backend {
	return &Backend{mon: x11.NewMonitor(display)}
}

func (b *Backend) Name() string { return i18n.T("backend.x11") }

func (b *Backend) Start(ctx context.Context) error {
	go b.mon.Run(ctx)
	return nil
}

func (b *Backend) Changes() <-chan struct{} { return b.mon.Changes() }

func (b *Backend) Close() error { return nil }

func (b *Backend) Snapshot() (backend.Snapshot, error) {
	clients, err := b.mon.Clients()
	if errors.Is(err, x11.ErrConnecting) {
		return backend.Snapshot{}, backend.ErrPending
	}
	if err != nil {
		return backend.Snapshot{}, err
	}
	snap := backend.Snapshot{Notice: i18n.T("notice.x11_only")}
	for _, c := range clients {
		if c.SkipTaskbar {
			continue
		}
		snap.Windows = append(snap.Windows, FromClient(c))
	}
	return snap, nil
}

// FromClient converts an X11 client into an XWayland backend.Window.
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
