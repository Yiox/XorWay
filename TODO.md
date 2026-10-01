# TODO

## Backend GNOME (Mutter)

- [ ] Extension GNOME Shell (GJS, GNOME 45+, modules ESM) `xorway@xorway.org` :
  - service D-Bus `org.xorway.Windows` sur `/org/xorway/Windows` ;
  - méthode `GetWindows()` (JSON) et signal `WindowsChanged` ;
  - type de client via `Meta.Window.get_client_type()` (`WAYLAND` / `X11`) ;
  - événements : `display::window-created`, `unmanaged`, `notify::title`.
- [ ] Package `internal/backend/gnome` : client D-Bus, suivi de
  `NameOwnerChanged`, message clair si l'extension n'est pas activée.
- [ ] Détection `GNOME` dans `XDG_CURRENT_DESKTOP`.
- [ ] Makefile : installer l'extension dans
  `~/.local/share/gnome-shell/extensions/`.
- [ ] README : extension AppIndicator obligatoire (pas de systray natif),
  activation de l'extension, déconnexion/reconnexion requise.
- [ ] Ne pas se connecter à Xwayland sous GNOME (cela démarrerait Xwayland à
  la demande) : la détection de jeux ne doit pas en dépendre.

## Idées

- [ ] Icônes : `_NET_WM_ICON` pour les fenêtres X11 sans fichier .desktop.
- [ ] Icônes : moteur SVG plus complet (certains SVG, comme celui de Dolphin,
  ne sont pas pris en charge par oksvg).
