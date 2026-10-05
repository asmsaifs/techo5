package notification

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/HuskerMinion/techo5/echod/internal/feature/hastate"
)

// Note is one notification as the card shows it.
type Note struct {
	// ID is the package and the post time, which is what the removed sensor names too.
	ID     string
	App    string // the app's name: "WhatsApp"
	Phone  string // the phone's name: "Pixel 8"
	Title  string // for a message, who sent it
	Text   string
	Posted time.Time
}

// fresh is how old a notification may be and still come up. It is what keeps the last one from
// coming back when the device restarts or Home Assistant reconnects and sends the sensor again; a
// little room past now is for a phone's clock a touch ahead of the device's.
const (
	fresh = 2 * time.Minute
	ahead = time.Minute
)

// values is one sensor as followed: its state and the attributes asked for, by name.
type values map[string]string

// noteFrom is the notification a sensor holds, or false when there is nothing to show: none yet, an
// ongoing one (music, navigation, a call), one with no words, or one too old to be news.
func noteFrom(entity string, v values, now time.Time) (Note, bool) {
	pkg := strings.TrimSpace(v["package"])
	posted, ok := postTime(v["post_time"])
	if pkg == "" || !ok {
		return Note{}, false
	}
	if isTrue(v["is_ongoing"]) {
		return Note{}, false
	}
	if now.Sub(posted) > fresh || posted.Sub(now) > ahead {
		return Note{}, false
	}
	title := clean(v["android.title"])
	// The state is the text, cut to Home Assistant's 255 characters, or the package when there is none.
	// It is read before the text attribute because Home Assistant does not send an attribute a new
	// notification lacks: the attribute can still hold the one before's.
	text := clean(v[""])
	if text == pkg || quietState(text) {
		text = ""
	}
	if long := clean(v["android.text"]); text != "" && strings.HasPrefix(long, text) {
		text = long
	}
	// A summary ("WhatsApp", "14 messages from 5 chats") stands in for the message it came with: the
	// companion app sends only the last of the two, and the summary's lines end with the message.
	if strings.EqualFold(title, appName(pkg)) {
		if from, said, ok := lastLine(v["android.textLines"]); ok {
			title, text = from, said
		}
	}
	if title == "" && text == "" {
		return Note{}, false
	}
	return Note{
		ID:     pkg + "|" + strconv.FormatInt(posted.UnixMilli(), 10),
		App:    appName(pkg),
		Phone:  phoneName(v["friendly_name"], entity),
		Title:  title,
		Text:   text,
		Posted: posted,
	}, true
}

// sender is how an inbox summary's line begins: who, then a colon. A group's is "Name @ Group: ",
// and WhatsApp marks somebody not in the phone's contacts with "~ ".
var sender = regexp.MustCompile(`^(~\s*)?([^:\n]{1,80}): `)

// lastLine is the newest of an inbox summary's lines (android.textLines), split into who sent it and
// what it says. The companion app sends the lines joined with ", ", and a message can hold ", " too, so
// the last line is found from the end: back to the last piece that starts the way a line does. Home
// Assistant sends a list as Python writes one, which is read as one.
func lastLine(s string) (from, said string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "None" {
		return "", "", false
	}
	var line string
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		items := hastate.Options(s)
		if len(items) == 0 {
			return "", "", false
		}
		line = items[len(items)-1]
	} else {
		pieces := strings.Split(s, ", ")
		for i := len(pieces) - 1; i >= 0; i-- {
			if sender.MatchString(pieces[i]) {
				line = strings.Join(pieces[i:], ", ")
				break
			}
		}
	}
	m := sender.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return clean(m[2]), clean(line[len(m[0]):]), true
}

// removedID is the notification a Last removed notification sensor names, in Note.ID's form.
func removedID(v values) string {
	pkg := strings.TrimSpace(v["package"])
	posted, ok := postTime(v["post_time"])
	if pkg == "" || !ok {
		return ""
	}
	return pkg + "|" + strconv.FormatInt(posted.UnixMilli(), 10)
}

// postTime reads the post_time attribute: milliseconds since 1970, as Android keeps it.
func postTime(s string) (time.Time, bool) {
	ms, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}, false
	}
	return time.UnixMilli(ms), true
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "on":
		return true
	}
	return false
}

func quietState(s string) bool {
	switch strings.ToLower(s) {
	case "", "unknown", "unavailable", "none":
		return true
	}
	return false
}

// clean is a line for the screen: what Android puts in a notification's words can carry line breaks
// and the invisible marks some apps use, and the card has one line for a title and two for the text.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			return ' '
		case r == '‌' || r == '‍':
			return r // joiners: Bangla and other scripts need them
		case unicode.Is(unicode.Cf, r) || unicode.IsControl(r):
			return -1
		}
		return r
	}, s)
	if s == "None" { // Python's word for an attribute that is there and empty
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

// apps are the names of the packages people most often let through; anything else is named from its
// package.
var apps = map[string]string{
	"com.whatsapp":                           "WhatsApp",
	"com.whatsapp.w4b":                       "WhatsApp Business",
	"org.telegram.messenger":                 "Telegram",
	"org.thoughtcrime.securesms":             "Signal",
	"com.facebook.orca":                      "Messenger",
	"com.facebook.katana":                    "Facebook",
	"com.instagram.android":                  "Instagram",
	"com.google.android.apps.messaging":      "Messages",
	"com.samsung.android.messaging":          "Messages",
	"com.android.mms":                        "Messages",
	"com.google.android.dialer":              "Phone",
	"com.samsung.android.dialer":             "Phone",
	"com.android.server.telecom":             "Phone",
	"com.google.android.gm":                  "Gmail",
	"com.microsoft.office.outlook":           "Outlook",
	"com.google.android.calendar":            "Calendar",
	"com.slack":                              "Slack",
	"com.discord":                            "Discord",
	"com.viber.voip":                         "Viber",
	"jp.naver.line.android":                  "LINE",
	"com.imo.android.imoim":                  "imo",
	"com.skype.raider":                       "Skype",
	"com.microsoft.teams":                    "Teams",
	"us.zoom.videomeetings":                  "Zoom",
	"com.ring.android":                       "Ring",
	"com.google.android.apps.chromecast.app": "Google Home",
	"com.amazon.dee.app":                     "Alexa",
	"com.google.android.youtube":             "YouTube",
	"com.twitter.android":                    "X",
	"com.google.android.apps.dynamite":       "Google Chat",
	"com.bKash.customerapp":                  "bKash",
}

// appName is what a package is called: its known name, or its last telling part with a capital
// ("com.acme.doorbell" → "Doorbell").
func appName(pkg string) string {
	if n, ok := apps[pkg]; ok {
		return n
	}
	parts := strings.Split(pkg, ".")
	last := parts[len(parts)-1]
	// "com.acme.app", "com.acme.android": the last part says nothing, the one before it does.
	for len(parts) > 1 && (last == "android" || last == "app" || last == "mobile" || last == "client") {
		parts = parts[:len(parts)-1]
		last = parts[len(parts)-1]
	}
	if last == "" {
		return pkg
	}
	return strings.ToUpper(last[:1]) + last[1:]
}

// phoneName is the phone a sensor is on: its name in Home Assistant without "Last notification", or
// failing that, the entity's object id the same way.
func phoneName(friendly, entity string) string {
	name := strings.TrimSpace(friendly)
	if name == "" {
		_, name, _ = strings.Cut(entity, ".")
		name = strings.ReplaceAll(name, "_", " ")
	}
	for _, tail := range []string{" last notification", " Last notification", " Last Notification"} {
		if strings.HasSuffix(name, tail) {
			name = strings.TrimSuffix(name, tail)
			break
		}
	}
	name = strings.TrimSpace(name)
	if name != "" && unicode.IsLower([]rune(name)[0]) {
		r := []rune(name)
		r[0] = unicode.ToUpper(r[0])
		name = string(r)
	}
	return name
}

// removedEntity is the Last removed notification sensor beside a Last notification one, by the name
// the companion app gives it; empty when the name is not the companion app's.
func removedEntity(entity string) string {
	const last, removed = "_last_notification", "_last_removed_notification"
	if !strings.HasSuffix(entity, last) {
		return ""
	}
	return strings.TrimSuffix(entity, last) + removed
}
