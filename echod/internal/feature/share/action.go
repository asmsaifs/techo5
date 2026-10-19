package share

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/feature/announce"
	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
)

// copyAction is Home Assistant's copy_settings: settings, by their object IDs, comma separated, or "all";
// devices, by name, comma separated, or "all" for every other device on the network now. Each device takes
// what it takes: one that has sharing off, keeps a setting its own or has no such setting says so, and the
// log has what each made of it.
var copyAction = &esphome.Action{
	Name: "copy_settings",
	Args: []esphome.Arg{{Name: "settings", Type: esphome.ArgString}, {Name: "devices", Type: esphome.ArgString}},
	Run: func(c esphome.Call) (any, error) {
		if !config.Get().Share.On {
			return nil, errors.New("settings sharing is off on this device")
		}
		ids := pickIDs(c.String("settings"))
		devices := pickDevices(c.String("devices"))
		if len(ids) == 0 || len(devices) == 0 {
			return nil, errors.New("name the settings and the devices, or say all")
		}
		safe.Go("copy settings", func() {
			for d, r := range Get().Copy(context.Background(), ids, devices) {
				slog.Info("share: copy", "to", d, "results", r)
			}
		})
		return nil, nil
	},
}

func (f *Feature) Actions() []*esphome.Action { return []*esphome.Action{copyAction} }

// pickIDs is the settings named, those that can be shared, or every one for "all".
func pickIDs(s string) []string {
	if strings.EqualFold(strings.TrimSpace(s), "all") {
		return IDs()
	}
	var out []string
	for _, id := range strings.Split(s, ",") {
		if id = strings.TrimSpace(id); Shareable(id) {
			out = append(out, id)
		}
	}
	return out
}

// pickDevices is the devices named, or every other device on the network now for "all".
func pickDevices(s string) []string {
	if strings.EqualFold(strings.TrimSpace(s), "all") {
		var out []string
		for _, p := range announce.Peers() {
			out = append(out, p.Name)
		}
		return out
	}
	var out []string
	for _, d := range strings.Split(s, ",") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}
