// SPDX-License-Identifier: AGPL-3.0-or-later

// Package kwin is the KDE Plasma 6 backend: a KWin script, loaded over
// D-Bus, pushes the window list to the daemon.
package kwin

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"

	"xorway/internal/backend"
	"xorway/internal/i18n"
	"xorway/internal/x11"
)

//go:embed script/xorway.js
var script []byte

const (
	busName    = "org.xorway.Daemon"
	objectPath = "/org/xorway/Daemon"
	iface      = "org.xorway.Daemon"

	kwinService = "org.kde.KWin"
	scripting   = "org.kde.kwin.Scripting"
	pluginName  = "xorway"

	// scriptTimeout is the delay after which missing data from the script
	// is reported as an error.
	scriptTimeout = 5 * time.Second
)

// window is a window as described by the KWin script.
type window struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	ResourceClass string `json:"resourceClass"`
	DesktopFile   string `json:"desktopFile"`
	PID           int    `json:"pid"`
	Kind          string `json:"kind"` // KWin C++ class, e.g. KWin::X11Window
}

// Backend implements backend.Backend for KWin.
type Backend struct {
	display    string
	changes    chan struct{}
	conn       *dbus.Conn
	kwinOwner  string // unique bus name of KWin, the only accepted sender
	scriptPath string

	mu       sync.Mutex
	err      error // fatal startup error
	started  time.Time
	received bool
	windows  []window

	// X11 cross-check, started only when a KWin class is unknown.
	ctx     context.Context
	monOnce sync.Once
	mon     *x11.Monitor
}

// New creates the backend. display is used by the fallback X11 cross-check.
func New(display string) *Backend {
	return &Backend{display: display, changes: make(chan struct{}, 1)}
}

func (b *Backend) Name() string { return "KWin" }

func (b *Backend) Changes() <-chan struct{} { return b.changes }

func (b *Backend) notify() {
	select {
	case b.changes <- struct{}{}:
	default:
	}
}

// Start registers the daemon D-Bus service, then loads the KWin script.
func (b *Backend) Start(ctx context.Context) error {
	b.mu.Lock()
	b.ctx = ctx
	b.started = time.Now()
	b.mu.Unlock()

	if err := b.start(); err != nil {
		b.mu.Lock()
		b.err = err
		b.mu.Unlock()
		return err
	}
	return nil
}

func (b *Backend) start() error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return i18n.Wrap(err, "error.session_bus")
	}
	b.conn = conn

	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, kwinService).Store(&b.kwinOwner); err != nil {
		return i18n.NewError("error.kwin_missing")
	}

	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return i18n.Wrap(err, "error.dbus_name", "Name", busName)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		slog.Debug("kwin: D-Bus name already taken", "name", busName)
		return backend.ErrAlreadyRunning
	}
	if err := conn.Export(receiver{b}, objectPath, iface); err != nil {
		return i18n.Wrap(err, "error.dbus_export")
	}

	if b.scriptPath, err = writeScript(); err != nil {
		return err
	}
	return b.loadScript()
}

// writeScript copies the embedded script to $XDG_RUNTIME_DIR/xorway/.
func writeScript() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "xorway")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", i18n.Wrap(err, "error.create_dir", "Path", dir)
	}
	path := filepath.Join(dir, "xorway.js")
	if err := os.WriteFile(path, script, 0o600); err != nil {
		return "", i18n.Wrap(err, "error.kwin_script_write")
	}
	return path, nil
}

func (b *Backend) loadScript() error {
	obj := b.conn.Object(kwinService, "/Scripting")

	// A script may still be loaded after the daemon crashed.
	var loaded bool
	if err := obj.Call(scripting+".isScriptLoaded", 0, pluginName).Store(&loaded); err != nil {
		return i18n.Wrap(err, "error.kwin_scripting")
	}
	if loaded {
		b.unloadScript()
	}

	var id int32
	if err := obj.Call(scripting+".loadScript", 0, b.scriptPath, pluginName).Store(&id); err != nil {
		return i18n.Wrap(err, "error.kwin_script_load")
	}
	if id < 0 {
		return i18n.NewError("error.kwin_script_refused")
	}

	script := b.conn.Object(kwinService, dbus.ObjectPath(fmt.Sprintf("/Scripting/Script%d", id)))
	if err := script.Call("org.kde.kwin.Script.run", 0).Err; err != nil {
		b.unloadScript()
		return i18n.Wrap(err, "error.kwin_script_run")
	}
	slog.Debug("kwin: script loaded", "id", id, "path", b.scriptPath)
	return nil
}

func (b *Backend) unloadScript() {
	obj := b.conn.Object(kwinService, "/Scripting")
	if err := obj.Call(scripting+".unloadScript", 0, pluginName).Err; err != nil {
		slog.Warn("kwin: unloading the script", "err", err)
	}
}

// Close unloads the script and releases the D-Bus name.
func (b *Backend) Close() error {
	if b.conn == nil {
		return nil
	}
	if b.scriptPath != "" {
		b.unloadScript()
		os.Remove(b.scriptPath)
	}
	return b.conn.Close()
}

// receiver is the D-Bus object called by the KWin script.
type receiver struct{ b *Backend }

// Update receives the window list as JSON.
func (r receiver) Update(sender dbus.Sender, payload string) *dbus.Error {
	if string(sender) != r.b.kwinOwner {
		return dbus.MakeFailedError(errors.New("sender not allowed"))
	}
	var ws []window
	if err := json.Unmarshal([]byte(payload), &ws); err != nil {
		slog.Warn("kwin: invalid data", "err", err)
		return dbus.MakeFailedError(err)
	}
	r.b.mu.Lock()
	r.b.windows, r.b.received = ws, true
	r.b.mu.Unlock()
	slog.Debug("kwin: update received", "windows", len(ws))
	r.b.notify()
	return nil
}

func (b *Backend) Snapshot() (backend.Snapshot, error) {
	b.mu.Lock()
	err, received, started := b.err, b.received, b.started
	ws := b.windows
	b.mu.Unlock()

	switch {
	case err != nil:
		return backend.Snapshot{}, err
	case !received && time.Since(started) > scriptTimeout:
		return backend.Snapshot{}, i18n.NewError("error.kwin_script_timeout")
	case !received:
		return backend.Snapshot{}, backend.ErrPending
	}

	snap := backend.Snapshot{NativeKnown: true}
	var unknown []window
	for _, w := range ws {
		typ, ok := classify(w.Kind)
		if !ok {
			unknown = append(unknown, w)
			continue
		}
		snap.Windows = append(snap.Windows, convert(w, typ))
	}
	if len(unknown) > 0 {
		snap.Windows = append(snap.Windows, b.crossCheck(unknown)...)
		snap.Notice = i18n.T("notice.cross_check")
	}
	return snap, nil
}

// classify infers the client type from the KWin C++ class.
func classify(kind string) (backend.ClientType, bool) {
	switch {
	case kind == "KWin::X11Window":
		return backend.XWayland, true
	case strings.HasPrefix(kind, "KWin::"):
		return backend.Wayland, true
	default:
		return backend.Wayland, false
	}
}

func convert(w window, typ backend.ClientType) backend.Window {
	app := w.DesktopFile
	if app == "" {
		app = w.ResourceClass
	}
	return backend.Window{ID: w.ID, Title: w.Title, AppID: app, PID: w.PID, Type: typ}
}

// crossCheck classifies windows by comparing them with Xwayland clients:
// same PID, or same class and same title.
func (b *Backend) crossCheck(ws []window) []backend.Window {
	b.monOnce.Do(func() {
		slog.Info("kwin: unknown window class, cross-checking with X11")
		b.mon = x11.NewMonitor(b.display)
		go b.mon.Run(b.ctx)
		go func() {
			for {
				select {
				case <-b.ctx.Done():
					return
				case <-b.mon.Changes():
					b.notify()
				}
			}
		}()
	})

	clients, err := b.mon.Clients()
	if err != nil {
		slog.Debug("kwin: X11 cross-check unavailable", "err", err)
	}
	out := make([]backend.Window, 0, len(ws))
	for _, w := range ws {
		typ := backend.Wayland
		for _, c := range clients {
			samePID := w.PID != 0 && c.PID == w.PID
			sameWin := strings.EqualFold(c.Class, w.ResourceClass) && c.Title == w.Title
			if samePID || sameWin {
				typ = backend.XWayland
				break
			}
		}
		out = append(out, convert(w, typ))
	}
	return out
}
