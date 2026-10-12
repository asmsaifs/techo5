//go:build !dot

package wifiwatch

import (
	"context"
	"log/slog"
	"time"

	esphome "github.com/ygelfand/go-esphome-device"

	"github.com/HuskerMinion/techo5/echod/internal/lib/safe"
	"github.com/HuskerMinion/techo5/echod/internal/lib/wifi"
)

// applyAfter is how long after the action answers that the new address is put on: Home Assistant has
// its answer before the connection it came over goes away.
var applyAfter = 2 * time.Second

// Actions: network_address sets how the device gets its address (lib/wifi/address.go). An empty address
// is DHCP, and the gateway and name servers are then ignored; otherwise the address, with or without its
// prefix, the gateway, and name servers if not the gateway. A new fixed address that does not work is
// dropped by the device itself, which keeps the setting in use. Not offered where the image has no
// techo5-net that can try one, and not built for the Echo Dot at all, whose image has a techo5-net of
// its own that does something else (wifi.AddressSupported).
func (w *Watch) Actions() []*esphome.Action {
	if !wifi.Available() || !wifi.AddressSupported() {
		return nil
	}
	return []*esphome.Action{networkAddressAction()}
}

// networkAddressAction is the action itself, whatever the device has.
func networkAddressAction() *esphome.Action {
	return &esphome.Action{
		Name: "network_address",
		Args: []esphome.Arg{
			{Name: "address", Type: esphome.ArgString},
			{Name: "gateway", Type: esphome.ArgString},
			{Name: "dns", Type: esphome.ArgString},
		},
		Run: func(c esphome.Call) (any, error) {
			a, err := wifi.ParseAddress(c.String("address"), c.String("gateway"), c.String("dns"))
			if err != nil {
				return nil, err
			}
			if wifi.AddressUnchanged(context.Background(), a) {
				return nil, nil // the setting in use, and in place: nothing to try
			}
			slog.Info("network address set from Home Assistant", "address", a.String())
			safe.Go("network address", func() {
				time.Sleep(applyAfter)
				changeAddress(a)
			})
			return nil, nil
		},
	}
}

// changeAddress tries a setting from the action and says in the log how it went: the action has
// answered long before, so a setting tried and not kept would otherwise go unnoticed.
func changeAddress(a wifi.Address) {
	if _, err := wifi.ChangeAddress(context.Background(), a); err != nil {
		slog.Warn("the network address could not be applied", "err", err)
		return
	}
	if why := wifi.AddressFallback(); why != "" {
		slog.Warn("the network address from Home Assistant was not kept", "address", a.String(), "reason", why)
	}
}
