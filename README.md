# XorWay

XorWay shows in the system tray which windows run under **XWayland** and
which ones are **native Wayland** clients, in a Wayland session.

- Green “W” icon: no XWayland window.
- Orange “X” icon: at least one XWayland window is open.
- Grey “!” icon: backend unavailable (the menu explains why).

The menu lists the windows with their application icon; detected games are
marked with 🎮. When a game opens under XWayland, a notification reports it
and suggests a way to run it as a native Wayland client.

The **Settings** submenu turns these notifications on or off and picks the
interface language; changes apply immediately and are saved in
`~/.config/xorway/config.json`. The bottom of the menu shows the backend in
use and the XorWay version (also printed by `xorway version`).

The interface is available in English and French (see
[Languages](#languages)).

## Requirements

- Go ≥ 1.22 (build only, no CGO)
- A Wayland session with Xwayland
- A StatusNotifierItem host: built into KDE Plasma.

## Installation

### From a GitHub Release (no build needed)

Download the latest version from
<https://github.com/Yiox/XorWay/releases>:

- **standalone binary** (any distribution, no root):

  ```sh
  tar xzf xorway_*_linux_amd64.tar.gz
  ./xorway install            # ~/.local/bin, launcher, autostart
  xorway                      # or from the application menu
  xorway uninstall            # uninstall (the configuration is kept)
  ```

  `xorway install --no-autostart` skips the autostart entry.
- **packages**: `.rpm` (Fedora…), `.deb` (Debian, Ubuntu…), `.pkg.tar.zst`
  (Arch). They install `/usr/bin/xorway`, the launcher and
  `/etc/xdg/autostart/xorway.desktop` (for all users).

### From source

```sh
make build      # builds bin/xorway
make test       # runs the tests
make install    # ~/.local/bin/xorway + ~/.config/autostart/xorway.desktop
make uninstall
```

`make install` also copies the .desktop file to
`~/.local/share/applications/`: Plasma uses it to attach notifications to
XorWay (configurable in Plasma's notification settings).
`PREFIX` (default `~/.local`) and `DESTDIR` can be overridden.

## Usage

```sh
xorway [--backend auto|kwin|x11] [--interval 3s] [--lang en|fr] [--debug]
xorway install [--no-autostart] | uninstall | version
```

In `auto` mode, the backend is chosen from `XDG_CURRENT_DESKTOP`:
`KDE` → `kwin`, otherwise `x11`. Only one instance can run at a time.

## Languages

The interface (menu, tooltips, notifications, errors, command-line help) is
translated. The language is chosen in this order:

1. the `--lang` option;
2. the language chosen in **Settings → Language**, stored as `"language"` in
   `~/.config/xorway/config.json` (e.g. `"fr"`; “Automatic” removes it);
3. the `LANGUAGE`, `LC_ALL`, `LC_MESSAGES` then `LANG` environment variables
   (set by Plasma from the system language);
4. English.

Logs (`--debug`) are always in English.

### Adding a language

Translations live in `internal/i18n/locales/<lang>.json` and use the
[go-i18n v2](https://github.com/nicksnyder/go-i18n) JSON format, supported by
translation platforms such as Weblate.

1. Copy `internal/i18n/locales/en.json` to `<lang>.json` (e.g. `de.json`).
2. Translate the values; keep the keys and the `{{.Placeholders}}` unchanged.
   `language.name` is the language's own name (`"Deutsch"`), shown in the
   language submenu.
3. Messages with plural forms are objects (`"one"`, `"other"`, and `"few"`,
   `"many"`… depending on the language, following the
   [CLDR plural rules](https://www.unicode.org/cldr/charts/latest/supplemental/language_plural_rules.html)).
4. Add `Name[<lang>]`, `GenericName[<lang>]` and `Comment[<lang>]` to
   `internal/install/xorway.desktop`.
5. Run `make test`: it checks that no key is missing and that placeholders
   match English.

The new file is embedded in the binary at build time; nothing else to change.

## Backends

| Backend | Desktop | Native windows | XWayland windows |
|---------|---------|----------------|------------------|
| `kwin`  | KDE Plasma 6 | KWin script | KWin script |
| `x11`   | any     | not visible    | Xwayland `_NET_CLIENT_LIST` |

### KDE Plasma 6 (`kwin`)

The daemon claims the `org.xorway.Daemon` name on the session bus, writes the
embedded script to `$XDG_RUNTIME_DIR/xorway/xorway.js` and loads it through
`org.kde.KWin /Scripting`. The script subscribes to `workspace.windowAdded`,
`windowRemoved` and title changes, then sends the window list to the daemon
with `callDBus` (`Update` method, JSON). Only calls coming from KWin are
accepted. The script is unloaded when the daemon stops.

The KWin 6 scripting API documents **no property** telling X11 from Wayland.
However, converting a window object to a string yields its C++ class:
`KWin::X11Window` for an XWayland client, `KWin::XdgToplevelWindow` for a
native Wayland client. XorWay relies on this behavior (checked on KWin 6.7).
If an unknown class shows up, the daemon falls back to a cross-check with
Xwayland's `_NET_CLIENT_LIST` (same PID, or same class and same title) and
says so in the menu.

### X11 fallback (`x11`)

The `x11` backend connects to the Xwayland `DISPLAY`: every window listed in
`_NET_CLIENT_LIST` is an XWayland window. Technical windows marked
`_NET_WM_STATE_SKIP_TASKBAR` (e.g. *xwaylandvideobridge*) are ignored.
Updates are event-driven (`PropertyNotify`), with a 3 s fallback poll.

## Application icons

The icon is looked up in the application's .desktop file (by app id,
`StartupWMClass` or id suffix), then in the current icon theme
(`kdeglobals`, default `breeze`), its parents and `hicolor`, and finally in
`/usr/share/pixmaps`. Steam games (`steam_app_<id>`) use `steam_icon_<id>`.
SVG files are rendered in pure Go (oksvg); otherwise the theme's generic icon
is used. `XORWAY_ICON_THEME` forces a theme.

## Game detection

A window is considered a game when:

| Clue | Displayed source | Suggestion |
|------|------------------|------------|
| class `steam_app_<id>`, or `SteamAppId`/`SteamGameId` in `/proc/<pid>/environ`, with Wine/Proton | Steam (Proton) | `PROTON_ENABLE_WAYLAND=1 %command%` (Proton ≥ 10) |
| same without Wine | Steam | `SDL_VIDEODRIVER=wayland` |
| `wine*` / `*-preloader` executable, or a command ending in `.exe` | Wine | `winewayland` driver (Wine ≥ 9) |
| `Game` category in the .desktop file | Game category | `SDL_VIDEODRIVER=wayland` |

Only one notification is sent per game (per PID) while it stays open. Games
already running when the daemon starts are reported once.

## Testing

```sh
make run                              # runs the daemon with --debug
xeyes &                               # XWayland window: the icon turns orange
GDK_BACKEND=x11 gnome-text-editor &   # GTK application forced to X11
QT_QPA_PLATFORM=xcb kwrite &          # Qt application forced to X11
LANG=fr_FR.UTF-8 ./bin/xorway         # French interface
```

Simulate a Steam game (notification + 🎮 in the menu):

```sh
SteamAppId=4242 xeyes &
```

Check the KDE backend:

```sh
busctl --user call org.kde.KWin /Scripting org.kde.kwin.Scripting isScriptLoaded s xorway
# → b true while xorway runs, b false after quitting it
```

## Releasing

Releases are built by GoReleaser (`.goreleaser.yaml`) through GitHub Actions
(`.github/workflows/release.yml`): pushing a tag is enough.

```sh
git tag -s v0.1.0 -m "XorWay 0.1.0"   # signed tag
git push origin v0.1.0
```

The workflow checks the code (`gofmt`, `go vet`, `go test`), builds for amd64
and arm64 (no CGO, version injected with `-X main.version`), then publishes
the archives, the packages and `checksums.txt`. Local dry run:
`goreleaser release --snapshot --clean` (output in `dist/`).

## Limitations

- The `x11` backend cannot see native Wayland windows.
- `kwin` backend: classification relies on the C++ class name of windows, an
  internal KWin detail that could change (automatic, less reliable fallback to
  the X11 cross-check).
- Windows hidden from the taskbar (`skipTaskbar`) are ignored.
- Heuristic game detection: any Windows application under Wine is treated as
  a game; a native game outside Steam without the `Game` category is not
  recognized.
- Some SVG files are not supported by oksvg (e.g. Dolphin): the generic icon
  is shown.
- A title that keeps changing (e.g. an animated terminal) updates its entry
  on every change; the menu is only rebuilt when windows appear or disappear.

## AI disclosure

This project was developed with the help of an AI: **Claude Opus 5.5**
(Anthropic), through Claude Code. The AI wrote most of the code, the KWin
script, the Makefile and this documentation, from the author's specifications
and under their direction; the author approved the plan, tested every step on
their machine and decided how the project evolves. The related commits carry a
`Co-Authored-By: Claude` trailer. `AGENTS.md` and `CLAUDE.md` describe the
project for coding agents.

## License

XorWay is free software: you can redistribute it and/or modify it under the
terms of the **GNU Affero General Public License** as published by the Free
Software Foundation, either version 3 of the License, or (at your option) any
later version (`AGPL-3.0-or-later`). See [`LICENSE`](LICENSE).
