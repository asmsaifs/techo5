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
}

// Every Bengali letter has a glyph: none shapes to .notdef, the box.
func TestNoBoxes(t *testing.T) {
	for _, s := range []string{"আমি তোমাকে ভালোবাসি", "ক্ষমা", "স্বাগতম।", "আজকের আবহাওয়া কেমন?", "০১২৩৪৫৬৭৮৯", "র‍্যাব"} {
		for _, bold := range []bool{false, true} {
			for _, rn := range runs(s) {
				if !rn.bengali {
					continue
				}
				sh := shape(bold, fixed.I(42), rn.s)
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
	sh := shape(false, fixed.I(42), "কি")
	if len(sh.glyphs) != 2 {
		t.Fatalf("got %d glyphs, want 2", len(sh.glyphs))
	}
	if sh.glyphs[0].ClusterIndex != 0 || sh.glyphs[1].ClusterIndex != 0 {
		t.Fatalf("the two should be one cluster: %+v", sh.glyphs)
	}
	f := fontFor(false)
	ka, _ := f.shape.NominalGlyph('ক')
	if sh.glyphs[0].GlyphID == ka {
		t.Error("ক came first; the vowel sign ি was not moved before it")
	}
}

// A conjunct is fewer glyphs than its letters: ক ্ ষ becomes one shape.
func TestConjunct(t *testing.T) {
	if sh := shape(false, fixed.I(42), "ক্ষ"); len(sh.glyphs) >= 3 {
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

	dst := image.NewRGBA(image.Rect(0, 0, 640, 80))
	Draw(dst, image.Black, face, "Hi আমি ক্ষমা কি", fixed.P(10, 56))
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
