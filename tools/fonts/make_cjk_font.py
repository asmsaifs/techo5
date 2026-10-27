#!/usr/bin/env python3
"""Makes the Japanese font a device downloads when its screen language is Japanese (#270): one file,
techo5-noto-sans-jp.tar.gz, with NotoSansJP-Regular.ttf and its license, OFL.txt.

The fonts the screen offers draw Latin only (make_fonts.py); a character none of them has is drawn
from the device's own fonts folder (echod/internal/lib/fonts, own.go). For Japanese, the daemon
fetches this archive into that folder (feature/cjkfont), so nothing has to be put there by hand and
no image carries it. It is Noto Sans JP from google/fonts (SIL Open Font License 1.1), at the commit
and SHA-256 below, cut down to:

  - one static weight, 400: the daemon's font package does not apply a variable font's axes;
  - kana, Japanese punctuation and symbols, full-width forms, and the JIS X 0208 level 1 kanji, the
    2,965 in everyday use, which hold the joyo kanji but a handful: about a tenth of the whole font;
  - no layout features but 'kern', no hinting, as make_fonts.py does.

The same fontTools, the same source and this script give the same bytes, and the archive is written
the same way every time, so the size and SHA-256 feature/cjkfont pins can be checked by anyone:

    pip install fonttools==4.65.0
    python3 tools/fonts/make_cjk_font.py [out.tar.gz]

Fetches its source from GitHub; writes bin/techo5-noto-sans-jp.tar.gz by default.
"""
import gzip
import hashlib
import io
import os
import sys
import tarfile
import urllib.request

import fontTools
from fontTools import subset
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer

FONTTOOLS = "4.65.0"
COMMIT = "e7db6ca603fd4862010383ff17ffd6cc4ffac1c0"
BASE = "https://raw.githubusercontent.com/google/fonts/" + COMMIT + "/ofl/notosansjp/"
SOURCES = {
    "NotoSansJP%5Bwght%5D.ttf": "c2f3b4d463500a2ddcd3849cded1fceeb9fd6d1c32e6cbecd568453ba50fc68f",
    "OFL.txt": "1c05c68c34f9708415aada51f17e1b0092d2cea709bf4a94cd38114f9e73d7d9",
}
ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..")

# A fixed time for the font's head table and the archive's entries: 2026-10-26 00:00 UTC.
EPOCH = 1792972800
# The font's head table counts from 1904.
MAC_EPOCH = EPOCH + 2082844800


def fetch(name):
    with urllib.request.urlopen(BASE + name) as r:
        data = r.read()
    if hashlib.sha256(data).hexdigest() != SOURCES[name]:
        sys.exit("%s is not the file this script was written for" % name)
    return data


def jis_level1():
    """The JIS X 0208 level 1 kanji, by Shift JIS's rows 0x889F to 0x9872."""
    out = []
    for lead in range(0x88, 0x99):
        for trail in list(range(0x40, 0x7F)) + list(range(0x80, 0xFD)):
            code = lead << 8 | trail
            if code < 0x889F or code > 0x9872:
                continue
            try:
                out.append(ord(bytes([lead, trail]).decode("shift_jis")))
            except UnicodeDecodeError:
                pass
    return out


def unicodes():
    u = set(jis_level1())
    for lo, hi in [
        (0x3000, 0x303F),  # Japanese punctuation
        (0x3040, 0x309F),  # hiragana
        (0x30A0, 0x30FF),  # katakana
        (0x31F0, 0x31FF),  # katakana extensions
        (0xFF00, 0xFFEF),  # full-width and half-width forms
        (0x2010, 0x2027),  # dashes, quotes, ellipsis as Japanese text sets them
        (0x3200, 0x32FF),  # enclosed letters and months
    ]:
        u.update(range(lo, hi + 1))
    u.update(ord(c) for c in "々〆〇ヶ")
    return sorted(u)


def main():
    if fontTools.version != FONTTOOLS:
        sys.exit("fontTools %s, not %s: the bytes would differ (pip install fonttools==%s)"
                 % (fontTools.version, FONTTOOLS, FONTTOOLS))
    out = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "bin", "techo5-noto-sans-jp.tar.gz")
    font = TTFont(io.BytesIO(fetch("NotoSansJP%5Bwght%5D.ttf")), recalcTimestamp=False)
    licence = fetch("OFL.txt")

    static = instancer.instantiateVariableFont(font, {"wght": 400}, updateFontNames=True)
    opts = subset.Options()
    opts.layout_features = ["kern"]
    opts.hinting = False
    opts.notdef_outline = True
    opts.name_IDs = ["*"]
    opts.name_languages = ["*"]
    sub = subset.Subsetter(opts)
    sub.populate(unicodes=unicodes())
    sub.subset(static)
    static["head"].created = MAC_EPOCH
    static["head"].modified = MAC_EPOCH
    static.recalcTimestamp = False
    ttf = io.BytesIO()
    static.save(ttf)
    ttf = ttf.getvalue()

    buf = io.BytesIO()
    with tarfile.open(fileobj=buf, mode="w", format=tarfile.USTAR_FORMAT) as tar:
        for name, data, mode in [("NotoSansJP-Regular.ttf", ttf, 0o644), ("OFL.txt", licence, 0o644)]:
            info = tarfile.TarInfo(name)
            info.size, info.mode, info.mtime = len(data), mode, EPOCH
            info.uid = info.gid = 0
            info.uname = info.gname = ""
            tar.addfile(info, io.BytesIO(data))
    os.makedirs(os.path.dirname(os.path.abspath(out)), exist_ok=True)
    with open(out, "wb") as f:
        with gzip.GzipFile(filename="", mode="wb", fileobj=f, mtime=0, compresslevel=9) as gz:
            gz.write(buf.getvalue())
    data = open(out, "rb").read()
    print("font: %d bytes, %d characters" % (len(ttf), len(unicodes())))
    print("built: %s %d bytes, %s" % (out, len(data), hashlib.sha256(data).hexdigest()))


if __name__ == "__main__":
    main()
