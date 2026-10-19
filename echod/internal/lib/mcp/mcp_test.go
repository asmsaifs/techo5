package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fake is a server like Home Assistant's: a session id from initialize, a token it checks, and answers
// as JSON or as events.
func fake(t *testing.T, events bool) (*httptest.Server, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var m struct {
			ID     *int            `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(b, &m)
		seen = append(seen, m.Method+" session="+r.Header.Get("Mcp-Session-Id"))
		if m.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result string
		switch m.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "s-1")
			result = `{"protocolVersion":"2025-03-26","capabilities":{"tools":{}}}`
		case "tools/list":
			result = `{"tools":[{"name":"HassTurnOn","description":"Turns on","inputSchema":{"type":"object"}},` +
				`{"name":"GetLiveContext","description":"What is on","inputSchema":{"type":"object"},"annotations":{"readOnlyHint":true}},` +
				`{"name":"bad name; drop","description":"x","inputSchema":{}}]}`
		case "tools/call":
			result = `{"content":[{"type":"text","text":"Turned on the kitchen light"}],"isError":false}`
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%d,"error":{"code":-32601,"message":"no such method"}}`, *m.ID)
			return
		}
		answer := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":%s}`, *m.ID, result)
		if events {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/progress\"}\n\n")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", answer)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestASession(t *testing.T) {
	for _, events := range []bool{false, true} {
		t.Run(fmt.Sprint("events=", events), func(t *testing.T) {
			srv, seen := fake(t, events)
			c := New(srv.URL, "secret")
			ctx := context.Background()
			if err := c.Start(ctx); err != nil {
				t.Fatal(err)
			}
			tools, err := c.Tools(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(tools) != 2 || tools[0].ReadOnly() || !tools[1].ReadOnly() {
				t.Fatalf("%+v", tools)
			}
			text, failed, err := c.Call(ctx, "HassTurnOn", map[string]any{"name": "kitchen light"})
			if err != nil || failed || text != "Turned on the kitchen light" {
				t.Fatalf("%q %v %v", text, failed, err)
			}
			if want := "tools/call session=s-1"; (*seen)[len(*seen)-1] != want {
				t.Errorf("the session was not carried: %v", *seen)
			}
		})
	}
}

func TestRefusals(t *testing.T) {
	srv, _ := fake(t, false)
	if err := New(srv.URL, "wrong").Start(context.Background()); err == nil || !strings.Contains(err.Error(), "token") {
		t.Errorf("a wrong token: %v", err)
	}
	c := New(srv.URL, "secret")
	if _, _, err := c.Call(context.Background(), "bad name", nil); err == nil {
		t.Error("a call to a name that is not one")
	}
	if err := c.call(context.Background(), "nothing/here", nil, nil); err == nil {
		t.Error("an error answer taken as an answer")
	}
	long := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxAnswer+10)))
	}))
	defer long.Close()
	if err := New(long.URL, "secret").Start(context.Background()); err == nil {
		t.Error("an answer past the limit taken")
	}
}

// The token goes only to the address given: a redirect is not followed.
func TestNoRedirect(t *testing.T) {
	got := false
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization") != ""
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	if err := New(srv.URL, "secret").Start(context.Background()); err == nil {
		t.Error("a redirect taken as an answer")
	}
	if got {
		t.Fatal("the token followed a redirect")
	}
}
