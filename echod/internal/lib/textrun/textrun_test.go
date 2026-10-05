package textrun

import (
	"image"
	"image/png"
	"os"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func testFace(t *testing.T, size float64) font.Face {
	t.Helper()
	f, err := opentype.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	fc, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	return New(fc, size, false)
}

func TestFontsLoad(t *testing.T) {
	regular, bold := fonts()
	if regular == nil || bold == nil {
		t.Fatal("the Bengali fonts did not parse")
	}
	if emojiFont() == nil {
		t.Fatal("the emoji font did not parse")
	}
}

// Every Bengali letter has a glyph: none shapes to .notdef, the box.
func TestNoBoxes(t *testing.T) {
	face := testFace(t, 42).(*Face)
	for _, s := range []string{"আমি তোমাকে ভালোবাসি", "ক্ষমা", "স্বাগতম।", "আজকের আবহাওয়া কেমন?", "০১২৩৪৫৬৭৮৯", "র‍্যাব"} {
		for _, bold := range []bool{false, true} {
			for _, rn := range face.runs(s) {
				if rn.k != bengali {
					continue
				}
				sh := shape(bengali, bold, fixed.I(42), rn.s)
				if sh == nil || len(sh.glyphs) == 0 {
					t.Fatalf("%q shaped to nothing", rn.s)
				}
				for _, g := range sh.glyphs {
					if g.GlyphID == 0 {
						t.Errorf("%q (bold %v) has a glyph missing", rn.s, bold)
					}
				}
			}
		}
	}
}

// কি is ক then the vowel sign ি in the text, but the sign is drawn first, to the consonant's left.
func TestPreBaseVowelReorders(t *testing.T) {
	sh := shape(bengali, false, fixed.I(42), "কি")
	if len(sh.glyphs) != 2 {
		t.Fatalf("got %d glyphs, want 2", len(sh.glyphs))
	}
	if sh.glyphs[0].ClusterIndex != 0 || sh.glyphs[1].ClusterIndex != 0 {
		t.Fatalf("the two should be one cluster: %+v", sh.glyphs)
	}
	f := fontFor(bengali, false)
	ka, _ := f.shape.NominalGlyph('ক')
	if sh.glyphs[0].GlyphID == ka {
		t.Error("ক came first; the vowel sign ি was not moved before it")
	}
}

// A conjunct is fewer glyphs than its letters: ক ্ ষ becomes one shape.
func TestConjunct(t *testing.T) {
	if sh := shape(bengali, false, fixed.I(42), "ক্ষ"); len(sh.glyphs) >= 3 {
		t.Errorf("ক্ষ came out as %d glyphs, not a conjunct", len(sh.glyphs))
	}
}

func TestMeasureAndDraw(t *testing.T) {
	face := testFace(t, 42)
	latin := Measure(face, "Hello")
	if latin != font.MeasureString(face, "Hello") {
		t.Error("Latin text measures differently through the wrapper")
	}
	mixed := Measure(face, "Hello আমি")
	if mixed <= latin {
		t.Errorf("mixed text %v not wider than its Latin part %v", mixed, latin)
	}
	b, adv := Bounds(face, "Hello আমি")
	if adv != mixed || b.Empty() {
		t.Errorf("bounds %v advance %v, measure %v", b, adv, mixed)
	}

	dst := image.NewRGBA(image.Rect(0, 0, 900, 80))
	Draw(dst, image.Black, face, "Hi আমি ক্ষমা কি 👍🏽 🇧🇩 ❤️ 1️⃣ ♪ 😂", fixed.P(10, 56))
	inked := 0
	for i := 3; i < len(dst.Pix); i += 4 {
		if dst.Pix[i] != 0 {
			inked++
		}
	}
	if inked == 0 {
		t.Fatal("nothing drawn")
	}
	if out := os.Getenv("TEXTRUN_PNG"); out != "" {
		w, _ := os.Create(out)
		png.Encode(w, dst)
		w.Close()
	}
}

// Each emoji is one run in the emoji font, and the ones made of several characters shape to one
// picture: a skin tone, a flag's two letters, a family's three people and their joiners.
func TestEmoji(t *testing.T) {
	face := testFace(t, 42).(*Face)
	for _, c := range []struct {
		s      string
		glyphs int
	}{
		{"😀", 1}, {"👍🏽", 1}, {"🇧🇩", 1}, {"👨‍👩‍👧", 1}, {"❤️", 1}, {"1️⃣", 1}, {"🫨", 1},
	} {
		rs := face.runs(c.s)
		if len(rs) != 1 || rs[0].k != emoji {
			t.Errorf("%q split into %+v, not one emoji run", c.s, rs)
			continue
		}
		sh := shape(emoji, false, fixed.I(42), c.s)
		if sh == nil {
			t.Fatalf("%q shaped to nothing", c.s)
		}
		n := 0
		for _, g := range sh.glyphs {
			if g.GlyphID == 0 {
				t.Errorf("%q has a glyph missing", c.s)
			}
			if g.Advance != 0 {
				n++
			}
		}
		if n != c.glyphs {
			t.Errorf("%q is %d pictures, want %d", c.s, n, c.glyphs)
		}
	}
}

// What the Go font has stays in it, a ♪ or a ♥, unless an emoji selector asks for the picture;
// the Bengali joiner stays Bengali, and the emoji one stays in the emoji.
func TestEmojiKeepsTextSymbols(t *testing.T) {
	face := testFace(t, 42).(*Face)
	for s, want := range map[string][]kind{
		"♪ playing": {latin},
		"♥":         {latin},
		"♥️":        {emoji},
		"Hi 😀!":     {latin, emoji, latin},
		"র‍্যাব":    {bengali},
		"ok 👨‍👩‍👧":  {latin, emoji},
		"© 2026":    {latin},
	} {
		rs := face.runs(s)
		var got []kind
		for _, rn := range rs {
			got = append(got, rn.k)
		}
		if len(got) != len(want) {
			t.Errorf("%q ran as %v, want %v", s, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%q ran as %v, want %v", s, got, want)
				break
			}
		}
	}
}

func TestSpeakable(t *testing.T) {
	for in, want := range map[string]string{
		"Done 👍":                     "Done",
		"It's sunny ☀️ and warm 🌡️.": "It's sunny and warm.",
		"♪ playing":                  "♪ playing",
		"আমি 😀 ভালো":                 "আমি ভালো",
		"😂😂":                         "",
		"plain text":                 "plain text",
	} {
		if got := Speakable(in); got != want {
			t.Errorf("Speakable(%q) = %q, want %q", in, got, want)
		}
	}
}
