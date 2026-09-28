package xiaozhi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// The session: one WebSocket from the upgrade to the close, and the hello handshake in the middle of
// it. Nothing here holds the device's state — what the messages mean is the feature's business, and
// this only moves them.

// dialTimeout is how long the upgrade gets, which is separate from how long the socket then lives.
const dialTimeout = 15 * time.Second

// quiet is how long the server may say nothing at all before the connection is treated as gone.
//
// The protocol's own keepalive is a `ping` message the server sends, not a WebSocket ping frame, so
// silence really is the only evidence there is that anything is wrong. Three minutes is far longer
// than a turn and far shorter than a socket that will never answer again: a TCP connection to a
// dropped network can stay open for hours without either end noticing.
const quiet = 3 * time.Minute

// helloTimeout is how long the server has to answer the hello. It answers immediately or not at all.
const helloTimeout = 10 * time.Second

// CloseNotActivated is the WebSocket close code an unactivated device is sent. The upgrade succeeds,
// the hello is accepted, and then this: the authorisation boundary is the code, not the handshake,
// so there is no way to discover it by dialling.
const CloseNotActivated = 1002

// Session is one conversation with the server: the socket, the id the hello handed back, and the rate
// the server said it will send audio in.
type Session struct {
	conn *websocket.Conn

	// write serialises the send side. gorilla allows one writer at a time, and a listen start from
	// the control socket and an abort from a button are two writers the moment M3 has a speaker.
	writeMu sync.Mutex

	// statMu guards the counters, which the periodic log reads while the socket is being served.
	statMu sync.Mutex

	id   string
	rate int

	stats Stats
}

// Stats is what a session has seen, for the periodic line in the log and for the diagnostics bundle.
type Stats struct {
	// Since is when the session opened.
	Since time.Time `json:"since,omitempty"`

	// Messages counts every message the server sent, of either direction.
	Messages int `json:"messages"`

	// Text and Audio split those, and Bytes is the total length of the audio.
	Text  int `json:"text"`
	Audio int `json:"audio"`
	Bytes int `json:"bytes"`

	// Pings is how many times the server asked whether this is still here.
	Pings int `json:"pings"`
}

// dial opens the socket with the headers the protocol specifies and completes the hello.
//
// The token and the identity go in the headers rather than the first message, so a server that
// refuses this device refuses it here and the failure is a status code rather than a close frame
// after the fact.
func dial(ctx context.Context, url, token string, id identity) (*Session, error) {
	hdr := http.Header{
		"Authorization":    {"Bearer " + token},
		"Protocol-Version": {"1"},
		"Device-Id":        {id.DeviceID},
		"Client-Id":        {id.ClientID},
	}

	dialer := &websocket.Dialer{HandshakeTimeout: dialTimeout}
	conn, resp, err := dialer.DialContext(ctx, url, hdr)
	if err != nil {
		if resp != nil {
			// The status is the interesting part: a 401 is a token that went stale, which the next
			// OTA call fixes, and anything else is the endpoint rather than this device.
			return nil, fmt.Errorf("connecting to %s: %s: %w", url, resp.Status, err)
		}
		return nil, fmt.Errorf("connecting to %s: %w", url, err)
	}

	s := &Session{conn: conn, stats: Stats{Since: time.Now()}}
	if err := s.hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return s, nil
}

// hello sends what this device can do and reads what the server will do.
//
// Both directions carry an audio_params, and they are not the same. Ours is the uplink: 16 kHz mono
// in 60 ms frames, which is what the microphones already produce. The server's is the downlink, and
// it is the only statement of what the TTS will arrive as.
func (s *Session) hello() error {
	if err := s.Send(&Hello{
		Type:      TypeHello,
		Version:   1,
		Transport: "websocket",
		AudioParams: &AudioParams{
			Format:        "opus",
			SampleRate:    uplinkRate,
			Channels:      1,
			FrameDuration: frameMS(),
		},
	}); err != nil {
		return fmt.Errorf("sending hello: %w", err)
	}

	if err := s.conn.SetReadDeadline(time.Now().Add(helloTimeout)); err != nil {
		return err
	}
	for {
		kind, data, err := s.conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("waiting for the server's hello: %w", err)
		}
		if kind != websocket.TextMessage {
			continue
		}
		var h Hello
		if err := json.Unmarshal(data, &h); err != nil || h.Type != TypeHello {
			continue
		}
		s.id, s.rate = h.SessionID, downlinkRate(h.AudioParams)
		if s.rate == 0 {
			return errors.New("the server's hello has no sample rate, so there is no way to decode it")
		}
		return nil
	}
}

// ID is the session id every message after the hello has to quote.
func (s *Session) ID() string { return s.id }

// Rate is the rate the server will send audio in, in hertz, from its own hello and not from ours.
func (s *Session) Rate() int { return s.rate }

// Stats is what the session has seen so far.
func (s *Session) Stats() Stats {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	return s.stats
}

// Send writes one message. It is the only way anything goes out on this socket.
func (s *Session) Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteMessage(websocket.TextMessage, b)
}

// Listen starts or stops the microphone. There is no audio on it yet, which is the whole of M1: the
// message is real, the server's answer to it is real, and nothing has been attached to the gap.
func (s *Session) Listen(state, mode string) error {
	l := &Listen{Type: TypeListen, State: state, SessionID: s.id}
	if state == ListenStart && mode != "" {
		l.Mode = mode
	}
	return s.Send(l)
}

// Abort cuts the turn short. reason is free text the server logs.
func (s *Session) Abort(reason string) error {
	return s.Send(&Abort{Type: TypeAbort, Reason: reason, SessionID: s.id})
}

// Serve reads until the socket closes, handing every message to on.
//
// The read deadline is the only thing standing between a dropped network and a session that looks
// alive for the rest of the day, and it is refreshed by every message, so a quiet server is fine and
// a silent one is not.
func (s *Session) Serve(ctx context.Context, on func(Event)) error {
	// The read blocks in the network, so something has to close the socket for the context to reach
	// it. Closing is what a Close would have done anyway, and the resulting error is the reason this
	// is happening rather than a fault.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = s.conn.Close()
		case <-done:
		}
	}()

	for {
		if err := s.conn.SetReadDeadline(time.Now().Add(quiet)); err != nil {
			return err
		}
		kind, data, err := s.conn.ReadMessage()
		if err != nil {
			return readError(err)
		}

		s.count(kind)

		if kind == websocket.BinaryMessage {
			// Downlink audio, or whatever else the server has decided to send as bytes. It goes to
			// the same place as text, carrying only its length, until there is a decoder to hand it
			// to — which is M3's job and not this milestone's.
			s.note(len(data))
			on(Event{Type: TypeTTS, Bytes: len(data), SessionID: s.id})
			continue
		}

		var e Event
		if err := json.Unmarshal(data, &e); err != nil {
			// Unreadable is not the same as fatal. A message this client does not understand is a
			// message the server is entitled to send.
			slog.Debug("xiaozhi: a message was not readable", "raw", truncate(data))
			continue
		}
		if e.Type == TypePing {
			s.statMu.Lock()
			s.stats.Pings++
			s.statMu.Unlock()
			if err := s.Send(map[string]string{"type": TypePong, "session_id": s.id}); err != nil {
				return fmt.Errorf("answering a ping: %w", err)
			}
			on(e)
			continue
		}
		on(e)
	}
}

func (s *Session) note(n int) {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	s.stats.Bytes += n
}

// count notes one message the server sent. An unreadable one is counted too: it arrived, and a
// diagnostic that hides the messages it could not parse hides the reason to look.
func (s *Session) count(kind int) {
	s.statMu.Lock()
	defer s.statMu.Unlock()
	s.stats.Messages++
	if kind == websocket.BinaryMessage {
		s.stats.Audio++
		return
	}
	s.stats.Text++
}

// Close shuts the socket down. It is safe to call twice, because the feature closes a session on the
// way out of one loop and the supervisor closes it again on the way into the next.
func (s *Session) Close() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Close()
}

// readError says what a failed read means, which for this protocol is mostly the close code.
func readError(err error) error {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		if closeErr.Code == CloseNotActivated {
			// Worth its own sentence: the device got all the way through the upgrade and the hello
			// and was dropped at the next step, which reads like a network fault from inside a
			// WebSocket library and is not one.
			return fmt.Errorf("the server closed the session as not activated: the code has to be redeemed at xiaozhi.me first")
		}
		if closeErr.Text == "" {
			return fmt.Errorf("the server closed the session, code %d", closeErr.Code)
		}
		return fmt.Errorf("the server closed the session, code %d: %s", closeErr.Code, closeErr.Text)
	}
	return err
}

// downlinkRate is the rate to build the decoder from, falling back to what the protocol documents
// when the server leaves it out. It is 24 kHz and not the 16 kHz we send, and reading the wrong one
// is a chipmunk.
func downlinkRate(p *AudioParams) int {
	if p == nil || p.SampleRate == 0 {
		return defaultDownlinkRate
	}
	return p.SampleRate
}

// truncate keeps an unprintable message out of the log whole.
func truncate(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}
