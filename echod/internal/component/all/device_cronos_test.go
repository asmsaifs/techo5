//go:build !dot && !spot

package all

// notOnThisDevice is empty on the Echo Show 5: registered is its whole list.
var notOnThisDevice []string

// deviceSpecific is what the Show has beyond the shared list: AirPlay and Spotify Connect, which the
// Spot does not offer.
var deviceSpecific = []string{"airplay", "spotify_connect"}
