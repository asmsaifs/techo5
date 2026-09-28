// Package xiaozhi is a second voice backend, sitting beside feature/voice rather than inside it.
//
// Where voice talks to Home Assistant over the ESPHome satellite protocol, this speaks xiaozhi's own:
// a WebSocket of JSON messages and Opus, to a service that does its own speech recognition, its own
// agent and its own text to speech. The two share the hardware underneath — the same post-AEC
// microphone frames, the same wake word, the same speaker — and nothing else. Which of them is
// listening is M5's decision, and this package is deliberately unaware of the question for now.
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
	defer f.mu.Unlock()
	return f.status
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

// set publishes the status: to the sensor, to the log, to the control socket and to anything that
// wants to draw it.
func (f *Feature) set(s Status) {
	if s.State == StateConnected {
		s.Uplink = uplinkRate
	}
	f.mu.Lock()
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
	err = sess.Serve(ctx, f.onEvent)

	f.mu.Lock()
	f.sess = nil
	f.mu.Unlock()
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

// onEvent is everything the server says, logged. M2 gives stt somewhere to go and M3 gives the audio
// somewhere to land; until then this is the whole of the downlink.
func (f *Feature) onEvent(e Event) {
	switch e.Type {
	case TypeSTT:
		slog.Info("xiaozhi: transcript", "text", e.Text)
	case TypeLLM:
		slog.Info("xiaozhi: reply", "state", e.State, "text", e.Text)
	case TypeTTS:
		if e.State != "" {
			slog.Info("xiaozhi: speech", "state", e.State)
		}
	case TypePing:
		slog.Debug("xiaozhi: ping")
	default:
		slog.Debug("xiaozhi: message", "type", e.Type, "state", e.State, "text", e.Text)
	}
	f.control.broadcast(reply{Event: eventMessage, Msg: &e})
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
				"bytes", st.Bytes, "pings", st.Pings)
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
