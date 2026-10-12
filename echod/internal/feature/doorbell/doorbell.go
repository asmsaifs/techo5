// Package doorbell makes the device a doorbell's chime: a press of the button rings it and puts the
// door's camera up on the screen, with no Home Assistant in between.
//
// The press comes from the Reolink recorder set up here (Home.Reolink), as the ONVIF event Reolink
// calls Visitor, by pull point (lib/onvifevent). It has to come through a recorder: a Reolink battery
// doorbell on its own lists Visitor among its events and never sends it, and has no web page for the
// webhook its newer firmware is said to have, while the same doorbell on a Home Hub sends a Visitor
// within a second of every press, on its own channel, apart from its motion and person events.
//
// A doorbell rings like any doorbell: every press, whatever the hour. Quiet hours are for the
// device's own business, and somebody at the door at night is not that. A call is the one thing it
// gives way to: during one there is no chime and no camera, only "Someone's at the door" on the call's
// own page (AtDoor), and a Dot, with no page, stays silent. The chime would go down the line, and the
// camera would be under the call page anyway.
//
// The chime goes out on the bell (speaker.Player.Bell), which an alarm or a timer being stopped
// clears: stopping one of those while the doorbell sounds cuts the chime short too. Both at once is
// rare, and whoever is stopping the alarm is in the room to hear the door.
package doorbell

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/phone"
	"github.com/HuskerMinion/techo5/echod/internal/feature/ring"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
	"github.com/HuskerMinion/techo5/echod/internal/lib/onvifevent"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

const (
	// life is how long a subscription lasts unless renewed, and renewEvery how often it is: well
	// inside it, so one renewal lost to a busy recorder does not lose the subscription.
	life       = 5 * time.Minute
	renewEvery = 2 * time.Minute

	// pullWait is how long one ask waits for something to happen, so an idle doorbell costs one
	// request every pullWait. pullFloor is the least time between asks: a recorder that answers at
	// once, every time, is not let keep the device in a loop.
	pullWait  = 20 * time.Second
	pullFloor = 200 * time.Millisecond

	// again is the least time between two rings. A press is one Visitor going true; a finger held
	// or pressing twice in a second is still one visitor. Past mostRings in a minute nothing rings
	// until the minute is up: a recorder sending presses by the dozen is not somebody at the door.
	again     = 3 * time.Second
	mostRings = 5
)

// retryMin and retryMax bound the wait before trying a recorder that failed again. lookEvery is how
// often a doorbell that cannot be listened to at all looks at its settings again, for a change the
// setup page did not poke about. Variables so a test need not wait them out.
var (
	retryMin  = 5 * time.Second
	retryMax  = time.Minute
	lookEvery = time.Minute
)

// Event is what a press puts on Home Assistant's bus, with the camera and the device's name, for an
// automation that wants to know about the door too.
const Event = "esphome.techo5_doorbell"

// chimeLevel is the chime's loudness within its volume step: its strikes peak near full scale, since
// a struck bar is loud only for a moment and then dies away, where a beep at the bell's own 0.6
// (ring/bell.go) holds its level all through.
const chimeLevel = 0.9

// atDoorFor is how long "Someone's at the door" stays on a call's page after a press.
const atDoorFor = time.Minute

// chime plays the chime at the louder of the alarm volume and the volume the device is at now: a
// doorbell is never quieter than an alarm, and turning the device up turns its doorbell up too.
func chime() {
	speaker.Sound().Interject(func(p *speaker.Player) {
		p.Bell(max(ring.Level(), p.Step()), chimeLevel, dingDong()...)
	})
}

// inCall is whether a call has the device: ringing, dialing or talking, over the phone line or the
// intercom.
func inCall() bool { return phone.Get().State().Phase != phone.Idle }

// announce tells Home Assistant about a press.
func announce(camera string) {
	component.Fire.Emit(component.Event{Name: Event, Data: map[string]string{
		"camera": camera,
		"device": config.Get().Device.Name,
	}})
}

// Status is how the doorbell is doing, for the setup page.
type Status struct {
	Camera    string    // the doorbell's camera, "" for none
	Listening bool      // a subscription is open and being asked
	Since     time.Time // when Listening or Problem last changed
	Problem   string    // why it is not listening, when it should be
	LastPress time.Time
}

// Feature is the doorbell.
type Feature struct {
	mu     sync.Mutex
	status Status
	rang   []time.Time // the rings in the last minute, oldest first
	atDoor time.Time   // a press during a call, shown on the call's page until atDoorFor after it
	poke   chan struct{}

	// Changed fires when AtDoor may have changed; listeners must not block.
	Changed hook.Hook[struct{}]

	// What a press does, the settings and the clock, as variables so a test can be the world.
	ring     func()
	show     func(entity string)
	tell     func(entity string)
	calling  func() bool
	settings func() (target, bool, string)
	now      func() time.Time
	waited   func(d time.Duration) // told of each wait Run takes, for a test; nil in use

	// callsEnd has over is called each time a call ends, from Run's start; nil in a test.
	callsEnd func(over func())
	hooked   sync.Once
}

var shared = &Feature{
	poke:     make(chan struct{}, 1),
	ring:     chime,
	show:     showCamera,
	tell:     announce,
	calling:  inCall,
	settings: current,
	now:      time.Now,
	callsEnd: func(over func()) {
		phone.Get().Changed.Listen(func(s phone.State) {
			if s.Phase == phone.Idle {
				over()
			}
		})
	},
}

// callOver forgets a press made during a call once the call is over: "Someone's at the door" was
// that call's news, not the next one's.
func (f *Feature) callOver() {
	f.mu.Lock()
	was := !f.atDoor.IsZero()
	f.atDoor = time.Time{}
	f.mu.Unlock()
	if was {
		f.Changed.Emit(struct{}{})
	}
}

func init() {
	component.Register(component.Network, shared, component.Order(76))
}

func Get() *Feature { return shared }

func (f *Feature) Name() string { return "doorbell" }

// Chime plays the chime once through, as a press would, without showing the camera: for somebody
// choosing a volume, or checking what a press will sound like.
func (f *Feature) Chime() { safe.Go("doorbell chime", f.ring) }

// AtDoor is whether somebody pressed the doorbell during a call in the last atDoorFor, for the call's
// page to say so.
func (f *Feature) AtDoor(now time.Time) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.atDoor.IsZero() && now.Sub(f.atDoor) < atDoorFor
}

// Status is how the doorbell is doing now.
func (f *Feature) Status() Status {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status
}

// Poke has the doorbell look at its settings now: the setup page changed them, or the recorder's.
func (f *Feature) Poke() {
	select {
	case f.poke <- struct{}{}:
	default:
	}
}

// target is what to listen to, as set: the recorder's ONVIF address and login, the doorbell's
// channel on it, and whether the recorder has other cameras (whose presses name their channel).
type target struct {
	camera     string
	base       string
	user, pass string
	channel    int
	several    bool
}

// current is the target as set. ok is false when there is no doorbell; problem says why a doorbell
// that is set cannot be listened to.
func current() (t target, ok bool, problem string) {
	c := config.Get()
	t.camera = c.Doorbell.Camera
	if t.camera == "" {
		return t, false, ""
	}
	r := c.Home.Reolink
	if r.Base == "" {
		return t, false, "no Reolink recorder is set up"
	}
	listed := false
	for _, cam := range r.Cameras {
		listed = listed || cam.Entity == t.camera
	}
	ch, err := strconv.Atoi(strings.TrimPrefix(t.camera, "reolink:"))
	if !listed || err != nil || ch < 0 || !strings.HasPrefix(t.camera, "reolink:") {
		return t, false, "the doorbell's camera is no longer on the recorder"
	}
	u, err := url.Parse(r.Base)
	if err != nil || u.Hostname() == "" {
		return t, false, "the recorder's address is not usable"
	}
	t.base = "http://" + net.JoinHostPort(u.Hostname(), strconv.Itoa(c.Doorbell.ONVIFPort()))
	t.user, t.pass, t.channel, t.several = r.User, r.Pass, ch, len(r.Cameras) > 1
	return t, true, ""
}

// Run listens for presses for as long as the device is up, starting again whenever the settings
// change or the recorder goes away.
func (f *Feature) Run(ctx context.Context) error {
	if f.callsEnd != nil {
		f.hooked.Do(func() { f.callsEnd(f.callOver) })
	}
	wait := retryMin
	for {
		t, ok, problem := f.settings()
		if !ok {
			f.setStatus(t.camera, false, problem)
			if !f.sleep(ctx, lookEvery, t, ok) {
				return nil
			}
			continue
		}
		if f.Status().Camera != t.camera {
			f.setStatus(t.camera, false, "") // a new doorbell: no old problem to show for it
		}
		subscribed, err := f.listen(ctx, t)
		if ctx.Err() != nil {
			return nil
		}
		if subscribed {
			wait = retryMin // it was heard until now: a fresh failure starts over at the shortest wait
		}
		if err == nil { // the settings changed: look again at once
			continue
		}
		f.setStatus(t.camera, false, explain(err, t))
		if !f.sleep(ctx, wait, t, ok) {
			return nil
		}
		wait = min(wait*2, retryMax)
	}
}

// sleep waits d, or until the settings no longer say t and ok: at once if they changed before it
// began, as a poke can be taken by a listen on its way out. A poke that leaves them as they were does
// not end it, so the wait between tries of a recorder that refuses the login is kept whatever else
// goes on; a burst of tries could have the recorder lock the account. False when the device is
// stopping.
func (f *Feature) sleep(ctx context.Context, d time.Duration, t target, ok bool) bool {
	if f.waited != nil {
		f.waited(d)
	}
	tm := time.NewTimer(d)
	defer tm.Stop()
	for {
		if now, nowOK, _ := f.settings(); nowOK != ok || now != t {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-f.poke:
		case <-tm.C:
			return true
		}
	}
}

// listen holds one subscription open and rings for what comes through it. It returns a nil error
// when the settings no longer match t, and the error that ended it otherwise; subscribed is whether
// the recorder was heard, a pull answered on a subscription, and not only subscribed to.
func (f *Feature) listen(ctx context.Context, t target) (subscribed bool, err error) {
	// A poke that changes what to listen to ends whatever is in progress, a subscribing or a
	// renewal as much as a pull; one that changes nothing ends nothing.
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var moved atomic.Bool
	done := make(chan struct{})
	defer close(done)
	safe.Go("doorbell settings", func() {
		for {
			select {
			case <-f.poke:
				select {
				case <-done: // listen is over: the poke is Run's, not this one's
					f.Poke()
					return
				default:
				}
				if now, ok, _ := f.settings(); !ok || now != t {
					moved.Store(true)
					cancel()
					return
				}
			case <-done:
				return
			}
		}
	})
	ended := func(err error) (bool, error) {
		if moved.Load() {
			return subscribed, nil
		}
		return subscribed, err
	}

	c := &onvifevent.Client{Base: t.base, User: t.user, Pass: t.pass,
		HTTP: &http.Client{Timeout: pullWait + 20*time.Second}}
	sub, err := c.Subscribe(sctx, life)
	if err != nil {
		return ended(err)
	}
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer ccancel()
		sub.Close(cctx)
	}()
	f.setStatus(t.camera, true, "")
	slog.Info("doorbell: listening", "camera", t.camera, "channel", t.channel)
	renewed := f.now()
	for {
		if f.now().Sub(renewed) >= renewEvery {
			if err := sub.Renew(sctx, life); err != nil {
				return ended(fmt.Errorf("renewing: %w", err))
			}
			renewed = f.now()
		}
		asked := time.Now()
		evs, err := sub.Pull(sctx, pullWait)
		if err == nil {
			// Heard, not just subscribed: a recorder that takes the subscription and refuses every
			// pull is still a failure, and keeps the growing wait between tries, or a refused login
			// every few seconds could lock the account.
			subscribed = true
		}
		for _, ev := range evs {
			if pressed(ev, t.channel, t.several) {
				f.press(t.camera)
			}
		}
		if err != nil {
			return ended(err)
		}
		if quick := pullFloor - time.Since(asked); quick > 0 {
			select {
			case <-sctx.Done():
				return ended(sctx.Err())
			case <-time.After(quick):
			}
		}
	}
}

// pressed is whether ev is the doorbell on channel being pressed: Visitor going true there. Those
// that only report the state as it stands (Initialized, as a subscription opens) are not a press. A
// press that names no channel is taken only from a recorder with the one camera, where it can be
// no other.
func pressed(ev onvifevent.Event, channel int, several bool) bool {
	if !strings.HasSuffix(ev.Topic, "/Visitor") || ev.Op != "Changed" || ev.Items["State"] != "true" {
		return false
	}
	src, ok := ev.Items["Source"]
	if !ok {
		return !several
	}
	n, err := strconv.Atoi(strings.TrimSpace(src))
	return err == nil && n == channel
}

// press rings, shows the door and tells Home Assistant, unless it rang a moment ago or has rung
// too often this minute. During a call it only says so on the call's page, and tells Home Assistant.
func (f *Feature) press(camera string) {
	calling := f.calling() // asked before taking mu: the phone has a lock of its own
	f.mu.Lock()
	now := f.now()
	for len(f.rang) > 0 && now.Sub(f.rang[0]) >= time.Minute {
		f.rang = f.rang[1:]
	}
	if n := len(f.rang); (n > 0 && now.Sub(f.rang[n-1]) < again) || n >= mostRings {
		f.mu.Unlock()
		return
	}
	f.rang = append(f.rang, now)
	f.status.LastPress = now
	if calling {
		f.atDoor = now
	}
	f.mu.Unlock()
	slog.Info("doorbell: pressed", "camera", camera, "during_call", calling)
	if calling {
		f.Changed.Emit(struct{}{})
		if !f.calling() {
			// The call ended between the look and the note: its end has been and gone, so the
			// note is forgotten here rather than kept for the next call.
			f.callOver()
		}
	} else {
		safe.Go("doorbell ring", f.ring)
		safe.Go("doorbell camera", func() { f.show(camera) })
	}
	f.tell(camera)
}

// setStatus records how the doorbell is doing; the log hears of a problem only when it changes, so
// a recorder that stays away is one line, not one a minute.
func (f *Feature) setStatus(camera string, listening bool, problem string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.status
	if s.Camera == camera && s.Listening == listening && s.Problem == problem {
		return
	}
	if problem != "" && problem != s.Problem {
		slog.Warn("doorbell: not listening", "camera", camera, "why", problem)
	}
	f.status = Status{Camera: camera, Listening: listening, Since: f.now(), Problem: problem, LastPress: s.LastPress}
}

// explain is an error as somebody at the setup page can act on it. What the recorder itself said is
// kept short: it is whatever answered at the address.
func explain(err error, t target) string {
	host := strings.TrimPrefix(t.base, "http://")
	var ne net.Error
	switch {
	case errors.Is(err, onvifevent.ErrAuth):
		return "the recorder refused the login"
	case errors.As(err, &ne) && ne.Timeout():
		return "the recorder at " + host + " did not answer"
	case strings.Contains(err.Error(), "connection refused"):
		return "nothing answers ONVIF at " + host + ": turn ONVIF on for the recorder in the Reolink app, and check the port"
	}
	s := err.Error()
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
