#!/usr/bin/env bash
# third-party-sources.sh — the source archive every release attaches for the programs its images carry that
# are built from others' sources: FFmpeg, Shairport Sync, nqptp and librespot (NOTICE). Each source is the one
# its build script names and checks by SHA-256; beside them go the patches, the license files
# (tools/linux/licenses) and the build scripts themselves.
#
#   tools/linux/third-party-sources.sh VERSION [OUT]
#
# VERSION only names the file and its README (v1.6.8); OUT defaults to
# build/techo5-third-party-sources-VERSION.tar.gz. Linux, WSL or Git Bash; needs curl.
set -euo pipefail
VERSION=${1:?usage: third-party-sources.sh VERSION [OUT]}
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${2:-$ROOT/build/techo5-third-party-sources-$VERSION.tar.gz}
L=$ROOT/tools/linux

# The versions and SHA-256s, read from the build scripts so the archive can't drift from what they build.
val() { sed -n "s/^$2=//p" "$L/$1" | head -1; }
SPS=$(val build-shairport-sync.sh SPS_VERSION) SPS_SHA=$(val build-shairport-sync.sh SPS_SHA256)
NQ=$(val build-shairport-sync.sh NQPTP_VERSION) NQ_SHA=$(val build-shairport-sync.sh NQPTP_SHA256)
FF=$(val build-ffmpeg.sh FFVER) FF_SHA=$(val build-ffmpeg.sh SHA256)
LR=$(val build-librespot.sh VERSION) LR_SHA=$(val build-librespot.sh SHA256)
[ "$FF" = "$(val build-shairport-sync.sh FF_VERSION)" ] || { echo "build-ffmpeg.sh and build-shairport-sync.sh name different FFmpegs" >&2; exit 1; }
for v in SPS SPS_SHA NQ NQ_SHA FF FF_SHA LR LR_SHA; do [ -n "${!v}" ] || { echo "no $v in the build scripts" >&2; exit 1; }; done

STAGE=$(mktemp -d)
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/sources" "$STAGE/patches" "$STAGE/build-scripts"
fetch() { # name url sha256
	echo "== $1"
	curl -fsSL -A "techo5 third-party-sources.sh" -o "$STAGE/sources/$1" "$2"
	echo "$3  $STAGE/sources/$1" | sha256sum -c --quiet - || { echo "$1: not the SHA-256 the build script names" >&2; exit 1; }
}
fetch "ffmpeg-$FF.tar.xz" "https://ffmpeg.org/releases/ffmpeg-$FF.tar.xz" "$FF_SHA"
fetch "shairport-sync-$SPS.tar.gz" "https://github.com/mikebrady/shairport-sync/archive/refs/tags/$SPS.tar.gz" "$SPS_SHA"
fetch "nqptp-$NQ.tar.gz" "https://github.com/mikebrady/nqptp/archive/refs/tags/$NQ.tar.gz" "$NQ_SHA"
fetch "librespot-$LR.crate" "https://crates.io/api/v1/crates/librespot/$LR/download" "$LR_SHA"
cp "$L/shairport-sync-stdout-delay.patch" "$L/nqptp-run-as-user.patch" "$L/librespot-volume-fifo.patch" "$STAGE/patches/"
cp "$L/build-ffmpeg.sh" "$L/build-ffmpeg-arm64.sh" "$L/build-shairport-sync.sh" "$L/build-librespot.sh" "$STAGE/build-scripts/"
cp -r "$L/licenses" "$STAGE/licenses"

cat > "$STAGE/README.txt" <<EOF
Third-party programs in TECHO5 $VERSION images, with their sources

The images carry these programs, built from the sources in sources/ by the scripts in build-scripts/
(from tools/linux/ in TECHO5), with the patches in patches/ applied. Each source archive is the one the
build script names and checks by SHA-256. Their licenses are in licenses/.

- techo5-shairport-sync (/usr/local/bin): Shairport Sync $SPS, the AirPlay receiver. MIT; see
  licenses/shairport-sync (its files name their own licenses). Built with FFmpeg $FF's libraries.
  Changed by patches/shairport-sync-stdout-delay.patch (a delay() for its stdout output).
- techo5-nqptp (/usr/local/bin): nqptp $NQ, the clock AirPlay 2 needs. GPL-2.0; see
  licenses/nqptp. Changed by patches/nqptp-run-as-user.patch (adds -u <user>), which is GPL-2.0 too.
- techo5-librespot (/usr/local/bin): librespot $LR, the Spotify Connect receiver. MIT; see
  licenses/librespot. Changed by patches/librespot-volume-fifo.patch. Its Rust dependencies are the
  ones its Cargo.lock names, fetched from crates.io by build-librespot.sh.
- techo5-ffmpeg, techo5-ffmpeg64 (/usr/local/bin) and FFmpeg's libraries in techo5-shairport-sync:
  FFmpeg $FF, built without its GPL or nonfree parts, so under the LGPL 2.1 or later; see
  licenses/ffmpeg.

sources/
  ffmpeg-$FF.tar.xz           https://ffmpeg.org/releases/ffmpeg-$FF.tar.xz
  shairport-sync-$SPS.tar.gz   https://github.com/mikebrady/shairport-sync/archive/refs/tags/$SPS.tar.gz
  nqptp-$NQ.tar.gz            https://github.com/mikebrady/nqptp/archive/refs/tags/$NQ.tar.gz
  librespot-$LR.crate         https://crates.io/api/v1/crates/librespot/$LR/download

The packages that come from Alpine Linux are Alpine's, with their sources at
https://gitlab.alpinelinux.org/alpine/aports. TECHO5's own code and the rest of its notices are in
TECHO5's repository (LICENSE, NOTICE).
EOF
# Line endings as the projects publish them, whatever the checkout did to the text files.
find "$STAGE/README.txt" "$STAGE/patches" "$STAGE/build-scripts" "$STAGE/licenses" -type f -exec perl -pi -e 's/\r$//' {} +

mkdir -p "$(dirname "$OUT")"
# --force-local: a Windows path's C: is not a remote host.
tar --force-local --sort=name --mtime=@0 --owner=0 --group=0 --numeric-owner -C "$STAGE" -cf - \
	README.txt sources patches licenses build-scripts | gzip -9n > "$OUT.part"
mv "$OUT.part" "$OUT"
echo "$(sha256sum "$OUT" | cut -d' ' -f1)  $OUT ($(wc -c < "$OUT") bytes)"
