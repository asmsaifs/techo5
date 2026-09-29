package xiaozhi

// The shape of a status on the wire.
//
// The CLI mirrors these types by hand rather than importing them, so the two sides can drift apart
// without anything noticing. They did: the CLI carried viper's dotted keys ("stats.sent") where this
// side nests, so a real session reported "sent 0 packets" after a turn that had demonstrably sent
// 45. Nothing on this side changed, no test failed, and the only place the disagreement was visible
// was a status line on a device.
//
// So the shape is pinned here, from the side that can be tested. A status with real counters has to
// marshal them under a nested "stats" object, because that is what the hand-written mirror decodes.

import (
	"encoding/json"
	"testing"
)

func TestStatusMarshalsStatsNested(t *testing.T) {
	b, err := json.Marshal(Status{State: StateConnected, Stats: Stats{
		Messages: 7, Text: 2, Audio: 1, Bytes: 640, Sent: 45, SentBytes: 8100, Pings: 3,
	}})
	if err != nil {
		t.Fatalf("marshalling a status: %v", err)
	}

	var got struct {
		Stats *struct {
			Sent      *int `json:"sent"`
			SentBytes *int `json:"sent_bytes"`
			Messages  *int `json:"messages"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshalling a status: %v", err)
	}

	if got.Stats == nil {
		t.Fatalf("no nested stats object in %s; the counters would not survive the round trip", b)
	}
	if got.Stats.Sent == nil || *got.Stats.Sent != 45 {
		t.Errorf("stats.sent: got %v, want 45; %s", got.Stats.Sent, b)
	}
	if got.Stats.SentBytes == nil || *got.Stats.SentBytes != 8100 {
		t.Errorf("stats.sent_bytes: got %v, want 8100; %s", got.Stats.SentBytes, b)
	}
	if got.Stats.Messages == nil || *got.Stats.Messages != 7 {
		t.Errorf("stats.messages: got %v, want 7; %s", got.Stats.Messages, b)
	}
}
