// SPDX-License-Identifier: AGPL-3.0-or-later

package x11

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jezek/xgb/xproto"
)

// RetryInterval est le délai entre deux tentatives de connexion à Xwayland.
const RetryInterval = 3 * time.Second

// ErrConnecting est renvoyée par Clients avant la première tentative de
// connexion.
var ErrConnecting = errors.New("x11: connexion en cours")

// Monitor maintient une connexion à Xwayland (avec reconnexion automatique)
// et signale les changements de la liste des fenêtres ou de leurs titres.
type Monitor struct {
	display string
	changes chan struct{}

	mu   sync.Mutex
	conn *conn
	err  error
}

// NewMonitor crée un moniteur pour le DISPLAY donné (ex. ":0").
func NewMonitor(display string) *Monitor {
	return &Monitor{
		display: display,
		changes: make(chan struct{}, 1),
		err:     ErrConnecting,
	}
}

// Changes reçoit une valeur (coalescée) à chaque changement détecté.
func (m *Monitor) Changes() <-chan struct{} { return m.changes }

// Clients renvoie les fenêtres X11 actuellement gérées.
func (m *Monitor) Clients() ([]Client, error) {
	m.mu.Lock()
	c, err := m.conn, m.err
	m.mu.Unlock()
	if c == nil {
		return nil, err
	}
	return c.clients()
}

// Run se connecte à Xwayland et écoute les événements jusqu'à l'annulation
// du contexte. En cas d'échec ou de perte de connexion, il réessaie.
func (m *Monitor) Run(ctx context.Context) {
	for {
		c, err := dial(m.display)
		m.set(c, err)
		if err != nil {
			slog.Debug("x11: connexion impossible", "err", err)
			if errors.Is(err, ErrNoDisplay) {
				return
			}
		} else {
			slog.Debug("x11: connecté", "display", m.display)
			m.watch(ctx, c)
			m.set(nil, errors.New("x11: connexion à Xwayland perdue"))
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(RetryInterval):
		}
	}
}

func (m *Monitor) set(c *conn, err error) {
	m.mu.Lock()
	m.conn, m.err = c, err
	m.mu.Unlock()
	m.notify()
}

func (m *Monitor) notify() {
	select {
	case m.changes <- struct{}{}:
	default:
	}
}

// listenClients s'abonne aux changements de propriétés de chaque client,
// pour être prévenu des changements de titre.
func listenClients(c *conn) {
	ids, err := c.clientIDs()
	if err != nil {
		return
	}
	for _, id := range ids {
		c.listen(id)
	}
}

// watch traite les événements X jusqu'à la fermeture de la connexion.
func (m *Monitor) watch(ctx context.Context, c *conn) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.close() // débloque WaitForEvent
		case <-done:
		}
	}()

	c.listen(c.root)
	listenClients(c)

	clientList := c.atoms["_NET_CLIENT_LIST"]
	netWMName := c.atoms["_NET_WM_NAME"]

	for {
		ev, xerr := c.x.WaitForEvent()
		if ev == nil && xerr == nil {
			return // connexion fermée
		}
		if xerr != nil {
			// Typiquement BadWindow sur une fenêtre détruite : sans gravité.
			continue
		}
		pe, ok := ev.(xproto.PropertyNotifyEvent)
		if !ok {
			continue
		}
		switch {
		case pe.Window == c.root && pe.Atom == clientList:
			listenClients(c)
			m.notify()
		case pe.Window != c.root && (pe.Atom == netWMName || pe.Atom == xproto.AtomWmName || pe.Atom == xproto.AtomWmClass):
			m.notify()
		}
	}
}
