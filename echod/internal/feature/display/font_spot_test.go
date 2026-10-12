//go:build spot

package display

import (
	"bytes"
	"image"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/fonts"
)

// A new font is drawn from the next frame on, with no restart: the round renderer makes its faces
// again, the settings screen's with them, and so does a font of one's own leaving the folder.
func TestTheSpotTakesANewFontAtOnce(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	ownFontDir(t)
	fonts.Rescan()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	r := newRoundRenderer(img)
	s := roundScene{now: time.Date(2026, 9, 16, 14, 7, 0, 0, time.Local), phase: "idle"}
	r.draw(s)
	before := bytes.Clone(img.Pix)
	clock, sheet := r.clock, r.fc

	if err := config.Set().Screen().Font("museomoderno"); err != nil {
		t.Fatal(err)
	}
	r.draw(s)
	if r.font != "museomoderno" || r.clock == clock || r.fc == sheet || bytes.Equal(before, img.Pix) {
		t.Error("the frame after the change is not in the new font")
	}
	gen := r.fontGen
	if err := fonts.RemoveOwn("extra.ttf"); err != nil {
		t.Fatal(err)
	}
	r.draw(s)
	if r.fontGen == gen {
		t.Error("removing a font of one's own did not make the faces again")
	}
	_ = config.Set().Screen().Font("")
}
