package denoise

import "testing"

// BenchmarkPush is one 20 ms window of real noisy speech at the microphone's 16 kHz, the work the
// microphone does fifty times a second.
func BenchmarkPush(b *testing.B) {
	noisy, _ := wav(b, "in_SNR5.wav")
	f := New(16000)
	window, hop := make([]float64, f.Frame()), make([]float64, f.Hop())
	frames := (len(noisy) - f.Frame()) / f.Hop()
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		at := (i % frames) * f.Hop()
		for n := range window {
			window[n] = float64(noisy[at+n])
		}
		f.Push(window, hop)
	}
}
