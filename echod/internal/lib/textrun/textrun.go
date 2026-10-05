// Package textrun draws text that may hold Bengali (Bangla) words and emoji among the Latin ones.
//
// The screens draw with the Go fonts, which have no Bengali letters and no emoji, so a transcript or
// an answer in Bangla, or a phone notification with a 👍 in it, came out as boxes. Letters alone
// would not be enough either: Bengali is written in clusters, where a vowel sign can sit before the
// consonant it follows in the text (কি is ক then ি, drawn ি first) and two consonants joined by a
// hasanta become one shape, and an emoji can be several characters drawn as one picture (👍🏽 is a
// thumb and a skin tone, 🇧🇩 two letters, 👨‍👩‍👧 three people joined). So those stretches of a line
// are shaped, with go-text's port of HarfBuzz, in Noto Sans Bengali or Noto Emoji, and the rest is
// drawn by the Go font as before, untouched. The emoji are Noto Emoji's outlines, one colour, drawn
// in the colour of the text around them.
//
// Noto Sans Bengali and Noto Emoji are under the SIL Open Font License 1.1 (OFL.txt,
// OFL-NotoEmoji.txt).
package textrun

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/go-text/typesetting/di"
	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

//go:embed NotoSansBengali-Regular.ttf
var regularTTF []byte

//go:embed NotoSansBengali-Bold.ttf
var boldTTF []byte

// The Regular instance of Noto Emoji's variable font: a bold emoji is not worth a second megabyte.
//
//go:embed NotoEmoji-Regular.ttf
var emojiTTF []byte

// kind is which font draws a stretch of a line.
type kind uint8

const (
	latin kind = iota
	bengali
	emoji
)

// Bengali reports whether r is written in the Bengali font: the Bengali block, the two dandas
// (shared with Devanagari, which the Go font lacks too), and the joiners that steer conjuncts.
func Bengali(r rune) bool {
	return (r >= 0x0980 && r <= 0x09FF) || r == 0x0964 || r == 0x0965 || r == 0x200C || r == 0x200D
}

// joiner reports whether r only shapes the characters next to it: the zero width joiners, and in an
// emoji the variation selectors, the keycap, the skin tones and the tags of a subdivision flag.
func joiner(r rune) bool {
	return r == 0x200C || r == 0x200D || r == 0xFE0E || r == 0xFE0F || r == 0x20E3 ||
		(r >= 0x1F3FB && r <= 0x1F3FF) || (r >= 0xE0020 && r <= 0xE007F)
}

// script is one font, parsed the three ways it is used: for shaping, for glyph outlines by number,
// and as an x/image face for the drawing that goes rune by rune.
type script struct {
	shape *gtfont.Face
	sfnt  *sfnt.Font
	ot    *opentype.Font
}

func parse(ttf []byte) *script {
	s := &script{}
	s.shape, _ = gtfont.ParseTTF(bytes.NewReader(ttf))
	s.sfnt, _ = sfnt.Parse(ttf)
	s.ot, _ = opentype.Parse(ttf)
	if s.shape == nil || s.sfnt == nil || s.ot == nil {
		return nil
	}
	return s
}

var fonts = sync.OnceValues(func() (regular, bold *script) {
	return parse(regularTTF), parse(boldTTF)
})

var emojiFont = sync.OnceValue(func() *script { return parse(emojiTTF) })

func fontFor(k kind, bold bool) *script {
	if k == emoji {
		return emojiFont()
	}
	regular, b := fonts()
	if bold {
		return b
	}
	return regular
}

// hasEmoji reports whether the emoji font has a picture for r.
func hasEmoji(r rune) bool {
	f := emojiFont()
	if f == nil {
		return false
	}
	_, ok := f.shape.NominalGlyph(r)
	return ok
}

// Face is a Go font face that falls back to the Bengali font for the Bengali letters, and to the
// emoji font for what neither has. Drawn rune by rune (by a font.Drawer) it has the letters and
// pictures but not the clusters; Draw and Measure shape them. Its metrics are the Go face's, so a
// line's height and every layout built on it stay as they were.
type Face struct {
	font.Face
	bn   font.Face
	em   font.Face
	bold bool
	ppem fixed.Int26_6
}

// New wraps latin, a face size pixels tall, with the Bengali font of the same weight and size and
// the emoji font.
func New(latin font.Face, size float64, bold bool) font.Face {
	if latin == nil {
		return nil
	}
	f := &Face{Face: latin, bold: bold, ppem: fixed.Int26_6(size * 64)}
	opts := &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingNone}
	if s := fontFor(bengali, bold); s != nil {
		f.bn, _ = opentype.NewFace(s.ot, opts)
	}
	if s := fontFor(emoji, bold); s != nil {
		f.em, _ = opentype.NewFace(s.ot, opts)
	}
	return f
}

// latinHas reports whether the Go face draws r itself: then a symbol it shares with the emoji font,
// a ♪ or a ♥, keeps the look of the text around it, unless an emoji presentation selector asks
// for the picture.
func (f *Face) latinHas(r rune) bool {
	_, ok := f.Face.GlyphAdvance(r)
	return ok
}

// kindOf is the font for r, given the font of the rune before it and the text after it.
func (f *Face) kindOf(r rune, prev kind, rest string) kind {
	switch {
	case f.bn != nil && Bengali(r) && !(prev == emoji && joiner(r)):
		return bengali
	case f.em == nil:
		return latin
	case joiner(r):
		return emoji
	}
	if next, _ := utf8.DecodeRuneInString(rest); (next == 0xFE0F || next == 0x20E3) && hasEmoji(r) {
		return emoji
	}
	if r >= 0x80 && !f.latinHas(r) && hasEmoji(r) {
		return emoji
	}
	return latin
}

func (f *Face) pick(r rune) font.Face {
	switch f.kindOf(r, latin, "") {
	case bengali:
		return f.bn
	case emoji:
		return f.em
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
	if f.pick(r0) != f.Face || f.pick(r1) != f.Face {
		return 0
	}
	return f.Face.Kern(r0, r1)
}

// run is a stretch of a line in one font.
type run struct {
	s string
	k kind
}

// plain reports whether s is all ASCII, which the Go font draws on its own.
func plain(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func (f *Face) runs(s string) []run {
	var out []run
	start, cur := 0, latin
	for i, r := range s {
		k := f.kindOf(r, cur, s[i+utf8.RuneLen(r):])
		if i > 0 && k != cur {
			out = append(out, run{s[start:i], cur})
			start = i
		}
		cur = k
	}
	if start < len(s) {
		out = append(out, run{s[start:], cur})
	}
	return out
}

// shaped is a Bengali or emoji run laid out: its glyphs, and how far it moves the dot.
type shaped struct {
	glyphs  []shaping.Glyph
	advance fixed.Int26_6
	ascent  fixed.Int26_6 // above the baseline, positive
	descent fixed.Int26_6 // below it, positive
}

type shapeKey struct {
	k    kind
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

var (
	bn  = language.NewLanguage("bn")
	und = language.NewLanguage("und")
)

func shape(k kind, bold bool, ppem fixed.Int26_6, s string) *shaped {
	if k == emoji {
		bold = false // one weight
	}
	f := fontFor(k, bold)
	if f == nil {
		return nil
	}
	key := shapeKey{k, bold, ppem, s}
	shaper.Lock()
	defer shaper.Unlock()
	if sh, ok := shaper.kept[key]; ok {
		return sh
	}
	text := []rune(s)
	in := shaping.Input{
		Text: text, RunStart: 0, RunEnd: len(text),
		Direction: di.DirectionLTR, Face: f.shape, Size: ppem,
		Script: language.Bengali, Language: bn,
	}
	if k == emoji {
		in.Script, in.Language = language.Common, und
	}
	out := shaper.hb.Shape(in)
	sh := &shaped{glyphs: out.Glyphs, advance: out.Advance, ascent: out.LineBounds.Ascent, descent: -out.LineBounds.Descent}
	if shaper.kept == nil || len(shaper.kept) > 512 {
		shaper.kept = map[shapeKey]*shaped{}
	}
	shaper.kept[key] = sh
	return sh
}

// glyphMask is one shaped glyph rasterized: its coverage, placed from the dot.
type glyphMask struct {
	mask *image.Alpha
	off  image.Point
}

type glyphKey struct {
	k    kind
	bold bool
	ppem fixed.Int26_6
	id   gtfont.GID
}

var masks struct {
	sync.Mutex
	kept map[glyphKey]*glyphMask
}

func rasterize(k kind, bold bool, ppem fixed.Int26_6, id gtfont.GID) *glyphMask {
	if k == emoji {
		bold = false
	}
	key := glyphKey{k, bold, ppem, id}
	masks.Lock()
	defer masks.Unlock()
	if g, ok := masks.kept[key]; ok {
		return g
	}
	f := fontFor(k, bold)
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
	masks.kept[key] = g
	return g
}

// Draw draws s on dst in face, from dot, the way a font.Drawer would, with its Bengali and emoji
// shaped.
func Draw(dst draw.Image, src image.Image, face font.Face, s string, dot fixed.Point26_6) {
	f, ok := face.(*Face)
	if !ok || plain(s) {
		(&font.Drawer{Dst: dst, Src: src, Face: face, Dot: dot}).DrawString(s)
		return
	}
	for _, rn := range f.runs(s) {
		sh := f.shapedRun(rn)
		if sh == nil {
			d := &font.Drawer{Dst: dst, Src: src, Face: f.Face, Dot: dot}
			d.DrawString(rn.s)
			dot = d.Dot
			continue
		}
		pen := dot
		for _, g := range sh.glyphs {
			m := rasterize(rn.k, f.bold, f.ppem, g.GlyphID)
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
	if !ok || plain(s) {
		return font.MeasureString(face, s)
	}
	var w fixed.Int26_6
	for _, rn := range f.runs(s) {
		if sh := f.shapedRun(rn); sh != nil {
			w += sh.advance
		} else {
			w += font.MeasureString(f.Face, rn.s)
		}
	}
	return w
}

// Bounds is the box s takes drawn in face from a zero dot, like font.BoundString, and the advance.
// A shaped run's box is its font's line, from its ascent to its descent: room for any of its marks.
func Bounds(face font.Face, s string) (fixed.Rectangle26_6, fixed.Int26_6) {
	f, ok := face.(*Face)
	if !ok || plain(s) {
		return font.BoundString(face, s)
	}
	var b fixed.Rectangle26_6
	var x fixed.Int26_6
	for _, rn := range f.runs(s) {
		var rb fixed.Rectangle26_6
		var adv fixed.Int26_6
		if sh := f.shapedRun(rn); sh != nil {
			rb = fixed.Rectangle26_6{Min: fixed.Point26_6{Y: -sh.ascent}, Max: fixed.Point26_6{X: sh.advance, Y: sh.descent}}
			adv = sh.advance
		} else {
			rb, adv = font.BoundString(f.Face, rn.s)
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

func (f *Face) shapedRun(rn run) *shaped {
	if rn.k == latin {
		return nil
	}
	return shape(rn.k, f.bold, f.ppem, rn.s)
}

// speech is a face for telling an emoji from a letter in text that is not drawn, and the lock on
// it: an x/image face is not safe for use from two goroutines.
var speech struct {
	sync.Mutex
	f *Face
}

// Speakable is s without its emoji, for text that is to be spoken: a voice reads a 👍 as "thumbs
// up", or stumbles on it. What the screen draws in the Go font, a ♪ or a ♥ with no emoji
// selector, is a letter here as well, and stays.
func Speakable(s string) string {
	if plain(s) {
		return s
	}
	speech.Lock()
	defer speech.Unlock()
	if speech.f == nil {
		ot, err := opentype.Parse(goregular.TTF)
		if err != nil {
			return s
		}
		fc, err := opentype.NewFace(ot, &opentype.FaceOptions{Size: 12, DPI: 72})
		if err != nil {
			return s
		}
		speech.f = New(fc, 12, false).(*Face)
	}
	var b strings.Builder
	removed := false
	for _, rn := range speech.f.runs(s) {
		if rn.k == emoji {
			removed = true
			b.WriteByte(' ')
			continue
		}
		b.WriteString(rn.s)
	}
	if !removed {
		return s
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	// "warm 🌡️." would leave "warm ." behind.
	for _, p := range []string{".", ",", "!", "?", ";", ":", "।"} {
		out = strings.ReplaceAll(out, " "+p, p)
	}
	return out
}
