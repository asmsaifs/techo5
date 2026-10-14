package api

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"
)

// A client that drops its connection as soon as it is made (#136: Home Assistant with the wrong key,
// some 20 times a second) put a "connection closed" line in the log for every try. The log is kept
// short, and the diagnostics carry only its last lines, so within a minute or so the flood had pushed
// out whatever came before it, which is what would say why. floodGuard lets through the first few
// each minute and the first from each address, says once that a client keeps reconnecting, counts
// the rest, and says how many when the minute is up. The library logs a refused handshake from any
// address on the network the same way, which is why each new address still gets its line.

// closedSeen is how many "connection closed" lines a minute pass through before the rest are counted,
// besides the first from each address.
const closedSeen = 3

// floodPeers is how many addresses a minute get their first line through.
const floodPeers = 32

// floodWindow is the minute.
const floodWindow = time.Minute

// closes is the count, shared by every server the device listens with.
var closes = &floodState{now: time.Now, after: time.AfterFunc}

type floodState struct {
	now   func() time.Time
	after func(time.Duration, func()) *time.Timer

	mu      sync.Mutex
	out     slog.Handler // where the minute's count is said: the handler the guard was made over
	since   time.Time    // the minute's start; zero for none running
	n       int          // closed connections this minute
	kept    int          // of them, counted rather than logged
	peers   map[string]bool
	lastErr string
}

// floodGuard is a handler that passes everything on but the "connection closed" lines past the first
// few and the first from each address in a minute. peer is the address a logger made With it carries.
type floodGuard struct {
	slog.Handler
	root slog.Handler
	peer string
	st   *floodState
}

// newFloodGuard is the guard over h, counting in closes.
func newFloodGuard(h slog.Handler) floodGuard { return floodGuard{Handler: h, root: h, st: closes} }

func (h floodGuard) Handle(ctx context.Context, r slog.Record) error {
	if r.Message != "connection closed" {
		return h.Handler.Handle(ctx, r)
	}
	errText := ""
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "err" {
			errText = a.Value.String()
			return false
		}
		return true
	})
	st := h.st
	st.mu.Lock()
	if st.since.IsZero() {
		st.since, st.n, st.kept, st.peers, st.out = st.now(), 0, 0, map[string]bool{}, h.root
		st.after(floodWindow, st.flush)
	}
	st.n++
	st.lastErr = errText
	newPeer := h.peer != "" && !st.peers[h.peer] && len(st.peers) < floodPeers
	if newPeer {
		st.peers[h.peer] = true
	}
	pass := st.n <= closedSeen || newPeer
	warn := false
	if !pass {
		st.kept++
		warn = st.kept == 1
	}
	st.mu.Unlock()
	switch {
	case pass:
		return h.Handler.Handle(ctx, r)
	case warn:
		w := slog.NewRecord(r.Time, slog.LevelWarn, "a client keeps reconnecting; further closed connections this minute are counted", 0)
		w.AddAttrs(slog.String("err", errText))
		return h.Handler.Handle(ctx, w)
	}
	return nil
}

// flush ends the minute: when any closed connections were counted rather than logged, says how many
// there were, over how long (at most the minute) and the last error.
func (st *floodState) flush() {
	st.mu.Lock()
	n, kept, since, last, out := st.n, st.kept, st.since, st.lastErr, st.out
	st.since, st.n, st.kept = time.Time{}, 0, 0
	now := st.now()
	st.mu.Unlock()
	if kept == 0 || out == nil {
		return
	}
	s := slog.NewRecord(now, slog.LevelWarn, "a client kept reconnecting", 0)
	s.AddAttrs(slog.Int("closed", n), slog.Duration("within", min(now.Sub(since), floodWindow).Round(time.Second)),
		slog.String("last_err", last))
	_ = out.Handle(context.Background(), s)
}

func (h floodGuard) WithAttrs(attrs []slog.Attr) slog.Handler {
	g := floodGuard{Handler: h.Handler.WithAttrs(attrs), root: h.root, peer: h.peer, st: h.st}
	for _, a := range attrs {
		if a.Key == "peer" {
			g.peer = a.Value.String()
			if host, _, err := net.SplitHostPort(g.peer); err == nil {
				g.peer = host // the address, not the port each connection gets a new one of
			}
		}
	}
	return g
}

func (h floodGuard) WithGroup(name string) slog.Handler {
	return floodGuard{Handler: h.Handler.WithGroup(name), root: h.root, peer: h.peer, st: h.st}
}
