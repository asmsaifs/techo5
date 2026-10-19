package setup

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// Saving keeps the rows as typed, in order. A kept station's row with its name cleared is taken out
// whatever its address still says, as the note under the form promises; the spare row at the bottom
// is skipped.
func TestSavingStationsTakesOutARowWithNoName(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := home.SetOwnStations([]config.Station{
		{Name: "Jazz", URL: "https://stream.example/jazz"},
		{Name: "News", URL: "http://stream.example/news"},
	}); err != nil {
		t.Fatal(err)
	}
	says := saveStations(form(url.Values{
		"name": {" Jazz ", "", "Talk", ""},
		"url":  {"https://stream.example/jazz", "http://stream.example/news", " https://stream.example/talk ", ""},
	}))
	if says != "" {
		t.Fatalf("saving said %q, want nothing", says)
	}
	got := home.OwnStations()
	want := []config.Station{{Name: "Jazz", URL: "https://stream.example/jazz"}, {Name: "Talk", URL: "https://stream.example/talk"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("kept %+v, want %+v", got, want)
	}
}

// A row that is named but cannot play is refused, and so is a new address typed without a name;
// nothing is saved, and the list as it was stays.
func TestSavingStationsRefusesARowItCannotKeep(t *testing.T) {
	for _, c := range []struct {
		what   string
		values url.Values
		says   string
	}{
		{"a name with no address", url.Values{"name": {"KXYZ"}, "url": {""}}, "no stream address"},
		{"a new address with no name", url.Values{"name": {""}, "url": {"https://stream.example/new"}}, "needs a name as well"},
		{"an address that is not a stream address", url.Values{"name": {"KXYZ"}, "url": {"file:///etc/passwd"}}, "http://"},
		{"a form that did not arrive whole", url.Values{"name": {"KXYZ", "KABC"}, "url": {"https://s/1"}}, "did not arrive whole"},
	} {
		config.Use(filepath.Join(t.TempDir(), "state.json"))
		kept := []config.Station{{Name: "Jazz", URL: "https://stream.example/jazz"}}
		if err := home.SetOwnStations(kept); err != nil {
			t.Fatal(err)
		}
		if got := saveStations(form(c.values)); !strings.Contains(got, c.says) {
			t.Errorf("%s: said %q, want it to say %q", c.what, got, c.says)
		}
		if got := home.OwnStations(); len(got) != 1 || got[0] != kept[0] {
			t.Errorf("%s: the list became %+v", c.what, got)
		}
	}
}
