package assistant

import (
	"strings"
	"testing"
)

// A page's menus are not what it is about: a schedule behind twenty thousand characters of them was
// cut off before the model saw it, and the model made a game up.
func TestPageTextKeepsTheContentNotTheMenus(t *testing.T) {
	page := `<html><head><title>Schedule</title><script>var x = "not this";</script></head><body>
	<header><nav><a>twitter for Baseball</a><a>Schedule for Golf</a></nav></header>
	<main><nav><a>Roster</a></nav>
	 <h1>Football Schedule</h1>
	 <table><tr><td>Sat, Oct 3</td><td>vs Maryland</td><td>3:00 PM CT</td></tr>
	  <tr><td>Sat, Oct 10</td><td>at Indiana</td></tr></table>` + strings.Repeat("<p>Game notes and more game notes.</p>", 30) + `
	</main><footer>Copyright</footer><aside>Buy tickets</aside></body></html>`
	got := pageText(page)
	for _, want := range []string{"Football Schedule", "Sat, Oct 3 vs Maryland 3:00 PM CT", "Sat, Oct 10 at Indiana"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, not := range []string{"twitter for Baseball", "Schedule for Golf", "Roster", "not this", "Copyright", "Buy tickets"} {
		if strings.Contains(got, not) {
			t.Errorf("kept %q", not)
		}
	}
	if !strings.HasPrefix(got, "Football Schedule") {
		t.Errorf("the content does not come first:\n%.200s", got)
	}
}
