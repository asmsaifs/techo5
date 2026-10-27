package streaming

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AirPlay 2's chain is put in the way where the firewall says, once, and taken out again; with nothing
// said, nothing is run.
func TestAirPlay2Door(t *testing.T) {
	wasJump, wasRun := airplay2Jump, iptables
	t.Cleanup(func() { airplay2Jump, iptables = wasJump, wasRun })
	airplay2Jump = filepath.Join(t.TempDir(), "jump")
	rules := map[string]int{}
	var ran []string
	iptables = func(tool string, args ...string) error {
		ran = append(ran, tool+" "+strings.Join(args, " "))
		key := tool + " " + strings.Join(args[1:], " ")
		switch args[0] {
		case "-C":
			if rules[key] == 0 {
				return errors.New("no such rule")
			}
		case "-I":
			rules[tool+" "+args[1]+" "+strings.Join(args[3:], " ")]++
		case "-D":
			if rules[key] == 0 {
				return errors.New("no such rule")
			}
			rules[key]--
		}
		return nil
	}

	airplay2Door(true)
	if len(ran) != 0 {
		t.Fatalf("ran with no jump said: %v", ran)
	}
	os.WriteFile(airplay2Jump, []byte("INPUT 2 -i wlan0\n"), 0o644)
	airplay2Door(true)
	airplay2Door(true)
	for _, tool := range []string{"iptables-legacy", "ip6tables-legacy"} {
		if n := rules[tool+" INPUT -i wlan0 -j TECHO5-AP2"]; n != 1 {
			t.Errorf("%s: the jump is in %d times, want once (%v)", tool, n, ran)
		}
	}
	if !strings.Contains(strings.Join(ran, "\n"), "iptables-legacy -I INPUT 2 -i wlan0 -j TECHO5-AP2") {
		t.Errorf("not inserted at 2: %v", ran)
	}
	airplay2Door(false)
	for k, n := range rules {
		if n != 0 {
			t.Errorf("%s left in %d times", k, n)
		}
	}
}
