package timezone

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
	_ "time/tzdata" // a zone database for the check below on a machine with none as files; the files win where there are some
)

// commonOffsets is each common zone's UTC offset in hours in mid-January and mid-July 2027, written
// out here so the rules are checked against facts and not only against whatever zone database the
// machine running the tests happens to have.
var commonOffsets = map[string][2]float64{
	"America/New_York": {-5, -4}, "America/Chicago": {-6, -5}, "America/Denver": {-7, -6},
	"America/Phoenix": {-7, -7}, "America/Los_Angeles": {-8, -7}, "America/Anchorage": {-9, -8},
	"Pacific/Honolulu": {-10, -10}, "America/Toronto": {-5, -4},
	"America/Vancouver":   {-7, -7}, // daylight time all year from November 2026, tzdata 2026c on
	"America/Mexico_City": {-6, -6}, "America/Sao_Paulo": {-3, -3}, "Europe/London": {0, 1},
	"Europe/Dublin": {0, 1}, "Europe/Paris": {1, 2}, "Europe/Berlin": {1, 2}, "Europe/Madrid": {1, 2},
	"Europe/Rome": {1, 2}, "Europe/Amsterdam": {1, 2}, "Europe/Stockholm": {1, 2}, "Europe/Warsaw": {1, 2},
	"Europe/Moscow": {3, 3}, "Africa/Johannesburg": {2, 2}, "Asia/Dubai": {4, 4}, "Asia/Kolkata": {5.5, 5.5},
	"Asia/Singapore": {8, 8}, "Asia/Shanghai": {8, 8}, "Asia/Tokyo": {9, 9}, "Asia/Seoul": {9, 9},
	"Australia/Perth": {8, 8}, "Australia/Sydney": {11, 10}, "Pacific/Auckland": {13, 12}, "UTC": {0, 0},
}

// commonChanges are daylight saving changes in 2027, one per kind of rule: the offset in hours just
// before the given instant and from it on.
var commonChanges = []struct {
	zone          string
	at            time.Time
	before, after float64
}{
	{"America/New_York", time.Date(2027, 3, 14, 7, 0, 0, 0, time.UTC), -5, -4},
	{"America/New_York", time.Date(2027, 11, 7, 6, 0, 0, 0, time.UTC), -4, -5},
	{"America/Los_Angeles", time.Date(2027, 11, 7, 9, 0, 0, 0, time.UTC), -7, -8},
	{"Europe/London", time.Date(2027, 3, 28, 1, 0, 0, 0, time.UTC), 0, 1},
	{"Europe/Dublin", time.Date(2027, 10, 31, 1, 0, 0, 0, time.UTC), 1, 0},
	{"Europe/Paris", time.Date(2027, 10, 31, 1, 0, 0, 0, time.UTC), 2, 1},
	{"Australia/Sydney", time.Date(2027, 4, 3, 16, 0, 0, 0, time.UTC), 11, 10},
	{"Australia/Sydney", time.Date(2027, 10, 2, 16, 0, 0, 0, time.UTC), 10, 11},
	{"Pacific/Auckland", time.Date(2027, 4, 3, 14, 0, 0, 0, time.UTC), 13, 12},
	{"Pacific/Auckland", time.Date(2027, 9, 25, 14, 0, 0, 0, time.UTC), 12, 13},
}

// offsetHours is loc's UTC offset at the given instant, in hours.
func offsetHours(at time.Time, loc *time.Location) float64 {
	_, offset := at.In(loc).Zone()
	return float64(offset) / 3600
}

// keepsTheFacts is whether loc has the offsets written down for name, in winter and summer and over
// the changes listed for it.
func keepsTheFacts(name string, loc *time.Location) error {
	want := commonOffsets[name]
	for i, month := range []time.Month{time.January, time.July} {
		if got := offsetHours(time.Date(2027, month, 15, 12, 0, 0, 0, time.UTC), loc); got != want[i] {
			return fmt.Errorf("offset %v in %s 2027, want %v", got, month, want[i])
		}
	}
	for _, c := range commonChanges {
		if c.zone != name {
			continue
		}
		if got := offsetHours(c.at.Add(-time.Second), loc); got != c.before {
			return fmt.Errorf("offset %v just before %s, want %v", got, c.at, c.before)
		}
		if got := offsetHours(c.at, loc); got != c.after {
			return fmt.Errorf("offset %v from %s, want %v", got, c.at, c.after)
		}
	}
	return nil
}

// Every common zone has a rule, and the rule keeps the zone's clock: the offsets written down above,
// and, where the machine's zone database keeps them too, the database's clock hour by hour across
// the next few years, over every daylight saving change. A database older than the facts (one from
// before British Columbia's all-year daylight time) is not checked against for that zone, so the
// test does not depend on the machine it runs on.
func TestCommonRulesMatchTheDatabase(t *testing.T) {
	if len(commonRules) != len(Common) {
		t.Errorf("%d rules for %d common zones", len(commonRules), len(Common))
	}
	if len(commonOffsets) != len(Common) {
		t.Errorf("%d written down offsets for %d common zones", len(commonOffsets), len(Common))
	}
	start := time.Now().UTC().Truncate(time.Hour)
	for _, name := range Common {
		rule, ok := commonRules[name]
		if !ok {
			t.Errorf("%s has no rule", name)
			continue
		}
		got, err := loadRule(rule)
		if err != nil {
			t.Errorf("%s: rule %q: %v", name, rule, err)
			continue
		}
		if err := keepsTheFacts(name, got); err != nil {
			t.Errorf("%s: rule %q: %v", name, rule, err)
			continue
		}
		want, err := time.LoadLocation(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := keepsTheFacts(name, want); err != nil {
			t.Logf("%s: not checked against this machine's zone database, which is older: %v", name, err)
			continue
		}
		for at := start; at.Before(start.AddDate(4, 0, 0)); at = at.Add(time.Hour) {
			if w, g := offsetHours(at, want), offsetHours(at, got); w != g {
				t.Errorf("%s at %s: rule offset %v, database %v", name, at, g, w)
				break
			}
		}
	}
}

// The facts the rules are checked against hold Vancouver to its new all-year rule, so a rule that
// went back to the old one would fail on any machine.
func TestVancouverIsOnMountainTimeAllYear(t *testing.T) {
	if rule := commonRules["America/Vancouver"]; rule != "MST7" {
		t.Errorf("America/Vancouver rule %q, want MST7", rule)
	}
	old, err := loadRule("PST8PDT,M3.2.0,M11.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if keepsTheFacts("America/Vancouver", old) == nil {
		t.Error("the old Pacific time rule passed for America/Vancouver")
	}
}

// A unit without the zone database still takes a common zone by name: the rule goes in force and
// the name is what is saved. Any other name is refused with something that says why.
func TestACommonZoneWithoutTheDatabase(t *testing.T) {
	dir := t.TempDir()
	nameFile, linkFile, zoneinfo = filepath.Join(dir, "timezone"), filepath.Join(dir, "localtime"), filepath.Join(dir, "no-zoneinfo")
	drainRestarts()
	if HasDatabase() {
		t.Fatal("an empty directory counted as the zone database")
	}

	z := &Zone{}
	if err := z.apply("Europe/Amsterdam"); err != nil {
		t.Fatal(err)
	}
	if got := z.Current(); got != "Europe/Amsterdam" {
		t.Errorf("saved zone = %q", got)
	}
	data, err := os.ReadFile(linkFile)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocationFromTZData("file", data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		when   time.Time
		offset int
	}{
		{time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC), 3600},
		{time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC), 2 * 3600},
	} {
		if _, offset := tc.when.In(loc).Zone(); offset != tc.offset {
			t.Errorf("%s: offset %d, want %d", tc.when, offset, tc.offset)
		}
	}
	if why := restartAsked(); why != "time zone Europe/Amsterdam" {
		t.Errorf("restart asked for %q", why)
	}

	if err := z.apply("America/Boise"); !errors.Is(err, errNoDatabase) {
		t.Errorf("a zone with no file and no rule: %v", err)
	}
	if got := z.Current(); got != "Europe/Amsterdam" {
		t.Errorf("a refused zone replaced the saved one: %q", got)
	}
}

// A rule from Home Assistant with a slash in its daylight saving part is a rule, not a name, so it is
// applied without the database.
func TestARuleWithASlashIsNoName(t *testing.T) {
	dir := t.TempDir()
	nameFile, linkFile, zoneinfo = filepath.Join(dir, "timezone"), filepath.Join(dir, "localtime"), filepath.Join(dir, "no-zoneinfo")
	drainRestarts()
	rule := "CET-1CEST,M3.5.0,M10.5.0/3"
	if err := (&Zone{}).apply(rule); err != nil {
		t.Fatal(err)
	}
	if why := restartAsked(); why != "time zone "+rule {
		t.Errorf("restart asked for %q", why)
	}
}
