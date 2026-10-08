// Package artwork génère des vignettes de remplacement.
//
// Aucune récupération de favicon ni de visuel distant : c'est une source de
// latence, de fuites réseau et de cas particuliers pour un gain esthétique
// nul au premier lancement. On produit une vignette déterministe par jeu, ce
// qui donne une bibliothèque lisible immédiatement, et l'utilisateur peut
// toujours poser ses propres images dans grid/ ensuite.
package artwork

import (
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// Render produit une vignette aux dimensions demandées : dégradé vertical
// dérivé du nom, plus une bande diagonale pour éviter l'aplat.
func Render(name string, w, h int) *image.RGBA {
	top, bottom := palette(name)
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	for y := 0; y < h; y++ {
		t := 0.0
		if h > 1 {
			t = float64(y) / float64(h-1)
		}
		c := lerp(top, bottom, t)
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}

	band := float64(w) * 0.18
	period := float64(w) * 1.7
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			d := float64(x) + float64(y)*float64(w)/math.Max(1, float64(h))
			if math.Mod(d, period) < band {
				img.Set(x, y, lighten(img.RGBAAt(x, y), 0.10))
			}
		}
	}
	return img
}

// WriteFile écrit une vignette PNG.
func WriteFile(path, name string, w, h int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, Render(name, w, h))
}

// Grid écrit les vignettes de bibliothèque attendues par Steam dans
// userdata/<id>/config/grid/. Les noms de fichiers sont l'appid en décimal
// non signé, ce qui est le seul point de couplage entre nos raccourcis et
// les visuels.
func Grid(dir string, appID uint32, name string) ([]string, error) {
	specs := []struct {
		suffix string
		w, h   int
	}{
		{"p.png", 600, 900},      // cover portrait
		{".png", 920, 430},       // capsule large
		{"_hero.png", 1920, 620}, // bannière
		{"_icon.png", 256, 256},  // icône de bibliothèque
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	for _, s := range specs {
		p := filepath.Join(dir, fmt.Sprintf("%d%s", appID, s.suffix))
		if err := WriteFile(p, name, s.w, s.h); err != nil {
			return written, err
		}
		written = append(written, p)
	}
	return written, nil
}

// Icon écrit l'icône carrée référencée par le champ "icon" du raccourci et
// par l'entrée de bureau de l'AppImage.
func Icon(path, name string) error { return WriteFile(path, name, 256, 256) }

// ---------------------------------------------------------------------------

func palette(name string) (color.RGBA, color.RGBA) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	hue := float64(h.Sum32() % 360)
	// Teintes sombres mais saturées : lisibles sur le fond de la bibliothèque.
	return hsv(hue, 0.62, 0.30), hsv(hue, 0.45, 0.72)
}

func hsv(h, s, v float64) color.RGBA {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return color.RGBA{
		R: uint8((r + m) * 255),
		G: uint8((g + m) * 255),
		B: uint8((b + m) * 255),
		A: 255,
	}
}

func lerp(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 {
		return uint8(float64(x) + (float64(y)-float64(x))*t)
	}
	return color.RGBA{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 255}
}

func lighten(c color.RGBA, amount float64) color.RGBA {
	adj := func(x uint8) uint8 {
		v := float64(x) + (255-float64(x))*amount
		return uint8(math.Min(255, v))
	}
	return color.RGBA{R: adj(c.R), G: adj(c.G), B: adj(c.B), A: 255}
}
