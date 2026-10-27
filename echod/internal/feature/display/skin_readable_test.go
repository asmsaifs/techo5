//go:build !dot

package display

import (
	"image/color"
	"math"
	"testing"
)

// contrast is the WCAG contrast ratio of two colors.
func contrast(a, b color.RGBA) float64 {
	lum := func(c color.RGBA) float64 {
		ch := func(v uint8) float64 {
			x := float64(v) / 255
			if x <= 0.03928 {
				return x / 12.92
			}
			return math.Pow((x+0.055)/1.055, 2.4)
		}
		return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
	}
	l1, l2 := lum(a), lum(b)
	return (math.Max(l1, l2) + 0.05) / (math.Min(l1, l2) + 0.05)
}

// The readable skins keep their words at 7:1 or more on their ground, quiet words included, and what
// they mark at 4.5:1.
func TestReadableSkinsContrast(t *testing.T) {
	for _, th := range []theme{prairieTheme, earthriseTheme, controlTheme, plainTheme} {
		c := th.colors
		for _, w := range []struct {
			role int
			min  float64
		}{{roleText, 7}, {roleDim, 7}, {roleAccent, 4.5}} {
			if got := contrast(c[w.role], c[roleGround]); got < w.min {
				t.Errorf("%s: role %d at %.1f:1, want %.1f", th.name, w.role, got, w.min)
			}
		}
	}
}

// A screen that wore one of the skins taken out wears its readable replacement.
func TestGoneSkinsReadAsTheirReplacements(t *testing.T) {
	t.Cleanup(func() { skinPreview = "" })
	for old, want := range map[string]string{"pipboy": skinControl, "amber": skinControl, "matrix": skinControl,
		"blueprint": skinPrairie, "gameboy": skinPlain, "lcars": skinLCARS} {
		skinPreview = old
		if got := skin(); got != want {
			t.Errorf("%q reads as %q, want %q", old, got, want)
		}
	}
}
