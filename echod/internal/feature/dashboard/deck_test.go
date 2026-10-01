//go:build !dot

package dashboard

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

// A deck whose connection is down keeps its last picture up, greyed; a live one is drawn as it is.
// The dashboard's own stream is never greyed.
func TestOfflineDeckIsGreyed(t *testing.T) {
	f := &Feature{}
	frame := image.NewRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(frame, frame.Rect, image.NewUniform(color.RGBA{200, 200, 200, 255}), image.Point{}, draw.Src)
	f.deckStream = &stream{f: f, w: 4, h: 4, deck: true, frame: frame, view: View{Ready: true}}
	f.stream = &stream{f: f, w: 4, h: 4, frame: frame, view: View{Ready: true, Offline: true}}

	dst := image.NewRGBA(frame.Rect)
	if !f.DrawDeck(dst) || dst.RGBAAt(1, 1).R != 200 {
		t.Fatalf("a live deck was not drawn as it is: %v", dst.RGBAAt(1, 1))
	}
	f.deckStream.view.Offline = true
	if !f.DrawDeck(dst) || dst.RGBAAt(1, 1).R >= 200 {
		t.Fatalf("an offline deck was not greyed: %v", dst.RGBAAt(1, 1))
	}
	if !f.DrawStream(dst) {
		t.Fatal("the dashboard stream was not drawn")
	}
}

func TestSetDeckRefusesANonAddress(t *testing.T) {
	if err := Get().SetDeck("not an address!", "k"); err == nil {
		t.Fatal("SetDeck kept something that is not an address")
	}
}
