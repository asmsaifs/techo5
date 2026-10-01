#!/bin/bash
# build-librespot.sh — cross-compile librespot (Spotify Connect) for the devices: armv7, musl, static.
# Runs in WSL (or any x86_64 Linux) with rustup's armv7-unknown-linux-musleabihf target, and zig as
# the cross linker through cargo-zigbuild:
#
#   rustup target add armv7-unknown-linux-musleabihf
#   cargo install cargo-zigbuild        # and zig on PATH (ziglang.org)
#   tools/linux/build-librespot.sh [-o bin/techo5-librespot-arm]
#
# Built rather than emulated: Alpine's Rust under QEMU cannot start rustc (musl's posix_spawn uses a
# clone QEMU user emulation does not support), and a cross build is far faster anyway. The source is
# the crate as published on crates.io, built --locked to the lockfile published with it.
#
# Built with only what the device uses: the pipe backend (always there), which hands the daemon raw
# audio on stdout; avahi for being found, the same avahi AirPlay uses; and Rust's own TLS, so it needs
# no OpenSSL. The crate itself is checked against the SHA-256 crates.io's index lists for this version
# (SHA256, below); the build is --locked to the lockfile inside it, and Cargo checks each dependency
# against crates.io's checksums.
set -euo pipefail
ROOT=${TECHO5_ROOT:-$(cd "$(dirname "$0")/../.." && pwd)}
OUT=$ROOT/bin/techo5-librespot-arm
VERSION=0.8.0
# The "cksum" for this version in https://index.crates.io/li/br/librespot.
SHA256=030c5e98cc06f20283b5948ae23bdac1de0dec58dee2c5ec4c8dafc7aaa796ab
FEATURES=with-avahi,rustls-tls-webpki-roots
TARGET=armv7-unknown-linux-musleabihf
while [ $# -gt 0 ]; do
	case "$1" in
	-o) OUT=$2; shift 2;;
	*) echo "unknown argument: $1" >&2; exit 1;;
	esac
done

[ -f "$HOME/.cargo/env" ] && . "$HOME/.cargo/env"
for z in "$HOME"/zig/zig-*/; do [ -x "$z/zig" ] && PATH="$z:$PATH"; done
command -v zig >/dev/null || { echo "zig is not on PATH" >&2; exit 1; }

WORK=$HOME/librespot-build
rm -rf "$WORK"; mkdir -p "$WORK"
echo "== fetching librespot $VERSION from crates.io"
curl -sSfL -A "techo5 build-librespot.sh" -o "$WORK/librespot.crate" "https://crates.io/api/v1/crates/librespot/$VERSION/download"
echo "$SHA256  $WORK/librespot.crate" | sha256sum -c - || { echo "the crate is not the one crates.io lists for $VERSION" >&2; exit 1; }
tar -xzf "$WORK/librespot.crate" -C "$WORK"
cd "$WORK/librespot-$VERSION"
[ -f Cargo.lock ] || { echo "the published crate has no Cargo.lock to build --locked from" >&2; exit 1; }
echo "== building for $TARGET (features: $FEATURES)"
CARGO_PROFILE_RELEASE_STRIP=true CARGO_PROFILE_RELEASE_LTO=true CARGO_PROFILE_RELEASE_CODEGEN_UNITS=1 \
	cargo zigbuild --release --locked --no-default-features --features "$FEATURES" --target "$TARGET"
mkdir -p "$(dirname "$OUT")"
cp "target/$TARGET/release/librespot" "$OUT"
file "$OUT" 2>/dev/null || true
ls -la "$OUT"
echo "built: $OUT"
sha256sum "$OUT"
