//go:build !dot

package wifiwatch

import (
	"testing"

	esphome "github.com/ygelfand/go-esphome-device"
)

// The action's name and arguments are what automations call it by: renaming one breaks them silently.
func TestTheNetworkAddressActionKeepsItsName(t *testing.T) {
	a := networkAddressAction()
	if a.Name != "network_address" {
		t.Errorf("named %q", a.Name)
	}
	want := []esphome.Arg{
		{Name: "address", Type: esphome.ArgString},
		{Name: "gateway", Type: esphome.ArgString},
		{Name: "dns", Type: esphome.ArgString},
	}
	if len(a.Args) != len(want) {
		t.Fatalf("arguments %v", a.Args)
	}
	for i := range want {
		if a.Args[i].Name != want[i].Name || a.Args[i].Type != want[i].Type {
			t.Errorf("argument %d is %v, want %v", i, a.Args[i], want[i])
		}
	}
}
