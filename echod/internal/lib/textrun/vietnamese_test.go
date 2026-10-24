package textrun

import (
	"bytes"
	"image"
	"testing"

	screenfonts "github.com/HuskerMinion/techo5/echod/internal/lib/fonts"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/unicode/norm"
)

// A server may encode the same Vietnamese answer with precomposed letters or
// combining marks. They must occupy the same layout and paint the same text.
func TestVietnameseCanonicalEquivalence(t *testing.T) {
	for _, bold := range []bool{false, true} {
		f, err := screenfonts.Font("inter", bold)
		if err != nil {
			t.Fatal(err)
		}
		face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: 36, DPI: 72})
		if err != nil {
			t.Fatal(err)
		}
		wrapped := New(face, 36, bold)
		for _, composed := range []string{
			"Hỏi đáp: Hôm nay ngày mấy?",
			"Ắ Ằ Ẳ Ẵ Ặ Ấ Ầ Ẩ Ẫ Ậ Ế Ề Ể Ễ Ệ Ợ Ự Ỹ đ",
			"Chào bạn 👋! Trường học, Nguyễn, khỏe mạnh.",
		} {
			decomposed := norm.NFD.String(composed)
			if a, b := Measure(wrapped, composed), Measure(wrapped, decomposed); a != b {
				t.Errorf("bold=%v equivalent answer widths differ: %v != %v", bold, a, b)
			}
			a, aa := Bounds(wrapped, composed)
			b, ba := Bounds(wrapped, decomposed)
			if a != b || aa != ba {
				t.Errorf("bold=%v equivalent answer bounds differ: %v/%v != %v/%v", bold, a, aa, b, ba)
			}
			first, second := image.NewAlpha(image.Rect(0, 0, 1800, 100)), image.NewAlpha(image.Rect(0, 0, 1800, 100))
			Draw(first, image.Opaque, wrapped, composed, fixed.P(12, 60))
			Draw(second, image.Opaque, wrapped, decomposed, fixed.P(12, 60))
			if !bytes.Equal(first.Pix, second.Pix) {
				t.Errorf("bold=%v equivalent Vietnamese answers paint differently", bold)
			}
		}
		if err := wrapped.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
