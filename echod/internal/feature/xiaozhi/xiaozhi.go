// Package xiaozhi is a second voice backend, sitting beside feature/voice rather than inside it.
//
// Where voice talks to Home Assistant over the ESPHome satellite protocol, this speaks xiaozhi's own:
// a WebSocket of JSON messages and Opus, to a service that does its own speech recognition, its own
// agent and its own text to speech. The two share the hardware underneath — the same post-AEC
// microphone frames, the same wake word, the same speaker — and nothing else.
//
// Which of them is listening is feature/voice's decision, and the arrangement is one hook wide:
// this package asks to be given the microphone before a turn opens it and hands it over when the
// switch moves (wake.go). That direction only is deliberate. A turn here cannot ask what the other
// backend is doing without importing it, and voice already imports this one to reach the speaker
// and the switch, so the question is asked from the side that can answer it.
//
// M1 is the protocol core with no audio in either direction: the OTA call, the WebSocket, the hello,
// the listen and abort messages, and the activation wait. The exit criterion is a session that
// survives ten minutes, and the log has to be able to say so.
package xiaozhi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/mic"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

func init() {
	component.Register(component.Device, Get(), component.Order(39),
		// The supervisor is a backstop, not the reconnect path: Run holds the control socket up
		// across a dropped connection on purpose, so a terminal watching a session sees the drop and
		// the retry. This is what catches Run itself giving up.
		component.Supervise())
}

const (
	// uplinkRate is what this client sends. It is the array's own capture rate rather than a number
	// chosen here, because the two have to be the same number or the frames going out are not the
	// frames going in.
	uplinkRate = mic.Rate

	// defaultDownlinkRate is what the server sends when its hello does not say. The protocol
	// documents 24 kHz, and it is a different number from the one above — which is the entire reason
	// this decoder has to be built from the server's answer.
	defaultDownlinkRate = 24000
)

// What the client is doing, in the order it does it. These are the words on the sensor and the
// screen; a failure adds its reason after a colon.
const (
	StateOff          = "off"
	StateConnecting   = "connecting"
	StateActivating   = "activating"
	StateConnected    = "connected"
	StateDisconnected = "disconnected"
)

// How the reconnect backs off, and how often the activation code is asked for again.
const (
	retryInitial = 5 * time.Second
	retryMax     = 5 * time.Minute

	// activatePoll is how often the OTA endpoint is asked whether the code has been redeemed. Five
	// seconds is the interval a person typing a six-digit code wants, and it is a small JSON request
	// from a device that is doing nothing else while it waits.
	activatePoll = 5 * time.Second

	// reportEvery is how often a live session says it is still live. Once a minute, so an operator
	// watching over ssh finds out in a minute rather than in ten minutes that the log has gone
	// quiet, and so a ten-minute hold leaves a line in it either way. M6 re-tunes this to thirty
	// seconds and adds the CPU and packet counters.
	reportEvery = time.Minute
)

// Status is what the client is doing, for the sensor, the log and the control socket.
//
// It deliberately carries no token and no client id. The socket is root-only, but a status is the
// one thing here likely to be pasted into a bug report, and neither of those belongs in one.
type Status struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`

	// Host is the OTA endpoint in use, and URL the WebSocket it handed back. Both are worth having
	// in the log: which endpoint answered is the first question when a session will not open.
	Host string `json:"host,omitempty"`
	URL  string `json:"url,omitempty"`

	// Code and Challenge are the activation, while there is one to show.
	Code      string `json:"activation_code,omitempty"`
	Challenge string `json:"challenge,omitempty"`

	// Session and the two rates are what a live session is using.
	Session  string `json:"session,omitempty"`
	Uplink   int    `json:"uplink_hz,omitempty"`
	Downlink int    `json:"downlink_hz,omitempty"`

	// Since is when the session opened, and Stats what it has seen since.
	Since time.Time `json:"since,omitempty"`
	Stats Stats     `json:"stats,omitempty"`

	// Listening says a turn is running. It is on the sensor and in the socket, and not in the log,
	// where the turn's own opening and closing lines already say it.
	Listening bool `json:"listening,omitempty"`

	// Speaking says an answer is running, which is not the same thing as a turn running: a turn
	// ends when the last of the microphone's audio has been sent, and the answer to it comes after.
	// A device can be silent mid-turn waiting for the cloud, and saying "listening" for that is
	// what a person watching the sensor cannot tell the difference between.
	Speaking bool `json:"speaking,omitempty"`
}

// Feature is the xiaozhi client: a switch, a state, and the connection behind them.
type Feature struct {
	// Changed fires when the status changes, which is how the settings page will learn there is a
	// code to put on the screen.
	Changed hook.Hook[struct{}]

	enabled *esphome.Switch
	state   *esphome.TextSensor
	control *Control

	mu     sync.Mutex
	status Status
	sess   *Session
	turn   *turn

	// down is the speaking direction of the current session, and the only way anything in this file
	// can make a sound. It is here rather than being made where it is used so that Stop has
	// something to reach: silencing the device has to work when there is no turn to cancel, because
	// the longest answers are all in that state — the microphone finished sending seconds ago and
	// the cloud is still talking.
	//
	// The lock is taken before the downlink's own and never after it, so the two cannot deadlock.
	// It is held for the length of a pointer read and nothing else.
	down *downlink

	// turns numbers the turns of a whole client, so a terminal that asked for one can tell that
	// one ending from another one starting. It does not reset with a session: a terminal that
	// asked across a reconnect is still waiting for the turn it asked for.
	turns atomic.Int32

	// asked records that a session was ended on request rather than by the endpoint, so hold can
	// tell the two apart. Without it a terminal asking to reconnect would either do nothing, or
	// reconnect in a tight loop past the backoff that is there to stop that happening.
	asked atomic.Bool

	// wake cuts a wait short, when the switch changes or the CLI asks for something.
	wake chan struct{}
}

// errParked is what a wait returns when the client was switched off while it was waiting. It is not
// a failure: nothing is wrong, and the Run loop parks on it rather than backing off from it.
var errParked = errors.New("switched off while waiting")

var (
	once   sync.Once
	shared *Feature
)

func Get() *Feature {
	once.Do(func() { shared = build() })
	return shared
}

func build() *Feature {
	f := &Feature{wake: make(chan struct{}, 1)}
	f.control = newControl(f)

	f.enabled = &esphome.Switch{
		Base: esphome.Base{
			ObjectID: "xiaozhi", Name: "Xiaozhi", Icon: "mdi:message-text-voice",
			Category: esphome.CategoryConfig,
		},
		OnCommand: func(on bool) {
			f.enabled.Set(on)
			if err := config.Set().Xiaozhi().Enabled(on); err != nil {
				slog.Error("saving a setting failed", "setting", f.enabled.ObjectID, "err", err)
			}
			slog.Info("xiaozhi: switched", "enabled", on)
			f.interrupt()
		},
	}
	f.state = &esphome.TextSensor{
		Base: esphome.Base{
			ObjectID: "xiaozhi_state", Name: "Xiaozhi state", Icon: "mdi:cloud-check-outline",
			Category: esphome.CategoryDiagnostic,
		},
	}
	f.set(Status{State: StateOff})
	return f
}

func (f *Feature) Name() string { return "xiaozhi" }

func (f *Feature) Entities() []esphome.Entity { return []esphome.Entity{f.enabled, f.state} }

// Restore only publishes the switch. Connecting is Run's business, so that a restart and a first
// boot take the same path.
func (f *Feature) Restore(c config.Config) {
	f.enabled.Set(c.Xiaozhi.Enabled)
	slog.Info("restored", "what", f.enabled.ObjectID, "using", c.Xiaozhi.Enabled)
}

// Actions: the three settings that would otherwise be entity state, and two of them are secrets.
//
// The token and the client id are credentials and Home Assistant's recorder keeps entity state in its
// history, so they are arguments here and are never published. The host is not a secret but has no
// entity of its own, and it is the one setting a self-hosted server needs.
func (f *Feature) Actions() []*esphome.Action {
	return []*esphome.Action{
		{
			Name: "xiaozhi_host",
			Args: []esphome.Arg{{Name: "host", Type: esphome.ArgString}},
			Run: func(c esphome.Call) (any, error) {
				host := strings.TrimSpace(c.String("host"))
				slog.Info("xiaozhi: host set", "host", host, "endpoint", otaURL(host))
				f.interrupt()
				return nil, config.Set().Xiaozhi().Host(host)
			},
		},
		{
			Name: "xiaozhi_token",
			Args: []esphome.Arg{{Name: "token", Type: esphome.ArgString}},
			Run: func(c esphome.Call) (any, error) {
				slog.Info("xiaozhi: token set", "set", c.String("token") != "")
				f.interrupt()
				return nil, config.Set().Xiaozhi().Token(strings.TrimSpace(c.String("token")))
			},
		},
		{
			Name: "xiaozhi_client_id",
			Args: []esphome.Arg{{Name: "client_id", Type: esphome.ArgString}},
			Run: func(c esphome.Call) (any, error) {
				id := strings.TrimSpace(c.String("client_id"))
				if id != "" && !strings.Contains(id, "-") {
					// The endpoint answers a bare 400 to an uncoloured id, with a body that names
					// neither field. Catching it here turns a mystery into a sentence.
					return nil, errClientID
				}
				slog.Info("xiaozhi: client id set", "set", id != "")
				f.interrupt()
				return nil, config.Set().Xiaozhi().ClientID(id)
			},
		},
	}
}

// Status is what the client is doing now.
func (f *Feature) Status() Status {
	f.mu.Lock()
	s, sess, down := f.status, f.sess, f.down
	f.mu.Unlock()

	// The counters are read off the session rather than kept here, because a second copy of them
	// is a copy that stops agreeing: the status is published once when a session opens and then
	// republished whole for every turn, and a copy taken at the first of those would still say
	// zero packets by the tenth turn.
	if sess != nil {
		s.Stats = sess.Stats()
	}
	// Asked of the downlink outside the lock above, for the same reason the counters are: it is a
	// different lock and there is no reason to hold this one while taking it.
	if down != nil {
		s.Speaking = down.isSpeaking()
	}
	return s
}

// downlink is the speaking direction of the live session, or nil: an off client, a failed one, or
// a moment between the two.
func (f *Feature) downlink() *downlink {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.down
}

// session is the live one, or an error saying why there is not: a terminal asking for a listen on a
// client that is off, or waiting for an activation code, is asking for something that does not exist
// yet, and the answer should say which.
func (f *Feature) session() (*Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sess == nil {
		return nil, fmt.Errorf("no xiaozhi session: it is %s", f.status.State)
	}
	return f.sess, nil
}

// interrupt makes the client stop what it is doing and look at the settings again.
//
// It closes the session rather than nudging it, because the loop spends nearly all of its time
// inside one: for the cloud that is up to ten minutes before it hangs up on its own, and a switch
// moved from a terminal in the meantime would not be looked at until then. A terminal turning the
// client off, or asking it to fetch a new token, means it now.
func (f *Feature) interrupt() {
	f.asked.Store(true)
	if sess, err := f.session(); err == nil {
		_ = sess.Close()
	}
	f.wakeMe()
}

// wakeMe is the nudge on its own, for a change that has nothing to close: a new host or token is
// still in the config, but the session in hand was opened against the old one.
func (f *Feature) wakeMe() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// pause is sleep, but a wait a person can cut short.
//
// The backoff reaches five minutes and the activation poll is five seconds. A switch moved from a
// terminal in the middle of either has to take effect when it is moved, not when the timer ends.
func (f *Feature) pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-f.wake:
		return true
	case <-t.C:
		return true
	}
}

// Activation is the code the owner still has to redeem, and whether there is one to show.
//
// It is a method of its own rather than a field of the published Status because the screen asks it
// every frame: Status is a snapshot built for somebody reading it once a minute, over the control
// socket or on the sensor, and walking it fifty times a second to pick one string out is a cost
// paid in the same lock the session takes to hand over a finished frame.
func (f *Feature) Activation() (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status.State != StateActivating || f.status.Code == "" {
		return "", false
	}
	return f.status.Code, true
}

// set publishes the status: to the sensor, to the log, to the control socket and to anything that
// wants to draw it.
func (f *Feature) set(s Status) {
	if s.State == StateConnected {
		s.Uplink = uplinkRate
	}
	f.mu.Lock()
	// Read here rather than passed in, because a turn opening is the thing that changes it and
	// whoever opened it cannot say so without a lock it does not hold.
	s.Listening = f.turn != nil
	f.status = s
	f.mu.Unlock()

	shown := s.State
	if s.Detail != "" {
		shown += ": " + s.Detail
	}
	f.state.Set(component.Fit(shown))
	f.control.broadcast(reply{Event: eventStatus, Status: &s})
	f.Changed.Emit(struct{}{})
}

// Run holds the client up: the control socket for as long as the process lives, and a session
// whenever the switch is on.
//
// Reconnection is here rather than left to the supervisor, because the control socket has to survive
// a dropped connection. An operator watching a session from a terminal wants to watch it drop and
// watch it come back, and a supervisor restart would take the socket with it and say nothing about
// which of the two happened.
func (f *Feature) Run(ctx context.Context) error {
	defer f.close()
	if err := f.control.serve(ctx); err != nil {
		return err
	}
	f.set(Status{State: StateOff})

	wait := retryInitial
	for ctx.Err() == nil {
		switch {
		case !config.Get().Xiaozhi.Enabled:
			// Off. Park until somebody wants it, rather than reconnecting into a switch that is off.
			f.set(Status{State: StateOff})
			select {
			case <-ctx.Done():
			case <-f.wake:
			}

		case f.hold(ctx) != nil:
			// hold says what went wrong and has already published it; here it is the delay.
			if !f.pause(ctx, wait) {
				return nil
			}
			slog.Info("xiaozhi: reconnecting", "in", wait, "host", otaURL(config.Get().Xiaozhi.Host))
			wait = min(wait*2, retryMax)

		default:
			// hold ended cleanly, which means the switch went off or the context is done. The top of
			// the loop sees that and parks, and the backoff is forgotten.
			wait = retryInitial
		}
	}
	return nil
}

// hold is one attempt: find the server, wait out the activation if there is one, open a session and
// serve it until it ends.
//
// It returns nil when there is nothing to retry — the switch went off, or the process is shutting
// down — and the Run loop parks on it. Anything else is a failure worth waiting before repeating.
func (f *Feature) hold(ctx context.Context) error {
	// Anything asked before this attempt is already what this attempt is. A wake that arrived while
	// the client was parked has been acted on by getting here, and carrying it into the session
	// would make the next real failure reconnect without a backoff.
	f.asked.Store(false)

	id, err := identify(ctx)
	if err != nil {
		return f.fail(err)
	}
	host := otaURL(config.Get().Xiaozhi.Host)
	f.set(Status{State: StateConnecting, Host: host})

	info, err := f.awaitActivation(ctx, id, host)
	if err != nil {
		// A wait that ended because the switch went off, or because the process is shutting down,
		// is not a failure to report and not one to back off from.
		if errors.Is(err, errParked) || ctx.Err() != nil {
			return nil
		}
		return f.fail(err)
	}

	// The token is a cache, not a setting: it comes from the OTA call, and the next call replaces
	// it. Keeping it means a session can be re-opened from a terminal without a round trip first.
	if tok := info.Websocket.Token; tok != "" && tok != config.Get().Xiaozhi.Token {
		if err := config.Set().Xiaozhi().Token(tok); err != nil {
			slog.Warn("xiaozhi: caching the token failed", "err", err)
		}
	}

	sess, err := dial(ctx, info.Websocket.URL, info.Websocket.Token, id)
	if err != nil {
		return f.fail(err)
	}
	defer func() { _ = sess.Close() }()

	f.mu.Lock()
	f.sess = sess
	f.mu.Unlock()
	f.set(Status{
		State: StateConnected, Host: host, URL: info.Websocket.URL,
		Session: sess.ID(), Uplink: uplinkRate, Downlink: sess.Rate(), Since: time.Now(),
	})
	slog.Info("xiaozhi: connected",
		"session", sess.ID(), "endpoint", info.Websocket.URL,
		"uplink_hz", uplinkRate, "downlink_hz", sess.Rate(),
		"frame_ms", frameMS(), "bitrate", config.Get().Xiaozhi.Bitrate)

	safe.Go("xiaozhi report", func() { f.report(ctx, sess) })

	// The decoder is built from the rate the speaker takes and told the rate the server will use,
	// so a session whose audio is not playable here is a sentence said once at the front rather than
	// a chipmunk heard all the way through. It belongs to this session and to the goroutine that
	// reads this socket: the codec is stateful, and a decoder two sessions old is one that has been
	// carrying a silence across a reconnect.
	down, err := newDownlink(speakerGet(), sess.Rate(), f.speech)
	if err != nil {
		return f.fail(err)
	}
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
	// Handed the pointer rather than looked up when it is needed, so that a packet read from this
	// socket cannot be decoded into a session that has already been replaced — a reconnect's
	// leftover audio arriving in the middle of the next answer is a sound with no message to go
	// with it.
	defer func() {
		f.mu.Lock()
		f.down = nil
		f.mu.Unlock()
		down.close()
	}()

	err = sess.Serve(ctx, func(e Event) { f.onEvent(e, down) })

	f.mu.Lock()
	f.sess = nil
	f.mu.Unlock()
	// A turn belonged to the socket that just went. Ending it here rather than letting its next
	// write discover the fact is what stops a dropped session from leaving the microphone open for
	// as long as the reconnect takes.
	f.Stop()

	if f.asked.Swap(false) {
		// Somebody asked for this session to end — the switch, or a terminal. That is not something
		// the endpoint did and there is nothing to back off from, so the loop looks at the settings
		// and tries again straight away.
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}
	return f.fail(err)
}

// awaitActivation asks the endpoint where to connect, and stays there while there is still a code to
// redeem.
//
// The official cloud does not refuse an unactivated device at the WebSocket: it completes the
// upgrade, accepts the hello and closes with 1002 a moment later. So this is the only place the
// wait can happen, and it is why the code is polled rather than shown once — the owner may not be in
// the room yet, and the device is doing nothing else while it waits.
func (f *Feature) awaitActivation(ctx context.Context, id identity, host string) (*ota, error) {
	endpoint := otaURL(config.Get().Xiaozhi.Host)
	info, err := fetchOTA(ctx, endpoint, id)
	if err != nil {
		return nil, err
	}
	shown := false

	for {
		code, needed := info.code()
		if !needed {
			break
		}
		if !shown {
			// Once, not every poll: the log is where somebody looks to find out what is happening,
			// and the sensor and the socket are where the code goes to be shown.
			slog.Info("xiaozhi: this device has to be activated",
				"code", code.Code, "at", "xiaozhi.me", "code_is_shown_on", f.state.ObjectID)
			shown = true
		}
		f.set(Status{State: StateActivating, Host: host, Code: code.Code, Challenge: code.Challenge})

		if !config.Get().Xiaozhi.Enabled {
			return nil, errParked
		}
		if !f.pause(ctx, activatePoll) {
			return nil, ctx.Err()
		}
		if info, err = fetchOTA(ctx, endpoint, id); err != nil {
			return nil, err
		}
	}

	if shown {
		if err := config.Set().Xiaozhi().Activated(true); err != nil {
			slog.Warn("xiaozhi: remembering the activation failed", "err", err)
		}
		slog.Info("xiaozhi: activated", "code_redeemed", true)
	}
	return info, nil
}

// onEvent is everything the server says. A tts start and a tts stop are the two moments the
// downlink needs to know about, and a binary frame is a packet to hand it; everything else is
// logged and passed on.
//
// The downlink is a parameter rather than something looked up, because this runs on the socket's
// goroutine and the session it belongs to may be gone by the time anybody asked.
func (f *Feature) onEvent(e Event, down *downlink) {
	switch e.Type {
	case TypeSTT:
		slog.Info("xiaozhi: transcript", "text", e.Text)
		onScreenEvent(e)
		// A transcript means the server has decided the utterance is over. A turn it ends itself
		// (auto mode) would otherwise keep sending through the answer and hold the microphone.
		f.mu.Lock()
		t := f.turn
		f.mu.Unlock()
		if t != nil && !t.manual {
			t.serverDone.Store(true)
			safe.Go("xiaozhi end turn", f.Cancel)
		}
	case TypeLLM:
		slog.Info("xiaozhi: reply", "state", e.State, "text", e.Text)
		onScreenEvent(e)
	case TypeTTS:
		onScreenEvent(e)
		f.onSpeech(e, down)
		return
	case TypePing:
		slog.Debug("xiaozhi: ping")
	default:
		slog.Debug("xiaozhi: message", "type", e.Type, "state", e.State, "text", e.Text)
	}
	f.control.broadcast(reply{Event: eventMessage, Msg: &e})
}

// onSpeech is the two directions of the same message: a tts start or stop is text, and anything
// with a packet in it is audio.
//
// A server that sends audio without a start is handled rather than refused — the packet is
// playable, so it is played — but that is a fallback, not a thing to rely on, and the log says
// so when it happens.
func (f *Feature) onSpeech(e Event, down *downlink) {
	switch {
	case len(e.Audio) > 0:
		down.push(e.Audio)
		return
	case e.State == TTSStart:
		slog.Debug("xiaozhi: speech started", "session", e.SessionID)
		down.started()
	case e.State == TTSStop:
		slog.Debug("xiaozhi: speech stopped", "session", e.SessionID)
		down.stopped()
	default:
		// A tts with neither a state nor a packet. The server is entitled to send it; there is
		// nothing to do with it, and saying so at anything above debug would be the log's own
		// version of a chipmunk: a noise with no explanation behind it.
		slog.Debug("xiaozhi: an empty speech message", "state", e.State, "bytes", e.Bytes)
		return
	}
	f.control.broadcast(reply{Event: eventMessage, Msg: &e})
}

// speech is where a stretch of speech goes when it has been heard. It is called from the speaker's
// write loop, off the lock, so the log and the socket are not answered while a frame is late.
func (f *Feature) speech(s *Speech) {
	slog.Info("xiaozhi: speech ended",
		"speech", s.ID, "state", s.State,
		"packets", s.Packets, "bytes", s.Bytes, "seconds", s.Seconds, "kbps", s.Kbps,
		"decode_cpu_pct", s.DecodePct, "latency_s", s.Latency, "peak", s.Peak,
		"late", s.Late, "dropped", s.Dropped, "failed", s.Failed)
	f.touch()
	f.control.broadcast(reply{Event: eventSpeech, Speech: s})

	// Only an answer heard to the end asks for a follow-up: a stopped or interrupted one means
	// somebody wanted it to stop.
	if s.State == "the tail was heard" && s.Packets > 0 {
		answered.mu.Lock()
		fn := answered.fn
		answered.mu.Unlock()
		if fn != nil {
			safe.Go("xiaozhi follow-up", fn)
		}
	}
}

// report says a live session is still live, on a timer rather than on traffic, because a session
// with nothing to say is exactly the one worth knowing about.
func (f *Feature) report(ctx context.Context, sess *Session) {
	tick := time.NewTicker(reportEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			st := sess.Stats()
			slog.Info("xiaozhi: still connected",
				"uptime", time.Since(st.Since).Round(time.Second),
				"messages", st.Messages, "text", st.Text, "audio", st.Audio,
				"bytes", st.Bytes, "pings", st.Pings,
				"sent", st.Sent, "sent_bytes", st.SentBytes)
		}
	}
}

// fail publishes a failure and hands it back for the backoff.
func (f *Feature) fail(err error) error {
	if err == nil {
		return nil
	}
	slog.Warn("xiaozhi: the session failed", "err", err)
	f.set(Status{State: StateDisconnected, Detail: err.Error()})
	return err
}

func (f *Feature) close() {
	// Before the session, so the turn's own listen stop has a socket to go out on. A turn that is
	// torn down after the socket has gone logs the failure and the server never hears about it,
	// which is the same outcome with a worse log line.
	f.Stop()

	f.mu.Lock()
	sess := f.sess
	f.sess = nil
	f.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
	f.control.stop()
}

// frameMS is the uplink frame length. Sixty is the measured setting: at twenty, a variable bitrate
// overshoots the one asked for by nearly twofold, and the protocol wants sixty anyway.
func frameMS() int { return config.Get().Xiaozhi.Frame() }
