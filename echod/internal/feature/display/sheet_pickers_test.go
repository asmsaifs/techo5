//go:build !dot

package display

import (
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Every row that shows a choice and has a list of choices opens that list when tapped. Quiet hours
// once had its list and no way to it: the row read "Never" and a tap did nothing.
func TestEveryChoiceRowOpensItsList(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	n := 0
	for cat := category(0); cat < categories; cat++ {
		sv := sheetView{st: settings{cat: cat}}
		rows, _ := categoryRows(sv)
		for _, row := range rows {
			if row.kind != ctlChoice {
				continue
			}
			if _, ok := pickerFor(row.id, sv); !ok {
				continue
			}
			d := &Display{}
			d.rowTap(row.id, partMain, 0)
			if d.picker != row.id {
				t.Errorf("%s: a tap on %q opened %q", categoryNames[cat], row.id, d.picker)
			}
			n++
		}
	}
	if n < 10 {
		t.Errorf("only %d choice rows checked", n)
	}
}
