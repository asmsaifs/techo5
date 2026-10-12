//go:build spot

package display

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
)

// The doorbell pressed during a call, on the round call face: said between the title and the name.
// With SPOT_PREVIEW set, it is written there to look at.
func TestAtTheDoorOnTheCallFace(t *testing.T) {
	at := time.Date(2026, 10, 10, 14, 7, 0, 0, time.Local)
	call := phone.State{Configured: true, Registered: true, Phase: phone.Talking, Peer: "Kitchen", Intercom: true, Since: at.Add(-75 * time.Second)}
	draw := func(atDoor bool) *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, side, side))
		newRoundRenderer(img).draw(roundScene{now: at, phase: "idle", call: call, atDoor: atDoor})
		return img
	}
	img, plain := draw(true), draw(false)
	differ := false
	for i := range img.Pix {
		differ = differ || img.Pix[i] != plain.Pix[i]
	}
	if !differ {
		t.Error("the face looks the same as a call with nobody at the door")
	}
	if dir := os.Getenv("SPOT_PREVIEW"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "call-at-door-spot.png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
	}
}
