// SPDX-License-Identifier: AGPL-3.0-or-later

package x11

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jezek/xgb/xproto"

	"xorway/internal/i18n"
)

// RetryInterval is the delay between two connection attempts to Xwayland.
const RetryInterval = 3 * time.Second

// ErrConnecting is returned by Clients before the first connection attempt.
var ErrConnecting = i18n.NewError("status.loading")

// errLost is reported after the connection to Xwayland was lost.
var errLost = i18n.NewError("error.x11_lost")

// Monitor keeps a connection to Xwayland (reconnecting automatically) and
// reports changes to the window list or to window titles.
type Monitor struct {
	display string
	changes chan struct{}

	mu   sync.Mutex
	conn *conn
	err  error
}

// NewMonitor creates a monitor for the given DISPLAY (e.g. ":0").
func NewMonitor(display string) *Monitor {
	return &Monitor{
		display: display,
		changes: make(chan struct{}, 1),
		err:     ErrConnecting,
	}
}

// Changes receives a (coalesced) value on every detected change.
func (m *Monitor) Changes() <-chan struct{} { return m.changes }

// Clients returns the X11 windows currently managed.
func (m *Monitor) Clients() ([]Client, error) {
	m.mu.Lock()
	c, err := m.conn, m.err
	m.mu.Unlock()
	if c == nil {
		return nil, err
	}
	return c.clients()
}

// Run connects to Xwayland and listens to events until the context is
// cancelled. It retries after a failure or a lost connection.
func (m *Monitor) Run(ctx context.Context) {
	for {
		c, err := dial(m.display)
		m.set(c, err)
		if err != nil {
			slog.Debug("x11: cannot connect", "err", err)
			if errors.Is(err, ErrNoDisplay) {
				return
			}
		} else {
			slog.Debug("x11: connected", "display", m.display)
			m.watch(ctx, c)
			m.set(nil, errLost)
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

// listenClients subscribes to property changes of every client, to be
// notified of title changes.
func listenClients(c *conn) {
	ids, err := c.clientIDs()
	if err != nil {
		return
	}
	for _, id := range ids {
		c.listen(id)
	}
}

// watch handles X events until the connection is closed.
func (m *Monitor) watch(ctx context.Context, c *conn) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			c.close() // unblocks WaitForEvent
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
			return // connection closed
		}
		if xerr != nil {
			// Typically BadWindow on a destroyed window: harmless.
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
