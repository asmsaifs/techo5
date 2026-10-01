package phone

import "github.com/HuskerMinion/techo5/echod/internal/lib/halfrate"

// A phone call is 8 kHz; the microphones and the speaker's voice path are 16 kHz. The conversions are
// lib/halfrate's, shared with talking back through a camera.

func newDown() *halfrate.Down { return halfrate.NewDown() }
func newUp() *halfrate.Up     { return halfrate.NewUp() }
func clamp(v float64) int16   { return halfrate.Clamp(v) }
