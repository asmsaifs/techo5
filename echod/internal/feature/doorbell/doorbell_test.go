package doorbell

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/lib/onvifevent"
)

func visitor(source, state, op string) onvifevent.Event {
	return onvifevent.Event{Topic: "tns1:RuleEngine/MyRuleDetector/Visitor", Op: op,
		Items: map[string]string{"Source": source, "State": state}}
}

func TestPressed(t *testing.T) {
	noSource := onvifevent.Event{Topic: "tns1:RuleEngine/MyRuleDetector/Visitor", Op: "Changed",
		Items: map[string]string{"State": "true"}}
	for _, c := range []struct {
		name    string
		ev      onvifevent.Event
		ch      int
		several bool
		want    bool
	}{
		{"press on its channel", visitor("000", "true", "Changed"), 0, true, true},
		{"press on the second channel", visitor("001", "true", "Changed"), 1, true, true},
		{"another camera's press", visitor("001", "true", "Changed"), 0, true, false},
		{"the press ending", visitor("000", "false", "Changed"), 0, true, false},
		{"the state as a subscription opens", visitor("000", "true", "Initialized"), 0, true, false},
		{"a person, not a press", onvifevent.Event{Topic: "tns1:RuleEngine/MyRuleDetector/PeopleDetect", Op: "Changed",
			Items: map[string]string{"Source": "000", "State": "true"}}, 0, true, false},
		{"no source, the recorder's one camera", noSource, 0, false, true},
		{"no source, a recorder with several", noSource, 0, true, false},
		{"a source that is not a number", visitor("door", "true", "Changed"), 0, true, false},
	} {
		if got := pressed(c.ev, c.ch, c.several); got != c.want {
			t.Errorf("%s: pressed = %v, want %v", c.name, got, c.want)
		}
	}
}

// world is a doorbell whose ring, camera and Home Assistant are counters, on a clock the test moves.
type world struct {
	f                   *Feature
	rings, shows, tells atomic.Int32
	mu                  sync.Mutex
	clock               time.Time
	tg                  target
	ok                  bool
	inCall              bool
	waits               chan time.Duration
}

func newWorld() *world {
	w := &world{clock: time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC), ok: true, waits: make(chan time.Duration, 64)}
	w.f = &Feature{
		poke: make(chan struct{}, 1),
		ring: func() { w.rings.Add(1) },
		show: func(string) { w.shows.Add(1) },
		tell: func(string) { w.tells.Add(1) },
		calling: func() bool {
			w.mu.Lock()
			defer w.mu.Unlock()
			return w.inCall
		},
		settings: func() (target, bool, string) {
			w.mu.Lock()
			defer w.mu.Unlock()
			return w.tg, w.ok, ""
		},
		now: func() time.Time {
			w.mu.Lock()
			defer w.mu.Unlock()
			return w.clock
		},
		waited: func(d time.Duration) {
			select {
			case w.waits <- d:
			default:
			}
		},
	}
	return w
}

func (w *world) advance(d time.Duration) {
	w.mu.Lock()
	w.clock = w.clock.Add(d)
	w.mu.Unlock()
}

func (w *world) set(tg target, ok bool) {
	w.mu.Lock()
	w.tg, w.ok = tg, ok
	w.mu.Unlock()
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for range 300 {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

func TestPressRingsOnceForAQuickPair(t *testing.T) {
	w := newWorld()
	w.f.press("reolink:0")
	w.f.press("reolink:0") // the same press, reported twice
	eventually(t, "one ring, one camera, one event", func() bool {
		return w.rings.Load() == 1 && w.shows.Load() == 1 && w.tells.Load() == 1
	})
	w.advance(again)
	w.f.press("reolink:0")
	eventually(t, "a second press rings again", func() bool { return w.rings.Load() == 2 && w.shows.Load() == 2 })
	if w.f.Status().LastPress != w.f.now() {
		t.Errorf("last press %v, want %v", w.f.Status().LastPress, w.f.now())
	}
}

// During a call a press neither chimes nor puts the camera over the call: the call's page says
// somebody is at the door for a minute, and Home Assistant still hears of it.
func TestPressDuringACall(t *testing.T) {
	w := newWorld()
	w.mu.Lock()
	w.inCall = true
	w.mu.Unlock()
	var changed atomic.Int32
	w.f.Changed.Listen(func(struct{}) { changed.Add(1) })
	if w.f.AtDoor(w.f.now()) {
		t.Fatal("at the door before any press")
	}
	w.f.press("reolink:0")
	if !w.f.AtDoor(w.f.now()) || changed.Load() != 1 {
		t.Fatalf("at door %v, changed %d", w.f.AtDoor(w.f.now()), changed.Load())
	}
	eventually(t, "Home Assistant told", func() bool { return w.tells.Load() == 1 })
	time.Sleep(50 * time.Millisecond)
	if w.rings.Load() != 0 || w.shows.Load() != 0 {
		t.Errorf("%d rings and %d cameras during a call", w.rings.Load(), w.shows.Load())
	}
	if w.f.AtDoor(w.f.now().Add(atDoorFor)) {
		t.Error("still at the door a minute on")
	}
	// The call over, the next press rings as usual and leaves the call's page alone.
	w.mu.Lock()
	w.inCall = false
	w.mu.Unlock()
	w.advance(atDoorFor)
	w.f.press("reolink:0")
	eventually(t, "a ring after the call", func() bool { return w.rings.Load() == 1 && w.shows.Load() == 1 })
	if w.f.AtDoor(w.f.now()) {
		t.Error("a press outside a call shown as one during it")
	}
}

func TestRingsCappedPerMinute(t *testing.T) {
	w := newWorld()
	for range 12 {
		w.f.press("reolink:0")
		w.advance(again)
	}
	eventually(t, "the cap's worth of rings", func() bool { return w.rings.Load() == mostRings })
	time.Sleep(50 * time.Millisecond)
	if n := w.rings.Load(); n != mostRings {
		t.Fatalf("%d rings in a minute, want %d", n, mostRings)
	}
	w.advance(time.Minute)
	w.f.press("reolink:0")
	eventually(t, "a ring once the minute is up", func() bool { return w.rings.Load() == mostRings+1 })
}

// hub is a recorder: it answers each pull with the next notifications queued, or waits for some.
// fail makes every request fail, and failPull every pull; renews counts renewals and pulls the pulls.
type hub struct {
	mu       sync.Mutex
	queue    []string
	more     chan struct{}
	fail     atomic.Bool
	failPull atomic.Bool
	renews   atomic.Int32
	pulls    atomic.Int32
	reqs     atomic.Int32
	closed   atomic.Bool
}

func newHub() *hub { return &hub{more: make(chan struct{}, 16)} }

func (h *hub) push(notes string) {
	h.mu.Lock()
	h.queue = append(h.queue, notes)
	h.mu.Unlock()
	h.more <- struct{}{}
}

const env = `<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" xmlns:tt="http://www.onvif.org/ver10/schema" xmlns:wsa5="http://www.w3.org/2005/08/addressing"><s:Body>BODY</s:Body></s:Envelope>`

func note(source, state, op string) string {
	return `<wsnt:NotificationMessage><wsnt:Topic>tns1:RuleEngine/MyRuleDetector/Visitor</wsnt:Topic><wsnt:Message><tt:Message PropertyOperation="` + op +
		`"><tt:Source><tt:SimpleItem Name="Source" Value="` + source + `"/></tt:Source><tt:Data><tt:SimpleItem Name="State" Value="` + state +
		`"/></tt:Data></tt:Message></wsnt:Message></wsnt:NotificationMessage>`
}

func (h *hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	io.ReadAll(r.Body)
	h.reqs.Add(1)
	if h.fail.Load() {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	ct := r.Header.Get("Content-Type")
	reply := func(body string) { io.WriteString(w, strings.Replace(env, "BODY", body, 1)) }
	switch {
	case strings.Contains(ct, "CreatePullPoint"):
		reply(`<CreatePullPointSubscriptionResponse><SubscriptionReference><wsa5:Address>http://198.51.100.9/onvif/PullSubManager?Idx=1</wsa5:Address></SubscriptionReference></CreatePullPointSubscriptionResponse>`)
	case strings.Contains(ct, "PullMessages"):
		h.pulls.Add(1)
		if h.failPull.Load() {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		h.mu.Lock()
		var next string
		if len(h.queue) > 0 {
			next, h.queue = h.queue[0], h.queue[1:]
		}
		h.mu.Unlock()
		if next == "" {
			select {
			case <-r.Context().Done():
				return
			case <-h.more:
			case <-time.After(300 * time.Millisecond):
			}
		}
		reply(`<PullMessagesResponse>` + next + `</PullMessagesResponse>`)
	case strings.Contains(ct, "Renew"):
		h.renews.Add(1)
		reply("")
	case strings.Contains(ct, "Unsubscribe"):
		h.closed.Store(true)
		reply("")
	default: // the clock and the capabilities
		reply("")
	}
}

func TestListenRingsForItsDoorbellOnly(t *testing.T) {
	h := newHub()
	h.queue = []string{
		note("000", "true", "Initialized"), // pressed as the subscription opened: not a new press
		note("001", "true", "Changed"),     // the other camera's button
		note("000", "true", "Changed") + note("000", "false", "Changed"),
	}
	srv := httptest.NewServer(h)
	defer srv.Close()

	w := newWorld()
	tg := target{camera: "reolink:0", base: srv.URL, user: "admin", pass: "x", channel: 0, several: true}
	w.set(tg, true)
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		subscribed bool
		err        error
	}
	done := make(chan result, 1)
	go func() {
		s, err := w.f.listen(ctx, tg)
		done <- result{s, err}
	}()

	eventually(t, "the press rings", func() bool { return w.rings.Load() == 1 && w.shows.Load() == 1 && w.tells.Load() == 1 })
	if !w.f.Status().Listening {
		t.Error("status does not say listening")
	}
	// Past renewEvery the subscription is renewed before the next pull.
	w.advance(renewEvery)
	h.push("")
	eventually(t, "a renewal", func() bool { return h.renews.Load() >= 1 })
	if w.rings.Load() != 1 {
		t.Errorf("%d rings, want 1", w.rings.Load())
	}
	cancel()
	select {
	case r := <-done:
		if !r.subscribed || !errors.Is(r.err, context.Canceled) {
			t.Errorf("listen ended with %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("listen did not stop")
	}
	eventually(t, "the subscription is closed", h.closed.Load)
}

// A changed setting ends a pull in the middle of its wait; a poke that changes nothing does not.
func TestPokeEndsAPullInProgress(t *testing.T) {
	h := newHub()
	srv := httptest.NewServer(h)
	defer srv.Close()
	w := newWorld()
	tg := target{camera: "reolink:0", base: srv.URL, channel: 0}
	w.set(tg, true)
	done := make(chan error, 1)
	go func() {
		_, err := w.f.listen(context.Background(), tg)
		done <- err
	}()
	eventually(t, "a pull waiting", func() bool { return h.pulls.Load() >= 1 })

	w.f.Poke() // nothing changed
	select {
	case err := <-done:
		t.Fatalf("an empty poke ended the listen: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	moved := tg
	moved.channel = 1
	w.set(moved, true)
	w.f.Poke()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("a changed setting ended it with %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a changed setting did not end the pull")
	}
}

// A recorder that keeps failing is tried again later and later; once it has worked, a failure starts
// over at the shortest wait.
func TestRunBacksOffAndResets(t *testing.T) {
	retryMin, retryMax, lookEvery = 10*time.Millisecond, 40*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { retryMin, retryMax, lookEvery = 5*time.Second, time.Minute, time.Minute })

	h := newHub()
	h.fail.Store(true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	w := newWorld()
	w.set(target{camera: "reolink:0", base: srv.URL}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.f.Run(ctx)

	var got []time.Duration
	for range 4 {
		got = append(got, <-w.waits)
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("waits %v, want %v", got, want)
		}
	}
	if p := w.f.Status().Problem; !strings.Contains(p, "500") {
		t.Errorf("problem %q", p)
	}

	h.fail.Store(false)
	eventually(t, "listening once the recorder is back", func() bool { return w.f.Status().Listening })
	for len(w.waits) > 0 {
		<-w.waits
	}
	// Heard, not only subscribed: a pull answered before the recorder goes away again.
	start := h.pulls.Load()
	eventually(t, "a pull answered", func() bool { return h.pulls.Load() >= start+1 })
	h.fail.Store(true) // and away again, after it worked
	if d := <-w.waits; d != retryMin {
		t.Errorf("first wait after a working session %v, want %v", d, retryMin)
	}

	w.set(target{}, false) // the doorbell taken away: Run looks again every lookEvery
	w.f.Poke()
	for {
		if d := <-w.waits; d == lookEvery {
			break
		}
	}
	if w.f.Status().Listening {
		t.Error("still listening with no doorbell")
	}
}

// A recorder that takes the subscription and refuses every pull is a failure like any other: the wait
// between tries keeps growing, rather than starting over at each subscription, which would be a
// refused login every few seconds.
func TestARecorderThatRefusesPullsBacksOff(t *testing.T) {
	retryMin, retryMax = 10*time.Millisecond, 40*time.Millisecond
	t.Cleanup(func() { retryMin, retryMax = 5*time.Second, time.Minute })

	h := newHub()
	h.failPull.Store(true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	w := newWorld()
	w.set(target{camera: "reolink:0", base: srv.URL}, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.f.Run(ctx)

	var got []time.Duration
	for range 4 {
		got = append(got, <-w.waits)
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 40 * time.Millisecond}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("waits %v, want %v", got, want)
		}
	}
}

// "Someone's at the door" is the news of the call it came during: once that call is over, the next
// call's page does not say it.
func TestTheDoorIsForgottenWhenTheCallEnds(t *testing.T) {
	w := newWorld()
	w.inCall = true
	w.f.press("reolink:0")
	if !w.f.AtDoor(w.clock) {
		t.Fatal("a press during a call is not on the call's page")
	}
	w.f.callOver()
	if w.f.AtDoor(w.clock) {
		t.Error("the press is still news after the call ended")
	}
}

// Pokes that change nothing, as a busy device sends, do not cut a wait short: a recorder refusing the
// login is not tried again any sooner, which could have it lock the account. One that changes the
// settings still does.
func TestPokesKeepTheBackoff(t *testing.T) {
	retryMin, retryMax = 400*time.Millisecond, 400*time.Millisecond
	t.Cleanup(func() { retryMin, retryMax = 5*time.Second, time.Minute })

	h := newHub()
	h.fail.Store(true)
	srv := httptest.NewServer(h)
	defer srv.Close()
	w := newWorld()
	tg := target{camera: "reolink:0", base: srv.URL}
	w.set(tg, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.f.Run(ctx)

	<-w.waits // the first try failed and Run is waiting
	before := h.reqs.Load()
	for range 50 {
		w.f.Poke()
		time.Sleep(4 * time.Millisecond)
	}
	if n := h.reqs.Load() - before; n != 0 {
		t.Fatalf("%d requests during the wait, from pokes that changed nothing", n)
	}

	moved := tg
	moved.channel = 1
	w.set(moved, true)
	w.f.Poke()
	start := time.Now()
	eventually(t, "a try with the new settings", func() bool { return h.reqs.Load() > before })
	if d := time.Since(start); d > 300*time.Millisecond {
		t.Errorf("a changed setting waited %v for the backoff", d)
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestExplain(t *testing.T) {
	tg := target{base: "http://192.0.2.5:8000"}
	if got := explain(onvifevent.ErrAuth, tg); got != "the recorder refused the login" {
		t.Errorf("auth: %q", got)
	}
	if got := explain(errors.New("dial tcp 192.0.2.5:8000: connect: connection refused"), tg); !strings.Contains(got, "turn ONVIF on") {
		t.Errorf("refused: %q", got)
	}
	if got := explain(timeoutErr{}, tg); got != "the recorder at 192.0.2.5:8000 did not answer" {
		t.Errorf("timeout: %q", got)
	}
	if got := explain(errors.New(strings.Repeat("x", 500)), tg); len(got) > 120 {
		t.Errorf("a long error kept %d characters", len(got))
	}
}

func TestChimeRenders(t *testing.T) {
	pcm := render(48000)
	secs := float64(len(pcm)) / 48000
	if secs < 3 || secs > 6 {
		t.Errorf("chime lasts %.1f s", secs)
	}
	most := 0
	for _, s := range pcm {
		most = max(most, abs(int(s)))
	}
	if most < 30000 || most > 32767 {
		t.Errorf("peak %d, want near full scale and unclipped", most)
	}
	// It dies away: the last tenth is far quieter than the first strike.
	tail := 0
	for _, s := range pcm[len(pcm)*9/10:] {
		tail = max(tail, abs(int(s)))
	}
	if tail > most/4 {
		t.Errorf("tail peak %d of %d: the bars do not ring out", tail, most)
	}
	n := dingDong()
	if len(n) != 1 || n[0].Clip == nil || n[0].Ms < 3000 || n[0].Clip.Ms() != n[0].Ms {
		t.Errorf("dingDong = %+v", n)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
