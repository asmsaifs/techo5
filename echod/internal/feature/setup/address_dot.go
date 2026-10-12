//go:build dot

package setup

import "net/http"

// The Echo Dot has no Network address setting: its image has a techo5-net of its own, which joins
// Wi-Fi and knows nothing of a fixed address (lib/wifi/address_dot.go). address.go is the Show's and
// the Spot's.

func addressSection(http.ResponseWriter, string) {}

func saveAddress(http.ResponseWriter, *http.Request) (problem string, handled bool) {
	return "this device cannot set its network address", false
}
