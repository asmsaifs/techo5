package voice

import "testing"

// A feature that has the microphones holds turns off for exactly as long as it says it has them.
func TestYieldTo(t *testing.T) {
	was := yield.Load()
	defer yield.Store(was)
	taken := true
	YieldTo(func() bool { return taken }, func() {})
	if !micTaken() {
		t.Error("the microphones were not taken")
	}
	taken = false
	if micTaken() {
		t.Error("the microphones were still taken")
	}
}
