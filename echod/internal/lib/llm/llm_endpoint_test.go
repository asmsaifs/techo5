package llm

import "testing"

func TestEndpointAddsV1ToABareServer(t *testing.T) {
	for base, want := range map[string]string{
		"http://192.168.1.20:8080":       "http://192.168.1.20:8080/v1/chat/completions",
		"http://192.168.1.20:8080/":      "http://192.168.1.20:8080/v1/chat/completions",
		"http://192.168.1.20:8080/v1":    "http://192.168.1.20:8080/v1/chat/completions",
		"https://example.com/openai/v1/": "https://example.com/openai/v1/chat/completions",
	} {
		if got := endpoint(base); got != want {
			t.Errorf("endpoint(%q) = %q, want %q", base, got, want)
		}
	}
}
