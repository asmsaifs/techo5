package media

import (
	"math"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/hardware/speaker"
)

// Louder speech (#304). Speech normalized to speechTargetRMS still sounds quieter than Amazon's, which
// compresses it: the quiet parts of words are lifted and the loud ones held down, so the whole reply
// sits nearer the top. Louder is that compressor, for what goes to the speaker only: replies and
// announcements, not music, not what is sent for transcription or into a call. It runs ahead of the
// speaker, so the echo canceller's reference (made from what the speaker was sent) includes it, and
// it ends in the same soft limiter as Scale, so a loud syllable compresses rather than cracks.
const (
	// louderThreshold is where compression starts, as a fraction of full scale (-16.5 dBFS, on the
	// level followed, which is the average of the waveform's size): the loud parts of normalized
	// speech are over it and held down, the quiet ones under it and lifted by the whole makeup.
	louderThreshold = 0.15
	// louderRatio is how much a level over the threshold is held down: 3 dB over comes out 1 dB over.
	louderRatio = 3.0
	// louderMakeup is the gain added after, +8 dB: the quiet parts of words come up by that much and
	// the loud ones by less, about +3 to +5 dB on average for normalized speech. A word's first few
	// milliseconds, before the level has followed it up, are caught by the soft limiter.
	louderMakeup = 2.5
	// louderAttack and louderRelease are how fast the level follows a word starting and ending.
	louderAttack  = 0.005
	louderRelease = 0.12
)

// Louder compresses speech chunk by chunk; one is kept for a whole reply so the level it follows runs
// on across chunks. The zero value is ready.
type Louder struct {
	env float64
}

var (
	louderAttackK  = math.Exp(-1 / (louderAttack * speaker.VoiceRate))
	louderReleaseK = math.Exp(-1 / (louderRelease * speaker.VoiceRate))
)

// Process returns in compressed, made up and soft-limited, at speaker.VoiceRate.
func (l *Louder) Process(in []int16) []int16 {
	out := make([]int16, len(in))
	for i, s := range in {
		x := float64(s) / 32768
		a := math.Abs(x)
		k := louderReleaseK
		if a > l.env {
			k = louderAttackK
		}
		l.env = k*l.env + (1-k)*a
		g := louderMakeup
		if l.env > louderThreshold {
			g *= math.Pow(l.env/louderThreshold, 1/louderRatio-1)
		}
		out[i] = softLimit(x * g * 32768)
	}
	return out
}

// LouderSpeech is whether the Louder speech switch is on.
func LouderSpeech() bool { return config.Get().Speaker.LouderSpeech }

// ForSpeaker is speech as the speaker should play it: compressed by l when Louder speech is on, as it
// is otherwise. l may be nil for a whole clip.
func ForSpeaker(in []int16, l *Louder) []int16 {
	if !LouderSpeech() {
		return in
	}
	if l == nil {
		l = &Louder{}
	}
	return l.Process(in)
}
