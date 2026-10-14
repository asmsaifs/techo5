//go:build mockup && !dot

package display

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type mockTile struct {
	label string
	img   *image.RGBA
}

// mockPhoto is a stand-in photo: a sky, hills, and a subject in the middle.
func mockPhoto(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			f := float64(y) / float64(h)
			c := color.RGBA{uint8(90 + 80*f), uint8(140 + 60*f), uint8(210 - 20*f), 255}
			hill := float64(h)*0.62 + float64(h)*0.06*math.Sin(float64(x)/float64(w)*5)
			if float64(y) > hill {
				c = color.RGBA{uint8(60 + 30*f), uint8(120 - 20*f), 50, 255}
			}
			dx, dy := float64(x-w/2), float64(y)-float64(h)*0.5
			if dx*dx/(float64(h)*0.14*float64(h)*0.14)+dy*dy/(float64(h)*0.26*float64(h)*0.26) < 1 {
				c = color.RGBA{200, 120, 80, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func mockSave(t *testing.T, dir, name string, img image.Image) {
	t.Helper()
	if dir == "" {
		return
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// mockSheet lays the tiles in a grid, cols across, each half size with its label above.
func mockSheet(t *testing.T, dir, name string, tiles []mockTile, cols int) {
	t.Helper()
	if dir == "" || len(tiles) == 0 {
		return
	}
	tw, th := tiles[0].img.Rect.Dx()/2, tiles[0].img.Rect.Dy()/2
	pad, lab := 10, 18
	rows := (len(tiles) + cols - 1) / cols
	sheet := image.NewRGBA(image.Rect(0, 0, cols*(tw+pad)+pad, rows*(th+lab+pad)+pad))
	draw.Draw(sheet, sheet.Rect, image.NewUniform(color.RGBA{24, 24, 24, 255}), image.Point{}, draw.Src)
	d := font.Drawer{Dst: sheet, Src: image.NewUniform(color.RGBA{230, 230, 230, 255}), Face: basicfont.Face7x13}
	for i, tl := range tiles {
		x := pad + (i%cols)*(tw+pad)
		y := pad + (i/cols)*(th+lab+pad)
		d.Dot = fixed.P(x, y+13)
		d.DrawString(tl.label)
		for yy := range th {
			for xx := range tw {
				sheet.Set(x+xx, y+lab+yy, tl.img.At(xx*2, yy*2))
			}
		}
	}
	mockSave(t, dir, name, sheet)
}
