// SPDX-License-Identifier: AGPL-3.0-or-later

// Package kwin est le backend KDE Plasma 6 : un script KWin, chargé via
// D-Bus, pousse la liste des fenêtres vers le daemon.
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

	// scriptTimeout : délai au-delà duquel l'absence de données du script
	// est signalée comme une erreur.
	scriptTimeout = 5 * time.Second
)

// window est une fenêtre telle que décrite par le script KWin.
type window struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	ResourceClass string `json:"resourceClass"`
	DesktopFile   string `json:"desktopFile"`
	PID           int    `json:"pid"`
	Kind          string `json:"kind"` // classe C++ KWin, ex. KWin::X11Window
}

// Backend implémente backend.Backend pour KWin.
type Backend struct {
	display    string
	changes    chan struct{}
	conn       *dbus.Conn
	kwinOwner  string // nom unique de KWin, seul expéditeur accepté
	scriptPath string

	mu       sync.Mutex
	err      error // erreur fatale de démarrage
	started  time.Time
	received bool
	windows  []window

	// Croisement X11, démarré seulement si une classe KWin est inconnue.
	ctx     context.Context
	monOnce sync.Once
	mon     *x11.Monitor
}

// New crée le backend. display sert au croisement X11 de secours.
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

// Start enregistre le service D-Bus du daemon puis charge le script KWin.
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
		return fmt.Errorf("connexion au bus de session impossible : %w", err)
	}
	b.conn = conn

	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, kwinService).Store(&b.kwinOwner); err != nil {
		return errors.New("KWin introuvable sur D-Bus (session KDE Plasma requise)")
	}

	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("réservation du nom %s : %w", busName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("%w (nom D-Bus %s occupé)", backend.ErrAlreadyRunning, busName)
	}
	if err := conn.Export(receiver{b}, objectPath, iface); err != nil {
		return fmt.Errorf("export D-Bus : %w", err)
	}

	if b.scriptPath, err = writeScript(); err != nil {
		return err
	}
	return b.loadScript()
}

// writeScript copie le script embarqué dans $XDG_RUNTIME_DIR/xorway/.
func writeScript() (string, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "xorway")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("création de %s : %w", dir, err)
	}
	path := filepath.Join(dir, "xorway.js")
	if err := os.WriteFile(path, script, 0o600); err != nil {
		return "", fmt.Errorf("écriture du script KWin : %w", err)
	}
	return path, nil
}

func (b *Backend) loadScript() error {
	obj := b.conn.Object(kwinService, "/Scripting")

	// Un script peut rester chargé après un arrêt brutal du daemon.
	var loaded bool
	if err := obj.Call(scripting+".isScriptLoaded", 0, pluginName).Store(&loaded); err != nil {
		return fmt.Errorf("API de scripting KWin indisponible : %w", err)
	}
	if loaded {
		b.unloadScript()
	}

	var id int32
	if err := obj.Call(scripting+".loadScript", 0, b.scriptPath, pluginName).Store(&id); err != nil {
		return fmt.Errorf("chargement du script KWin : %w", err)
	}
	if id < 0 {
		return errors.New("KWin a refusé le script XorWay")
	}

	script := b.conn.Object(kwinService, dbus.ObjectPath(fmt.Sprintf("/Scripting/Script%d", id)))
	if err := script.Call("org.kde.kwin.Script.run", 0).Err; err != nil {
		b.unloadScript()
		return fmt.Errorf("exécution du script KWin : %w", err)
	}
	slog.Debug("kwin: script chargé", "id", id, "path", b.scriptPath)
	return nil
}

func (b *Backend) unloadScript() {
	obj := b.conn.Object(kwinService, "/Scripting")
	if err := obj.Call(scripting+".unloadScript", 0, pluginName).Err; err != nil {
		slog.Warn("kwin: déchargement du script", "err", err)
	}
}

// Close décharge le script et libère le nom D-Bus.
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

// receiver est l'objet D-Bus appelé par le script KWin.
type receiver struct{ b *Backend }

// Update reçoit la liste des fenêtres au format JSON.
func (r receiver) Update(sender dbus.Sender, payload string) *dbus.Error {
	if string(sender) != r.b.kwinOwner {
		return dbus.MakeFailedError(errors.New("expéditeur non autorisé"))
	}
	var ws []window
	if err := json.Unmarshal([]byte(payload), &ws); err != nil {
		slog.Warn("kwin: données invalides", "err", err)
		return dbus.MakeFailedError(err)
	}
	r.b.mu.Lock()
	r.b.windows, r.b.received = ws, true
	r.b.mu.Unlock()
	slog.Debug("kwin: mise à jour reçue", "windows", len(ws))
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
		return backend.Snapshot{}, errors.New("le script KWin ne répond pas (journalctl --user -g xorway)")
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
		snap.Notice = "Classification par croisement X11 (heuristique)"
	}
	return snap, nil
}

// classify déduit le type de client de la classe C++ KWin.
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

// crossCheck classe les fenêtres en les comparant aux clients Xwayland :
// même PID, ou même classe et même titre.
func (b *Backend) crossCheck(ws []window) []backend.Window {
	b.monOnce.Do(func() {
		slog.Info("kwin: classe de fenêtre inconnue, croisement avec X11")
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
		slog.Debug("kwin: croisement X11 impossible", "err", err)
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
