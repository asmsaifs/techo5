package voice

import (
	"strings"
	"time"
	"unicode"

	"github.com/HuskerMinion/techo5/echod/internal/lib/hook"
)

// errNoText is what Home Assistant reports when speech-to-text heard nothing it could write down.
const errNoText = "stt-no-text-recognized"

// Mistake is a turn the wake word opened that nobody meant: nothing was said after it, or only what
// speech-to-text writes when all it got was noise.
type Mistake struct {
	Slot int

	// Heard is what speech-to-text wrote, empty when it wrote nothing.
	Heard string

	// Opened is when the turn opened, which tells its wake from an earlier one of the same slot.
	Opened time.Time
}

// Mistaken is told of every wake taken as a mistake, after the turn has ended.
var Mistaken hook.Hook[Mistake]

// noise is what speech-to-text writes for a room with nobody speaking to it: Whisper fills silence and
// noise with the endings of the videos it learned from. Said on its own after a wake word, none of it
// asks for anything, so a reply to it is the device talking to an empty room.
var noise = map[string]bool{
	"thank you": true, "thank you very much": true, "thanks": true, "thanks for watching": true,
	"thank you for watching": true, "you": true, "so": true, "yeah": true, "bye": true, "oh": true,
	"okay": true, "ok": true, "hmm": true, "um": true, "uh": true, "huh": true, "what": true, "the": true,
	"i": true, "a": true,
	// The same in the other languages the screen offers.
	"danke": true, "gracias": true, "merci": true, "grazie": true, "bedankt": true, "спасибо": true,
	"спасибо за просмотр": true, "продолжение следует": true,
}

// onlyNoise is whether text is nothing but what speech-to-text writes for noise: case, spacing and
// punctuation aside, one of the phrases above.
func onlyNoise(text string) bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})
	if len(words) == 0 {
		return false
	}
	return noise[strings.Join(words, " ")]
}
