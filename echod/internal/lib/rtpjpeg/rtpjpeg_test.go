package rtpjpeg

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"
)

func picture(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// What Go's encoder makes is taken apart, and its packets carry it whole.
func TestAGoJPEGGoesIntoPacketsWhole(t *testing.T) {
	f, err := Parse(picture(t, 640, 480))
	if err != nil {
		t.Fatal(err)
	}
	if f.Type != 1 || f.Width != 640 || f.Height != 480 || len(f.Tables) != 128 || len(f.Scan) == 0 {
		t.Fatalf("parsed type %d %dx%d, %d table bytes, %d scan bytes", f.Type, f.Width, f.Height, len(f.Tables), len(f.Scan))
	}
	seq := uint16(65534) // it wraps
	packets := f.Packets(&seq, 1234, 0xCAFE, 1400)
	if len(packets) < 2 {
		t.Fatalf("%d packets for a whole frame", len(packets))
	}
	var scan []byte
	for i, p := range packets {
		if len(p) > 1400 {
			t.Errorf("packet %d is %d bytes", i, len(p))
		}
		if p[0] != 0x80 || p[1]&0x7F != PayloadType {
			t.Errorf("packet %d header % x", i, p[:2])
		}
		marker := p[1]&0x80 != 0
		if marker != (i == len(packets)-1) {
			t.Errorf("packet %d marker %v", i, marker)
		}
		if got := binary.BigEndian.Uint16(p[2:]); got != uint16(65534+i) {
			t.Errorf("packet %d sequence %d", i, got)
		}
		j := p[12:]
		offset := int(j[1])<<16 | int(j[2])<<8 | int(j[3])
		if offset != len(scan) {
			t.Errorf("packet %d says offset %d, %d come before it", i, offset, len(scan))
		}
		if j[4] != 1 || j[5] != 255 || j[6] != 80 || j[7] != 60 {
			t.Errorf("packet %d jpeg header % x", i, j[:8])
		}
		body := j[8:]
		if i == 0 {
			if binary.BigEndian.Uint16(body[2:]) != 128 || !bytes.Equal(body[4:132], f.Tables) {
				t.Error("the first packet does not carry the tables")
			}
			body = body[132:]
		}
		scan = append(scan, body...)
	}
	if !bytes.Equal(scan, f.Scan) {
		t.Error("the packets do not carry the scan whole")
	}
}

func TestWhatRTPJPEGCannotCarry(t *testing.T) {
	gray := image.NewGray(image.Rect(0, 0, 64, 64))
	var g bytes.Buffer
	_ = jpeg.Encode(&g, gray, nil)
	if _, err := Parse(g.Bytes()); err == nil {
		t.Error("a grayscale JPEG was taken")
	}
	if _, err := Parse([]byte("not a picture")); err == nil {
		t.Error("something that is not a JPEG was taken")
	}
	if _, err := Parse(picture(t, 2048, 16)); err != errTooBig {
		t.Errorf("a picture too wide: %v", err)
	}
}
