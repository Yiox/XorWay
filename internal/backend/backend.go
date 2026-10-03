// SPDX-License-Identifier: AGPL-3.0-or-later

// Package backend defines the interface shared by window sources (KWin,
// GNOME Shell, X11 only).
package backend

import (
	"context"

	"xorway/internal/game"
	"xorway/internal/i18n"
)

// ErrPending means the backend is starting and has no data yet. It is not
// shown as an error.
var ErrPending = i18n.NewError("status.loading")

// ErrAlreadyRunning means another XorWay instance is active.
var ErrAlreadyRunning = i18n.NewError("error.already_running")

// ClientType is the protocol used by a window.
type ClientType int

const (
	Wayland ClientType = iota
	XWayland
)

func (t ClientType) String() string {
	switch t {
	case Wayland:
		return "Wayland"
	case XWayland:
		return "XWayland"
	default:
		return "unknown"
	}
}

// Window describes a top-level window.
type Window struct {
	ID    string
	Title string
	AppID string // Wayland app_id, .desktop file name or WM_CLASS
	PID   int
	Type  ClientType
	// Game is filled in by the daemon afterwards (backends leave it to
	// game.None).
	Game game.Source
}

// Snapshot is the state of the windows at a given time.
type Snapshot struct {
	Windows []Window
	// NativeKnown is false when the backend cannot see native Wayland
	// windows (X11-only backend).
	NativeKnown bool
	// Notice is an optional, already translated message shown in the menu.
	Notice string
}

// Count returns the number of windows of the given type.
func (s Snapshot) Count(t ClientType) int {
	n := 0
	for _, w := range s.Windows {
		if w.Type == t {
			n++
		}
	}
	return n
}

// Backend is a source of windows.
type Backend interface {
	// Name is a short, translated name shown in the menu.
	Name() string
	// Start initializes the backend. Background work stops when the context
	// is cancelled.
	Start(ctx context.Context) error
	// Snapshot returns the current state. An error means the backend is
	// unavailable; its message is shown as is to the user, so it must be
	// translatable (see package i18n).
	Snapshot() (Snapshot, error)
	// Changes receives a value when the state may have changed.
	Changes() <-chan struct{}
	// Close releases resources (KWin script, connections…).
	Close() error
}
