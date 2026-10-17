#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""The TECHO5 amp tuning slot: the `techo5,dsp-config` property of the board DTS.

Exactly SLOT_SIZE (8192) bytes:
    0..7     magic, ASCII "T5DSPCF1"
    8..11    N, u32 little-endian: length of the data (0 = empty)
    12..43   SHA-256 of the data (32 zero bytes when empty)
    44..     N bytes of data, then zeros to the end

The data is a mainline tas5805m DSP blob (register, value byte pairs, as tas5805m_dsp_*.bin).
The published boot image carries the slot empty; an installer fills it in place (same size) and
then fixes the boot image header's SHA-1 id. patches/patch-tas5805m-dsp-slot.py reads it at probe.

    dsp_slot.py empty OUT.bin     write an empty slot (build.sh does this for the DTS /incbin/)
    dsp_slot.py show FILE         find the slot in FILE (a boot image, DTB or slot) and describe it
"""
import hashlib
import struct
import sys

MAGIC = b"T5DSP" + b"CF1"
SLOT_SIZE = 8192
HDR_SIZE = 44
MAX_DATA = SLOT_SIZE - HDR_SIZE


def make_slot(data=b""):
    """Return the 8192-byte slot holding data (empty by default)."""
    if len(data) > MAX_DATA or len(data) % 2:
        raise ValueError("data must be an even number of bytes, at most %d" % MAX_DATA)
    digest = hashlib.sha256(data).digest() if data else bytes(32)
    slot = MAGIC + struct.pack("<I", len(data)) + digest + data
    return slot.ljust(SLOT_SIZE, b"\0")


def parse_slot(slot):
    """Check a slot and return its data (b"" when empty). Raises ValueError when it is invalid."""
    if len(slot) != SLOT_SIZE:
        raise ValueError("slot is %d bytes, not %d" % (len(slot), SLOT_SIZE))
    if slot[:8] != MAGIC:
        raise ValueError("bad magic")
    n = struct.unpack_from("<I", slot, 8)[0]
    digest = slot[12:HDR_SIZE]
    if n > MAX_DATA or n % 2:
        raise ValueError("bad data length %d" % n)
    data = slot[HDR_SIZE:HDR_SIZE + n]
    if any(slot[HDR_SIZE + n:]):
        raise ValueError("padding after the data is not zero")
    if n == 0:
        if any(digest):
            raise ValueError("empty slot with a non-zero hash")
    elif hashlib.sha256(data).digest() != digest:
        raise ValueError("SHA-256 mismatch")
    return data


def find_slot(blob):
    """Return (offset, slot bytes) of the only slot in blob. Raises ValueError otherwise."""
    count = blob.count(MAGIC)
    if count != 1:
        raise ValueError("magic found %d times, expected exactly once" % count)
    off = blob.index(MAGIC)
    if off + SLOT_SIZE > len(blob):
        raise ValueError("slot runs past the end of the file")
    return off, blob[off:off + SLOT_SIZE]


def main(argv):
    if len(argv) == 3 and argv[1] == "empty":
        with open(argv[2], "wb") as f:
            f.write(make_slot())
        return 0
    if len(argv) == 3 and argv[1] == "show":
        with open(argv[2], "rb") as f:
            blob = f.read()
        off, slot = find_slot(blob)
        data = parse_slot(slot)
        print("slot at offset %d: N=%d%s" % (off, len(data), " (empty)" if not data else
                                              ", sha256 " + hashlib.sha256(data).hexdigest()))
        return 0
    print("usage: dsp_slot.py empty OUT.bin | dsp_slot.py show FILE", file=sys.stderr)
    return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
