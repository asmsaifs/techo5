//go:build !dot

package dashboard

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
	"time"
)

// pngHeader is the start of a PNG of w by h pixels, depth bits a sample, RGBA: all a decoder reads to
// say how large the picture is.
func pngHeader(w, h uint32, depth byte) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = depth, 6
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	_ = binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// A picture's limit is the memory it takes decoded: a 16-bit PNG is twice an 8-bit one's, so one the
// 8-bit limit would let through is too large; and a size whose pixels overflow an int on the 32-bit
// devices is too large, not wrapped around to small.
func TestAPicturesLimitIsInBytes(t *testing.T) {
	ctx := context.Background()
	wide := wanted{url: "/local/plan.png", wide: true}
	for _, c := range []struct {
		what  string
		b     []byte
		w     wanted
		large bool
	}{
		{"a 16-bit 3000 by 3000 kept wide", pngHeader(3000, 3000, 16), wide, true},
		{"an 8-bit 4096 by 4096 kept wide", pngHeader(4096, 4096, 8), wide, false},
		{"a 16-bit 1920 by 1080 on a card", pngHeader(1920, 1080, 16), wanted{url: "/local/a.png"}, true},
		{"an 8-bit 1920 by 1080 on a card", pngHeader(1920, 1080, 8), wanted{url: "/local/a.png"}, false},
		{"65535 by 65535", pngHeader(65535, 65535, 8), wide, true},
	} {
		sh, err := decodePicture(ctx, c.b, c.w)
		if large := errors.Is(err, errTooLarge); large != c.large || sh.tooLarge != c.large {
			t.Errorf("%s: too large %v (%v), want %v", c.what, sh.tooLarge, err, c.large)
		}
	}
}

// A picture for a session that has ended is not decoded: the memory is not spent on nobody.
func TestAPictureForAnEndedSessionIsNotDecoded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 800, 10))); err != nil {
		t.Fatal(err)
	}
	if sh, err := decodePicture(ctx, b.Bytes(), wanted{url: "/local/a.png"}); !errors.Is(err, context.Canceled) || sh.img != nil {
		t.Errorf("decoded for an ended session: %v, %v", sh.img != nil, err)
	}
	sh, err := decodePicture(context.Background(), b.Bytes(), wanted{url: "/local/a.png"})
	if err != nil || sh.img == nil || sh.img.Bounds().Dx() != pictureWidth || sh.size != image.Pt(800, 10) {
		t.Errorf("a picture on a card: %v, %v", sh, err)
	}
}

// An image that would not load is tried again in half a minute, then twice as long each time, up to
// the usual ten minutes; once it loads, the usual. A camera keeps its few seconds either way.
func TestAFailedPictureIsRetriedSooner(t *testing.T) {
	w := wanted{url: "/local/a.png"}
	retry := map[string]time.Duration{}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, pictureEvery, pictureEvery}
	for i, d := range want {
		if got := retryAfter(w, true, retry); got != d {
			t.Errorf("failure %d: again in %v, want %v", i+1, got, d)
		}
	}
	if got := retryAfter(w, false, retry); got != pictureEvery {
		t.Errorf("loaded: again in %v, want %v", got, pictureEvery)
	}
	if got := retryAfter(w, true, retry); got != pictureRetry {
		t.Errorf("failed after loading: again in %v, want %v", got, pictureRetry)
	}
	if got := retryAfter(wanted{camera: "camera.door"}, true, retry); got != cameraEvery {
		t.Errorf("a camera: again in %v, want %v", got, cameraEvery)
	}
}
