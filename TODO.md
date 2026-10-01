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

## Organisation du dépôt (GitHub Flow)

- [ ] CI sur les Pull Requests et les pushs sur `main` (`.github/workflows/ci.yml`) :
  `gofmt`, `go vet`, build avec Go 1.22 (`GOTOOLCHAIN=go1.22.12`) ; à faire
  sur une branche `chore/ci-pr` pour tester le flux.
- [ ] Ruleset GitHub sur `main` : bloquer les force-push et la suppression,
  exiger les commits signés, exiger une PR (0 approbation), exiger le check
  de la CI une fois en place.
- [ ] Convention : `main` toujours publiable ; branches courtes `feat/`,
  `fix/`, `docs/`, `chore/` ; squash merge pour les petites branches, rebase
  merge pour les grosses ; tags signés `vX.Y.Z` posés uniquement sur `main`.
- [ ] Documenter le flux dans `AGENTS.md` (et `CONTRIBUTING.md` si des
  contributeurs arrivent).

## Idées

- [ ] Icônes : `_NET_WM_ICON` pour les fenêtres X11 sans fichier .desktop.
- [ ] Icônes : moteur SVG plus complet (certains SVG, comme celui de Dolphin,
  ne sont pas pris en charge par oksvg).


## Sécu

1. Balises dans les notifications. Plasma interprète un peu de HTML dans le texte des notifications (<a>, <img>, <b>…), et le titre d'une fenêtre y est inséré tel quel. Une fenêtre au titre piégé pourrait donc afficher un lien ou une image distante. La correction est simple : remplacer <, > et & avant l'envoi.
2. Dossier de repli dans /tmp. Le script KWin est écrit dans $XDG_RUNTIME_DIR (privé, droits 0700). Si cette variable était absente, il irait dans /tmp/xorway, qu'un autre utilisateur de la machine pourrait créer avant toi pour remplacer le script. En pratique, XDG_RUNTIME_DIR est toujours défini dans une session Wayland. Je propose de refuser de démarrer dans ce cas au lieu de passer par /tmp.
3. Actions GitHub référencées par version (@v7) plutôt que par identifiant de commit. Épingler par identifiant protège contre une action compromise en amont. C'est une bonne pratique, pas un problème actuel.
