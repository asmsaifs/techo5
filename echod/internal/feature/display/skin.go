//go:build !dot

package display

import (
	"log/slog"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/lib/fonts"
)

// Skins: a whole look for the screen, over and above the theme: the Show's (skin_show.go), which the
// Panel app wears too, and the Spot's (skin_spot.go). A skin brings its own colors and often its own
// font, and may change what reaches the panel or draw behind the home clock: E-ink turns it to paper and
// ink. LCARS is a starship's console, with its frame around the home clock; Synthwave a neon sunset over
// a grid; HAL 9000 black and red with the computer's eye. Commodore 64 is the home computer's blue
// screen, its border and its READY prompt (skin_c64.go); TECHO5 BASIC is the same screen saying which
// TECHO5 it is and how much memory it has. Big and plain, Control room, Prairie and Earthrise
// are the readable ones (skin_readable.go): full color, no filter, the words at 7:1 or more. While a skin is
// on, the theme and the font chosen are kept and come back when it is taken off. None until somebody
// chooses.

const (
	skinNone      = ""
	skinLCARS     = "lcars"
	skinEInk      = "eink"
	skinSynthwave = "synthwave"
	skinHAL       = "hal9000"
	skinC64       = "c64"
	skinBASIC     = "techo5basic"
	skinControl   = "controlroom"
	skinPrairie   = "prairie"
	skinPlain     = "bigplain"
	skinEarthrise = "earthrise"
)

// skins are the choices, as the screen and Home Assistant name them, with what the config keeps.
var skins = []struct {
	label, value string
	font         string // the font the skin draws in
}{
	{"None", skinNone, ""},
	{"LCARS", skinLCARS, fonts.Antonio},
	{"E-ink", skinEInk, ""},
	{"Synthwave", skinSynthwave, "exo"},
	{"HAL 9000", skinHAL, ""},
	{"Commodore 64", skinC64, fonts.C64},
	{"TECHO5 BASIC", skinBASIC, fonts.C64},
	{"Prairie", skinPrairie, "overpass"},
	{"Control room", skinControl, "inter"},
	{"Big and plain", skinPlain, "inter"},
	{"Earthrise", skinEarthrise, "inter"},
}

// skinsGone are the one-color skins taken out in v1.6.9, hard to read on some screens, and the
// readable skin a screen that wore one wears now.
var skinsGone = map[string]string{
	"pipboy": skinControl, "amber": skinControl, "matrix": skinControl,
	"blueprint": skinPrairie, "gameboy": skinPlain,
}

func skinOptions() []string {
	out := make([]string, len(skins))
	for i, s := range skins {
		out[i] = s.label
	}
	return out
}

// skinIndex is the saved choice's place in skins; a value no choice has, or any skin on a screen that
// has none, reads as None.
func skinIndex() int {
	if !hasSkins {
		return 0
	}
	v := config.Get().Screen.Skin
	if skinPreview != "" {
		v = skinPreview
	}
	if to, ok := skinsGone[v]; ok {
		v = to
	}
	for i, s := range skins {
		if s.value == v {
			return i
		}
	}
	return 0
}

// skinKeeps is the sub-line of a setting the skin in force sets aside (themeRows, fontRow): it can
// still be changed, and is what the screen wears once the skin is off.
const skinKeeps = "Kept for when the skin is off"

// skinPreview is a skin the preview tests draw every screen in, whatever the config says (PREVIEW_SKIN);
// empty in the daemon.
var skinPreview string

// skin is the skin in force.
func skin() string { return skins[skinIndex()].value }

// drawnFont is the font the screen draws in: the skin's, or the one chosen.
func drawnFont() string {
	if f := skins[skinIndex()].font; f != "" {
		return f
	}
	return screenFont()
}

// setSkin saves the choice at i and shows it in Home Assistant.
func setSkin(s *esphome.Select, i int) {
	if i < 0 || i >= len(skins) {
		return
	}
	if err := config.Set().Screen().Skin(skins[i].value); err != nil {
		slog.Error("saving the skin failed", "err", err)
		return
	}
	if s != nil {
		s.Set(skins[i].label)
	}
}

// skinSelect is the Home Assistant setting; wake redraws the screen in the new skin.
func skinSelect(wake func()) *esphome.Select {
	s := &esphome.Select{
		Base: esphome.Base{
			ObjectID: "screen_skin",
			Name:     "Screen skin",
			Icon:     "mdi:palette-swatch-variant",
			Category: esphome.CategoryConfig,
		},
		Options: skinOptions(),
	}
	s.OnCommand = func(v string) {
		for i, sk := range skins {
			if sk.label == v {
				setSkin(s, i)
				wake()
				return
			}
		}
	}
	return s
}
