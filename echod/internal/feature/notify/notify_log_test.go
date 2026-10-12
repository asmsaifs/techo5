package notify

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// A notification's words stay out of the log at Info: diagnostics bundles carry the log, and they get
// pasted into public issues.
func TestANotificationsWordsAreNotLoggedAtInfo(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	r := newRig(t, true)
	if err := r.f.Notify("the private message", "Private Title", "", "off", "", "", 60); err != nil {
		t.Fatal(err)
	}
	if err := r.f.NotifyPicture("another private message", "Another Title", "/local/door.png", "off", "", "", "", 60); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "notification") {
		t.Fatal("nothing about the notification was logged")
	}
	for _, words := range []string{"private message", "Private Title", "Another Title"} {
		if strings.Contains(out, words) {
			t.Errorf("the log at Info carries %q:\n%s", words, out)
		}
	}
}
