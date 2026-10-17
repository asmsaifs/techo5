// Package unseen names the characters a short piece of text shown to people must not hold: a device's
// name, a wake word's phrase. Each one either shows as nothing or changes what is shown without
// changing what is stored, so text holding one is not what it looks like.
package unseen

import "unicode"

// Rune is a character such text must not hold: a control character (Unicode's Cc), which shows as
// nothing or a box and some programs stop at; one that turns the text's direction (the bidi marks,
// embeddings, overrides and isolates), which makes what is shown differ from what is stored; or one
// that takes no room (the zero-width space, joiners, word joiner and byte order mark), which makes two
// names that look the same different.
func Rune(r rune) bool {
	switch {
	case unicode.Is(unicode.Cc, r):
		return true
	case r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
		return true // embeddings, overrides, isolates
	case r == '\u200e', r == '\u200f', r == '\u061c':
		return true // left-to-right, right-to-left and Arabic letter marks
	case r >= '\u200b' && r <= '\u200d', r == '\u2060', r == '\ufeff':
		return true // zero-width space, non-joiner, joiner, word joiner, byte order mark
	}
	return false
}
