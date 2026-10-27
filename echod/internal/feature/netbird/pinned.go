//go:build !dot

package netbird

import "github.com/HuskerMinion/techo5/echod/internal/update"

// Version is the NetBird client a device downloads, and client is where from, its size and its SHA-256:
// tools/linux/build-netbird.sh's archive, the client and its license, as the release that first
// carried it published it. The same source builds the same bytes, so anyone can check it. Built into
// the daemon, so what a device runs is what its release said, wherever the file comes from.
const Version = "0.81.4"

var client = update.Binary{
	URL:    "https://github.com/HuskerMinion/techo5/releases/download/v1.6.8/netbird-arm64.tar.gz",
	SHA256: "9b9f448b5e9969765b1f30b218a64a94da1ca52207d37af720c76f5e0bf776d7",
	Size:   13993038,
}
