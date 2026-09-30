//go:build !dot

package screen

import (
	"strconv"
	"testing"
	"unsafe"
)

// The kernel copies its own fb_fix_screeninfo into ours whatever size ours is, so the sizes must
// match: 68 bytes on 32-bit, 80 on arm64.
func TestFixInfoSize(t *testing.T) {
	want := uintptr(68)
	if strconv.IntSize == 64 {
		want = 80
	}
	if got := unsafe.Sizeof(fixInfo{}); got != want {
		t.Fatalf("fixInfo is %d bytes, want %d", got, want)
	}
}
