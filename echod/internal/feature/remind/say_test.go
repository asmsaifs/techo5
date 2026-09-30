package remind

import (
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/component"
	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Home Assistant 2026.9 refuses preannounce sent as the text "false", so the label went unspoken; it
// goes as a template now, which renders to a real false (#58).
func TestSayingALabelSendsPreannounceAsATemplate(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	var got []component.Call
	stop := component.CallService.Listen(func(c component.Call) { got = append(got, c) })
	defer stop()

	Say("Take the trash out")

	if len(got) != 1 {
		t.Fatalf("%d calls, want 1", len(got))
	}
	c := got[0]
	if c.Service != "assist_satellite.announce" || c.Data["message"] != "Take the trash out" {
		t.Errorf("call %+v", c)
	}
	if _, asText := c.Data["preannounce"]; asText {
		t.Error("preannounce is still sent as text, which Home Assistant 2026.9 refuses")
	}
	if c.Templates["preannounce"] != "{{ false }}" {
		t.Errorf("preannounce template %q, want {{ false }}", c.Templates["preannounce"])
	}
}
