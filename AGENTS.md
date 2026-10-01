# AGENTS.md — XorWay

Daemon Go : icône systray (StatusNotifierItem) listant les fenêtres Wayland
natives / XWayland, notifie quand un jeu tourne sous XWayland.
Cibles : KDE Plasma 6 (fait), GNOME (voir `TODO.md`). Lire ce fichier avant
d'ouvrir le code ; n'ouvrir que les fichiers concernés.

## Carte

| Chemin | Rôle |
|--------|------|
| `cmd/xorway/main.go` | flags, choix du backend (`XDG_CURRENT_DESKTOP`), type `app` : boucle `update()` (événements + debounce 200 ms + polling 3 s) → détection jeux → `alert` → `tray` |
| `internal/backend/backend.go` | interface `Backend` (`Start/Snapshot/Changes/Close`), `Window`, `Snapshot`, `ErrPending`, `ErrAlreadyRunning` |
| `internal/backend/kwin/` | charge `script/xorway.js` (embarqué) via `org.kde.KWin /Scripting` ; le script appelle `org.xorway.Daemon.Update(json)` ; repli croisement X11 |
| `internal/backend/x11only/` | backend de repli : XWayland seulement |
| `internal/x11/` | xgb : `_NET_CLIENT_LIST`, titres, `WM_CLASS`, PID, `SKIP_TASKBAR` ; `Monitor` avec reconnexion |
| `internal/tray/` | menu dbusmenu (fyne.io/systray), icônes d'état dessinées en Go (`icon.go`) |
| `internal/desktop/` | index des fichiers .desktop (Icon, Categories, StartupWMClass) |
| `internal/appicon/` | spec Icon Theme (`theme.go`) + rendu PNG/SVG (`appicon.go`) |
| `internal/game/` | heuristiques jeu : `steam_app_*`, `SteamAppId` (/proc environ), Wine, catégorie Game |
| `internal/alert/`, `internal/notify/` | dédoublonnage par PID + `org.freedesktop.Notifications` |
| `internal/config/` | `~/.config/xorway/config.json` (`notifyGames`) |

## Invariants / pièges

- **Pas de CGO** (`CGO_ENABLED=0`). **Go minimum 1.22** : ne pas monter une
  dépendance qui exige plus (`golang.org/x/image` épinglé en v0.24.0). Vérifier
  avec `GOTOOLCHAIN=go1.22.12 go build ./...`.
- KWin 6 n'expose aucune propriété X11/Wayland : on lit la classe C++ via
  `String(window)` → `KWin::X11Window` = XWayland. Classe inconnue → croisement X11.
- `Update` D-Bus n'accepte que l'expéditeur propriétaire de `org.kde.KWin`.
- dbusmenu : `_` = mnémonique → `escape()` dans `tray.go`.
- `systray.ResetMenu()` ferme les `ClickedCh` : les goroutines `listen` s'arrêtent seules.
- `tray.setMenu` met à jour sur place si la « forme » du menu est inchangée,
  sinon reconstruit. `Tray.Update` : une seule goroutine (la boucle).
- `backend.Window.Game` est rempli par `main`, jamais par les backends.
- Sous GNOME, ne pas se connecter à Xwayland (démarrage à la demande).

## Conventions

- Commentaires, messages d'erreur, UI, README, commits : **français** (accents corrects).
- En-tête `// SPDX-License-Identifier: AGPL-3.0-or-later` sur tout nouveau fichier source.
- Erreurs utilisateur lisibles : leur message est affiché tel quel dans le menu.
- `gofmt` + `go vet ./...` avant commit.

## Tester (session Plasma Wayland)

```sh
make build && ./bin/xorway --debug
xeyes &                  # fenêtre XWayland
SteamAppId=4242 xeyes &  # faux jeu Steam → notification
# lire le menu sans l'ouvrir :
N=$(busctl --user list --no-legend | awk -v p="$(pgrep -x xorway)" '$2==p && $1 ~ /^:/ {print $1; exit}')
busctl --user call "$N" /StatusNotifierItem/menu com.canonical.dbusmenu GetLayout iias -- 0 -1 0
busctl --user call org.kde.KWin /Scripting org.kde.kwin.Scripting isScriptLoaded s xorway
```

Arrêter avec `pkill -x xorway` (jamais `pkill -f xorway` : tue aussi le shell
appelant).
