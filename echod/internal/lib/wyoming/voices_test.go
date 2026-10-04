package wyoming

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

// A Piper-shaped info event: one program, voices with and without languages, one not downloaded.
const pipersInfo = `{"tts":[{"name":"piper","voices":[
	{"name":"en_US-ryan-medium","languages":["en_US"],"installed":true},
	{"name":"en_GB-alan-medium","languages":["en_GB"],"installed":true},
	{"name":"de_DE-thorsten-medium","languages":["de_DE"],"installed":true},
	{"name":"en_US-amy-low","languages":["en_US"],"installed":false},
	{"name":"en_US-joe-medium"}
]}]}`

// fakeServer answers one describe with info, the way a Wyoming server does: the header line, then
// the data it says is coming.
func fakeServer(t *testing.T, info string) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		line, err := bufio.NewReader(c).ReadBytes('\n')
		if err != nil {
			return
		}
		var h header
		if json.Unmarshal(line, &h) != nil || h.Type != "describe" {
			return
		}
		hdr, _ := json.Marshal(header{Type: "info", DataLength: len(info)})
		_, _ = c.Write(append(append(hdr, '\n'), info...))
	}()
	return l.Addr().String()
}

func TestVoicesByLanguage(t *testing.T) {
	cases := []struct {
		language string
		want     []string
	}{
		{"en", []string{"en_US-ryan-medium", "en_GB-alan-medium", "en_US-joe-medium"}},
		{"en_US", []string{"en_US-ryan-medium", "en_US-joe-medium"}},
		{"de", []string{"de_DE-thorsten-medium"}},
		{"", []string{"en_US-ryan-medium", "en_GB-alan-medium", "de_DE-thorsten-medium", "en_US-joe-medium"}},
		{"fr", nil},
	}
	for _, c := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		got, err := Voices(ctx, fakeServer(t, pipersInfo), c.language)
		cancel()
		if err != nil {
			t.Fatalf("%q: %v", c.language, err)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: got %v, want %v", c.language, got, c.want)
		}
	}
}

func TestVoicesFromNoServer(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Voices(ctx, addr, "en"); err == nil {
		t.Fatal("no server: want an error")
	}
}

func TestVoicesOddInfo(t *testing.T) {
	// Fields of the wrong type, and no tts at all, give no voices rather than a panic.
	for _, info := range []string{`{}`, `{"tts":"piper"}`, `{"tts":[{"voices":[1,"x",{"name":7}]}]}`} {
		var data map[string]any
		if err := json.Unmarshal([]byte(info), &data); err != nil {
			t.Fatal(err)
		}
		if got := voicesIn(data, "en"); len(got) != 0 {
			t.Errorf("%s: got %v", info, got)
		}
	}
}

// A server listing a voice twice, names too long or unprintable to show, and more voices than anyone
// scrolls through gives each good name once, short ones only, and no more than maxVoices.
func TestVoicesBounded(t *testing.T) {
	long := strings.Repeat("x", maxVoiceName+1)
	var voices []any
	voices = append(voices,
		map[string]any{"name": "en_US-ryan-medium"},
		map[string]any{"name": "en_US-ryan-medium"},
		map[string]any{"name": "en_US-" + long + "-medium"},
		map[string]any{"name": "en_US-bad\x07-medium"},
		map[string]any{"name": "en_US-\xff-medium"},
	)
	for i := range maxVoices + 50 {
		voices = append(voices, map[string]any{"name": fmt.Sprintf("en_US-v%d-medium", i)})
	}
	got := voicesIn(map[string]any{"tts": []any{map[string]any{"voices": voices}}}, "en")
	if len(got) != maxVoices {
		t.Fatalf("got %d voices, want %d", len(got), maxVoices)
	}
	if got[0] != "en_US-ryan-medium" || got[1] == got[0] {
		t.Errorf("duplicates or the first voice lost: %v", got[:3])
	}
	for _, n := range got {
		if !fitName(n) {
			t.Errorf("unfit name kept: %q", n)
		}
	}
}

// The language is free text: "en-US" means en_US.
func TestVoicesLanguageWithDash(t *testing.T) {
	var data map[string]any
	if err := json.Unmarshal([]byte(pipersInfo), &data); err != nil {
		t.Fatal(err)
	}
	if got, want := voicesIn(data, "en-US"), []string{"en_US-ryan-medium", "en_US-joe-medium"}; !slices.Equal(got, want) {
		t.Errorf("en-US: got %v, want %v", got, want)
	}
}

// A header with no newline is refused once it passes the limit, rather than read for as long as the
// server keeps sending; one within it is read whole, past the reader's own buffer.
func TestReadLineBounded(t *testing.T) {
	endless := bufio.NewReaderSize(strings.NewReader(strings.Repeat("a", 10000)), 16)
	if _, err := readLine(endless, 1000); err == nil {
		t.Error("an endless header was read")
	}
	ok := bufio.NewReaderSize(strings.NewReader(strings.Repeat("b", 500)+"\nrest"), 16)
	line, err := readLine(ok, 1000)
	if err != nil || len(line) != 501 {
		t.Errorf("a long header: %d bytes, %v", len(line), err)
	}
}
