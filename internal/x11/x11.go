// SPDX-License-Identifier: AGPL-3.0-or-later

// Package x11 liste les fenêtres gérées par le serveur Xwayland.
//
// Sous une session Wayland, tout client présent dans _NET_CLIENT_LIST du
// DISPLAY Xwayland est, par définition, une fenêtre XWayland.
package x11

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// Client décrit une fenêtre X11 de premier niveau.
type Client struct {
	ID       uint32
	Title    string
	Instance string // première partie de WM_CLASS (res_name)
	Class    string // seconde partie de WM_CLASS (res_class)
	PID      int    // 0 si _NET_WM_PID est absent
	// SkipTaskbar indique une fenêtre technique, absente de la barre des
	// tâches (ex. xwaylandvideobridge).
	SkipTaskbar bool
}

// ErrNoDisplay est renvoyée quand DISPLAY n'est pas défini.
var ErrNoDisplay = errors.New("x11: DISPLAY non défini (Xwayland désactivé ?)")

var atomNames = []string{
	"_NET_CLIENT_LIST",
	"_NET_WM_NAME",
	"_NET_WM_PID",
	"_NET_WM_STATE",
	"_NET_WM_STATE_SKIP_TASKBAR",
	"UTF8_STRING",
}

// conn encapsule une connexion xgb et les atomes utiles.
type conn struct {
	x     *xgb.Conn
	root  xproto.Window
	atoms map[string]xproto.Atom
}

func dial(display string) (*conn, error) {
	if display == "" {
		return nil, ErrNoDisplay
	}
	x, err := xgb.NewConnDisplay(display)
	if err != nil {
		return nil, fmt.Errorf("x11: connexion à %s: %w", display, err)
	}
	c := &conn{
		x:     x,
		root:  xproto.Setup(x).DefaultScreen(x).Root,
		atoms: make(map[string]xproto.Atom, len(atomNames)),
	}
	for _, name := range atomNames {
		r, err := xproto.InternAtom(x, false, uint16(len(name)), name).Reply()
		if err != nil {
			x.Close()
			return nil, fmt.Errorf("x11: InternAtom %s: %w", name, err)
		}
		c.atoms[name] = r.Atom
	}
	return c, nil
}

func (c *conn) close() { c.x.Close() }

// listen demande à recevoir les PropertyNotify d'une fenêtre.
// La requête n'est pas vérifiée : la fenêtre peut disparaître entre-temps.
func (c *conn) listen(w xproto.Window) {
	xproto.ChangeWindowAttributes(c.x, w, xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange})
}

func (c *conn) property(w xproto.Window, atom xproto.Atom) (*xproto.GetPropertyReply, error) {
	return xproto.GetProperty(c.x, false, w, atom, xproto.GetPropertyTypeAny, 0, 1<<16).Reply()
}

func (c *conn) clientIDs() ([]xproto.Window, error) {
	r, err := c.property(c.root, c.atoms["_NET_CLIENT_LIST"])
	if err != nil {
		return nil, fmt.Errorf("x11: lecture de _NET_CLIENT_LIST: %w", err)
	}
	if r.Format != 32 {
		return nil, nil
	}
	ids := make([]xproto.Window, 0, r.ValueLen)
	for i := 0; i+4 <= len(r.Value); i += 4 {
		ids = append(ids, xproto.Window(xgb.Get32(r.Value[i:])))
	}
	return ids, nil
}

func (c *conn) client(w xproto.Window) Client {
	cl := Client{ID: uint32(w)}

	if r, err := c.property(w, c.atoms["_NET_WM_NAME"]); err == nil && len(r.Value) > 0 {
		cl.Title = string(r.Value)
	} else if r, err := c.property(w, xproto.AtomWmName); err == nil {
		cl.Title = string(r.Value)
	}

	if r, err := c.property(w, xproto.AtomWmClass); err == nil {
		parts := bytes.Split(bytes.TrimRight(r.Value, "\x00"), []byte{0})
		if len(parts) > 0 {
			cl.Instance = string(parts[0])
		}
		if len(parts) > 1 {
			cl.Class = string(parts[1])
		}
	}

	if r, err := c.property(w, c.atoms["_NET_WM_PID"]); err == nil && r.Format == 32 && len(r.Value) >= 4 {
		cl.PID = int(xgb.Get32(r.Value))
	}

	if r, err := c.property(w, c.atoms["_NET_WM_STATE"]); err == nil && r.Format == 32 {
		skip := uint32(c.atoms["_NET_WM_STATE_SKIP_TASKBAR"])
		for i := 0; i+4 <= len(r.Value); i += 4 {
			if xgb.Get32(r.Value[i:]) == skip {
				cl.SkipTaskbar = true
			}
		}
	}

	cl.Title = strings.TrimSpace(cl.Title)
	return cl
}

func (c *conn) clients() ([]Client, error) {
	ids, err := c.clientIDs()
	if err != nil {
		return nil, err
	}
	out := make([]Client, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.client(id))
	}
	return out, nil
}
