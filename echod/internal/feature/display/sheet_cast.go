//go:build !dot && !spot

package display

import (
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/cast"
)

// The Cast rows, at the end of Connections: turn receiving on, choose whether a phone has to be accepted
// here, and show the code a phone scans to pair. Everything but the key is also a switch in Home
// Assistant; the key is made here if there is none, so none of this needs Home Assistant at all.

func castRows() []settingRow {
	c := config.Get().Cast
	sub := "Off"
	if c.Enabled {
		sub = "Phones on this network can cast here"
	}
	rows := []settingRow{{id: "cast", label: "Cast from a phone", sub: sub, kind: ctlToggle, on: c.Enabled}}
	if !c.Enabled {
		return rows
	}
	pair := "Scan it in TECHO5 Cast on the phone"
	if c.Key == "" {
		pair = "Makes a key, then shows it"
	}
	return append(rows,
		settingRow{id: "castask", label: "Ask before a phone casts", sub: "Accept or Decline, here on the screen", kind: ctlToggle, on: !c.NoPrompt},
		settingRow{id: "castpair", label: "Pairing code", sub: pair, kind: ctlButton, button: "Show"},
	)
}

// castRowTap is a tap on one of those rows.
func (d *Display) castRowTap(id string) {
	f := cast.Get()
	switch id {
	case "cast":
		f.SetEnabled(!config.Get().Cast.Enabled)
	case "castask":
		f.SetAsk(config.Get().Cast.NoPrompt)
	case "castpair":
		d.closeSheet()
		f.ShowPairing(true)
	}
}
