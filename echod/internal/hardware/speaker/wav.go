package speaker

import (
	"encoding/binary"
	"fmt"
)

// WAVFormat is what a WAVE file's fmt chunk said about its audio. Format is the format tag, 1 for plain
// PCM; for WAVE_FORMAT_EXTENSIBLE, the one its sub-format names, which converters write for PCM with
// more than two channels or more than 16 bits.
type WAVFormat struct {
	Format, Channels, Rate, Bits int
}

const (
	wavPCM        = 1
	wavExtensible = 0xFFFE
)

// MonoWAV takes 16-bit samples out of a RIFF/WAVE body as one channel at the file's own rate, walking
// the chunks rather than assuming a 44-byte header: a converted file can carry extra chunks before the
// data. A file with no fmt chunk is taken to be 16-bit mono at VoiceRate.
//
// Sizes are handled as uint64. A 32-bit int cannot hold a large RIFF size, and a chunk claiming
// one turns negative, which slips past a bounds check and panics on the slice.
func MonoWAV(body []byte) ([]int16, WAVFormat, error) {
	f := WAVFormat{Format: wavPCM, Channels: 1, Rate: VoiceRate, Bits: 16}
	if len(body) < 12 || string(body[0:4]) != "RIFF" || string(body[8:12]) != "WAVE" {
		return nil, f, fmt.Errorf("not a WAVE file: %d bytes", len(body))
	}

	total := uint64(len(body))
	for off := uint64(12); off+8 <= total; {
		id := string(body[off : off+4])
		size := uint64(binary.LittleEndian.Uint32(body[off+4 : off+8]))
		off += 8

		end := off + size
		if end > total {
			end = total
		}

		switch id {
		case "fmt ":
			if end-off >= 16 {
				f.Format = int(binary.LittleEndian.Uint16(body[off:]))
				f.Channels = int(binary.LittleEndian.Uint16(body[off+2:]))
				f.Rate = int(binary.LittleEndian.Uint32(body[off+4:]))
				f.Bits = int(binary.LittleEndian.Uint16(body[off+14:]))
				if f.Format == wavExtensible {
					f.Format = 0 // unknown, unless the sub-format says
					if end-off >= 26 {
						f.Format = int(binary.LittleEndian.Uint16(body[off+24:]))
					}
				}
			}
		case "data":
			if f.Format != wavPCM {
				return nil, f, fmt.Errorf("unsupported WAVE: format %#x, not PCM", f.Format)
			}
			if f.Bits != 16 || f.Channels < 1 {
				return nil, f, fmt.Errorf("unsupported WAVE: %d-bit, %d channels", f.Bits, f.Channels)
			}
			pcm := body[off:end]
			frames := len(pcm) / (2 * f.Channels)
			mono := make([]int16, frames)
			for i := range mono {
				var sum int
				for c := 0; c < f.Channels; c++ {
					sum += int(int16(binary.LittleEndian.Uint16(pcm[(i*f.Channels+c)*2:])))
				}
				mono[i] = int16(sum / f.Channels)
			}
			return mono, f, nil
		}

		off = end
		if size%2 == 1 {
			off++
		}
	}
	return nil, f, fmt.Errorf("no data chunk in %d bytes", total)
}
