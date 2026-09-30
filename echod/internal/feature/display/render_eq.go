//go:build !dot && !spot

package display

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
)

// eqLine is one line of words under the bars.
type eqLine struct {
	face  font.Face
	text  string
	c     color.RGBA
	lineH int
}

// eqWords lays out what goes under the bars: what it is doing, what was heard, and the answer, the
// answer a size smaller when it is long.
func (r *renderer) eqWords(s scene, room int) []eqLine {
	accent, heardCol, replyCol := color.RGBA{120, 220, 140, 255}, color.RGBA{170, 178, 190, 255}, color.RGBA{240, 244, 250, 255}
	if s.eq.night {
		accent, heardCol, replyCol = color.RGBA{255, 90, 70, 255}, color.RGBA{150, 110, 105, 255}, color.RGBA{255, 140, 120, 255}
	}
	maxW := r.w - 2*r.margin
	var out []eqLine
	switch s.phase {
	case "listening":
		out = append(out, eqLine{r.small, "Listening…", accent, r.s(42)})
	case "thinking":
		out = append(out, eqLine{r.small, "Thinking…", accent, r.s(42)})
	}
	if s.heard != "" && s.phase != "listening" {
		for _, l := range r.wrap(r.small, "“"+s.heard+"”", maxW) {
			out = append(out, eqLine{r.small, l, heardCol, r.s(40)})
		}
		out[len(out)-1].lineH += r.s(10) // a breath between the question and the answer
	}
	if s.reply == "" || (s.phase != "replying" && s.phase != "lingering") {
		return out
	}
	used := 0
	for _, l := range out {
		used += l.lineH
	}
	face, lineH := r.body, r.s(52)
	lines := r.wrap(face, s.reply, maxW)
	if used+len(lines)*lineH > room {
		face, lineH = r.small, r.s(42)
		lines = r.wrap(face, s.reply, maxW)
	}
	for _, l := range lines {
		out = append(out, eqLine{face, l, replyCol, lineH})
	}
	return out
}

// eqGround is the turn page's background on the Show.
var eqGround = color.RGBA{8, 10, 16, 255}

// equalizer draws the whole turn page in the bars style.
func (r *renderer) equalizer(s scene) {
	draw.Draw(r.dst, r.dst.Rect, image.NewUniform(eqGround), image.Point{}, draw.Src)
	top, bot, under, bottom, lines := r.eqLayout(s, 54)
	drawBars(r.dst, image.Rect(r.s(40), top, r.w-r.s(40), bot), s.eq, eqGround, max(1, r.s(2)), r.s(4))
	r.eqText(lines, bot+under, bottom)
}

// wave draws the whole turn page in the wave style.
func (r *renderer) wave(s scene) {
	draw.Draw(r.dst, r.dst.Rect, image.NewUniform(eqGround), image.Point{}, draw.Src)
	top, bot, under, bottom, lines := r.eqLayout(s, 58)
	if r.wb == nil {
		r.wb = &waveBuf{}
	}
	drawWave(r.dst, r.wb, image.Rect(0, top, r.w, bot), r.s(20), s.eq, eqGround, s.now, max(2, r.s(5)/2), 0)
	r.eqText(lines, bot+under, bottom)
}

// eqLayout splits the page between the picture and the words: the picture takes the top part (pct of
// the height) and gives way to a long answer down to a quarter of the screen. It returns the picture's
// top and bottom, the gap under it, the words' last baseline, and the words.
func (r *renderer) eqLayout(s scene, pct int) (top, bot, under, bottom int, lines []eqLine) {
	top, bottom = headerH+r.s(8), r.h-r.s(14)
	under = r.s(54)
	lines = r.eqWords(s, bottom-(r.h*28/100)-under)
	words := 0
	for _, l := range lines {
		words += l.lineH
	}
	bot = min(r.h*pct/100, bottom-under-words)
	bot = max(bot, r.h*28/100)
	return top, bot, under, bottom, lines
}

// eqText draws the words centered from y down; what still does not fit ends in an ellipsis.
func (r *renderer) eqText(lines []eqLine, y, bottom int) {
	for i, l := range lines {
		base := y + l.lineH - r.s(14)
		last := i+1 < len(lines) && base+lines[i+1].lineH > bottom+r.s(10)
		text := l.text
		if last {
			text += " …"
		}
		r.text(l.face, text, (r.w-r.width(l.face, text))/2, base, l.c)
		if last {
			return
		}
		y += l.lineH
	}
}
