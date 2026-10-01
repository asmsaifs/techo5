package streaming

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
	"github.com/HuskerMinion/techo5/echod/internal/feature/media"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// spotifyEvent is the program librespot runs on each player event: it writes one line, the event and
// the song, tab-separated, to a pipe the daemon reads. Tabs and newlines in what it writes are made
// spaces, so a song's name cannot end the line early, and each field is cut short. The pipe is opened
// for reading as well as writing (1<>), which never waits: a pipe nobody reads any more, as when the
// receivers are stopped, cannot leave the program stuck.
const spotifyEvent = `#!/bin/sh
case "$PLAYER_EVENT" in
track_changed|stopped|session_disconnected) ;;
*) exit 0 ;;
esac
clean() { printf '%s' "$1" | tr '\t\n' '  ' | cut -c1-300; }
artists() { printf '%s' "$ARTISTS" | awk 'NR > 1 { printf ", " } { printf "%s", $0 }' | tr '\t' ' ' | cut -c1-300; }
printf '%s\t%s\t%s\t%s\n' "$PLAYER_EVENT" "$(clean "$NAME")" "$(artists)" "$(clean "$ALBUM")" 1<>"$TECHO5_SPOTIFY_EVENTS"
`

// spotifyPort is where librespot listens for the Spotify app handing it a login: fixed, so a firewall
// can let it through (the Dot's does).
const spotifyPort = "4070"

// spotifyCache is where the login a phone hands over is kept, so the device is still the account's
// after a restart: on userdata beside the device's own state, not in it (that is root's alone, and
// librespot runs as the receivers' user), readable by that user alone. Turning Spotify Connect off
// forgets it (forgetSpotify).
func spotifyCache() string { return filepath.Join(filepath.Dir(layout.StateDir), "techo5-spotify") }

// forgetSpotify removes the kept login: a device switched off from Spotify Connect, or given away, is
// no longer the account's.
func forgetSpotify() {
	if _, err := os.Stat(spotifyCache()); err != nil {
		return
	}
	if err := os.RemoveAll(spotifyCache()); err != nil {
		slog.Warn("streaming: forgetting the Spotify login failed", "err", err)
		return
	}
	slog.Info("streaming: Spotify login forgotten")
}

// runSpotifyReceiver runs librespot under the device's name until ctx ends, playing what it sends and
// showing what it says is playing.
func runSpotifyReceiver(ctx context.Context, name string) {
	r, w, err := os.Pipe()
	if err != nil {
		slog.Error("streaming: Spotify's audio pipe failed", "err", err)
		return
	}
	defer r.Close()
	defer w.Close()
	cred := receiverCred()
	events := filepath.Join(runDir, "spotify-events")
	script := filepath.Join(runDir, "spotify-event")
	_ = os.Remove(events)
	if err := syscall.Mkfifo(events, 0o600); err != nil {
		slog.Warn("streaming: Spotify's event pipe failed; no song names", "err", err)
	} else if err := os.WriteFile(script, []byte(spotifyEvent), 0o755); err != nil {
		slog.Warn("streaming: writing Spotify's event program failed; no song names", "err", err)
	} else {
		handTo(cred, events)
		safe.Go("spotify events", func() { readSpotifyEvents(ctx, events) })
	}
	cache := spotifyCache()
	_ = os.MkdirAll(cache, 0o700)
	_ = os.Chmod(cache, 0o700)
	handTo(cred, cache)
	safe.Go("spotify audio", func() { pump(ctx, home.SpotifyName, r) })
	supervise(ctx, program{
		name: "librespot",
		path: librespotPath,
		args: []string{
			"--name", name,
			"--backend", "pipe",
			"--format", "S16",
			"--bitrate", "160",
			"--device-type", "speaker",
			"--disable-audio-cache",
			"--cache", cache,
			"--zeroconf-port", spotifyPort,
			"--onevent", script,
			"--initial-volume", "100",
		},
		stdout: w,
		env:    []string{"TECHO5_SPOTIFY_EVENTS=" + events},
		cred:   cred,
		keep:   librespotWorthLogging,
	})
}

// librespotWorthLogging keeps librespot's warnings and errors, and what it says as it fails to start or
// crashes, and drops the rest, which names the account that logged in.
func librespotWorthLogging(line string) bool {
	for _, s := range []string{" WARN ", " ERROR ", "panicked", "error:", "Usage:"} {
		if strings.Contains(line, s) {
			return true
		}
	}
	return false
}

// readSpotifyEvents follows the event pipe, telling the media player the song. Opened for writing as
// well, so it neither waits for librespot nor sees an end between events.
func readSpotifyEvents(ctx context.Context, path string) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		slog.Warn("streaming: opening Spotify's event pipe failed", "err", err)
		return
	}
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	defer f.Close()
	followSpotifyEvents(f, func(title, artist, album string) {
		media.Get().SetReceivedTrack(home.SpotifyName, title, artist, album)
	})
}

// followSpotifyEvents reads event lines from r until it ends, calling told with the song on a new
// track, and with nothing when playing stops. A line too long to be one of the program's is skipped,
// not taken as the end.
func followSpotifyEvents(r io.Reader, told func(title, artist, album string)) {
	br := bufio.NewReaderSize(r, 4096)
	for {
		line, err := readLine(br, 16<<10)
		if err != nil {
			return
		}
		f := strings.Split(line, "\t")
		if len(f) < 4 {
			continue
		}
		// The program cuts each field at a byte count, which can fall inside a character.
		for i := range f {
			f[i] = strings.ToValidUTF8(f[i], "")
		}
		switch f[0] {
		case "track_changed":
			told(f[1], f[2], f[3])
		case "stopped", "session_disconnected":
			told("", "", "")
		}
	}
}

// readLine is the next line from br without its newline, or "" for one longer than most, which is read
// through and dropped.
func readLine(br *bufio.Reader, most int) (string, error) {
	var line []byte
	over := false
	for {
		part, isPrefix, err := br.ReadLine()
		if err != nil {
			return "", err
		}
		if !over {
			line = append(line, part...)
			over = len(line) > most
		}
		if !isPrefix {
			break
		}
	}
	if over {
		return "", nil
	}
	return string(line), nil
}
