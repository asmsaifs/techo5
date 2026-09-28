package xiaozhi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// The endpoint is never worked out from the WebSocket url, and never the other way round. What is
// configured is the OTA host, and everything else comes back from the answer.
func TestOTAURL(t *testing.T) {
	for _, tc := range []struct{ name, host, want string }{
		{"unset is the official cloud", "", "https://" + OfficialHost + OTAPath},
		{"blank is the official cloud", "   ", "https://" + OfficialHost + OTAPath},
		{"a self-hosted server", "xiaozhi.example", "https://xiaozhi.example" + OTAPath},
		{"a host with a port", "10.0.0.4:8080", "https://10.0.0.4:8080" + OTAPath},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := otaURL(tc.host); got != tc.want {
				t.Errorf("otaURL(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}

// Whether a code is still to be redeemed is the one thing the OTA answer decides, and the shapes it
// arrives in are three: an object, a null, and nothing at all.
func TestActivationCode(t *testing.T) {
	// The real answer, from a live call on 2026-09-28 with the device's own values redacted.
	const live = `{"server_time":{"timestamp":1790599920799,"timezone_offset":360},
	  "firmware":{"version":"8.7.1","url":""},
	  "websocket":{"url":"wss://api.tenclass.net/xiaozhi/v1/","token":"test-token"},
	  "activation":{"code":"054672","message":"xiaozhi.me\n054672",
	                "challenge":"74ad54e9-ac7d-410d-9e52-dfcf547c3d8b"}}`

	for _, tc := range []struct {
		name   string
		body   string
		wantOK bool
		code   string
	}{
		{"a code is still to be redeemed", live, true, "054672"},
		{"null is a device that has been redeemed", `{"websocket":{"url":"wss://x/"},"activation":null}`, false, ""},
		{"absent is a server that does not activate", `{"websocket":{"url":"wss://x/"}}`, false, ""},
		{"an object with no code in it is not a code", `{"activation":{}}`, false, ""},
		// Present but unreadable is still an unactivated device. Treating it as "no code" would leave
		// the client dialling, and the server closing it with 1002 for as long as the shape is odd.
		{"present but unreadable still means activated", `{"activation":"soon"}`, true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var o ota
			if err := json.Unmarshal([]byte(tc.body), &o); err != nil {
				t.Fatalf("unmarshalling: %v", err)
			}
			act, ok := o.code()
			if ok != tc.wantOK {
				t.Fatalf("code() ok = %v, want %v", ok, tc.wantOK)
			}
			if act.Code != tc.code {
				t.Errorf("code = %q, want %q", act.Code, tc.code)
			}
		})
	}

	// The challenge is what a QR on the settings page would carry, and it is the only part of the
	// answer that identifies the redemption rather than describing it.
	var o ota
	if err := json.Unmarshal([]byte(live), &o); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if act, _ := o.code(); act.Challenge != "74ad54e9-ac7d-410d-9e52-dfcf547c3d8b" {
		t.Errorf("challenge = %q", act.Challenge)
	}
	if o.Websocket.URL != "wss://api.tenclass.net/xiaozhi/v1/" || o.Websocket.Token != "test-token" {
		t.Errorf("websocket = %+v", o.Websocket)
	}
}

// The identity travels in headers the endpoint is strict about, and the body says the same two
// things again. A MAC without colons or a client id without dashes is answered with a bare 400 that
// names neither field, so both shapes are pinned here.
func TestFetchOTASendsTheIdentityTheEndpointWillTake(t *testing.T) {
	type seen struct {
		device, client, activation, agent, method, path string
		body                                            map[string]any
		status                                          int
	}
	var got seen

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.device, got.client = r.Header.Get("Device-Id"), r.Header.Get("Client-Id")
		got.activation, got.agent = r.Header.Get("Activation-Version"), r.Header.Get("User-Agent")
		got.method = r.Method
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"websocket":{"url":"wss://example/xiaozhi/v1/","token":"t"}}`))
	}))
	defer srv.Close()

	id := identity{DeviceID: "b0:f7:c4:ff:e5:92", ClientID: "6b2f7e9c-3d1a-4c58-9f0e-2a1b3c4d5e6f"}
	info, err := fetchOTA(context.Background(), srv.URL, id)
	if err != nil {
		t.Fatalf("fetchOTA: %v", err)
	}

	if got.method != http.MethodPost {
		t.Errorf("asked %s, want POST", got.method)
	}
	if got.device != id.DeviceID || got.client != id.ClientID {
		t.Errorf("headers carry %q and %q", got.device, got.client)
	}
	if got.activation != "1" || got.agent == "" {
		t.Errorf("Activation-Version %q, User-Agent %q", got.activation, got.agent)
	}
	if got.body["mac_address"] != id.DeviceID || got.body["uuid"] != id.ClientID {
		t.Errorf("body = %v, want the same two values", got.body)
	}
	if info.Websocket.URL != "wss://example/xiaozhi/v1/" || info.Websocket.Token != "t" {
		t.Errorf("websocket = %+v", info.Websocket)
	}
}

// A 200 with nothing in it is a different problem from the endpoint being down, and the two want
// different sentences. So does a refusal.
func TestFetchOTASaysWhatWentWrong(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		want       string
	}{
		{"refused", `nope`, http.StatusBadRequest, "400"},
		{"nothing to connect to", `{"websocket":{}}`, http.StatusOK, "without a websocket url"},
		{"not the shape it should be", `<html>maintenance</html>`, http.StatusOK, "not the shape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, err := fetchOTA(context.Background(), srv.URL, identity{})
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestIdentifyGeneratesOnceAndKeeps(t *testing.T) {
	restore(t)

	// A client id that is already there is left alone, because replacing it would orphan whatever the
	// cloud has bound to the old one.
	if err := config.Set().Xiaozhi().ClientID("kept-1-2-3-4-5"); err != nil {
		t.Fatalf("seeding: %v", err)
	}
	kept, err := identify(context.Background())
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if kept.ClientID != "kept-1-2-3-4-5" {
		t.Errorf("client id = %q, want the one already stored", kept.ClientID)
	}

	if err := config.Set().Xiaozhi().ClientID(""); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	first, err := identify(context.Background())
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if !uuidV4.MatchString(first.ClientID) {
		t.Errorf("generated %q, which the endpoint would answer with a bare 400", first.ClientID)
	}
	if got := config.Get().Xiaozhi.ClientID; got != first.ClientID {
		t.Errorf("stored %q, want the one it generated %q: it has to survive a reboot", got, first.ClientID)
	}

	second, err := identify(context.Background())
	if err != nil {
		t.Fatalf("identify again: %v", err)
	}
	if second.ClientID != first.ClientID {
		t.Errorf("a second call generated %q, want the same %q", second.ClientID, first.ClientID)
	}
}

// A MAC the factory did not record is not something to invent. The endpoint would answer it with the
// same bare 400, and a made-up address is worse than a failure.
func TestIdentifyRefusesAnAddresslessDevice(t *testing.T) {
	restore(t)
	withMAC(t, func() (string, error) { return "", errors.New("no idme") })

	if _, err := identify(context.Background()); err == nil {
		t.Error("identify invented an identity on a device with no address")
	}
}

var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUIDv4IsDashed(t *testing.T) {
	seen := map[string]bool{}
	for range 32 {
		id, err := newUUIDv4()
		if err != nil {
			t.Fatalf("newUUIDv4: %v", err)
		}
		if !uuidV4.MatchString(id) {
			t.Fatalf("%q is not a dashed version 4 UUID", id)
		}
		if seen[id] {
			t.Fatalf("%q came up twice", id)
		}
		seen[id] = true
	}
}

// restore points the process-wide settings at a file of this test's own and stands in for the
// factory address, so nothing here writes to the device's real state or needs one.
func restore(t *testing.T) {
	t.Helper()
	config.Use(t.TempDir() + "/state.json")
	if err := config.Set().Xiaozhi().ClientID(""); err != nil {
		t.Fatalf("clearing: %v", err)
	}
	withMAC(t, func() (string, error) { return "b0:f7:c4:ff:e5:92", nil })
}

// withMAC stands in for the kernel interface the factory address comes from.
func withMAC(t *testing.T, f func() (string, error)) {
	t.Helper()
	old := factoryMAC
	factoryMAC = f
	t.Cleanup(func() { factoryMAC = old })
}

// configEnabled reads the switch the way a restart would: off the disk, not out of the entity.
func configEnabled(t *testing.T) bool {
	t.Helper()
	return config.Get().Xiaozhi.Enabled
}

// The host is the one setting a self-hosted server needs, and that server is a box on the same
// network with no certificate for it. So a bare name has to mean the cloud and a scheme in the
// setting has to be believed.
func TestOTAURLFollowsTheScheme(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", "https://" + OfficialHost + OTAPath},
		{"  ", "https://" + OfficialHost + OTAPath},
		{OfficialHost, "https://" + OfficialHost + OTAPath},
		{"xiaozhi.lan:8002", "https://xiaozhi.lan:8002" + OTAPath},
		{"http://xiaozhi.lan:8002", "http://xiaozhi.lan:8002" + OTAPath},
		{"http://xiaozhi.lan:8002/", "http://xiaozhi.lan:8002" + OTAPath},
	} {
		if got := otaURL(tc.in); got != tc.want {
			t.Errorf("otaURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
