package setup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func saveRT(v url.Values) string {
	r := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(v.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return saves["realtime"](r)
}

func TestRealtimeForm(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if p := saveRT(url.Values{"provider": {"openai"}, "key": {"sk-secret"}, "mcp": {"ha-secret"}, "minutes": {"20"},
		"slot0": {"yes"}}); p != "" {
		t.Fatal(p)
	}
	rt := config.Get().Realtime
	if rt.Key != "sk-secret" || rt.MCPToken != "ha-secret" || rt.Daily() != 20 || !config.Get().Wake.Words[0].Realtime {
		t.Fatalf("%+v", rt)
	}
	// Saved again as shown, with the key and token empty: both kept; the slot unticked.
	if p := saveRT(url.Values{"provider": {"openai"}, "minutes": {"20"}}); p != "" {
		t.Fatal(p)
	}
	if rt := config.Get().Realtime; rt.Key != "sk-secret" || rt.MCPToken != "ha-secret" || config.Get().Wake.Words[0].Realtime {
		t.Fatalf("%+v", rt)
	}

	w := httptest.NewRecorder()
	realtimeSection(w, "token")
	body := w.Body.String()
	if strings.Contains(body, "sk-secret") || strings.Contains(body, "ha-secret") {
		t.Fatal("the page shows the key or the token")
	}
	if !strings.Contains(body, "sends what you say, as audio, to\n\t OpenAI") && !strings.Contains(body, "OpenAI, while you speak") {
		t.Error("the page does not say where the voice goes")
	}
	if !strings.Contains(body, "They bill for it") {
		t.Error("the page does not say it is billed")
	}

	for name, v := range map[string]url.Values{
		"a provider that is not one": {"provider": {"someone"}, "minutes": {"20"}},
		"an http relay":              {"provider": {"openai"}, "endpoint": {"http://relay"}, "minutes": {"20"}},
		"a ws:// relay":              {"provider": {"openai"}, "endpoint": {"ws://relay"}, "minutes": {"20"}},
		"a relay with a login":       {"provider": {"openai"}, "endpoint": {"wss://u:p@relay"}, "minutes": {"20"}},
		"no minutes":                 {"provider": {"openai"}, "minutes": {"0"}},
		"too many minutes":           {"provider": {"openai"}, "minutes": {"601"}},
		"a key of two lines":         {"provider": {"openai"}, "minutes": {"20"}, "key": {"a\nb"}},
	} {
		if saveRT(v) == "" {
			t.Errorf("%s was taken", name)
		}
	}
	if config.Get().Realtime.Key != "sk-secret" {
		t.Error("a refused form changed the key")
	}

	// Another provider: the key was for the one before, and goes.
	_ = saveRT(url.Values{"provider": {"gemini"}, "minutes": {"20"}})
	if config.Get().Realtime.Key != "" {
		t.Error("the key went on to another provider")
	}
}
