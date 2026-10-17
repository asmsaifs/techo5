#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline mt7663s: load the MT7663 firmware once at init and hold it for the life of the
kernel, so that later requests by the same name are served from memory.

athena's boot image carries the MT7663 firmware as separate files in a small initramfs (the boot
image's ramdisk, lib/firmware/mediatek/...; MediaTek's license does not allow it to be linked into
the kernel). The initramfs has no /init, so the kernel goes on to mount the real root (root= from
LK's command line) once its initcalls are done, and from then on the firmware loader looks in the
real root's /lib/firmware, which has none. Two requests come too late for the initramfs:

  - mt7663s loads its firmware from a work item queued at probe (mt7663s_init_work), which can run
    after the root switch;
  - btmtksdio registers 8 s after boot (patch-btmtksdio-late-register.py) and asks for
    mediatek/mt7663pr2h.bin then.

The firmware loader shares one loaded copy between all requests for the same name while any
holder keeps it ("batched requests", alloc_lookup_fw_priv). So mt7663s's init, at device_initcall
time while the initramfs is still the root (the loader waits for it to be unpacked), requests all
four MT7663 files and never releases them: every later request_firmware() for them, from Wi-Fi or
Bluetooth, finds them held. About 1.2 MB of memory stays allocated.

Usage: patch-mt7663s-firmware-hold.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: hold firmware"
p = tree + "/drivers/net/wireless/mediatek/mt76/mt7615/sdio.c"
s = open(p).read()
if MARK in s:
    print(p + ": already patched")
    sys.exit(0)

pairs = [
    ("#include <linux/kernel.h>\n",
     "#include <linux/firmware.h>\n#include <linux/kernel.h>\n"),
    ("module_sdio_driver(mt7663s_driver);\n",
     """/* athena: hold firmware. The firmware comes from the initramfs, which the
 * real root replaces after the initcalls. Load it here, while it is still
 * there, and keep it: later requests by name (the init work below, btmtksdio)
 * share the held copy instead of looking on the real root.
 */
static const char * const mt7663s_held_fw_names[] = {
	MT7663_OFFLOAD_ROM_PATCH,
	MT7663_OFFLOAD_FIRMWARE_N9,
	MT7663_ROM_PATCH,
	MT7663_FIRMWARE_N9,
};

static const struct firmware *mt7663s_held_fw[ARRAY_SIZE(mt7663s_held_fw_names)];

static int __init mt7663s_init(void)
{
	int i, held = 0;

	for (i = 0; i < ARRAY_SIZE(mt7663s_held_fw_names); i++) {
		if (!firmware_request_nowarn(&mt7663s_held_fw[i],
					     mt7663s_held_fw_names[i], NULL))
			held++;
		else
			mt7663s_held_fw[i] = NULL;
	}
	pr_info("mt7663s: holding %d of %zu firmware files\\n", held,
		ARRAY_SIZE(mt7663s_held_fw_names));

	return sdio_register_driver(&mt7663s_driver);
}
module_init(mt7663s_init);

static void __exit mt7663s_exit(void)
{
	int i;

	sdio_unregister_driver(&mt7663s_driver);
	for (i = 0; i < ARRAY_SIZE(mt7663s_held_fw); i++)
		release_firmware(mt7663s_held_fw[i]);
}
module_exit(mt7663s_exit);
"""),
]
for a, b in pairs:
    assert s.count(a) == 1, (a, s.count(a))
    s = s.replace(a, b)
open(p, "w").write(s)
print(p + ": patched")
