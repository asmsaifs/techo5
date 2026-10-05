package notification

import (
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ygelfand/go-esphome-device/api"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
)

func ms(t time.Time) string { return strconv.FormatInt(t.UnixMilli(), 10) }

func TestNoteFrom(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	base := values{
		"":              "On my way, ten minutes",
		"friendly_name": "Pixel 8 Last notification",
		"package":       "com.whatsapp",
		"post_time":     ms(now.Add(-5 * time.Second)),
		"is_ongoing":    "False",
		"android.title": "Rahim",
		"android.text":  "On my way, ten minutes",
	}
	with := func(k, v string) values {
		out := values{}
		for a, b := range base {
			out[a] = b
		}
		out[k] = v
		return out
	}
	const e = "sensor.pixel_8_last_notification"

	n, ok := noteFrom(e, base, now)
	if !ok || n.App != "WhatsApp" || n.Phone != "Pixel 8" || n.Title != "Rahim" || n.Text != "On my way, ten minutes" {
		t.Fatalf("got %+v %v", n, ok)
	}
	if n.ID != "com.whatsapp|"+base["post_time"] {
		t.Errorf("id %q", n.ID)
	}

	for name, v := range map[string]values{
		"ongoing":    with("is_ongoing", "True"),
		"old":        with("post_time", ms(now.Add(-3*time.Minute))),
		"far ahead":  with("post_time", ms(now.Add(2*time.Minute))),
		"no package": with("package", ""),
		"no time":    with("post_time", "None"),
	} {
		if _, ok := noteFrom(e, v, now); ok {
			t.Errorf("%s: shown", name)
		}
	}

	// No text: the state is the package, and the attribute may still hold the one before's.
	v := with("", "com.whatsapp")
	v["android.title"] = "2 new messages"
	if n, ok := noteFrom(e, v, now); !ok || n.Text != "" || n.Title != "2 new messages" {
		t.Errorf("no text: %+v %v", n, ok)
	}

	// The state is cut at 255 characters; the attribute has the rest.
	long := "a message that goes on "
	for len(long) < 300 {
		long += "and on "
	}
	v = with("", long[:255])
	v["android.text"] = long
	if n, _ := noteFrom(e, v, now); n.Text != clean(long) {
		t.Errorf("long text not taken from the attribute: %d", len(n.Text))
	}

	// Line breaks and invisible marks go; Bangla's joiners stay.
	if got := clean("আমি‍\nআসছি⁦ "); got != "আমি‍ আসছি" {
		t.Errorf("clean: %q", got)
	}
}

func TestNames(t *testing.T) {
	for pkg, want := range map[string]string{
		"com.whatsapp":        "WhatsApp",
		"com.acme.doorbell":   "Doorbell",
		"com.acme.camera.app": "Camera",
		"net.example.android": "Example",
		"single":              "Single",
	} {
		if got := appName(pkg); got != want {
			t.Errorf("appName(%q) = %q, want %q", pkg, got, want)
		}
	}
	if got := phoneName("", "sensor.pixel_8_last_notification"); got != "Pixel 8" {
		t.Errorf("phone from entity: %q", got)
	}
	if got := removedEntity("sensor.pixel_8_last_notification"); got != "sensor.pixel_8_last_removed_notification" {
		t.Errorf("removed: %q", got)
	}
	if got := removedEntity("sensor.my_template"); got != "" {
		t.Errorf("removed of another sensor: %q", got)
	}
}

func TestList(t *testing.T) {
	got, err := list(" sensor.a_last_notification, Sensor.A_last_notification ,sensor.b,, ")
	if err != nil || len(got) != 2 || got[0] != "sensor.a_last_notification" || got[1] != "sensor.b" {
		t.Errorf("list: %v %v", got, err)
	}
	if _, err := list("binary_sensor.door"); err == nil {
		t.Error("a binary sensor was taken")
	}
	if _, err := list("sensor.a,sensor.b,sensor.c,sensor.d,sensor.e"); err == nil {
		t.Error("five were taken")
	}
	if got, err := list(""); err != nil || len(got) != 0 {
		t.Errorf("empty: %v %v", got, err)
	}
}

// send is Home Assistant sending one value.
func send(entity, attribute, value string) {
	hastate.Get().Handle(nil, nil, &api.HomeAssistantStateResponse{EntityId: entity, Attribute: attribute, State: value})
}

func TestShowAndRemove(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	const e, r = "sensor.pixel_8_last_notification", "sensor.pixel_8_last_removed_notification"
	if err := config.Set().Notifications().Entities([]string{e}); err != nil {
		t.Fatal(err)
	}
	f := &Feature{seen: map[string]string{}, linesAt: map[string]time.Time{}}
	f.showText = Get().showText

	now := time.Now()
	posted := ms(now.Add(-time.Second))
	send(e, "", "Dinner is ready")
	send(e, "package", "com.google.android.apps.messaging")
	send(e, "post_time", posted)
	send(e, "android.title", "Mum")
	f.look(now)

	n, ok := f.Showing()
	if !ok || n.Title != "Mum" || n.App != "Messages" {
		t.Fatalf("not shown: %+v %v", n, ok)
	}
	if n.Text != "" {
		t.Errorf("text shown with show text off: %q", n.Text)
	}
	if err := config.Set().Notifications().ShowText(true); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.Showing(); n.Text != "Dinner is ready" {
		t.Errorf("text with show text on: %q", n.Text)
	}

	// Words and no title, with the words hidden: the card still says something.
	f.mu.Lock()
	f.showing.Title = ""
	f.mu.Unlock()
	if err := config.Set().Notifications().ShowText(false); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.Showing(); n.Title != "New notification" || n.Text != "" {
		t.Errorf("untitled with text hidden: %+v", n)
	}

	// Looked at again, the same one does not come back once tapped away.
	f.Dismiss()
	f.look(now)
	if _, ok := f.Showing(); ok {
		t.Error("the same notification came up twice")
	}

	// A new one, then removed on the phone.
	posted = ms(now)
	send(e, "post_time", posted)
	f.look(now)
	if _, ok := f.Showing(); !ok {
		t.Fatal("new one not shown")
	}
	send(r, "package", "com.google.android.apps.messaging")
	send(r, "post_time", posted)
	f.look(now)
	if _, ok := f.Showing(); ok {
		t.Error("still up after it was removed on the phone")
	}
}

// WhatsApp's summary, the shape the companion app sends it in: lines joined with ", ", some of them
// with ", " of their own and line breaks, the newest last.
const summaryLines = "Karim @ Family: 📷 Sent a photo, ~ Abdul @ Investors' Club (IC): এখানে যে সব, ফসল হয়।, " +
	"Sanji: Hello, ~ Rijvi @ SQUARE FAMILY: প্রিয় @~Sanjoy অনেক ধন্যবাদ।\n\n* আগামীকাল দেখা করে, Submit করার পর।, " +
	"Sanji: How are you?, Sanji: Hey, are you free at 3:30, or later?"

func TestLastLine(t *testing.T) {
	for in, want := range map[string][2]string{
		summaryLines: {"Sanji", "Hey, are you free at 3:30, or later?"},
		"~ Rijvi @ SQUARE FAMILY: প্রিয়, ধন্যবাদ": {"Rijvi @ SQUARE FAMILY", "প্রিয়, ধন্যবাদ"},
		"['Sanji: Hello', 'Mum: Dinner, now']":     {"Mum", "Dinner, now"},
	} {
		from, said, ok := lastLine(in)
		if !ok || from != want[0] || said != want[1] {
			t.Errorf("lastLine(%.30q) = %q, %q, %v; want %q, %q", in, from, said, ok, want[0], want[1])
		}
	}
	for _, in := range []string{"", "None", "no sender here", "[]"} {
		if _, _, ok := lastLine(in); ok {
			t.Errorf("lastLine(%q) found a line", in)
		}
	}
}

func TestSummaryShowsTheNewestMessage(t *testing.T) {
	now := time.Now()
	v := values{
		"":                  "14 messages from 5 chats",
		"friendly_name":     "AIN065 Last notification",
		"package":           "com.whatsapp",
		"post_time":         ms(now),
		"is_ongoing":        "off",
		"android.title":     "WhatsApp",
		"android.text":      "14 messages from 5 chats",
		"android.textLines": summaryLines,
	}
	n, ok := noteFrom("sensor.ain065_last_notification", v, now)
	if !ok || n.Title != "Sanji" || n.Text != "Hey, are you free at 3:30, or later?" || n.Phone != "AIN065" {
		t.Fatalf("got %+v %v", n, ok)
	}
	// A chat's own notification is left as it is, lines or none.
	v["android.title"] = "Sanji"
	if n, _ := noteFrom("sensor.ain065_last_notification", v, now); n.Title != "Sanji" || n.Text != "14 messages from 5 chats" {
		t.Errorf("a chat's notification was read as a summary: %+v", n)
	}
}

// Lines left over from the last summary are not read into the next notification.
func TestStaleLinesAreLeftOut(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	const e = "sensor.tab_last_notification"
	if err := config.Set().Notifications().Entities([]string{e}); err != nil {
		t.Fatal(err)
	}
	f := &Feature{seen: map[string]string{}, linesAt: map[string]time.Time{}}
	now := time.Now()
	send(e, "android.textLines", summaryLines)
	send(e, "", "2 new emails")
	send(e, "package", "com.acme.mail")
	send(e, "post_time", ms(now))
	send(e, "android.title", "Mail")
	f.linesAt[e] = now.Add(-time.Minute) // arrived with a summary long gone
	f.look(now)
	f.mu.Lock()
	n := f.showing
	f.mu.Unlock()
	if n == nil || n.Title != "Mail" || n.Text != "2 new emails" {
		t.Errorf("got %+v", n)
	}
}
