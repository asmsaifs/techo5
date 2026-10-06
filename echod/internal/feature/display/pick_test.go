//go:build !dot && !spot

package display

import (
	"image"
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/touch"
)

// The left-edge swipe asks which page only when both are set up; with one it opens that one, and
// with neither it does nothing.
func TestOpenPage(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       config.DashboardMode
		deck       string
		wantOpened bool
		wantPick   bool
		wantDeck   bool
		wantDash   bool
	}{
		{"both", config.DashboardStreamed, "192.168.1.20:9555", true, true, false, false},
		{"dashboard only", config.DashboardStreamed, "", true, false, false, true},
		{"deck only", config.DashboardOff, "192.168.1.20:9555", true, false, true, false},
		{"neither", config.DashboardOff, "", false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config.Use(filepath.Join(t.TempDir(), "state.json"))
			if err := config.Set().Dashboard().Mode(tc.mode); err != nil {
				t.Fatal(err)
			}
			if err := config.Set().StreamDeck().Server(tc.deck, "k"); err != nil {
				t.Fatal(err)
			}
			d := &Display{poke: make(chan struct{}, 1)}
			if got := d.openPage(); got != tc.wantOpened {
				t.Errorf("openPage = %v, want %v", got, tc.wantOpened)
			}
			if d.pickUp() != tc.wantPick || d.streamDeck != tc.wantDeck || d.dash != tc.wantDash {
				t.Errorf("pick %v deck %v dash %v, want %v %v %v", d.pickUp(), d.streamDeck, d.dash, tc.wantPick, tc.wantDeck, tc.wantDash)
			}
		})
	}
}

// A tap on a button of the chooser opens that page, a tap elsewhere only puts the chooser away.
func TestPickGesture(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	_ = config.Set().Dashboard().Mode(config.DashboardStreamed)
	_ = config.Set().StreamDeck().Server("192.168.1.20:9555", "k")
	for _, tc := range []struct {
		name      string
		at        func(r *renderer) image.Point
		dash, dck bool
	}{
		{"dashboard button", func(r *renderer) image.Point { b, _ := r.pickButtons(); return b.Min.Add(image.Pt(5, 5)) }, true, false},
		{"deck button", func(r *renderer) image.Point { _, b := r.pickButtons(); return b.Min.Add(image.Pt(5, 5)) }, false, true},
		{"elsewhere", func(*renderer) image.Point { return image.Pt(2, 2) }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Display{r: newRenderer(image.NewRGBA(image.Rect(0, 0, 960, 480))), poke: make(chan struct{}, 1)}
			d.openPage()
			p := tc.at(d.r)
			if !d.pickGesture(touch.Gesture{Kind: touch.Tap, X: p.X, Y: p.Y}) {
				t.Fatal("the chooser did not take a tap")
			}
			if d.pickUp() || d.dash != tc.dash || d.streamDeck != tc.dck {
				t.Errorf("pick %v dash %v deck %v, want false %v %v", d.pickUp(), d.dash, d.streamDeck, tc.dash, tc.dck)
			}
		})
	}
}

// The buttons sit inside the card and do not overlap.
func TestPickButtonsFit(t *testing.T) {
	r := newRenderer(image.NewRGBA(image.Rect(0, 0, 960, 480)))
	dash, deck := r.pickButtons()
	box := r.pickBox()
	if !dash.In(box) || !deck.In(box) || !dash.Intersect(deck).Empty() {
		t.Fatalf("buttons %v %v in card %v", dash, deck, box)
	}
}
