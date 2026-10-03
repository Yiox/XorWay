// SPDX-License-Identifier: AGPL-3.0-or-later

package tray

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// Icons are drawn at runtime to keep binary files out of the repository: a
// colored disc carrying a letter drawn with white strokes.

const iconSize = 64

type point struct{ x, y float64 }

type iconState int

const (
	stateNative   iconState = iota // no XWayland window
	stateXWayland                  // at least one XWayland window
	stateError                     // backend unavailable
)

var (
	colorNative   = color.NRGBA{0x2e, 0x9e, 0x5b, 0xff}
	colorXWayland = color.NRGBA{0xe8, 0x71, 0x0a, 0xff}
	colorError    = color.NRGBA{0x80, 0x80, 0x80, 0xff}
)

// Strokes (polylines) of each glyph, in 64×64 coordinates.
var glyphs = map[iconState][][]point{
	stateNative:   {{{16, 22}, {24, 44}, {32, 29}, {40, 44}, {48, 22}}}, // W
	stateXWayland: {{{21, 21}, {43, 43}}, {{43, 21}, {21, 43}}},         // X
	stateError:    {{{32, 17}, {32, 36}}, {{32, 45}, {32, 46}}},         // !
}

var icons = map[iconState][]byte{}

func init() {
	for st, col := range map[iconState]color.NRGBA{
		stateNative: colorNative, stateXWayland: colorXWayland, stateError: colorError,
	} {
		icons[st] = renderIcon(col, glyphs[st])
	}
}

func renderIcon(bg color.NRGBA, strokes [][]point) []byte {
	const (
		radius    = 30.0
		halfWidth = 3.6
		samples   = 4 // 4×4 supersampling for anti-aliasing
	)
	img := image.NewNRGBA(image.Rect(0, 0, iconSize, iconSize))
	c := point{iconSize / 2, iconSize / 2}

	for py := 0; py < iconSize; py++ {
		for px := 0; px < iconSize; px++ {
			var disc, glyph int
			for sy := 0; sy < samples; sy++ {
				for sx := 0; sx < samples; sx++ {
					p := point{
						float64(px) + (float64(sx)+0.5)/samples,
						float64(py) + (float64(sy)+0.5)/samples,
					}
					if math.Hypot(p.x-c.x, p.y-c.y) > radius {
						continue
					}
					disc++
					if onStrokes(p, strokes, halfWidth) {
						glyph++
					}
				}
			}
			if disc == 0 {
				continue
			}
			// Blend background/white according to glyph coverage.
			g := float64(glyph) / float64(disc)
			mix := func(v uint8) uint8 { return uint8(float64(v)*(1-g) + 255*g) }
			img.SetNRGBA(px, py, color.NRGBA{
				R: mix(bg.R), G: mix(bg.G), B: mix(bg.B),
				A: uint8(255 * disc / (samples * samples)),
			})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // cannot happen with an in-memory NRGBA
	}
	return buf.Bytes()
}

func onStrokes(p point, strokes [][]point, halfWidth float64) bool {
	for _, s := range strokes {
		for i := 0; i+1 < len(s); i++ {
			if distToSegment(p, s[i], s[i+1]) <= halfWidth {
				return true
			}
		}
	}
	return false
}

func distToSegment(p, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := ((p.x-a.x)*dx + (p.y-a.y)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}
