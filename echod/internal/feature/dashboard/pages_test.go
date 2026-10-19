//go:build !dot

package dashboard

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"net"
	"path/filepath"
	"slices"
	"testing"

	"github.com/flynn/noise"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// fakeDashcast is a dashcast server that answers one connection: the hello it reads goes to got, and
// answer is what it sends back, as framed messages (nil for nothing: a server from before web pages).
func fakeDashcast(t *testing.T, key string, answer func(hello map[string]any) [][]byte) (addr string, got chan map[string]any) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan map[string]any, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		hs, err := noise.NewHandshakeState(noise.Config{
			CipherSuite: suite(), Random: rand.Reader, Pattern: noise.HandshakeNN, Initiator: false,
			Prologue: []byte(prologue), PresharedKey: psk(key), PresharedKeyPlacement: 0,
		})
		if err != nil {
			return
		}
		first, err := readFrame(c)
		if err != nil {
			return
		}
		if _, _, _, err := hs.ReadMessage(nil, first); err != nil {
			return
		}
		reply, fromDevice, toDevice, err := hs.WriteMessage(nil, nil)
		if err != nil || writeFrame(c, reply) != nil {
			return
		}
		sc := &secureConn{Conn: c, send: toDevice, recv: fromDevice}
		line, err := bufio.NewReader(sc).ReadBytes('\n')
		if err != nil {
			return
		}
		var h map[string]any
		_ = json.Unmarshal(line, &h)
		got <- h
		for _, m := range answer(h) {
			var hdr [4]byte
			binary.BigEndian.PutUint32(hdr[:], uint32(len(m)))
			_, _ = sc.Write(append(hdr[:], m...))
		}
	}()
	return ln.Addr().String(), got
}

// The device asks for the web pages by a hello of its own, and takes the names back.
func TestPagesAsksTheServer(t *testing.T) {
	addr, got := fakeDashcast(t, "a-long-enough-test-key", func(map[string]any) [][]byte {
		return [][]byte{append([]byte{kindPages}, `["Status","Art"]`...)}
	})
	names, err := Pages(addr, "a-long-enough-test-key", "Kitchen")
	if err != nil || !slices.Equal(names, []string{"Status", "Art"}) {
		t.Fatalf("names %q, %v", names, err)
	}
	if h := <-got; h["list"] != true || h["name"] != "Kitchen" {
		t.Errorf("hello %v", h)
	}
}

// A server from before web pages closes the connection: no pages, and no error.
func TestPagesFromAnOlderServer(t *testing.T) {
	addr, _ := fakeDashcast(t, "a-long-enough-test-key", func(map[string]any) [][]byte { return nil })
	names, err := Pages(addr, "a-long-enough-test-key", "Kitchen")
	if err != nil || len(names) != 0 {
		t.Errorf("names %q, %v", names, err)
	}
}

// The server's pages are offered in the list as "Web: name", after Home Assistant's dashboards; one
// that cannot be asked just now leaves the pages last known.
func TestWebPagesAreChoices(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	addr, _ := fakeDashcast(t, "a-long-enough-test-key", func(map[string]any) [][]byte {
		return [][]byte{append([]byte{kindPages}, `["Status"]`...)}
	})
	got := webChoices(config.Dashboard{Server: addr, Key: "a-long-enough-test-key"})
	want := []config.DashboardChoice{{Label: "Web: Status", Path: "web:Status", Streamed: true}}
	if !slices.Equal(got, want) {
		t.Fatalf("choices %v", got)
	}
	away := webChoices(config.Dashboard{Server: "127.0.0.1:1", Key: "k", Known: append([]config.DashboardChoice{{Label: "Overview", Path: "lovelace"}}, want...)})
	if !slices.Equal(away, want) {
		t.Errorf("with the server away: %v", away)
	}
	if webChoices(config.Dashboard{}) != nil {
		t.Error("pages offered with no server")
	}
	f := pickerFeature()
	f.listBoards(config.Dashboard{Known: append(slices.Clone(pickerBoards), want...), Path: "web:Status"})
	if f.board.Get() != "Web: Status" {
		t.Errorf("chosen %q", f.board.Get())
	}
}

// A web page is asked for by its name; a dashboard by its path.
func TestWebPage(t *testing.T) {
	if name, ok := WebPage("web:Status"); !ok || name != "Status" {
		t.Errorf("%q, %v", name, ok)
	}
	if _, ok := WebPage("lovelace/0"); ok {
		t.Error("a dashboard taken for a web page")
	}
}

// The stream asks for a chosen web page by its name, and never with kiosk, which is Home Assistant's.
func TestTheStreamAsksForAPageByName(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	addr, got := fakeDashcast(t, "a-long-enough-test-key", func(map[string]any) [][]byte { return nil })
	if err := config.Set().Dashboard().Server(addr, "a-long-enough-test-key"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Dashboard().Path("web:Status"); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Dashboard().Kiosk(true); err != nil {
		t.Fatal(err)
	}
	s := &stream{f: &Feature{}, w: 480, h: 480}
	_ = s.once()
	h := <-got
	if h["page"] != "Status" || h["kiosk"] != nil {
		t.Errorf("hello %v", h)
	}
}

// Drawn, a web page is never fetched from Home Assistant as a dashboard, listed or no longer.
func TestADrawnWebPageIsStreamedOnly(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Dashboard().Path("web:Gone"); err != nil {
		t.Fatal(err)
	}
	if d := pickerFeature().Drawn(480); d.Problem == "" {
		t.Error("a web page no longer listed was drawn")
	}
}
