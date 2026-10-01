# XorWay

XorWay affiche dans le systray quelles fenêtres tournent sous **XWayland** et
lesquelles sont en **Wayland natif**, dans une session Wayland.

- Icône verte « W » : aucune fenêtre XWayland.
- Icône orange « X » : au moins une fenêtre XWayland est ouverte.
- Icône grise « ! » : backend indisponible (le menu explique pourquoi).

Le menu liste les fenêtres avec l'icône de leur application ; les jeux
détectés sont marqués 🎮. Quand un jeu s'ouvre sous XWayland, une notification
le signale et propose une piste pour le lancer en Wayland natif (option
« Notifier les jeux lancés sous XWayland » du menu, mémorisée dans
`~/.config/xorway/config.json`).

## Dépendances

- Go ≥ 1.22 (compilation uniquement, sans CGO)
- Une session Wayland avec Xwayland
- Un hôte StatusNotifierItem : natif sous KDE Plasma.

## Installation

### Depuis une Release GitHub (sans compiler)

Téléchargez la dernière version sur
<https://github.com/Yiox/XorWay/releases> :

- **binaire seul** (toutes distributions, sans root) :

  ```sh
  tar xzf xorway_*_linux_amd64.tar.gz
  ./xorway install            # ~/.local/bin, lanceur, démarrage automatique
  xorway                      # ou depuis le menu des applications
  xorway uninstall            # désinstallation (la configuration est gardée)
  ```

  `xorway install --no-autostart` n'active pas le démarrage automatique.
- **paquets** : `.rpm` (Fedora…), `.deb` (Debian, Ubuntu…), `.pkg.tar.zst`
  (Arch). Ils installent `/usr/bin/xorway`, le lanceur et
  `/etc/xdg/autostart/xorway.desktop` (pour tous les utilisateurs).

### Depuis les sources

```sh
make build      # produit bin/xorway
make install    # ~/.local/bin/xorway + ~/.config/autostart/xorway.desktop
make uninstall
```

`make install` copie aussi le fichier .desktop dans
`~/.local/share/applications/` : il sert à rattacher les notifications à
XorWay (réglables dans les paramètres de notification de Plasma).
`PREFIX` (défaut `~/.local`) et `DESTDIR` sont personnalisables.

## Utilisation

```sh
xorway [--backend auto|kwin|x11] [--interval 3s] [--debug]
xorway install [--no-autostart] | uninstall | version
```

En mode `auto`, le backend est choisi selon `XDG_CURRENT_DESKTOP` :
`KDE` → `kwin`, sinon `x11`. Une seule instance peut tourner à la fois.

## Backends

| Backend | Bureau | Fenêtres natives | Fenêtres XWayland |
|---------|--------|------------------|-------------------|
| `kwin`  | KDE Plasma 6 | script KWin | script KWin |
| `x11`   | tous   | non visibles     | `_NET_CLIENT_LIST` de Xwayland |

### KDE Plasma 6 (`kwin`)

Le daemon réserve le nom `org.xorway.Daemon` sur le bus de session, écrit le
script embarqué dans `$XDG_RUNTIME_DIR/xorway/xorway.js` et le charge via
`org.kde.KWin /Scripting`. Le script s'abonne à `workspace.windowAdded`,
`windowRemoved` et aux changements de titre, puis envoie la liste des fenêtres
au daemon avec `callDBus` (méthode `Update`, JSON). Seuls les appels venant de
KWin sont acceptés. Le script est déchargé à l'arrêt du daemon.

L'API de scripting KWin 6 ne documente **aucune propriété** distinguant X11 de
Wayland. En revanche, la conversion en chaîne d'un objet fenêtre donne sa
classe C++ : `KWin::X11Window` pour un client XWayland,
`KWin::XdgToplevelWindow` pour un client Wayland natif. XorWay s'appuie sur ce
comportement (vérifié sur KWin 6.7). Si une classe inconnue apparaît, le daemon
bascule sur un croisement avec `_NET_CLIENT_LIST` de Xwayland (même PID, ou
même classe et même titre) et l'indique dans le menu.

### Repli X11 (`x11`)

Le backend `x11` se connecte au `DISPLAY` de Xwayland : toute fenêtre listée
dans `_NET_CLIENT_LIST` est une fenêtre XWayland. Les fenêtres techniques
marquées `_NET_WM_STATE_SKIP_TASKBAR` (ex. *xwaylandvideobridge*) sont ignorées.
Le rafraîchissement se fait par événements (`PropertyNotify`), avec un polling
de secours toutes les 3 s.

## Icônes d'applications

L'icône est cherchée dans le fichier .desktop de l'application (par app id,
`StartupWMClass` ou suffixe de l'id), puis dans le thème d'icônes courant
(`kdeglobals`, défaut `breeze`), ses parents et `hicolor`, et enfin dans
`/usr/share/pixmaps`. Les jeux Steam (`steam_app_<id>`) utilisent
`steam_icon_<id>`. Les SVG sont rendus en pur Go (oksvg) ; à défaut, l'icône
générique du thème est utilisée. `XORWAY_ICON_THEME` force un thème.

## Détection des jeux

Une fenêtre est considérée comme un jeu si :

| Indice | Source affichée | Piste proposée |
|--------|-----------------|----------------|
| classe `steam_app_<id>`, ou `SteamAppId`/`SteamGameId` dans `/proc/<pid>/environ`, avec Wine/Proton | Steam (Proton) | `PROTON_ENABLE_WAYLAND=1 %command%` (Proton ≥ 10) |
| idem sans Wine | Steam | `SDL_VIDEODRIVER=wayland` |
| exécutable `wine*` / `*-preloader`, ou commande en `.exe` | Wine | pilote `winewayland` (Wine ≥ 9) |
| catégorie `Game` dans le fichier .desktop | catégorie Game | `SDL_VIDEODRIVER=wayland` |

Une seule notification est envoyée par jeu (par PID) tant qu'il reste ouvert.
Les jeux déjà ouverts au démarrage du daemon sont signalés une fois.

## Tester

```sh
make run                      # lance le daemon avec --debug
xeyes &                       # fenêtre XWayland : l'icône passe à l'orange
GDK_BACKEND=x11 gnome-text-editor &   # application GTK forcée en X11
QT_QPA_PLATFORM=xcb kwrite &          # application Qt forcée en X11
```

Simuler un jeu Steam (notification + 🎮 dans le menu) :

```sh
SteamAppId=4242 xeyes &
```

Vérifier le backend KDE :

```sh
busctl --user call org.kde.KWin /Scripting org.kde.kwin.Scripting isScriptLoaded s xorway
# → b true pendant que xorway tourne, b false après l'avoir quitté
```

## Publier une version

Les Releases sont produites par GoReleaser (`.goreleaser.yaml`) via GitHub
Actions (`.github/workflows/release.yml`) : il suffit de pousser un tag.

```sh
git tag -s v0.1.0 -m "XorWay 0.1.0"   # tag signé
git push origin v0.1.0
```

Le workflow vérifie le code (`gofmt`, `go vet`), compile pour amd64 et arm64
(sans CGO, version injectée via `-X main.version`), puis publie les archives,
les paquets et `checksums.txt`. Test local sans publication :
`goreleaser release --snapshot --clean` (résultat dans `dist/`).

## Limitations

- Le backend `x11` ne voit pas les fenêtres Wayland natives.
- Backend `kwin` : la classification repose sur le nom de classe C++ des
  fenêtres, un détail interne de KWin qui pourrait changer (repli automatique
  sur le croisement X11, moins fiable).
- Les fenêtres absentes de la barre des tâches (`skipTaskbar`) sont ignorées.
- Détection de jeux heuristique : une application Windows quelconque sous
  Wine est traitée comme un jeu ; un jeu natif hors Steam sans catégorie
  `Game` n'est pas reconnu.
- Certains SVG ne sont pas pris en charge par oksvg (ex. Dolphin) : l'icône
  générique est affichée.
- Un titre qui change en continu (ex. terminal avec animation) met à jour
  l'entrée correspondante à chaque changement ; le menu n'est reconstruit que
  quand des fenêtres apparaissent ou disparaissent.

## Divulgation IA

Ce projet a été développé avec l'assistance d'une IA : **Claude Opus 5.5**
(Anthropic), via Claude Code. L'IA a rédigé la majeure partie du code, du
script KWin, du Makefile et de cette documentation, à partir des
spécifications et sous la direction de l'auteur, qui a validé le plan, testé
chaque étape sur sa machine et décidé des évolutions. Les commits concernés
portent la mention `Co-Authored-By: Claude`. Les fichiers `AGENTS.md` et
`CLAUDE.md` décrivent le projet pour les agents de code.

## Licence

XorWay est un logiciel libre : vous pouvez le redistribuer et/ou le modifier
selon les termes de la **GNU Affero General Public License** telle que publiée
par la Free Software Foundation, version 3 de la licence, ou (à votre choix)
toute version ultérieure (`AGPL-3.0-or-later`). Voir le fichier
[`LICENSE`](LICENSE).
