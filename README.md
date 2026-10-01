# XorWay

XorWay affiche dans le systray quelles fenêtres tournent sous **XWayland** et
lesquelles sont en **Wayland natif**, dans une session Wayland.

- Icône verte « W » : aucune fenêtre XWayland.
- Icône orange « X » : au moins une fenêtre XWayland est ouverte.
- Icône grise « ! » : backend indisponible (le menu explique pourquoi).

## Dépendances

- Go ≥ 1.22 (compilation uniquement, sans CGO)
- Une session Wayland avec Xwayland
- Un hôte StatusNotifierItem : natif sous KDE Plasma.

## Installation

```sh
make build      # produit bin/xorway
make install    # ~/.local/bin/xorway + ~/.config/autostart/xorway.desktop
make uninstall
```

`PREFIX` (défaut `~/.local`) et `DESTDIR` sont personnalisables.

## Utilisation

```sh
xorway [--backend auto|x11] [--interval 3s] [--debug]
```

## Backends

| Backend | Bureau | Fenêtres natives | Fenêtres XWayland |
|---------|--------|------------------|-------------------|
| `x11`   | tous   | non visibles     | `_NET_CLIENT_LIST` de Xwayland |

Le backend `x11` se connecte au `DISPLAY` de Xwayland : toute fenêtre listée
dans `_NET_CLIENT_LIST` est une fenêtre XWayland. Les fenêtres techniques
marquées `_NET_WM_STATE_SKIP_TASKBAR` (ex. *xwaylandvideobridge*) sont ignorées.
Le rafraîchissement se fait par événements (`PropertyNotify`), avec un polling
de secours toutes les 3 s.

## Tester

```sh
make run                      # lance le daemon avec --debug
xeyes &                       # fenêtre XWayland : l'icône passe à l'orange
GDK_BACKEND=x11 gnome-text-editor &   # application GTK forcée en X11
QT_QPA_PLATFORM=xcb kwrite &          # application Qt forcée en X11
```

## Limitations

- Le backend `x11` ne voit pas les fenêtres Wayland natives.
