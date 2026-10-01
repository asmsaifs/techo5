// Package g711 encodes 16-bit samples as G.711, the 8 kHz telephone code nearly every camera with a
// speaker takes on its talk-back channel: μ-law (PCMU) in North America and Japan, A-law (PCMA) elsewhere.
// The standard's segment tables, computed rather than looked up: eight bits a sample, 64 kbit/s.
package g711

// ULaw is s in μ-law.
func ULaw(s int16) byte {
	const bias, clip = 0x84, 32635
	v := int32(s)
	sign := byte(0)
	if v < 0 {
		v = -v
		sign = 0x80
	}
	if v > clip {
		v = clip
	}
	v += bias
	exp := byte(7)
	for mask := int32(0x4000); v&mask == 0 && exp > 0; mask >>= 1 {
		exp--
	}
	mant := byte(v>>(exp+3)) & 0x0F
	return ^(sign | exp<<4 | mant)
}

// ALaw is s in A-law.
func ALaw(s int16) byte {
	v := int32(s) >> 3 // A-law works on 13 bits
	sign := byte(0x80)
	if v < 0 {
		v = -v - 1
		sign = 0
	}
	if v > 0x0FFF {
		v = 0x0FFF
	}
	var b byte
	if v < 32 {
		b = byte(v >> 1)
	} else {
		exp := byte(1)
		for t := v >> 5; t > 1; t >>= 1 {
			exp++
		}
		b = exp<<4 | byte(v>>exp)&0x0F
	}
	return (b | sign) ^ 0x55
}

// ULawDecode is b in μ-law as a sample, for tests and for checking what was sent.
func ULawDecode(b byte) int16 {
	b = ^b
	sign, exp, mant := b&0x80, (b>>4)&7, b&0x0F
	v := (int32(mant)<<3 + 0x84) << exp
	v -= 0x84
	if sign != 0 {
		return int16(-v)
	}
	return int16(v)
}

// ALawDecode is b in A-law as a sample.
func ALawDecode(b byte) int16 {
	b ^= 0x55
	sign, exp, mant := b&0x80, (b>>4)&7, int32(b&0x0F)
	var v int32
	if exp == 0 {
		v = mant<<4 + 8
	} else {
		v = (mant<<4 + 0x108) << (exp - 1)
	}
	if sign == 0 {
		return int16(-v)
	}
	return int16(v)
}
