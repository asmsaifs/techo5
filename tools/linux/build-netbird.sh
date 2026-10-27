#!/bin/bash
# build-netbird.sh — build NetBird's client for the devices that offer it: those that carry Tailscale (the
# Shows, the Echo Show 8 2nd gen, the Skylight and the Spot; docs/netbird.md). Linux or WSL, with Go:
#
#   tools/linux/build-netbird.sh [-o FILE]
#
# writes bin/netbird-arm64.tar.gz: the client and its license. It is not in any image: a release
# publishes it, and a device downloads it only when NetBird is turned on, checked against the size and
# SHA-256 feature/netbird is built with (pinned.go). A new build means new values there.
#
# arm64 and static (CGO off), as tailscaled is, so the one build runs on every one of them. Built from
# NetBird's own release source, checked against the SHA-256 below, as NetBird's release builds it
# (.goreleaser.yaml): the client is one program, daemon and command line both. Only client/ and what it
# uses is built, all BSD-3-Clause; the AGPL parts of NetBird's source (management, signal, relay) are
# servers, and the build fails if the client ever comes to use them.
#
# The archive is the same bytes from the same source: sorted, owned by root, dated 1970, gzip without a
# name or time.
#
# Go: any; the one it builds with (GOTOOLCHAIN below) is fetched, checked by Go's checksum database.
#
# Environment:
#   WORK  scratch (default ~/netbird-build)
set -euo pipefail
ROOT=${TECHO5_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}
WORK=${WORK:-$HOME/netbird-build}
OUT=$ROOT/bin/netbird-arm64.tar.gz
NB_VERSION=0.81.4
NB_SHA256=c735ffec7dece473e61c8764fec7a5cb971bacc766620e9639c97c9a249bbcbb
while [ $# -gt 0 ]; do
	case "$1" in
	-o) OUT=$2; shift 2;;
	*) echo "unknown argument: $1" >&2; exit 1;;
	esac
done
command -v go >/dev/null || { echo "go is not on PATH" >&2; exit 1; }

mkdir -p "$WORK"
TGZ=$WORK/netbird-$NB_VERSION.tar.gz
[ -f "$TGZ" ] || { curl -fsSL -A "techo5 build-netbird.sh" -o "$TGZ.part" \
	"https://github.com/netbirdio/netbird/archive/refs/tags/v$NB_VERSION.tar.gz" && mv "$TGZ.part" "$TGZ"; }
echo "$NB_SHA256  $TGZ" | sha256sum -c - || { echo "the NetBird source is not the one this script was written for" >&2; exit 1; }
SRC=$WORK/netbird-$NB_VERSION
rm -rf "$SRC"; mkdir -p "$SRC"
tar -xzf "$TGZ" -C "$SRC" --strip-components=1

cd "$SRC/client"
# One Go, so that the same source is the same bytes on any machine (the size and SHA-256 are pinned).
export GOTOOLCHAIN=go1.26.8 CGO_ENABLED=0 GOOS=linux GOARCH=arm64 GOFLAGS=-mod=mod
agpl=$(go list -deps . | grep -E '^github.com/netbirdio/netbird/(management|signal|relay|combined)(/|$)' || true)
[ -z "$agpl" ] || { echo "the client uses NetBird's AGPL parts:" >&2; echo "$agpl" >&2; exit 1; }

echo "== netbird $NB_VERSION, arm64"
STAGE=$WORK/stage
rm -rf "$STAGE"; mkdir -p "$STAGE"
go build -trimpath -buildvcs=false \
	-ldflags "-s -w -X github.com/netbirdio/netbird/version.version=$NB_VERSION" \
	-o "$STAGE/netbird" .
cp "$SRC/LICENSE" "$STAGE/LICENSE"
chmod 0755 "$STAGE/netbird"; chmod 0644 "$STAGE/LICENSE"
mkdir -p "$(dirname "$OUT")"
tar --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C "$STAGE" -cf - netbird LICENSE | gzip -9n > "$OUT.part"
mv "$OUT.part" "$OUT"
echo "built: $OUT $(stat -c %s "$OUT") bytes, $(sha256sum "$OUT" | cut -d' ' -f1)"
echo "netbird itself: $(stat -c %s "$STAGE/netbird") bytes"
