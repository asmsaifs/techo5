#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
"""Make the athena boot image's ramdisk: a gzip'd newc cpio holding only firmware files.

    mkfwcpio.py FWDIR OUT.cpio.gz FILE...

Each FILE (a path relative to FWDIR, e.g. mediatek/mt7663pr2h.bin) goes in at lib/firmware/FILE,
with its parent directories. There is no /init: the kernel finds none in the initramfs, goes on to
mount the real root from root= on the command line and runs init= there, as without a ramdisk;
until then the firmware loader finds these files in the initramfs.

The output depends only on the files: entries sorted, owner 0:0, mode 0755/0644, mtime 0, inode
numbers in order, gzip header without name or time (the compressed bytes can still differ between
zlib versions).
"""
import gzip
import os
import sys

FW_ROOT = "lib/firmware"


def newc_entry(ino, name, mode, data=b"", nlink=1):
    name_b = name.encode() + b"\0"
    hdr = "070701" + "".join("%08X" % v for v in (
        ino, mode, 0, 0, nlink, 0, len(data), 0, 0, 0, 0, len(name_b), 0))
    out = hdr.encode() + name_b
    out += b"\0" * (-len(out) % 4)
    out += data
    out += b"\0" * (-len(out) % 4)
    return out


def make_cpio(fwdir, files):
    entries = {}
    for rel in files:
        path = FW_ROOT + "/" + rel
        parts = path.split("/")
        for i in range(1, len(parts)):
            entries.setdefault("/".join(parts[:i]), None)
        with open(os.path.join(fwdir, rel), "rb") as f:
            entries[path] = f.read()
    out = b""
    for ino, name in enumerate(sorted(entries), start=1):
        data = entries[name]
        if data is None:
            out += newc_entry(ino, name, 0o040755, nlink=2)
        else:
            out += newc_entry(ino, name, 0o100644, data)
    out += newc_entry(0, "TRAILER!!!", 0, nlink=1)
    return out


def main(argv):
    if len(argv) < 4:
        print("usage: mkfwcpio.py FWDIR OUT.cpio.gz FILE...", file=sys.stderr)
        return 2
    cpio = make_cpio(argv[1], argv[3:])
    with open(argv[2], "wb") as f:
        f.write(gzip.compress(cpio, compresslevel=9, mtime=0))
    print("ramdisk: %d files, cpio %d bytes" % (len(argv) - 3, len(cpio)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
