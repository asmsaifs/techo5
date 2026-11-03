package voice

import (
	"context"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"
	"github.com/ygelfand/go-esphome-device/api"
)

func subscribe(t *testing.T, v *Voice, c *esphome.Conn) {
	t.Helper()
	if err := v.Handle(context.Background(), c, &api.SubscribeVoiceAssistantRequest{Subscribe: true}); err != nil {
		t.Fatal(err)
	}
}

// Home Assistant subscribes once per connection. When one closes, the next one's subscription has to
// take voice, or the device stays deaf until it restarts.
func TestAClosedConnectionLetsTheNextOneSubscribe(t *testing.T) {
	vs := &esphome.VoiceSatellite{}
	v := &Voice{vs: vs, turn: newConversation(vs)}
	first, second := &esphome.Conn{}, &esphome.Conn{}

	subscribe(t, v, first)
	v.ConnClosed(first)
	if v.HomeAssistant() || v.turn.conn.Load() != nil {
		t.Fatal("a closed connection still holds voice")
	}
	subscribe(t, v, second)
	if !v.HomeAssistant() || v.turn.conn.Load() != second {
		t.Fatal("the new connection's subscription did not take voice")
	}
}

// An old connection not yet seen to close gives way to a new subscription, and its closing later
// leaves the new one alone.
func TestANewSubscriptionTakesOverFromAnOldConnection(t *testing.T) {
	vs := &esphome.VoiceSatellite{}
	v := &Voice{vs: vs, turn: newConversation(vs)}
	old, fresh := &esphome.Conn{}, &esphome.Conn{}

	subscribe(t, v, old)
	subscribe(t, v, fresh)
	if v.turn.conn.Load() != fresh {
		t.Fatal("the turn kept the old connection")
	}
	v.ConnClosed(old)
	if !v.HomeAssistant() || v.turn.conn.Load() != fresh {
		t.Fatal("the old connection closing took voice from the new one")
	}
	v.ConnClosed(fresh)
	if v.HomeAssistant() {
		t.Fatal("voice still held after the new connection closed")
	}
}
