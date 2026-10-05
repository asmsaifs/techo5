// Package notification puts a phone's notifications on the Show's screen, as Home Assistant's
// companion app reports them (docs/phone-notifications-plan.md).
//
// The companion app's Last notification sensor takes each notification an allowed app posts: the text
// as its state, the title, the package and the post time as attributes. The device follows the sensors
// it was given (hastate), and a new notification on one is a card on the screen until it is tapped,
// times out, or is dismissed on the phone, which the Last removed notification sensor beside it says.
//
// Nothing runs on the phone but the companion app, and no port is opened here: the notifications come
// over the connection Home Assistant already has.
package notification

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
)

func init() {
	component.Register(component.Device, Get(), component.Order(41))
}

const (
	// showFor is how long a card stays up untouched.
	showFor = 2 * time.Minute

	// settle is how long after a sensor's first value arrives the rest are waited for: Home Assistant
	// sends the state and each attribute as values of their own, so a new notification is a handful
	// of them in a row, and reading the first alone would pair a new title with the last text.
	settle = 400 * time.Millisecond

	// linesWith is how close to a look a summary's lines must have arrived to belong to it.
	linesWith = 3 * settle

	// most is how many phones it follows.
	most = 4
)

// attributes are what is followed of each Last notification sensor, besides its state.
var attributes = []string{"friendly_name", "package", "post_time", "is_ongoing", "android.title", "android.text",
	"android.textLines"}

type Feature struct {
	// Changed fires when a card comes up or goes away.
	Changed hook.Hook[struct{}]

	showText *esphome.Switch

	mu      sync.Mutex
	showing *Note
	expire  *time.Timer
	waiting *time.Timer
	// seen is the notification each sensor held when last looked at, so it comes up once.
	seen map[string]string
	// linesAt is when each sensor's summary lines last arrived. Home Assistant does not send an
	// attribute a notification lacks, so lines that did not come with this one are the last summary's.
	linesAt map[string]time.Time
}

var (
	once   sync.Once
	shared *Feature
)

func Get() *Feature {
	once.Do(func() {
		shared = &Feature{seen: map[string]string{}, linesAt: map[string]time.Time{}}
		shared.showText = &esphome.Switch{
			Base: esphome.Base{
				ObjectID: "phone_notifications_text", Name: "Phone notifications: show text",
				Icon: "mdi:message-text-lock", Category: esphome.CategoryConfig,
			},
			OnCommand: shared.SetShowText,
		}
		hastate.Get().Changed.Listen(shared.stateChanged)
	})
	return shared
}

func (f *Feature) Name() string { return "phone notifications" }

func (f *Feature) Entities() []esphome.Entity { return []esphome.Entity{f.showText} }

func (f *Feature) Restore(c config.Config) {
	f.showText.Set(c.Notifications.ShowText)
	follow(c.Notifications.Entities)
}

// SetShowText chooses whether a card says what the notification says, or only who it is from.
func (f *Feature) SetShowText(on bool) {
	f.showText.Set(on)
	if err := config.Set().Notifications().ShowText(on); err != nil {
		slog.Error("saving a setting failed", "setting", f.showText.ObjectID, "err", err)
	}
	f.Changed.Emit(struct{}{})
}

// follow asks Home Assistant for the sensors: each Last notification sensor's state and attributes,
// and the post time and package of the Last removed notification sensor beside it.
func follow(entities []string) {
	var keys []hastate.Key
	for _, e := range entities {
		keys = append(keys, hastate.Key{Entity: e})
		for _, a := range attributes {
			keys = append(keys, hastate.Key{Entity: e, Attribute: a})
		}
		if r := removedEntity(e); r != "" {
			keys = append(keys, hastate.Key{Entity: r, Attribute: "package"}, hastate.Key{Entity: r, Attribute: "post_time"})
		}
	}
	hastate.Get().Follow("notification", keys...)
}

// stateChanged is a value arriving. One of ours has the sensors looked at once the rest have come.
func (f *Feature) stateChanged(u hastate.Update) {
	entities := config.Get().Notifications.Entities
	if !slices.Contains(entities, u.Entity) && !slices.ContainsFunc(entities, func(e string) bool {
		return removedEntity(e) == u.Entity
	}) {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if u.Attribute == "android.textLines" {
		f.linesAt[u.Entity] = time.Now()
	}
	if f.waiting != nil {
		f.waiting.Stop()
	}
	f.waiting = time.AfterFunc(settle, func() { f.look(time.Now()) })
}

// read is a sensor's values as last sent.
func read(entity string, names ...string) values {
	t := hastate.Get()
	v := values{"": t.State(entity)}
	for _, a := range names {
		v[a], _ = t.Value(entity, a)
	}
	return v
}

// look reads the sensors: a notification removed on the phone takes its card down, and a new one
// puts its card up, in place of whatever was.
func (f *Feature) look(now time.Time) {
	var next *Note
	gone := false
	for _, e := range config.Get().Notifications.Entities {
		if r := removedEntity(e); r != "" {
			if id := removedID(read(r, "package", "post_time")); id != "" {
				f.mu.Lock()
				if f.showing != nil && f.showing.ID == id {
					f.showing, gone = nil, true
				}
				f.mu.Unlock()
			}
		}
		v := read(e, attributes...)
		f.mu.Lock()
		if now.Sub(f.linesAt[e]) > linesWith {
			delete(v, "android.textLines")
		}
		f.mu.Unlock()
		n, ok := noteFrom(e, v, now)
		if !ok {
			f.skipped(e, v, now)
			continue
		}
		f.mu.Lock()
		if f.seen[e] == n.ID {
			f.mu.Unlock()
			continue
		}
		f.seen[e] = n.ID
		f.mu.Unlock()
		if next == nil || n.Posted.After(next.Posted) {
			next = &n
		}
	}
	if next != nil {
		f.show(*next)
		return
	}
	if gone {
		slog.Info("notification: dismissed on the phone")
		f.Changed.Emit(struct{}{})
	}
}

// skipped says in the log, once for each, why a value on a sensor was not shown: it is how to tell a
// notification left out on purpose (old, ongoing) from one that arrived without what it needs. Only
// the facts that decide it go in the log, never the words.
func (f *Feature) skipped(e string, v values, now time.Time) {
	key := v["package"] + "|" + v["post_time"]
	f.mu.Lock()
	if f.seen[e] == key {
		f.mu.Unlock()
		return
	}
	f.seen[e] = key
	f.mu.Unlock()
	if isTrue(v["is_ongoing"]) {
		return // expected, and some apps post one every few seconds
	}
	age := "none"
	if at, ok := postTime(v["post_time"]); ok {
		age = now.Sub(at).Round(time.Second).String()
	}
	slog.Info("notification: not shown", "sensor", e, "package", v["package"], "age", age,
		"ongoing", v["is_ongoing"], "title", v["android.title"] != "", "state", v[""] != "")
}

func (f *Feature) show(n Note) {
	f.mu.Lock()
	f.showing = &n
	if f.expire != nil {
		f.expire.Stop()
	}
	f.expire = time.AfterFunc(showFor, func() { f.dismiss(n.ID) })
	f.mu.Unlock()
	slog.Info("notification: shown", "app", n.App, "phone", n.Phone)
	f.Changed.Emit(struct{}{})
}

// dismiss takes the card down if it is still the one named.
func (f *Feature) dismiss(id string) {
	f.mu.Lock()
	up := f.showing != nil && f.showing.ID == id
	if up {
		f.showing = nil
	}
	f.mu.Unlock()
	if up {
		f.Changed.Emit(struct{}{})
	}
}

// Dismiss takes the card down: a tap on it.
func (f *Feature) Dismiss() {
	f.mu.Lock()
	up := f.showing != nil
	f.showing = nil
	f.mu.Unlock()
	if up {
		f.Changed.Emit(struct{}{})
	}
}

// Showing is the card up now, if one is, with its text left out unless the text is to be shown.
func (f *Feature) Showing() (Note, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.showing == nil {
		return Note{}, false
	}
	n := *f.showing
	if !config.Get().Notifications.ShowText {
		n.Text = ""
		if n.Title == "" {
			n.Title = "New notification" // its words were all it had
		}
	}
	return n, true
}

// list is the sensors in a comma-separated list: trimmed, each once, in order.
func list(s string) ([]string, error) {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e == "" || slices.Contains(out, e) {
			continue
		}
		if !strings.HasPrefix(e, "sensor.") || len(e) == len("sensor.") {
			return nil, fmt.Errorf("phone_notifications: %q is not a sensor, like sensor.pixel_8_last_notification", e)
		}
		out = append(out, e)
	}
	if len(out) > most {
		return nil, fmt.Errorf("phone_notifications: %d sensors, and %d is the most", len(out), most)
	}
	return out, nil
}

func (f *Feature) Actions() []*esphome.Action {
	return []*esphome.Action{{
		Name: "phone_notifications",
		Args: []esphome.Arg{{Name: "entities", Type: esphome.ArgString}},
		Run: func(c esphome.Call) (any, error) {
			entities, err := list(c.String("entities"))
			if err != nil {
				return nil, err
			}
			if err := config.Set().Notifications().Entities(entities); err != nil {
				return nil, err
			}
			slog.Info("notification: following", "sensors", entities)
			follow(entities)
			f.mu.Lock()
			f.seen = map[string]string{}
			f.mu.Unlock()
			if len(entities) == 0 {
				f.Dismiss()
			}
			// Home Assistant asks what to follow once per connection.
			component.Reconnect.Emit(struct{}{})
			return nil, nil
		},
	}}
}
