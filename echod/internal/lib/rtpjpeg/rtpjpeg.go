// Package rtpjpeg turns a JPEG into the RTP packets that carry it (RFC 2435), for a camera served over
// RTSP without a video encoder: every frame is a JPEG the device already knows how to make, and ffmpeg,
// go2rtc, Frigate and the recorders built on them read RTP/JPEG as they read any other stream.
//
// Only what Go's own encoder makes is taken: baseline, 8-bit, three components sampled 4:2:0 (or 4:2:2),
// no restart markers. Its quantization tables go in the first packet of every frame (Q 255), so nothing
// about the picture has to be agreed beforehand.
package rtpjpeg

import (
	"encoding/binary"
	"errors"
)

// PayloadType is JPEG's static RTP payload type, and ClockRate its clock.
const (
	PayloadType = 26
	ClockRate   = 90000
)

// Frame is a JPEG taken apart into what RTP/JPEG carries.
type Frame struct {
	Type          byte   // 0 for 4:2:2, 1 for 4:2:0
	Width, Height int    // in pixels, multiples of 8 up to 2040
	Tables        []byte // the luma and chroma quantization tables, 64 bytes each, as in the JPEG
	Scan          []byte // the entropy-coded data, from after the scan header to before the end marker
}

var (
	errNotJPEG     = errors.New("rtpjpeg: not a JPEG")
	errUnsupported = errors.New("rtpjpeg: a JPEG RTP/JPEG cannot carry (not baseline 4:2:0 or 4:2:2 with 8-bit tables)")
	errTooBig      = errors.New("rtpjpeg: wider or taller than 2040 pixels")
)

// Parse takes a JPEG apart.
func Parse(b []byte) (Frame, error) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return Frame{}, errNotJPEG
	}
	var f Frame
	tables := map[byte][]byte{}
	var tableOf [3]byte
	sof := false
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return Frame{}, errNotJPEG
		}
		marker := b[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		length := int(binary.BigEndian.Uint16(b[i+2:]))
		if length < 2 || i+2+length > len(b) {
			return Frame{}, errNotJPEG
		}
		seg := b[i+4 : i+2+length]
		switch marker {
		case 0xDB: // quantization tables
			for len(seg) > 0 {
				pq, tq := seg[0]>>4, seg[0]&0x0F
				if pq != 0 || len(seg) < 65 {
					return Frame{}, errUnsupported
				}
				tables[tq] = seg[1:65]
				seg = seg[65:]
			}
		case 0xC0: // baseline frame
			if len(seg) < 6+3*3 || seg[0] != 8 || seg[5] != 3 {
				return Frame{}, errUnsupported
			}
			f.Height = int(binary.BigEndian.Uint16(seg[1:]))
			f.Width = int(binary.BigEndian.Uint16(seg[3:]))
			ys := seg[6+1]
			switch ys {
			case 0x22:
				f.Type = 1
			case 0x21:
				f.Type = 0
			default:
				return Frame{}, errUnsupported
			}
			if seg[9+1] != 0x11 || seg[12+1] != 0x11 {
				return Frame{}, errUnsupported
			}
			tableOf = [3]byte{seg[6+2], seg[9+2], seg[12+2]}
			sof = true
		case 0xC1, 0xC2, 0xC3, 0xC5, 0xC6, 0xC7, 0xC9, 0xCA, 0xCB, 0xCD, 0xCE, 0xCF:
			return Frame{}, errUnsupported // not baseline
		case 0xDD: // restart interval
			if len(seg) >= 2 && binary.BigEndian.Uint16(seg) != 0 {
				return Frame{}, errUnsupported
			}
		case 0xDA: // start of scan: the rest, to the end marker, is the scan
			if !sof {
				return Frame{}, errUnsupported
			}
			scan := b[i+2+length:]
			end := len(scan)
			if end >= 2 && scan[end-2] == 0xFF && scan[end-1] == 0xD9 {
				end -= 2
			}
			f.Scan = scan[:end]
			luma, chroma := tables[tableOf[0]], tables[tableOf[1]]
			if luma == nil || chroma == nil || tableOf[1] != tableOf[2] {
				return Frame{}, errUnsupported
			}
			f.Tables = append(append([]byte(nil), luma...), chroma...)
			if f.Width > 2040 || f.Height > 2040 {
				return Frame{}, errTooBig
			}
			return f, nil
		}
		i += 2 + length
	}
	return Frame{}, errNotJPEG
}

// Packets are a frame's RTP packets, each at most mtu bytes, with the RTP header filled in from seq (which
// is advanced), ts and ssrc, and the marker on the last.
func (f Frame) Packets(seq *uint16, ts, ssrc uint32, mtu int) [][]byte {
	const rtpHeader, jpegHeader, qtHeader = 12, 8, 4
	var out [][]byte
	offset := 0
	for offset < len(f.Scan) || offset == 0 {
		head := rtpHeader + jpegHeader
		if offset == 0 {
			head += qtHeader + len(f.Tables)
		}
		room := mtu - head
		if room <= 0 {
			return nil
		}
		n := min(room, len(f.Scan)-offset)
		p := make([]byte, head+n)
		last := offset+n >= len(f.Scan)

		p[0] = 0x80
		p[1] = PayloadType
		if last {
			p[1] |= 0x80
		}
		binary.BigEndian.PutUint16(p[2:], *seq)
		*seq++
		binary.BigEndian.PutUint32(p[4:], ts)
		binary.BigEndian.PutUint32(p[8:], ssrc)

		j := p[rtpHeader:]
		j[0] = 0 // type-specific
		j[1], j[2], j[3] = byte(offset>>16), byte(offset>>8), byte(offset)
		j[4] = f.Type
		j[5] = 255 // the tables come in-band
		j[6] = byte(f.Width / 8)
		j[7] = byte(f.Height / 8)
		k := jpegHeader
		if offset == 0 {
			j[k], j[k+1] = 0, 0 // MBZ, precision 8-bit
			binary.BigEndian.PutUint16(j[k+2:], uint16(len(f.Tables)))
			copy(j[k+qtHeader:], f.Tables)
			k += qtHeader + len(f.Tables)
		}
		copy(j[k:], f.Scan[offset:offset+n])
		out = append(out, p)
		offset += n
		if len(f.Scan) == 0 {
			break
		}
	}
	return out
}
