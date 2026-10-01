package g711

import "testing"

// Known codes from the standard's tables, and a round trip that stays within the step size of the
// segment each sample falls in.
func TestKnownCodes(t *testing.T) {
	for _, c := range []struct {
		s    int16
		u, a byte
	}{
		{0, 0xFF, 0xD5},
		{-1, 0x7F, 0x55},
		{32767, 0x80, 0xAA},
		{-32768, 0x00, 0x2A},
	} {
		if got := ULaw(c.s); got != c.u {
			t.Errorf("ULaw(%d) = %#x, want %#x", c.s, got, c.u)
		}
		if got := ALaw(c.s); got != c.a {
			t.Errorf("ALaw(%d) = %#x, want %#x", c.s, got, c.a)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for s := -32768; s <= 32767; s += 7 {
		v := int16(s)
		abs := int32(v)
		if abs < 0 {
			abs = -abs
		}
		tol := abs/16 + 16 // G.711 keeps about four bits within each segment
		if d := int32(ULawDecode(ULaw(v))) - int32(v); d > tol || d < -tol {
			t.Fatalf("μ-law %d came back as %d", v, ULawDecode(ULaw(v)))
		}
		if d := int32(ALawDecode(ALaw(v))) - int32(v); d > tol || d < -tol {
			t.Fatalf("A-law %d came back as %d", v, ALawDecode(ALaw(v)))
		}
	}
}
