package clap

import (
	"math"
	"math/rand/v2"
	"testing"
)

// room is a block of a quiet room, and loud one of a clap or a voice at amplitude a.
func room(r *rand.Rand) []int16 {
	b := make([]int16, block)
	for i := range b {
		b[i] = int16(r.IntN(121) - 60)
	}
	return b
}

// thud is a block of a low sound at amplitude a: a knock, a footstep, a cup set down.
func thud(n, a int) []int16 {
	b := make([]int16, block)
	for i := range b {
		b[i] = int16(float64(a) * math.Sin(2*math.Pi*150*float64(n*block+i)/16000))
	}
	return b
}

// struck is a block of a hard thing struck, ringing at 2 kHz: bright enough for Normal, not for Strict.
func struck(n, a int) []int16 {
	b := make([]int16, block)
	for i := range b {
		b[i] = int16(float64(a) * math.Sin(2*math.Pi*2000*float64(n*block+i)/16000))
	}
	return b
}

func loud(r *rand.Rand, a int) []int16 {
	b := make([]int16, block)
	for i := range b {
		b[i] = int16(r.IntN(2*a+1) - a)
	}
	return b
}

// run plays a script to a detector after a second of the room: "." a block of room, "x" a block of clap
// loud, "q" a quieter clap, "b" a bounce a little quieter than "x", "k" a knock, "t" a hard desk struck,
// "r" a block rising (a voice starting), and answers how many double claps it heard, with Normal's limits.
func run(script string) int { n, _ := runWith(normal, script); return n }

// runWith is run with the limits given, and answers the detector too, for what it heard.
func runWith(l limits, script string) (int, *detector) {
	r := rand.New(rand.NewPCG(7, 8))
	d := newDetector(l)
	for range 100 {
		d.add(room(r))
	}
	n := 0
	for i, c := range script {
		b := room(r)
		switch c {
		case 'x':
			b = loud(r, 6000)
		case 'q':
			b = loud(r, 1200)
		case 'b':
			b = loud(r, 4000)
		case 't':
			b = struck(i, 6000)
		case 'k':
			b = thud(i, 8000)
		case 'r':
			b = loud(r, 250)
		}
		if d.add(b) {
			n++
		}
	}
	return n, d
}

func repeat(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

func TestADoubleClapIsHeard(t *testing.T) {
	gap := repeat(".", 30)
	if n := run("xx" + gap + "xx" + repeat(".", 60)); n != 1 {
		t.Errorf("a double clap heard %d times", n)
	}
}

func TestWhatIsNotADoubleClap(t *testing.T) {
	for name, script := range map[string]string{
		"one clap":          "xx" + repeat(".", 120),
		"three claps":       "xx" + repeat(".", 30) + "xx" + repeat(".", 20) + "xx" + repeat(".", 80),
		"too close":         "xx" + repeat(".", 5) + "xx" + repeat(".", 80),
		"too far apart":     "xx" + repeat(".", 100) + "xx" + repeat(".", 80),
		"somebody talking":  repeat("x", 50) + repeat(".", 30) + repeat("x", 50) + repeat(".", 80),
		"applause":          repeat("xx"+repeat(".", 12), 12) + repeat(".", 80),
		"a knock, twice":    "kk" + repeat(".", 30) + "kk" + repeat(".", 80),
		"a loud and a soft": "xx" + repeat(".", 30) + "qq" + repeat(".", 80),
		"no sharp start":    "rxx" + repeat(".", 30) + "rxx" + repeat(".", 80),
	} {
		if n := run(script); n != 0 {
			t.Errorf("%s heard as %d double claps", name, n)
		}
	}
}

// After a reset, what came before is not half of a double clap.
func TestResetForgetsTheFirstClap(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 1))
	d := newDetector(normal)
	for range 100 {
		d.add(room(r))
	}
	for _, c := range "xx" + repeat(".", 30) {
		b := room(r)
		if c == 'x' {
			b = loud(r, 6000)
		}
		d.add(b)
	}
	d.reset()
	heard := false
	for _, c := range "xx" + repeat(".", 60) {
		b := room(r)
		if c == 'x' {
			b = loud(r, 6000)
		}
		heard = heard || d.add(b)
	}
	if heard {
		t.Error("a clap before a reset made a double clap with one after")
	}
}

// Strict still hears two real claps, and leaves out the two things #373 found passing in a room with a hard
// desk: a hit and its quieter bounce, and a sound bright enough for Normal but not as bright as hands.
func TestStrictLeavesOutADeskButNotAClap(t *testing.T) {
	gap := repeat(".", 30)
	if n, _ := runWith(strict, "xx"+gap+"xx"+repeat(".", 60)); n != 1 {
		t.Errorf("Strict heard a double clap %d times", n)
	}
	for name, script := range map[string]string{
		"a hit and its bounce": "xx" + gap + "bb" + repeat(".", 60),
		"a hard desk, twice":   "tt" + gap + "tt" + repeat(".", 60),
	} {
		if n := run(script); n != 1 {
			t.Errorf("%s: Normal heard %d, want 1 (else this test is not testing Strict)", name, n)
		}
		if n, _ := runWith(strict, script); n != 0 {
			t.Errorf("%s: Strict heard %d double claps", name, n)
		}
	}
}

// What the two claps measured goes with the event: how loud, how bright, and how far apart.
func TestADoubleClapKeepsWhatItMeasured(t *testing.T) {
	_, d := runWith(normal, "xx"+repeat(".", 30)+"xx"+repeat(".", 60))
	p := d.heard
	if p.first.peak < 30 || p.second.peak < 30 {
		t.Errorf("peaks %.1f and %.1f dB over the room, want a clap's", p.first.peak, p.second.peak)
	}
	if p.first.bright < 1 || p.second.bright < 1 {
		t.Errorf("brightness %.2f and %.2f, want white noise's (about 1.4)", p.first.bright, p.second.bright)
	}
	if p.apart < twoApartMin || p.apart > twoApartMax {
		t.Errorf("%d blocks apart, want %d to %d", p.apart, twoApartMin, twoApartMax)
	}
}
