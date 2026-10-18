package fonts

import "testing"

// Vietnamese answers contain letters outside Latin Extended-A/B. The screen's
// Inter cuts must keep these in both weights, rather than draw missing-glyph boxes.
func TestInterDrawsVietnameseLetters(t *testing.T) {
	const letters = "ÀÁẢÃẠĂẰẮẲẴẶÂẦẤẨẪẬĐÈÉẺẼẸÊỀẾỂỄỆÌÍỈĨỊÒÓỎÕỌÔỒỐỔỖỘƠỜỚỞỠỢÙÚỦŨỤƯỪỨỬỮỰỲÝỶỸỴàáảãạăằắẳẵặâầấẩẫậđèéẻẽẹêềếểễệìíỉĩịòóỏõọôồốổỗộơờớởỡợùúủũụưừứửữựỳýỷỹỵ"
	for _, bold := range []bool{false, true} {
		f, err := Font("inter", bold)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range letters {
			if glyph, err := f.GlyphIndex(nil, r); err != nil || glyph == 0 {
				t.Errorf("Inter bold=%v cannot draw %q (U+%04X): %v", bold, r, r, err)
			}
		}
	}
}
