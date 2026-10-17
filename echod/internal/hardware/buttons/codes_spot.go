//go:build spot

package buttons

// longHoldAfter is zero: the round screen is where pairing and everything else is asked for.
const longHoldAfter = 0

// The Echo Spot's three buttons, by evdev key code. Volume comes from gpio-keys (`keys`); the mute
// button is the keypad's power key (`mtk-kpd`), 116. On LineageOS's kernel that key only reports a
// press: it does not cut the microphones (docs/hardware.md in techo5-spot, "Mute button").
var codes = map[uint16]Name{
	114: VolumeDown,
	115: VolumeUp,
	116: Mute,
}

// notButtons are input devices that are not buttons, by name: none here.
var notButtons map[string]bool
