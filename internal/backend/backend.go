// Package backend définit l'interface commune aux sources de fenêtres
// (KWin, GNOME Shell, X11 seul).
package backend

import "context"

// ClientType indique le protocole utilisé par une fenêtre.
type ClientType int

const (
	Wayland ClientType = iota
	XWayland
)

func (t ClientType) String() string {
	switch t {
	case Wayland:
		return "Wayland"
	case XWayland:
		return "XWayland"
	default:
		return "inconnu"
	}
}

// Window décrit une fenêtre de premier niveau.
type Window struct {
	ID    string
	Title string
	AppID string // app_id Wayland, fichier .desktop ou classe WM_CLASS
	PID   int
	Type  ClientType
}

// Snapshot est l'état des fenêtres à un instant donné.
type Snapshot struct {
	Windows []Window
	// NativeKnown vaut false si le backend ne peut pas voir les fenêtres
	// Wayland natives (backend X11 seul).
	NativeKnown bool
	// Notice est un message d'information optionnel affiché dans le menu.
	Notice string
}

// Count renvoie le nombre de fenêtres du type donné.
func (s Snapshot) Count(t ClientType) int {
	n := 0
	for _, w := range s.Windows {
		if w.Type == t {
			n++
		}
	}
	return n
}

// Backend est une source de fenêtres.
type Backend interface {
	// Name est un nom court, affiché dans le menu.
	Name() string
	// Start initialise le backend. Les traitements de fond s'arrêtent à
	// l'annulation du contexte.
	Start(ctx context.Context) error
	// Snapshot renvoie l'état courant. Une erreur signifie que le backend
	// est indisponible ; son message est affiché tel quel à l'utilisateur.
	Snapshot() (Snapshot, error)
	// Changes reçoit une valeur quand l'état a pu changer.
	Changes() <-chan struct{}
	// Close libère les ressources (script KWin, connexions…).
	Close() error
}
