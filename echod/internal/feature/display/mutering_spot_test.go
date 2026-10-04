//go:build spot

package display

import (
	"image"
	"image/color"
	"testing"
	"time"
)

// The subtle muted ring is thinner than the full one and darker, and both still say muted.
func TestSubtleMuteRingIsThinAndDim(t *testing.T) {
	at := time.Date(2026, 10, 4, 22, 0, 0, 0, time.Local)
	draw := func(subtle bool) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, side, side))
		newRoundRenderer(img).draw(roundScene{now: at, phase: "idle", muted: true, mutedSubtle: subtle})
		return img
	}
	full, soft := draw(false), draw(true)
	inner := (rimIn + rimOut - mutedSoftWidth) / 2 // in the full ring, inside the subtle one
	edge := rimOut - 2                             // in both
	at2 := func(img *image.RGBA, r int) color.RGBA { return img.RGBAAt(center, center-r) }

	if c := at2(full, inner); c != colMuted {
		t.Errorf("full ring at %d from the center is %v, want %v", inner, c, colMuted)
	}
	if c := at2(soft, inner); c == colMuted || c == colMutedSoft {
		t.Errorf("subtle ring reaches %d from the center (%v), the full ring's width", inner, c)
	}
	if c := at2(soft, edge); c != colMutedSoft {
		t.Errorf("subtle ring at its edge is %v, want %v", c, colMutedSoft)
	}
	if c := at2(full, edge); c != colMuted {
		t.Errorf("full ring at its edge is %v, want %v", c, colMuted)
	}
	if colMutedSoft.R >= colMuted.R || colMutedSoft.R <= colMutedSoft.G {
		t.Errorf("subtle red %v is not a dimmer red than %v", colMutedSoft, colMuted)
	}
}
