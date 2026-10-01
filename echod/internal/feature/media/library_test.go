package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// aacStation is a station in a format the device does not decode.
func aacStation(t *testing.T) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/aac")
		w.Write([]byte{0xFF, 0xF1, 0x50, 0x80, 0x00, 0x1F, 0xFC, 0, 0, 0, 0, 0})
		w.(http.Flusher).Flush() // a station sends as it goes
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// A stream the device cannot decode is its own failure, told apart from a station that is down.
func TestAnUndecodableStreamIsToldApart(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := speaker.NewDriver(speaker.New())
	s := NewStream(d, speaker.New(), func() {}, func(string) {})
	err := s.PlayChecked(aacStation(t), 5*time.Second)
	if !IsUnplayable(err) {
		t.Errorf("an AAC station failed as %v", err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer down.Close()
	if err := s.PlayChecked(down.URL, 5*time.Second); err == nil || IsUnplayable(err) {
		t.Errorf("a station that is down failed as %v", err)
	}
}

// One started without waiting (the radio page) goes to the music library when the device cannot
// decode it; with no library it is left failed.
func TestAnUndecodableStationIsHandedOn(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := speaker.NewDriver(speaker.New())
	s := NewStream(d, speaker.New(), func() {}, func(string) {})
	var mu sync.Mutex
	var handed []string
	s.handOff = func(url string, wanted func() bool) {
		mu.Lock()
		handed = append(handed, url)
		mu.Unlock()
	}
	url := aacStation(t)
	s.Play(url)
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		n := len(handed)
		mu.Unlock()
		if n > 0 {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(handed) != 1 || handed[0] != url {
		t.Errorf("handed on %v", handed)
	}
}

// The library is asked to play the stream on this device, and the answer is whether it did.
func TestTheLibraryPlaysWhatTheDeviceCannot(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := viaLibrary("https://203.0.113.9/a.aac", nil); err != errNoLibrary {
		t.Errorf("with no library: %v", err)
	}
	var mu sync.Mutex
	var played []string
	lib := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Command string         `json:"command"`
			Args    map[string]any `json:"args"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		defer mu.Unlock()
		switch body.Command {
		case "players/all":
			state := "idle"
			if len(played) > 0 {
				state = "playing"
			}
			json.NewEncoder(w).Encode([]map[string]any{{"player_id": "upc1", "available": true, "playback_state": state,
				"output_protocols": []map[string]any{{"output_protocol_id": "aa:bb:cc:dd:ee:ff"}}}})
		case "player_queues/play_media":
			if body.Args["queue_id"] != "upc1" {
				t.Errorf("played on %v, not the device's player", body.Args["queue_id"])
			}
			played = append(played, body.Args["media"].(string))
			w.Write([]byte("null"))
		}
	}))
	defer lib.Close()
	tok := "tok"
	if err := config.Set().MusicAssistant().Server(lib.URL, &tok); err != nil {
		t.Fatal(err)
	}
	was := libraryPlayer
	libraryPlayer = func() (string, error) { return "aa:bb:cc:dd:ee:ff", nil }
	defer func() { libraryPlayer = was }()
	// An address into a home, or one handed on after something else was asked for, is not given.
	if err := viaLibrary("http://192.168.1.20:8000/a.aac", nil); err == nil {
		t.Error("an address in the home was handed to the library")
	}
	if err := viaLibrary("https://203.0.113.9/a.aac", func() bool { return false }); err != errOvertaken {
		t.Errorf("an overtaken hand-off: %v", err)
	}
	if err := viaLibrary("https://203.0.113.9/a.aac", nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(played) != 1 || played[0] != "https://203.0.113.9/a.aac" {
		t.Errorf("the library was given %v", played)
	}
}

// A checked play is answered by its caller, which hands on itself: the stream does not hand it off a
// second time. And a hand-off on its way knows once it is overtaken: by a stop, or another play.
func TestAHandOffIsOnceAndKnowsWhenItIsOvertaken(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	d := speaker.NewDriver(speaker.New())
	s := NewStream(d, speaker.New(), func() {}, func(string) {})
	var mu sync.Mutex
	var wants []func() bool
	s.handOff = func(url string, wanted func() bool) {
		mu.Lock()
		wants = append(wants, wanted)
		mu.Unlock()
	}
	url := aacStation(t)
	if err := s.PlayChecked(url, 5*time.Second); !IsUnplayable(err) {
		t.Fatalf("checked: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	if len(wants) != 0 {
		t.Error("a checked play was handed off by the stream as well")
	}
	mu.Unlock()

	s.Play(url)
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		n := len(wants)
		mu.Unlock()
		if n > 0 {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(wants) != 1 {
		t.Fatalf("handed off %d times", len(wants))
	}
	if !wants[0]() {
		t.Error("a hand-off nothing has overtaken is not wanted")
	}
	s.Stop()
	if wants[0]() {
		t.Error("a hand-off is still wanted after a stop")
	}
}
