package remote

import (
	"strings"
	"testing"
)

// The stream gives what was said, and what the assistant's tools read, as a length only.
func TestTheStreamCarriesNoWords(t *testing.T) {
	for _, line := range []string{
		`2026/10/17 07:00:00 INFO heard slot=1 text="what's on the calendar today"`,
		`2026/10/17 07:00:01 INFO replying slot=1 text=hello`,
		`2026/10/17 07:00:02 INFO assistant: tool name=calendar args="{\"day\":\"today\"}" result="Dentist at 3"`,
		`2026/10/17 07:00:03 INFO announcing text="dinner is ready" seconds=0 to=3`,
		`2026/10/17 07:00:04 INFO calendar: pop-up summary="Dentist at noon" starts=12:00PM night=false`,
		`2026/10/17 07:00:05 INFO reminder id=r1 label="Dentist call" from=""`,
	} {
		got := withoutWords(line)
		for _, said := range []string{"calendar today", "hello", "Dentist", "dinner is ready", `\"day\"`} {
			if strings.Contains(got, said) {
				t.Errorf("%q kept %q", got, said)
			}
		}
		if !strings.Contains(got, "characters>") {
			t.Errorf("%q gives no length", got)
		}
	}
	if got := withoutWords(`INFO restored what=remote_api using=true`); got != `INFO restored what=remote_api using=true` {
		t.Errorf("a line with no words changed: %q", got)
	}
}

// What was last heard and said is not in the API's list, nor changeable through it.
func TestTheLastWordsAreNotListed(t *testing.T) {
	for _, id := range []string{"last_heard", "last_reply"} {
		if !strings.Contains(strings.Join(unlisted, " "), id) {
			t.Errorf("%s is listed", id)
		}
	}
}
