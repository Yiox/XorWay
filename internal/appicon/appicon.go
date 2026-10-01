// SPDX-License-Identifier: AGPL-3.0-or-later

// Package appicon retrouve l'icône d'une application (fichier .desktop puis
// thème d'icônes freedesktop) et la rend en PNG carré, sans CGO.
package appicon

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "image/jpeg"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
	xdraw "golang.org/x/image/draw"

	"xorway/internal/desktop"
)

type key struct {
	app  string
	size int
}

type result struct {
	img image.Image
	png []byte
}

// Resolver résout et met en cache les icônes par app id et par taille.
type Resolver struct {
	idx   *desktop.Index
	bases []string
	chain []*theme

	mu    sync.Mutex
	cache map[key]*result // une entrée nil mémorise un échec
}

// New crée un résolveur pour le thème d'icônes courant.
func New(idx *desktop.Index) *Resolver {
	var bases []string
	if h, err := os.UserHomeDir(); err == nil {
		bases = append(bases, filepath.Join(h, ".icons"))
	}
	for _, d := range desktop.DataDirs() {
		bases = append(bases, filepath.Join(d, "icons"))
	}
	return &Resolver{
		idx:   idx,
		bases: bases,
		chain: themeChain(currentThemeName(), bases),
		cache: map[key]*result{},
	}
}

// Image renvoie l'icône de l'application, ou nil si introuvable.
func (r *Resolver) Image(appID string, size int) image.Image {
	if res := r.lookup(appID, size); res != nil {
		return res.img
	}
	return nil
}

// PNG renvoie l'icône encodée en PNG, ou nil si introuvable.
func (r *Resolver) PNG(appID string, size int) []byte {
	if res := r.lookup(appID, size); res != nil {
		return res.png
	}
	return nil
}

// genericKey est la clé de cache de l'icône générique.
const genericKey = "\x00generic"

// Generic renvoie l'icône générique d'application du thème, en PNG.
func (r *Resolver) Generic(size int) []byte {
	if res := r.lookup(genericKey, size); res != nil {
		return res.png
	}
	return nil
}

func (r *Resolver) lookup(appID string, size int) *result {
	if appID == "" {
		return nil
	}
	k := key{appID, size}
	r.mu.Lock()
	defer r.mu.Unlock()
	if res, ok := r.cache[k]; ok {
		return res
	}
	var res *result
	if img := r.resolve(appID, size); img != nil {
		var buf bytes.Buffer
		if png.Encode(&buf, img) == nil {
			res = &result{img: img, png: buf.Bytes()}
		}
	}
	r.cache[k] = res
	return res
}

// names liste les noms d'icône plausibles, du plus fiable au moins fiable.
func (r *Resolver) names(appID string) (names []string, file string) {
	add := func(n string) {
		for _, x := range names {
			if x == n {
				return
			}
		}
		if n != "" {
			names = append(names, n)
		}
	}
	if e := r.idx.Lookup(appID); e != nil && e.Icon != "" {
		if filepath.IsAbs(e.Icon) {
			file = e.Icon
		} else {
			add(e.Icon)
		}
	}
	lower := strings.ToLower(appID)
	// Steam installe les icônes de jeux sous le nom steam_icon_<appid>.
	if id, ok := strings.CutPrefix(lower, "steam_app_"); ok {
		add("steam_icon_" + id)
	}
	add(appID)
	add(lower)
	if i := strings.LastIndexByte(lower, '.'); i >= 0 {
		add(lower[i+1:])
	}
	return names, file
}

func (r *Resolver) resolve(appID string, size int) image.Image {
	if appID == genericKey {
		return r.resolveNames([]string{"application-x-executable", "applications-other"}, size)
	}
	names, file := r.names(appID)
	if file != "" {
		if img, err := load(file, size); err == nil {
			return img
		}
	}
	return r.resolveNames(names, size)
}

// resolveNames cherche chaque nom dans toute la chaîne de thèmes, puis dans
// /usr/share/pixmaps.
func (r *Resolver) resolveNames(names []string, size int) image.Image {
	for _, n := range names {
		for _, t := range r.chain {
			for _, p := range t.find(n, size) {
				if img, err := load(p, size); err == nil {
					return img
				}
			}
		}
		for _, ext := range []string{".png", ".svg"} {
			if img, err := load(filepath.Join("/usr/share/pixmaps", n+ext), size); err == nil {
				return img
			}
		}
	}
	return nil
}

// load lit un PNG/JPEG ou rasterise un SVG, au format size×size.
func load(path string, size int) (image.Image, error) {
	if strings.EqualFold(filepath.Ext(path), ".svg") {
		return rasterSVG(path, size)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return fit(src, size), nil
}

// fit redimensionne en conservant les proportions, centré dans un carré.
func fit(src image.Image, size int) image.Image {
	b := src.Bounds()
	if b.Dx() == size && b.Dy() == size {
		return src
	}
	w, h := size, size
	if b.Dx() > b.Dy() {
		h = b.Dy() * size / b.Dx()
	} else if b.Dy() > b.Dx() {
		w = b.Dx() * size / b.Dy()
	}
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := image.Rect((size-w)/2, (size-h)/2, (size-w)/2+w, (size-h)/2+h)
	xdraw.CatmullRom.Scale(dst, r, src, b, xdraw.Over, nil)
	return dst
}

func rasterSVG(path string, size int) (img image.Image, err error) {
	// oksvg peut paniquer sur certains SVG exotiques.
	defer func() {
		if p := recover(); p != nil {
			img, err = nil, fmt.Errorf("svg %s : %v", path, p)
		}
	}()
	icon, err := oksvg.ReadIcon(path, oksvg.IgnoreErrorMode)
	if err != nil {
		return nil, err
	}
	icon.SetTarget(0, 0, float64(size), float64(size))
	rgba := image.NewRGBA(image.Rect(0, 0, size, size))
	scanner := rasterx.NewScannerGV(size, size, rgba, rgba.Bounds())
	icon.Draw(rasterx.NewDasher(size, size, scanner), 1)
	if blank(rgba) {
		return nil, errors.New("svg non pris en charge (rendu vide)")
	}
	return rgba, nil
}

func blank(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			return false
		}
	}
	return true
}
