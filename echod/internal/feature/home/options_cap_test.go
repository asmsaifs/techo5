package home

import (
	"fmt"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// A Home Assistant with thousands of media players or weather entities gets a list the select can
// send: at most optionsMax options and optionBytes in all, with the one chosen always among them.
func TestOptionsAreCapped(t *testing.T) {
	var many []string
	for i := range 3000 {
		many = append(many, fmt.Sprintf("media_player.%s_%d", strings.Repeat("p", 30), i))
	}
	chosen := "media_player.the_one_chosen"
	for name, opts := range map[string][]string{
		"follow":  followOptions(config.Home{PlayerSources: many, FollowPlayer: chosen}),
		"weather": weatherOptions(config.Home{WeatherSources: append([]string{"weather.x"}, many...)}),
	} {
		size := 0
		for _, o := range opts {
			size += len(o)
		}
		if len(opts) > optionsMax || size > optionBytes {
			t.Errorf("%s: %d options, %d bytes", name, len(opts), size)
		}
		if len(opts) < 10 {
			t.Errorf("%s: only %d options kept", name, len(opts))
		}
	}
	if opts := followOptions(config.Home{PlayerSources: many, FollowPlayer: chosen}); opts[1] != chosen {
		t.Errorf("the chosen player is not kept: %q", opts[:2])
	}
	if opts := followOptions(config.Home{PlayerSources: []string{"media_player.a", "media_player.b"}}); len(opts) != 3 {
		t.Errorf("a short list was cut: %q", opts)
	}
}
