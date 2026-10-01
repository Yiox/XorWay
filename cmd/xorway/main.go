// SPDX-License-Identifier: AGPL-3.0-or-later

// Commande xorway : indique dans le systray quelles fenêtres tournent sous
// XWayland et lesquelles sont en Wayland natif.
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
	"xorway/internal/install"
	"xorway/internal/notify"
	"xorway/internal/tray"
)

const debounce = 200 * time.Millisecond

// version est renseignée au build : -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `Usage :
  xorway [options]          lance le daemon (icône dans le systray)
  xorway install [--no-autostart]
                            installe ce binaire pour l'utilisateur
                            (~/.local/bin, lanceur, démarrage automatique)
  xorway uninstall          désinstalle (la configuration est conservée)
  xorway version            affiche la version

Options du daemon :
`

func main() {
	if len(os.Args) > 1 {
		if handled := subcommand(os.Args[1], os.Args[2:]); handled {
			return
		}
	}

	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	backendFlag := flag.String("backend", "auto", "backend à utiliser : auto, kwin, x11")
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
	slog.Info("démarrage", "version", version, "backend", b.Name())

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

// subcommand exécute une sous-commande ; renvoie false si name n'en est pas une.
func subcommand(name string, args []string) bool {
	var err error
	switch name {
	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		noAutostart := fs.Bool("no-autostart", false, "ne pas lancer XorWay à l'ouverture de session")
		fs.Parse(args)
		if err = install.Install(os.Stdout, !*noAutostart); err == nil {
			fmt.Println("XorWay est installé. Lancez-le depuis le menu des applications ou avec « xorway ».")
		}
	case "uninstall":
		if err = install.Uninstall(os.Stdout); err == nil {
			fmt.Println("XorWay est désinstallé. Quittez l'instance en cours depuis son menu si besoin.")
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

// selectBackend choisit le backend selon le drapeau ou XDG_CURRENT_DESKTOP.
func selectBackend(name string) (backend.Backend, error) {
	display := os.Getenv("DISPLAY")
	switch name {
	case "kwin":
		return kwin.New(display), nil
	case "x11":
		return x11only.New(display), nil
	case "auto":
		current := strings.ToUpper(os.Getenv("XDG_CURRENT_DESKTOP"))
		slog.Debug("détection du bureau", "XDG_CURRENT_DESKTOP", current)
		// XDG_CURRENT_DESKTOP est une liste séparée par « : ».
		for _, d := range strings.Split(current, ":") {
			if d == "KDE" {
				return kwin.New(display), nil
			}
		}
		return x11only.New(display), nil
	default:
		return nil, fmt.Errorf("backend inconnu %q", name)
	}
}

// app regroupe les composants du daemon.
type app struct {
	backend  backend.Backend
	tray     *tray.Tray
	games    *game.Detector
	alerter  *alert.Alerter
	refresh  chan struct{} // demande de rafraîchissement immédiat
	interval time.Duration
}

func onReady(ctx context.Context, b backend.Backend, interval time.Duration) {
	cfg, err := config.Load()
	if err != nil {
		slog.Warn("lecture de la configuration", "err", err)
	}

	idx := desktop.NewIndex()
	icons := appicon.New(idx)
	notifier, err := notify.New()
	if err != nil {
		slog.Warn("notifications indisponibles", "err", err)
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
				slog.Warn("enregistrement de la configuration", "err", err)
			}
			a.requestRefresh()
		},
	})
	a.tray.Init()

	if err := b.Start(ctx); err != nil {
		slog.Error("démarrage du backend", "err", err)
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

// update lit l'état du backend, repère les jeux et met à jour l'affichage.
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

// loop rafraîchit sur événement (avec anti-rebond) et, à défaut, par polling.
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
