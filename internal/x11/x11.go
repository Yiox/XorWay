// SPDX-License-Identifier: AGPL-3.0-or-later

// Package x11 lists the windows managed by the Xwayland server.
//
// In a Wayland session, every client in _NET_CLIENT_LIST of the Xwayland
// DISPLAY is, by definition, an XWayland window.
package x11

import (
	"bytes"
	"strings"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"xorway/internal/i18n"
)

// Client describes a top-level X11 window.
type Client struct {
	ID       uint32
	Title    string
	Instance string // first part of WM_CLASS (res_name)
	Class    string // second part of WM_CLASS (res_class)
	PID      int    // 0 when _NET_WM_PID is missing
	// SkipTaskbar marks a technical window, hidden from the taskbar
	// (e.g. xwaylandvideobridge).
	SkipTaskbar bool
}

// ErrNoDisplay is returned when DISPLAY is not set.
var ErrNoDisplay = i18n.NewError("error.x11_no_display")

var atomNames = []string{
	"_NET_CLIENT_LIST",
	"_NET_WM_NAME",
	"_NET_WM_PID",
	"_NET_WM_STATE",
	"_NET_WM_STATE_SKIP_TASKBAR",
	"UTF8_STRING",
}

// conn wraps an xgb connection and the atoms we need.
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
		return nil, i18n.Wrap(err, "error.x11_connect", "Display", display)
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
			return nil, i18n.Wrap(err, "error.x11_connect", "Display", display)
		}
		c.atoms[name] = r.Atom
	}
	return c, nil
}

func (c *conn) close() { c.x.Close() }

// listen subscribes to PropertyNotify events of a window. The request is
// unchecked: the window may disappear in the meantime.
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
		return nil, i18n.Wrap(err, "error.x11_client_list")
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
