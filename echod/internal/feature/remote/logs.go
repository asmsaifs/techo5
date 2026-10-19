package remote

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/diag"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
)

// The log, as it is written, as server-sent events: the last of it first, then each line as it comes,
// with the same parts taken out as the diagnostics download takes out (diag.Redactor). What is left still
// names rooms, devices and the like, which is why it is behind the key.

const (
	// maxStreams is how many can follow the log at once: one somebody watching, one spare.
	maxStreams = 2

	// streamFor is the longest one stream runs; a client that wants more asks again.
	streamFor = 30 * time.Minute

	// backlog is how many lines from before it was asked for a stream starts with, read from at most
	// backlogBytes of the end of the file.
	backlog      = 200
	backlogBytes = 256 << 10

	// pollEvery is how often the file is looked at for more, and keepAliveEvery how often a stream with
	// nothing new says so, so a proxy between does not take it for dead.
	pollEvery      = 500 * time.Millisecond
	keepAliveEvery = 15 * time.Second

	// redactEvery is how often a stream learns again what to take out.
	redactEvery = time.Minute
)

// logPath is the log; a variable for the tests.
var logPath = layout.LogPath

// redactLine takes the private parts out of a line; set per stream, a variable for the tests.
var newRedactor = func() func(string) string { return diag.Redactor().Text }

var streams atomic.Int32

// logs follows the log.
func (f *Remote) logs(w http.ResponseWriter, r *http.Request) {
	if streams.Add(1) > maxStreams {
		streams.Add(-1)
		http.Error(w, "the log is being followed twice already", http.StatusTooManyRequests)
		return
	}
	defer streams.Add(-1)
	flush, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "this connection cannot stream", http.StatusInternalServerError)
		return
	}

	secretAtStart := config.Get().Remote.Secret
	file, err := os.Open(logPath)
	if err != nil {
		http.Error(w, "the log could not be read", http.StatusServiceUnavailable)
		return
	}
	defer func() { file.Close() }()
	redact, redactedAt := newRedactor(), time.Now()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)

	send := func(line string) bool {
		// A line is one event: a carriage return inside one would end it early, and what followed would
		// read as a field of the stream's own.
		line = strings.ReplaceAll(line, "\r", "")
		if _, err := fmt.Fprintf(w, "data: %s\n\n", redact(withoutWords(line))); err != nil {
			return false
		}
		return true
	}

	offset, last := tail(file)
	for _, line := range last {
		if !send(line) {
			return
		}
	}
	flush.Flush()

	end := time.After(streamFor)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	quietSince := time.Now()
	partial := ""
	for {
		select {
		case <-r.Context().Done():
			return
		case <-end:
			fmt.Fprint(w, "event: end\ndata: the stream ran its course; ask again for more\n\n")
			flush.Flush()
			return
		case <-tick.C:
		}
		// Turned off, or a new key made: what was let in by the old one stops here.
		if !f.open() || config.Get().Remote.Secret != secretAtStart {
			return
		}
		// Names learned since the stream began (a Wi-Fi joined, a speaker paired) are taken out too.
		if time.Since(redactedAt) >= redactEvery {
			redact, redactedAt = newRedactor(), time.Now()
		}

		// A log that got shorter was started again (it is when it grows past a size as the daemon starts):
		// read the new one from its beginning.
		if st, err := os.Stat(logPath); err == nil && st.Size() < offset {
			if again, err := os.Open(logPath); err == nil {
				file.Close()
				file, offset, partial = again, 0, ""
			}
		}
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			return
		}
		data, err := io.ReadAll(io.LimitReader(file, backlogBytes))
		if err != nil {
			return
		}
		offset += int64(len(data))
		text := partial + string(data)
		lines := strings.Split(text, "\n")
		partial = lines[len(lines)-1]
		sent := false
		for _, line := range lines[:len(lines)-1] {
			if !send(strings.TrimRight(line, "\r")) {
				return
			}
			sent = true
		}
		switch {
		case sent:
			quietSince = time.Now()
			flush.Flush()
		case time.Since(quietSince) >= keepAliveEvery:
			if _, err := fmt.Fprint(w, ": still here\n\n"); err != nil {
				return
			}
			quietSince = time.Now()
			flush.Flush()
		}
	}
}

// tail is where the file ends now, and its last lines, from at most backlogBytes before that.
func tail(file *os.File) (int64, []string) {
	st, err := file.Stat()
	if err != nil {
		return 0, nil
	}
	size := st.Size()
	from := max(size-backlogBytes, 0)
	if _, err := file.Seek(from, io.SeekStart); err != nil {
		return size, nil
	}
	var lines []string
	sc := bufio.NewScanner(io.LimitReader(file, size-from))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	first := from > 0
	for sc.Scan() {
		if first {
			first = false // the rest of a line that began before the window
			continue
		}
		lines = append(lines, sc.Text())
	}
	if len(lines) > backlog {
		lines = lines[len(lines)-backlog:]
	}
	return size, lines
}

// spoken is a log line's fields that hold words: what was heard and said, what was announced, what
// the assistant's tools were asked and answered (a page's text, a calendar's events), an event's,
// alarm's or reminder's words and a notification's message. The stream
// gives only their length: the device's own log keeps them, and the API is not encrypted.
var spoken = regexp.MustCompile(`\b(text|result|args|summary|words|label|message)=("(?:[^"\\]|\\.)*"|\S*)`)

// withoutWords is a line with the words in it given as their length.
func withoutWords(line string) string {
	return spoken.ReplaceAllStringFunc(line, func(m string) string {
		k, v, _ := strings.Cut(m, "=")
		if unq, err := strconv.Unquote(v); err == nil {
			v = unq
		}
		return k + "=<" + strconv.Itoa(len([]rune(v))) + " characters>"
	})
}
