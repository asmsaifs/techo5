//go:build !dot

package display

import (
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/btaudio"
)

// Every device's Connections card has Forget under the earbuds or speaker it plays to, connected or
// only remembered, and none when there is nothing to forget; a first tap arms it.
func TestForgetUnderTheSpeaker(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	find := func(rows []settingRow, id string) (settingRow, int) {
		for i, r := range rows {
			if r.id == id {
				return r, i
			}
		}
		return settingRow{}, -1
	}
	for name, bt := range map[string]btaudio.State{
		"connected":  {Available: true, Connected: "Earbuds"},
		"remembered": {Available: true, Remembered: "Earbuds"},
	} {
		rows := connectionRows(sheetView{bt: bt, now: now})
		f, i := find(rows, "btforget")
		_, b := find(rows, "bt")
		if i != b+1 || f.label != "Forget Earbuds" || f.kind != ctlDanger || f.button != "Forget" {
			t.Errorf("%s: forget row %+v at %d, bt at %d", name, f, i, b)
		}
		armed := connectionRows(sheetView{bt: bt, now: now, st: settings{forgetArm: now.Add(-time.Second)}})
		if f, _ := find(armed, "btforget"); f.button != "Confirm" {
			t.Errorf("%s: armed Forget reads %q", name, f.button)
		}
	}
	for name, bt := range map[string]btaudio.State{
		"none yet":      {Available: true},
		"not available": {Connected: "Earbuds"},
	} {
		if _, i := find(connectionRows(sheetView{bt: bt, now: now}), "btforget"); i >= 0 {
			t.Errorf("%s: a Forget with nothing to forget", name)
		}
	}
}
