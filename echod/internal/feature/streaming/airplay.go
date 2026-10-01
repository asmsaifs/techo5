package streaming

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// runAirPlayReceiver runs shairport-sync under the device's name until ctx ends, playing what it sends
// and showing what it says is playing.
func runAirPlayReceiver(ctx context.Context, name string) {
	r, w, err := os.Pipe()
	if err != nil {
		slog.Error("streaming: AirPlay's audio pipe failed", "err", err)
		return
	}
	defer r.Close()
	defer w.Close()
	cred := receiverCred()
	meta := filepath.Join(runDir, "airplay-metadata")
	_ = os.Remove(meta)
	if err := syscall.Mkfifo(meta, 0o600); err != nil {
		slog.Warn("streaming: AirPlay's metadata pipe failed; no song names", "err", err)
	} else {
		handTo(cred, meta)
		safe.Go("airplay metadata", func() { readAirPlayMetadata(ctx, meta) })
	}
	conf := filepath.Join(runDir, "shairport-sync.conf")
	if err := os.WriteFile(conf, []byte(shairportConf(name, meta)), 0o644); err != nil {
		slog.Error("streaming: writing shairport-sync's configuration failed", "err", err)
		return
	}
	safe.Go("airplay audio", func() { pump(ctx, home.AirPlayName, r) })
	supervise(ctx, program{name: "shairport-sync", path: shairportPath, args: []string{"-c", conf}, stdout: w, cred: cred})
}

// The ports AirPlay is reached on, fixed so a firewall can let them through (the Dot's does): the
// session on airplayPort, and the audio, control and timing on airplayUDPCount ports from
// airplayUDPBase.
const (
	airplayPort     = 5000
	airplayUDPBase  = 6001
	airplayUDPCount = 10
)

// shairportConf is shairport-sync's configuration: the name phones show, the audio to standard output
// as 16-bit stereo at 44.1 kHz, and what is playing to the metadata pipe. Its volume stays its own: the
// phone's slider scales what it sends, and the device's volume is applied on top as for anything else.
func shairportConf(name, metaPipe string) string {
	return fmt.Sprintf(`general = {
	name = %s;
	output_backend = "stdout";
	mdns_backend = "avahi";
	interpolation = "soxr";
	port = %d;
	udp_port_base = %d;
	udp_port_range = %d;
};
sessioncontrol = {
	session_timeout = 60;
};
stdout = {
	output_rate = %d;
	output_format = "S16_LE";
	output_channels = %d;
};
metadata = {
	enabled = "yes";
	include_cover_art = "no";
	pipe_name = %s;
	pipe_timeout = 5000;
};
`, confString(name), airplayPort, airplayUDPBase, airplayUDPCount, audioRate, audioChannels, confString(metaPipe))
}

// confString is s as a libconfig string: quoted, with quotes and backslashes escaped and anything
// unprintable left out, since a device's name is whatever somebody typed.
func confString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// metaItem is one of shairport-sync's metadata items: a type and a code, each four characters written
// as hex, and data in base64.
type metaItem struct {
	Type string `xml:"type"`
	Code string `xml:"code"`
	Data string `xml:"data"`
}

// readAirPlayMetadata follows the metadata pipe, telling the media player the song as it changes. The
// pipe is opened for writing as well as reading, so it neither waits for shairport-sync nor sees an
// end each time shairport-sync restarts.
func readAirPlayMetadata(ctx context.Context, path string) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		slog.Warn("streaming: opening AirPlay's metadata pipe failed", "err", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	defer f.Close()
	followAirPlayMetadata(f, func(title, artist, album string) {
		media.Get().SetReceivedTrack(home.AirPlayName, title, artist, album)
	})
}

// followAirPlayMetadata reads items from r until it ends, calling told with the song whenever a field of
// it changes, and with nothing when the session ends. Anything the decoder cannot take is passed over,
// and reading goes on after it: only r failing ends it.
func followAirPlayMetadata(r io.Reader, told func(title, artist, album string)) {
	src := &readErr{r: r}
	br := bufio.NewReaderSize(src, 64<<10)
	newDecoder := func() *xml.Decoder {
		// A bufio.Reader is a ByteReader, so the decoder reads no further than it has to: a new one
		// picks up where an old one gave up.
		d := xml.NewDecoder(br)
		d.Strict = false
		return d
	}
	dec := newDecoder()
	var title, artist, album string
	// stuck counts decoder errors in a row that took nothing from the pipe.
	stuck, at := 0, int64(-1)
	for {
		var it metaItem
		if err := dec.Decode(&it); err != nil {
			// The pipe itself failed (closed as the receivers stop), or the decoder keeps giving up
			// without taking anything: the end.
			taken := src.n - int64(br.Buffered())
			if taken == at {
				stuck++
			} else {
				stuck, at = 0, taken
			}
			if src.err != nil || stuck > 100 {
				return
			}
			dec = newDecoder()
			continue
		}
		typ, code := fourCC(it.Type), fourCC(it.Code)
		data := ""
		if d, err := base64.StdEncoding.DecodeString(strings.TrimSpace(it.Data)); err == nil && len(d) <= 1024 {
			data = string(d)
		}
		switch {
		case typ == "core" && code == "minm":
			title = data
		case typ == "core" && code == "asar":
			artist = data
		case typ == "core" && code == "asal":
			album = data
		case typ == "ssnc" && (code == "pend" || code == "disc"):
			title, artist, album = "", "", ""
		default:
			continue
		}
		told(title, artist, album)
	}
}

// readErr is a reader that keeps the error its reader last gave, so a decoder's own complaints can be
// told from the pipe failing, and how much it has read.
type readErr struct {
	r   io.Reader
	n   int64 // bytes read so far
	err error
}

func (e *readErr) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	e.n += int64(n)
	if err != nil {
		e.err = err
	}
	return n, err
}

// fourCC is a four-character code written as eight hex digits.
func fourCC(h string) string {
	b, err := hex.DecodeString(strings.TrimSpace(h))
	if err != nil || len(b) != 4 {
		return ""
	}
	return string(b)
}
