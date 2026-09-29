//go:build !dot

package display

import (
	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/voice"
	"github.com/HuskerMinion/techo5/echod/internal/feature/xiaozhi"
)

// The Voice assistant row, on Sound & Voice above the wake words it is what those words reach.
//
// It goes above the wake word rather than below for a reason worth stating: the wake word row
// configures which words are listened for, and this row decides who answers them. A person looking
// for "which assistant does this talk to" will look at the wake word, because that is where the
// words are — and the row that changes the words is not where the answer is.

// voiceSub says what the setting costs, which is not the same as what it is.
//
// The whole weight of this row is on the sub. The value names one of two assistants and both of them
// are the same function, so the only thing a person needs told is where their microphone audio is
// about to go — and that differs per backend, and per moment within a backend, because the client
// can be off, connecting, or waiting for a code while the switch says xiaozhi is in force. A row
// that said only "Xiaozhi" in that state would be a switch that reads as working and is not.
func voiceSub() string {
	if !voice.OnXiaozhi() {
		// Not a reassurance. The distinction that matters to somebody choosing between these is where
		// the audio goes, and for Home Assistant that is the connection this device already holds.
		return "Answers over this device's own connection"
	}
	switch st := xiaozhi.Get().Status().State; st {
	case xiaozhi.StateOff:
		return "The client is off, so nothing will answer"
	case xiaozhi.StateActivating:
		return "Waiting for the activation code"
	case xiaozhi.StateConnected:
		return "Microphone audio goes to the xiaozhi cloud"
	case xiaozhi.StateConnecting:
		return "Connecting"
	default:
		return "Not connected: " + st
	}
}

// voiceBackendIndex is which of the two is in force, for the picker's tick. Zero rather than -1 when
// the value is somehow neither, because a picker with nothing ticked is a picker that looks broken
// and one with the wrong thing ticked is a row that will be changed by the next tap.
func voiceBackendIndex() int {
	cur := voice.Backend()
	for i, b := range config.VoiceBackends() {
		if b == cur {
			return i
		}
	}
	return 0
}

// chooseVoiceBackend puts the i'th backend in force.
func chooseVoiceBackend(i int) {
	all := config.VoiceBackends()
	if i < 0 || i >= len(all) {
		return
	}
	voice.Get().ChooseBackend(all[i])
}
