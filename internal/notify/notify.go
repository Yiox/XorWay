// SPDX-License-Identifier: AGPL-3.0-or-later

// Package notify envoie des notifications de bureau via
// org.freedesktop.Notifications.
package notify

import (
	"fmt"
	"image"
	"image/draw"

	"github.com/godbus/dbus/v5"
)

const (
	service = "org.freedesktop.Notifications"
	path    = "/org/freedesktop/Notifications"
	method  = service + ".Notify"
)

// Notification décrit une notification à afficher.
type Notification struct {
	Summary string
	Body    string
	Icon    string      // nom d'icône du thème (repli si Image est nil)
	Image   image.Image // image affichée dans la notification (optionnelle)
}

// Client parle au serveur de notifications du bureau.
type Client struct {
	conn *dbus.Conn
}

// New ouvre une connexion dédiée au bus de session.
func New() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("notifications : %w", err)
	}
	return &Client{conn: conn}, nil
}

// imageData est le format de l'indice « image-data » : (iiibiiay).
type imageData struct {
	Width, Height, Rowstride int32
	HasAlpha                 bool
	BitsPerSample, Channels  int32
	Data                     []byte
}

func toImageData(img image.Image) imageData {
	b := img.Bounds()
	rgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	return imageData{
		Width: int32(b.Dx()), Height: int32(b.Dy()), Rowstride: int32(rgba.Stride),
		HasAlpha: true, BitsPerSample: 8, Channels: 4, Data: rgba.Pix,
	}
}

// Send affiche la notification.
func (c *Client) Send(n Notification) error {
	hints := map[string]dbus.Variant{
		"desktop-entry": dbus.MakeVariant("xorway"),
		"urgency":       dbus.MakeVariant(byte(1)), // normale
	}
	if n.Image != nil {
		hints["image-data"] = dbus.MakeVariant(toImageData(n.Image))
	}
	obj := c.conn.Object(service, path)
	call := obj.Call(method, 0,
		"XorWay", uint32(0), n.Icon, n.Summary, n.Body,
		[]string{}, hints, int32(-1))
	if call.Err != nil {
		return fmt.Errorf("notifications : %w", call.Err)
	}
	return nil
}

// Close ferme la connexion.
func (c *Client) Close() error { return c.conn.Close() }
