//go:build !dot && !panel

package bluetooth

import (
	"path/filepath"
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// Connections are lent only over a keyed Home Assistant link: in the clear, anyone who can reach the
// device's port could read and write the Bluetooth devices near it.
func TestConnectionsNeedAKeyedLink(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "config.json"))
	if err := config.Set().Bluetooth().Proxy(true); err != nil {
		t.Fatal(err)
	}
	if err := config.Set().Bluetooth().ProxyConnections(true); err != nil {
		t.Fatal(err)
	}
	keyed := false
	was := linkKeyed
	linkKeyed = func() bool { return keyed }
	t.Cleanup(func() { linkKeyed = was })

	b := &Proxy{}
	if b.ConnectionsEnabled() {
		t.Fatal("connections lent over a link in the clear")
	}
	keyed = true
	if !b.ConnectionsEnabled() {
		t.Fatal("connections refused over a keyed link")
	}
}

// Connections saved on before the key rule read off until the link is keyed, and on once it is: the
// switch shows what is lent, while the saved setting stays on.
func TestConnectionsSwitchShowsWhatIsLent(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "config.json"))
	if err := config.Set().Bluetooth().ProxyConnections(true); err != nil {
		t.Fatal(err)
	}
	keyed := false
	was := linkKeyed
	linkKeyed = func() bool { return keyed }
	t.Cleanup(func() { linkKeyed = was })

	b := &Proxy{connect: &esphome.Switch{}}
	b.showConnections()
	if b.connect.Get() {
		t.Fatal("switch on over a link in the clear")
	}
	if !config.Get().Bluetooth.ProxyConnections {
		t.Fatal("saved setting lost")
	}
	keyed = true
	b.showConnections()
	if !b.connect.Get() {
		t.Fatal("switch still off once the link is keyed")
	}
}
