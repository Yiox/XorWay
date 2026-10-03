// SPDX-License-Identifier: AGPL-3.0-or-later

// Command xorway shows in the system tray which windows run under XWayland
// and which ones are native Wayland clients.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"fyne.io/systray"

	"xorway/internal/alert"
	"xorway/internal/appicon"
	"xorway/internal/backend"
	"xorway/internal/backend/kwin"
	"xorway/internal/backend/x11only"
	"xorway/internal/config"
	"xorway/internal/desktop"
	"xorway/internal/game"
	"xorway/internal/i18n"
	"xorway/internal/install"
	"xorway/internal/notify"
	"xorway/internal/tray"
)

const debounce = 200 * time.Millisecond

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	// The language must be known before printing anything, help included.
	cfg, cfgErr := config.Load()
	i18n.SetLanguage(cfg.Language)

	if len(os.Args) > 1 {
		if handled := subcommand(os.Args[1], os.Args[2:]); handled {
			return
		}
	}

	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), i18n.T("cli.usage"))
		flag.PrintDefaults()
	}
	backendFlag := flag.String("backend", "auto", i18n.T("cli.flag.backend"))
	interval := flag.Duration("interval", 3*time.Second, i18n.T("cli.flag.interval"))
	debug := flag.Bool("debug", false, i18n.T("cli.flag.debug"))
	lang := flag.String("lang", "", i18n.T("cli.flag.lang", "Languages", strings.Join(i18n.Available(), ", ")))
	flag.Parse()
	if *lang != "" {
		i18n.SetLanguage(*lang)
	}

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	if cfgErr != nil {
		slog.Warn("reading the configuration", "err", cfgErr)
	}

	b, err := selectBackend(*backendFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "xorway:", err)
		os.Exit(2)
	}
	slog.Info("starting", "version", version, "language", i18n.Language(), "backend", b.Name())

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		systray.Quit()
	}()

	systray.Run(func() { onReady(ctx, b, cfg, *interval) }, func() {
		cancel()
		if err := b.Close(); err != nil {
			slog.Warn("closing the backend", "err", err)
		}
	})
}

// subcommand runs a subcommand; it returns false if name is not one.
func subcommand(name string, args []string) bool {
	var err error
	switch name {
	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		noAutostart := fs.Bool("no-autostart", false, i18n.T("cli.flag.no_autostart"))
		fs.Parse(args)
		if err = install.Install(os.Stdout, !*noAutostart); err == nil {
			fmt.Println(i18n.T("install.done"))
		}
	case "uninstall":
		if err = install.Uninstall(os.Stdout); err == nil {
			fmt.Println(i18n.T("install.uninstalled"))
		}
	case "version", "--version", "-version":
		fmt.Println("xorway", version)
	default:
		return false
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "xorway:", err)
		os.Exit(1)
	}
	return true
}

// selectBackend picks the backend from the flag or XDG_CURRENT_DESKTOP.
func selectBackend(name string) (backend.Backend, error) {
	display := os.Getenv("DISPLAY")
	switch name {
	case "kwin":
		return kwin.New(display), nil
	case "x11":
		return x11only.New(display), nil
	case "auto":
		current := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
		slog.Debug("detecting the desktop", "XDG_CURRENT_DESKTOP", current)
		// XDG_CURRENT_DESKTOP is a colon-separated list.
		for _, d := range strings.Split(current, ":") {
			if d == "KDE" {
				return kwin.New(display), nil
			}
		}
		return x11only.New(display), nil
	default:
		return nil, i18n.NewError("error.unknown_backend", "Name", name)
	}
}

// app groups the daemon components.
type app struct {
	backend  backend.Backend
	tray     *tray.Tray
	games    *game.Detector
	alerter  *alert.Alerter
	refresh  chan struct{} // request for an immediate refresh
	interval time.Duration
}

func onReady(ctx context.Context, b backend.Backend, cfg config.Config, interval time.Duration) {
	idx := desktop.NewIndex()
	icons := appicon.New(idx)
	notifier, err := notify.New()
	if err != nil {
		slog.Warn("notifications unavailable", "err", err)
	}

	a := &app{
		backend:  b,
		games:    game.NewDetector(idx),
		alerter:  alert.New(notifier, icons, idx, cfg.NotifyGames),
		refresh:  make(chan struct{}, 1),
		interval: interval,
	}
	a.tray = tray.New(tray.Options{
		BackendName: b.Name(),
		Icons:       func(appID string) []byte { return icons.PNG(appID, tray.IconSize) },
		GenericIcon: icons.Generic(tray.IconSize),
		NotifyGames: a.alerter.Enabled,
		OnToggleNotify: func() {
			cfg.NotifyGames = !a.alerter.Enabled()
			a.alerter.SetEnabled(cfg.NotifyGames)
			if err := config.Save(cfg); err != nil {
				slog.Warn("saving the configuration", "err", err)
			}
			a.requestRefresh()
		},
	})
	a.tray.Init()

	if err := b.Start(ctx); err != nil {
		slog.Error("starting the backend", "err", err)
		if errors.Is(err, backend.ErrAlreadyRunning) {
			systray.Quit()
			return
		}
		a.tray.Update(backend.Snapshot{}, err)
	}

	go a.loop(ctx)
}

func (a *app) requestRefresh() {
	select {
	case a.refresh <- struct{}{}:
	default:
	}
}

// update reads the backend state, detects games and refreshes the display.
func (a *app) update() {
	snap, err := a.backend.Snapshot()
	if err != nil {
		slog.Debug("snapshot", "err", err)
		a.tray.Update(snap, err)
		return
	}

	type winKey struct {
		pid   int
		appID string
	}
	present := map[winKey]bool{}
	for i := range snap.Windows {
		w := &snap.Windows[i]
		w.Game = a.games.Detect(w.AppID, w.PID)
		present[winKey{w.PID, w.AppID}] = true
	}
	a.games.Prune(func(pid int, appID string) bool { return present[winKey{pid, appID}] })

	slog.Debug("snapshot", "windows", len(snap.Windows), "xwayland", snap.Count(backend.XWayland))
	a.alerter.Process(snap)
	a.tray.Update(snap, nil)
}

// loop refreshes on events (debounced) and, failing that, by polling.
func (a *app) loop(ctx context.Context) {
	a.update()

	ticker := time.NewTicker(a.interval)
	defer ticker.Stop()
	timer := time.NewTimer(debounce)
	timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.backend.Changes():
			timer.Reset(debounce)
		case <-timer.C:
			a.update()
		case <-a.refresh:
			a.update()
		case <-ticker.C:
			a.update()
		}
	}
}
