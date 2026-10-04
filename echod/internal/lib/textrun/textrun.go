// Package textrun draws text that may hold Bengali (Bangla) words among the Latin ones.
//
// The screens draw with the Go fonts, which have no Bengali letters, so a transcript or an answer
// in Bangla came out as a row of boxes. Letters alone would not be enough either: Bengali is
// written in clusters, where a vowel sign can sit before the consonant it follows in the text
// (কি is ক then ি, drawn ি first) and two consonants joined by a hasanta become one shape. So the
// Bengali stretches of a line are shaped, with go-text's port of HarfBuzz, in Noto Sans Bengali,
// and the rest is drawn by the Go font as before, untouched.
//
// Noto Sans Bengali is under the SIL Open Font License 1.1 (OFL.txt).
package textrun

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"sync"

	"github.com/go-text/typesetting/di"
	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

//go:embed NotoSansBengali-Regular.ttf
var regularTTF []byte

//go:embed NotoSansBengali-Bold.ttf
var boldTTF []byte

// Bengali reports whether r is written in the Bengali font: the Bengali block, the two dandas
// (shared with Devanagari, which the Go font lacks too), and the joiners that steer conjuncts.
func Bengali(r rune) bool {
	return (r >= 0x0980 && r <= 0x09FF) || r == 0x0964 || r == 0x0965 || r == 0x200C || r == 0x200D
}

// hasBengali reports whether s has anything the Bengali font must draw.
func hasBengali(s string) bool {
	for _, r := range s {
		if Bengali(r) {
			return true
		}
	}
	return false
}

// script is one weight of the Bengali font, parsed the three ways it is used: for shaping, for
// glyph outlines by number, and as an x/image face for the drawing that goes rune by rune.
type script struct {
	shape *gtfont.Face
	sfnt  *sfnt.Font
	ot    *opentype.Font
}

var fonts = sync.OnceValues(func() (regular, bold *script) {
	load := func(ttf []byte) *script {
		s := &script{}
		s.shape, _ = gtfont.ParseTTF(bytes.NewReader(ttf))
		s.sfnt, _ = sfnt.Parse(ttf)
		s.ot, _ = opentype.Parse(ttf)
		if s.shape == nil || s.sfnt == nil || s.ot == nil {
			return nil
		}
		return s
	}
	return load(regularTTF), load(boldTTF)
})

func fontFor(bold bool) *script {
	regular, b := fonts()
	if bold {
		return b
	}
	return regular
}

// Face is a Go font face that falls back to the Bengali font for the Bengali letters. Drawn rune by
// rune (by a font.Drawer) it has the letters but not the clusters; Draw and Measure shape them.
// Its metrics are the Go face's, so a line's height and every layout built on it stay as they were.
type Face struct {
	font.Face
	bn   font.Face
	bold bool
	ppem fixed.Int26_6
}

// New wraps latin, a face size pixels tall, with the Bengali font of the same weight and size.
func New(latin font.Face, size float64, bold bool) font.Face {
	if latin == nil {
		return nil
	}
	f := &Face{Face: latin, bold: bold, ppem: fixed.Int26_6(size * 64)}
	if s := fontFor(bold); s != nil {
		f.bn, _ = opentype.NewFace(s.ot, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone})
	}
	return f
}

func (f *Face) pick(r rune) font.Face {
	if f.bn != nil && Bengali(r) {
		return f.bn
	}
	return f.Face
}

func (f *Face) Glyph(dot fixed.Point26_6, r rune) (image.Rectangle, image.Image, image.Point, fixed.Int26_6, bool) {
	return f.pick(r).Glyph(dot, r)
}

func (f *Face) GlyphBounds(r rune) (fixed.Rectangle26_6, fixed.Int26_6, bool) {
	return f.pick(r).GlyphBounds(r)
}

func (f *Face) GlyphAdvance(r rune) (fixed.Int26_6, bool) {
	return f.pick(r).GlyphAdvance(r)
}

func (f *Face) Kern(r0, r1 rune) fixed.Int26_6 {
	if Bengali(r0) || Bengali(r1) {
		return 0
	}
	return f.Face.Kern(r0, r1)
}

// run is a stretch of a line in one font: Bengali, shaped, or the Go font's.
type run struct {
	s       string
	bengali bool
}

func runs(s string) []run {
	var out []run
	start, cur := 0, false
	for i, r := range s {
		b := Bengali(r)
		if i > 0 && b != cur {
			out = append(out, run{s[start:i], cur})
			start = i
		}
		cur = b
	}
	if start < len(s) {
		out = append(out, run{s[start:], cur})
	}
	return out
}

// shaped is a Bengali run laid out: its glyphs, and how far it moves the dot.
type shaped struct {
	glyphs  []shaping.Glyph
	advance fixed.Int26_6
	ascent  fixed.Int26_6 // above the baseline, positive
	descent fixed.Int26_6 // below it, positive
}

type shapeKey struct {
	bold bool
	ppem fixed.Int26_6
	s    string
}

// The same few lines are measured and drawn every frame, word by word as they wrap, so a run is
// shaped once and kept. HarfbuzzShaper is not safe for use from two goroutines.
var shaper struct {
	sync.Mutex
	hb   shaping.HarfbuzzShaper
	kept map[shapeKey]*shaped
}

var bn = language.NewLanguage("bn")

func shape(bold bool, ppem fixed.Int26_6, s string) *shaped {
	f := fontFor(bold)
	if f == nil {
		return nil
	}
	k := shapeKey{bold, ppem, s}
	shaper.Lock()
	defer shaper.Unlock()
	if sh, ok := shaper.kept[k]; ok {
		return sh
	}
	text := []rune(s)
	out := shaper.hb.Shape(shaping.Input{
		Text: text, RunStart: 0, RunEnd: len(text),
		Direction: di.DirectionLTR, Face: f.shape, Size: ppem,
		Script: language.Bengali, Language: bn,
	})
	sh := &shaped{glyphs: out.Glyphs, advance: out.Advance, ascent: out.LineBounds.Ascent, descent: -out.LineBounds.Descent}
	if shaper.kept == nil || len(shaper.kept) > 512 {
		shaper.kept = map[shapeKey]*shaped{}
	}
	shaper.kept[k] = sh
	return sh
}

// glyphMask is one Bengali glyph rasterized: its coverage, placed from the dot.
type glyphMask struct {
	mask *image.Alpha
	off  image.Point
}

type glyphKey struct {
	bold bool
	ppem fixed.Int26_6
	id   gtfont.GID
}

var masks struct {
	sync.Mutex
	kept map[glyphKey]*glyphMask
}

func rasterize(bold bool, ppem fixed.Int26_6, id gtfont.GID) *glyphMask {
	k := glyphKey{bold, ppem, id}
	masks.Lock()
	defer masks.Unlock()
	if g, ok := masks.kept[k]; ok {
		return g
	}
	f := fontFor(bold)
	var buf sfnt.Buffer
	idx := sfnt.GlyphIndex(id)
	g := &glyphMask{}
	b, _, err := f.sfnt.GlyphBounds(&buf, idx, ppem, font.HintingNone)
	segs, err2 := f.sfnt.LoadGlyph(&buf, idx, ppem, nil)
	if err == nil && err2 == nil {
		r := image.Rect(b.Min.X.Floor(), b.Min.Y.Floor(), b.Max.X.Ceil(), b.Max.Y.Ceil())
		if !r.Empty() {
			z := vector.NewRasterizer(r.Dx(), r.Dy())
			ox, oy := float32(-r.Min.X), float32(-r.Min.Y)
			pt := func(p fixed.Point26_6) (float32, float32) {
				return float32(p.X)/64 + ox, float32(p.Y)/64 + oy
			}
			for _, s := range segs {
				switch s.Op {
				case sfnt.SegmentOpMoveTo:
					z.MoveTo(pt(s.Args[0]))
				case sfnt.SegmentOpLineTo:
					z.LineTo(pt(s.Args[0]))
				case sfnt.SegmentOpQuadTo:
					x1, y1 := pt(s.Args[0])
					x2, y2 := pt(s.Args[1])
					z.QuadTo(x1, y1, x2, y2)
				case sfnt.SegmentOpCubeTo:
					x1, y1 := pt(s.Args[0])
					x2, y2 := pt(s.Args[1])
					x3, y3 := pt(s.Args[2])
					z.CubeTo(x1, y1, x2, y2, x3, y3)
				}
			}
			z.ClosePath()
			g.mask = image.NewAlpha(image.Rect(0, 0, r.Dx(), r.Dy()))
			z.Draw(g.mask, g.mask.Bounds(), image.Opaque, image.Point{})
			g.off = r.Min
		}
	}
	if masks.kept == nil || len(masks.kept) > 2048 {
		masks.kept = map[glyphKey]*glyphMask{}
	}
	masks.kept[k] = g
	return g
}

// Draw draws s on dst in face, from dot, the way a font.Drawer would, with its Bengali shaped.
func Draw(dst draw.Image, src image.Image, face font.Face, s string, dot fixed.Point26_6) {
	f, ok := face.(*Face)
	if !ok || !hasBengali(s) {
		(&font.Drawer{Dst: dst, Src: src, Face: face, Dot: dot}).DrawString(s)
		return
	}
	for _, rn := range runs(s) {
		var sh *shaped
		if rn.bengali {
			sh = shape(f.bold, f.ppem, rn.s)
		}
		if sh == nil {
			d := &font.Drawer{Dst: dst, Src: src, Face: f, Dot: dot}
			d.DrawString(rn.s)
			dot = d.Dot
			continue
		}
		pen := dot
		for _, g := range sh.glyphs {
			m := rasterize(f.bold, f.ppem, g.GlyphID)
			if m.mask != nil {
				at := image.Pt((pen.X + g.XOffset).Round(), (pen.Y - g.YOffset).Round()).Add(m.off)
				draw.DrawMask(dst, m.mask.Rect.Add(at), src, image.Point{}, m.mask, image.Point{}, draw.Over)
			}
			pen.X += g.Advance
		}
		dot.X += sh.advance
	}
}

// Measure is how far drawing s in face moves the dot.
func Measure(face font.Face, s string) fixed.Int26_6 {
	f, ok := face.(*Face)
	if !ok || !hasBengali(s) {
		return font.MeasureString(face, s)
	}
	var w fixed.Int26_6
	for _, rn := range runs(s) {
		if sh := shapedRun(f, rn); sh != nil {
			w += sh.advance
		} else {
			w += font.MeasureString(f, rn.s)
		}
	}
	return w
}

// Bounds is the box s takes drawn in face from a zero dot, like font.BoundString, and the advance.
// A Bengali run's box is the font's line, from its ascent to its descent: room for any of its marks.
func Bounds(face font.Face, s string) (fixed.Rectangle26_6, fixed.Int26_6) {
	f, ok := face.(*Face)
	if !ok || !hasBengali(s) {
		return font.BoundString(face, s)
	}
	var b fixed.Rectangle26_6
	var x fixed.Int26_6
	for _, rn := range runs(s) {
		var rb fixed.Rectangle26_6
		var adv fixed.Int26_6
		if sh := shapedRun(f, rn); sh != nil {
			rb = fixed.Rectangle26_6{Min: fixed.Point26_6{Y: -sh.ascent}, Max: fixed.Point26_6{X: sh.advance, Y: sh.descent}}
			adv = sh.advance
		} else {
			rb, adv = font.BoundString(f, rn.s)
		}
		rb = rb.Add(fixed.Point26_6{X: x})
		if b.Empty() {
			b = rb
		} else if !rb.Empty() {
			b = b.Union(rb)
		}
		x += adv
	}
	return b, x
}

func shapedRun(f *Face, rn run) *shaped {
	if !rn.bengali {
		return nil
	}
	return shape(f.bold, f.ppem, rn.s)
}
