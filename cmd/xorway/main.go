// Commande xorway : indique dans le systray quelles fenêtres tournent sous
// XWayland et lesquelles sont en Wayland natif.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fyne.io/systray"

	"xorway/internal/backend"
	"xorway/internal/backend/x11only"
	"xorway/internal/tray"
)

const debounce = 200 * time.Millisecond

func main() {
	backendFlag := flag.String("backend", "auto", "backend à utiliser : auto, x11")
	interval := flag.Duration("interval", 3*time.Second, "intervalle du polling de secours")
	debug := flag.Bool("debug", false, "journalisation détaillée")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	b, err := selectBackend(*backendFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xorway:", err)
		os.Exit(2)
	}
	slog.Info("backend sélectionné", "backend", b.Name())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		systray.Quit()
	}()

	systray.Run(func() { onReady(ctx, b, *interval) }, func() {
		cancel()
		if err := b.Close(); err != nil {
			slog.Warn("fermeture du backend", "err", err)
		}
	})
}

// selectBackend choisit le backend selon le drapeau ou XDG_CURRENT_DESKTOP.
func selectBackend(name string) (backend.Backend, error) {
	display := os.Getenv("DISPLAY")
	switch name {
	case "x11":
		return x11only.New(display), nil
	case "auto":
		desktop := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
		slog.Debug("détection du bureau", "XDG_CURRENT_DESKTOP", desktop)
		return x11only.New(display), nil
	default:
		return nil, fmt.Errorf("backend inconnu %q", name)
	}
}

func onReady(ctx context.Context, b backend.Backend, interval time.Duration) {
	t := tray.New(b.Name())
	t.Init()

	if err := b.Start(ctx); err != nil {
		slog.Error("démarrage du backend", "err", err)
		t.Update(backend.Snapshot{}, err)
	}

	go loop(ctx, b, t, interval)
}

// loop rafraîchit le tray sur événement (avec anti-rebond) et, à défaut,
// par polling.
func loop(ctx context.Context, b backend.Backend, t *tray.Tray, interval time.Duration) {
	refresh := func() {
		snap, err := b.Snapshot()
		if err != nil {
			slog.Debug("snapshot", "err", err)
		} else {
			slog.Debug("snapshot", "windows", len(snap.Windows),
				"xwayland", snap.Count(backend.XWayland))
		}
		t.Update(snap, err)
	}
	refresh()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	timer := time.NewTimer(debounce)
	timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-b.Changes():
			timer.Reset(debounce)
		case <-timer.C:
			refresh()
		case <-ticker.C:
			refresh()
		}
	}
}
