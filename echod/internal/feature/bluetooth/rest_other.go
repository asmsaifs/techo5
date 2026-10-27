//go:build !spot

package bluetooth

// restWhilePlaying is the Spot's (rest_spot.go): elsewhere the proxy scans on while the speaker plays.
func (b *Proxy) restWhilePlaying() {}
