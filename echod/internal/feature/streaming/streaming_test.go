package streaming

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
)

func item(typ, code, data string) string {
	return fmt.Sprintf("<item><type>%s</type><code>%s</code><length>%d</length>\n<data encoding=\"base64\">\n%s</data></item>\n",
		hex.EncodeToString([]byte(typ)), hex.EncodeToString([]byte(code)), len(data), base64.StdEncoding.EncodeToString([]byte(data)))
}

// AirPlay's metadata names the song field by field, and the end of a session clears it; what the decoder
// cannot take on the way is passed over.
func TestAirPlayMetadata(t *testing.T) {
	stream := item("ssnc", "mdst", "") + item("core", "minm", "Take It Easy") + `<?xml version="2.0"?>` + "</junk>" +
		item("core", "asar", "Eagles") + item("core", "asal", "Eagles") + item("ssnc", "mden", "") + item("ssnc", "pend", "")
	var got [][3]string
	followAirPlayMetadata(strings.NewReader(stream), func(title, artist, album string) {
		got = append(got, [3]string{title, artist, album})
	})
	if len(got) != 4 || got[2] != [3]string{"Take It Easy", "Eagles", "Eagles"} || got[3] != [3]string{} {
		t.Errorf("told %v", got)
	}
}

// librespot's event lines name a new song, and a stop clears it; anything else is let by, a line far
// too long included, without losing what comes after it.
func TestSpotifyEvents(t *testing.T) {
	lines := "track_changed\tHotel California\tEagles, Don Henley\tHotel California\n" +
		"half a line\n" +
		"track_changed\t" + strings.Repeat("x", 40<<10) + "\ta\tb\n" +
		"track_changed\tCaf\xc3\tEagles\tB\n" + // cut inside a character
		"stopped\t\t\t\n"
	var got [][3]string
	followSpotifyEvents(strings.NewReader(lines), func(title, artist, album string) {
		got = append(got, [3]string{title, artist, album})
	})
	if len(got) != 3 || got[0] != [3]string{"Hotel California", "Eagles, Don Henley", "Hotel California"} ||
		got[1] != [3]string{"Caf", "Eagles", "B"} || got[2] != [3]string{} {
		t.Errorf("told %v", got)
	}
}

// The event program writes one tab-separated line per event, the artists joined and nothing in a name
// able to break the line.
func TestTheSpotifyEventProgram(t *testing.T) {
	dir := t.TempDir()
	script, out := filepath.Join(dir, "event"), filepath.Join(dir, "events")
	if err := os.WriteFile(script, []byte(spotifyEvent), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(env ...string) {
		cmd := exec.Command("/bin/sh", script)
		cmd.Env = append([]string{"TECHO5_SPOTIFY_EVENTS=" + out}, env...)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, b)
		}
	}
	run("PLAYER_EVENT=track_changed", "NAME=One\tTwo", "ARTISTS=Eagles\nDon Henley", "ALBUM=Hotel California")
	b, _ := os.ReadFile(out)
	if got := string(b); got != "track_changed\tOne Two\tEagles, Don Henley\tHotel California\n" {
		t.Errorf("wrote %q", got)
	}
	_ = os.Remove(out)
	run("PLAYER_EVENT=volume_changed")
	if _, err := os.Stat(out); err == nil {
		t.Error("an event that is not a song's wrote a line")
	}
}

// A name is a libconfig string whatever was typed for it.
func TestShairportConf(t *testing.T) {
	c := shairportConf(`Den's "Desk" \ one`+"\x01", "/run/x")
	if !strings.Contains(c, `name = "Den's \"Desk\" \\ one";`) {
		t.Errorf("conf:\n%s", c)
	}
}

// A receiver's audio plays as a track when it arrives; once that track is over (quiet, or something
// else played), what the receiver goes on sending is set aside until it pauses, and then plays again.
func TestThePumpLeavesTheSpeakerToWhatWasPlayedLast(t *testing.T) {
	was, wasAside := play, setAside
	setAside = 150 * time.Millisecond
	t.Cleanup(func() { play, setAside = was, wasAside })
	var mu sync.Mutex
	var started []media.PCMSource
	play = func(name string, src media.PCMSource, rate, channels int) {
		mu.Lock()
		started = append(started, src)
		mu.Unlock()
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(started) }
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pump(ctx, "AirPlay", r)

	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 1 })
	// The track is taken over (something else played): the sender keeps going, and is set aside.
	mu.Lock()
	started[0].Close()
	mu.Unlock()
	for i := 0; i < 5; i++ {
		w.Write(make([]byte, 4096))
		time.Sleep(50 * time.Millisecond)
	}
	if count() != 1 {
		t.Fatal("a sender still going took the speaker straight back")
	}
	// It pauses, then plays again: that is asked for.
	time.Sleep(300 * time.Millisecond)
	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 2 })
}

// A track that ended on its own, the sender gone quiet (paused on the phone), is simply over: what
// arrives next plays at once, with nothing set aside.
func TestAQuietEndIsNotSetAside(t *testing.T) {
	was, wasAside := play, setAside
	setAside = 5 * time.Second // long, so a drain would show
	t.Cleanup(func() { play, setAside = was, wasAside })
	var mu sync.Mutex
	started := 0
	play = func(name string, src media.PCMSource, rate, channels int) {
		mu.Lock()
		started++
		mu.Unlock()
		// As the player does: reads until the sender has been quiet a while, then closes.
		go func() {
			buf := make([]byte, 4096)
			for {
				_ = src.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				if _, err := src.Read(buf); err != nil {
					src.Close()
					return
				}
			}
		}()
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return started }
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go pump(ctx, "AirPlay", r)

	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 1 })
	time.Sleep(300 * time.Millisecond) // quiet: the track ends
	w.Write(make([]byte, 4096))
	waitUntil(t, func() bool { return count() == 2 })
}

// A program that keeps stopping is started again, each time after a longer pause.
func TestAProgramIsStartedAgain(t *testing.T) {
	was, wasMost := restartFirst, restartMost
	restartFirst, restartMost = 20*time.Millisecond, 80*time.Millisecond
	t.Cleanup(func() { restartFirst, restartMost = was, wasMost })
	runs := filepath.Join(t.TempDir(), "runs")
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	supervise(ctx, program{name: "test", path: "/bin/sh", args: []string{"-c", "echo x >> " + runs + "; exit 1"}})
	b, _ := os.ReadFile(runs)
	if n := strings.Count(string(b), "x"); n < 3 {
		t.Errorf("started %d times in 600 ms", n)
	}
}

// The receivers start only once avahi says it is up, and when avahi stops they stop with it and start
// again once a new avahi is up: they register with it once, as they start.
func TestTheReceiversFollowAvahi(t *testing.T) {
	wasRun, wasUp, wasAir, wasSpot, wasFirst, wasLeft := runAvahi, avahiUp, runAirPlay, runSpotify, restartFirst, leftovers
	t.Cleanup(func() {
		runAvahi, avahiUp, runAirPlay, runSpotify, restartFirst, leftovers = wasRun, wasUp, wasAir, wasSpot, wasFirst, wasLeft
	})
	leftovers = func() {}
	restartFirst = 10 * time.Millisecond
	var mu sync.Mutex
	var up bool
	var avahis, airplays, spotifies, running int
	stopAvahi := make(chan struct{}, 1)
	runAvahi = func(ctx context.Context) {
		mu.Lock()
		avahis++
		first := avahis == 1
		mu.Unlock()
		if first {
			time.Sleep(400 * time.Millisecond) // slow to come up
		}
		mu.Lock()
		up = true
		mu.Unlock()
		select {
		case <-ctx.Done():
		case <-stopAvahi:
		}
		mu.Lock()
		up = false
		mu.Unlock()
	}
	avahiUp = func(context.Context) bool { mu.Lock(); defer mu.Unlock(); return up }
	receiver := func(count *int) func(context.Context, string) {
		return func(ctx context.Context, _ string) {
			mu.Lock()
			if !up {
				t.Error("a receiver started before avahi was up")
			}
			*count++
			running++
			mu.Unlock()
			<-ctx.Done()
			mu.Lock()
			running--
			mu.Unlock()
		}
	}
	runAirPlay, runSpotify = receiver(&airplays), receiver(&spotifies)
	get := func() (int, int, int, int) { mu.Lock(); defer mu.Unlock(); return avahis, airplays, spotifies, running }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runGroup(ctx, "Kitchen", true, true); close(done) }()
	waitUntil(t, func() bool { _, a, s, r := get(); return a == 1 && s == 1 && r == 2 })
	stopAvahi <- struct{}{}
	waitUntil(t, func() bool { n, a, s, r := get(); return n == 2 && a == 2 && s == 2 && r == 2 })
	cancel()
	<-done
	if _, _, _, r := get(); r != 0 {
		t.Errorf("%d receivers still running after the group ended", r)
	}
}

func waitUntil(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
