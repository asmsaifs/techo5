package tflite

import (
	"encoding/binary"
	"fmt"
	"math"
)

// FlatBuffers accessors, enough of the format to read a .tflite file.
//
// A table is addressed by its position in the buffer and reached through a vtable that maps
// field slots to byte offsets within the table. A slot past the end of the vtable, or holding
// zero, means the field was not written and takes its default. Offsets to tables, strings and
// vectors are stored relative to the position of the offset itself, so following one is always
// an addition. Unions occupy two slots: a type byte followed by the value.
type buf []byte

func (b buf) u8(p uint32) uint8   { return b[p] }
func (b buf) u16(p uint32) uint16 { return binary.LittleEndian.Uint16(b[p:]) }
func (b buf) u32(p uint32) uint32 { return binary.LittleEndian.Uint32(b[p:]) }
func (b buf) i32(p uint32) int32  { return int32(b.u32(p)) }
func (b buf) u64(p uint32) uint64 { return binary.LittleEndian.Uint64(b[p:]) }
func (b buf) f32(p uint32) float32 {
	return math.Float32frombits(b.u32(p))
}

type table struct {
	b   buf
	pos uint32
}

func root(b buf) table {
	return table{b, b.u32(0)}
}

// slot returns the offset of a field within the table, or zero when the field is absent.
func (t table) slot(id int) uint32 {
	// The soffset is signed and points backwards, and unsigned wraparound gets the subtraction
	// right for either sign.
	vt := t.pos - uint32(t.b.i32(t.pos))
	off := uint32(4 + 2*id)
	if off >= uint32(t.b.u16(vt)) {
		return 0
	}
	return uint32(t.b.u16(vt + off))
}

func (t table) u8f(id int, def uint8) uint8 {
	if o := t.slot(id); o != 0 {
		return t.b.u8(t.pos + o)
	}
	return def
}

func (t table) i32f(id int, def int32) int32 {
	if o := t.slot(id); o != 0 {
		return t.b.i32(t.pos + o)
	}
	return def
}

func (t table) u32f(id int, def uint32) uint32 {
	if o := t.slot(id); o != 0 {
		return t.b.u32(t.pos + o)
	}
	return def
}

func (t table) u64f(id int, def uint64) uint64 {
	if o := t.slot(id); o != 0 {
		return t.b.u64(t.pos + o)
	}
	return def
}

func (t table) f32f(id int, def float32) float32 {
	if o := t.slot(id); o != 0 {
		return t.b.f32(t.pos + o)
	}
	return def
}

func (t table) boolf(id int, def bool) bool {
	if o := t.slot(id); o != 0 {
		return t.b.u8(t.pos+o) != 0
	}
	return def
}

func (t table) table(id int) (table, bool) {
	o := t.slot(id)
	if o == 0 {
		return table{}, false
	}
	p := t.pos + o
	return table{t.b, p + t.b.u32(p)}, true
}

func (t table) str(id int) string {
	o := t.slot(id)
	if o == 0 {
		return ""
	}
	p := t.pos + o
	p += t.b.u32(p)
	n := t.b.u32(p)
	t.b.fits(p, n, 1)
	return string(t.b[p+4 : p+4+n])
}

// fits panics unless the n elements of size bytes after the length word at p lie within the buffer.
// A length is a number the file states, and a vector is the one place a file says how much to
// allocate: four bytes can claim four billion elements. Checking that they are there to read before
// anything is made of them keeps a lie about a length a refusal rather than an allocation the
// runtime cannot take back. The sum is done wide, so no claim wraps around to a small one.
func (b buf) fits(p, n, size uint32) {
	if uint64(p)+4+uint64(n)*uint64(size) > uint64(len(b)) {
		panic(fmt.Sprintf("a vector at %d claims %d elements of %d bytes, past the end of %d", p, n, size, len(b)))
	}
}

type vector struct {
	b   buf
	pos uint32
	n   int
}

// vec is the vector in a field, whose elements are size bytes each: 4 for offsets to tables and for
// 32-bit scalars, 1 for bytes. One that would run past the end of the buffer panics (see fits).
func (t table) vec(id int, size uint32) vector {
	o := t.slot(id)
	if o == 0 {
		return vector{}
	}
	p := t.pos + o
	p += t.b.u32(p)
	n := t.b.u32(p)
	t.b.fits(p, n, size)
	return vector{t.b, p + 4, int(n)}
}

func (v vector) len() int { return v.n }

func (v vector) i32(i int) int32 { return v.b.i32(v.pos + 4*uint32(i)) }

func (v vector) table(i int) table {
	p := v.pos + 4*uint32(i)
	return table{v.b, p + v.b.u32(p)}
}

func (v vector) bytes() []byte {
	if v.n == 0 {
		return nil
	}
	return v.b[v.pos : v.pos+uint32(v.n)]
}

func (v vector) ints() []int {
	if v.n == 0 {
		return nil
	}
	v.b.fits(v.pos-4, uint32(v.n), 4)
	out := make([]int, v.n)
	for i := range out {
		out[i] = int(v.i32(i))
	}
	return out
}
