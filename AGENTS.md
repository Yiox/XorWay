# AGENTS.md — XorWay

Go daemon: system tray icon (StatusNotifierItem) listing native Wayland /
XWayland windows, notifies when a game runs under XWayland.
Targets: KDE Plasma 6 (done), GNOME (see `TODO.md`). Read this file before
opening the code; only open the files you need.

## Map

| Path | Role |
|------|------|
| `cmd/xorway/main.go` | language setup, flags, subcommands, backend choice (`XDG_CURRENT_DESKTOP`), `app` type: `update()` loop (events + 200 ms debounce + 3 s polling) → game detection → `alert` → `tray` |
| `internal/backend/backend.go` | `Backend` interface (`Start/Snapshot/Changes/Close`), `Window`, `Snapshot`, `ErrPending`, `ErrAlreadyRunning` |
| `internal/backend/kwin/` | loads `script/xorway.js` (embedded) through `org.kde.KWin /Scripting`; the script calls `org.xorway.Daemon.Update(json)`; X11 cross-check fallback |
| `internal/backend/x11only/` | fallback backend: XWayland windows only |
| `internal/x11/` | xgb: `_NET_CLIENT_LIST`, titles, `WM_CLASS`, PID, `SKIP_TASKBAR`; `Monitor` with reconnection |
| `internal/tray/` | dbusmenu menu (fyne.io/systray): window lists, Settings submenu (notifications, language), backend + version lines; state icons drawn in Go (`icon.go`) |
| `internal/i18n/` | translations: `T`/`N`/`NewError`/`Wrap`, catalogs `locales/<lang>.json` (go-i18n v2 format), language detection |
| `internal/desktop/` | .desktop file index (Icon, Categories, StartupWMClass, localized Name) |
| `internal/appicon/` | Icon Theme spec (`theme.go`) + PNG/SVG rendering (`appicon.go`) |
| `internal/game/` | game heuristics: `steam_app_*`, `SteamAppId` (/proc environ), Wine, Game category |
| `internal/alert/`, `internal/notify/` | per-PID deduplication + `org.freedesktop.Notifications` |
| `internal/config/` | `~/.config/xorway/config.json` (`notifyGames`, `language`) |
| `internal/install/` | `install`/`uninstall` subcommands (copy to ~/.local/bin, embedded `xorway.desktop`, `@BINDIR@` replaced) |
| `.goreleaser.yaml`, `.github/workflows/release.yml` | Release on `v*` tag: tar.gz, deb, rpm, archlinux (amd64/arm64) |

## Invariants / pitfalls

- **No CGO** (`CGO_ENABLED=0`). **Go 1.22 minimum**: do not bump a
  dependency that requires more (`golang.org/x/image` pinned to v0.24.0,
  `golang.org/x/text` v0.22.0; go-i18n itself needs Go 1.24, hence our own
  loader). Check with `GOTOOLCHAIN=go1.22.12 go build ./...`.
- KWin 6 exposes no X11/Wayland property: we read the C++ class through
  `String(window)` → `KWin::X11Window` = XWayland. Unknown class → X11 cross-check.
- D-Bus `Update` only accepts the sender owning `org.kde.KWin`.
- dbusmenu: `_` is a mnemonic marker → `escape()` in `tray.go`.
- `systray.ResetMenu()` closes the `ClickedCh` channels: `listen` goroutines end by themselves.
- `tray.setMenu` updates in place when the menu "shape" is unchanged,
  otherwise rebuilds. `Tray.Update`: a single goroutine (the loop).
- `backend.Window.Game` is filled in by `main`, never by backends.
- Under GNOME, do not connect to Xwayland (on-demand startup).
- `xorway.desktop` (`@BINDIR@` template) is the single source: used by
  `install`, the Makefile and the GoReleaser `before` hook (`build/`).
- `main.version` is injected at build time (Makefile: `git describe`); without
  it, `buildVersion()` uses the VCS revision (`dev-0ff7d2a-dirty`) or the
  module version (`go install`).
- Menu entries are a flat list; `entry.depth` > 0 nests an entry under the
  closest preceding entry of depth-1 (submenus). Settings changes arrive on
  `listen` goroutines: `app.mu` guards `cfg`/`lang`, then `requestRefresh()`
  redraws the menu (language switch included, no restart).
- The menu only uses SNI + dbusmenu features (submenus, checkmarks, disabled
  items): it must keep working on any tray host (Plasma, XFCE, GNOME with the
  AppIndicator extension, waybar…). No GUI toolkit window (no CGO).

## Translations (i18n)

- **Every user-facing string** (menu, tooltips, notifications, errors shown in
  the menu or the CLI, CLI help, `install` output) goes through `i18n.T`,
  `i18n.N` (plurals, `{{.Count}}`) or `i18n.NewError`/`i18n.Wrap` (translatable
  errors, wrapped error available as `{{.Err}}`). Placeholders are key/value
  pairs: `i18n.T("menu.backend", "Name", name)`.
- New message: add the key to `locales/en.json` **and** `locales/fr.json`.
  `go test ./internal/i18n` fails on a missing key, a key used in code but
  absent from `en.json`, or placeholders that differ between languages.
- Sentinel errors are `i18n.NewError(id)`; `errors.Is` matches on the ID.
- The language is set at startup (`--lang` > config `language` >
  `LANGUAGE`/`LC_ALL`/`LC_MESSAGES`/`LANG` > English) and can change at
  runtime from the menu: never cache a translated string, translate when
  displaying. Each catalog has `language.name` (its own name, `i18n.Name`).
- Logs (`slog`) are **not** translated and stay in English.

## Conventions

- Code, comments, logs, README, AGENTS.md: **English**. Reference UI
  language: English; French is provided through `locales/fr.json`.
- Commit messages: French.
- Header `// SPDX-License-Identifier: AGPL-3.0-or-later` on every new source file.
- `gofmt`, `go vet ./...` and `go test ./...` before committing.

## Testing (Plasma Wayland session)

```sh
make build && ./bin/xorway --debug
LANG=fr_FR.UTF-8 ./bin/xorway   # French interface
xeyes &                  # XWayland window
SteamAppId=4242 xeyes &  # fake Steam game → notification
# read the menu without opening it:
N=$(busctl --user list --no-legend | awk -v p="$(pgrep -x xorway)" '$2==p && $1 ~ /^:/ {print $1; exit}')
busctl --user call "$N" /StatusNotifierItem/menu com.canonical.dbusmenu GetLayout iias -- 0 -1 0
busctl --user call org.kde.KWin /Scripting org.kde.kwin.Scripting isScriptLoaded s xorway
```

Stop with `pkill -x xorway` (never `pkill -f xorway`: it also kills the
calling shell).
