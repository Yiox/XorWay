// SPDX-License-Identifier: AGPL-3.0-or-later

// Package notify sends desktop notifications through
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

// Notification describes a notification to show.
type Notification struct {
	Summary string
	Body    string
	Icon    string      // theme icon name (fallback when Image is nil)
	Image   image.Image // image shown in the notification (optional)
}

// Client talks to the desktop notification server.
type Client struct {
	conn *dbus.Conn
}

// New opens a dedicated session bus connection.
func New() (*Client, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, fmt.Errorf("notifications: %w", err)
	}
	return &Client{conn: conn}, nil
}

// imageData is the format of the "image-data" hint: (iiibiiay).
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

// Send shows the notification.
func (c *Client) Send(n Notification) error {
	hints := map[string]dbus.Variant{
		"desktop-entry": dbus.MakeVariant("xorway"),
		"urgency":       dbus.MakeVariant(byte(1)), // normal
	}
	if n.Image != nil {
		hints["image-data"] = dbus.MakeVariant(toImageData(n.Image))
	}
	obj := c.conn.Object(service, path)
	call := obj.Call(method, 0,
		"XorWay", uint32(0), n.Icon, n.Summary, n.Body,
		[]string{}, hints, int32(-1))
	if call.Err != nil {
		return fmt.Errorf("notifications: %w", call.Err)
	}
	return nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }
