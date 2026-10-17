#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline btmtksdio: register the driver 8 s after boot, so Wi-Fi starts the MT7663 alone.

athena's MT7663 is one chip with Wi-Fi (mt7663s, SDIO function 1) and Bluetooth (btmtksdio,
function 2). Both drivers are built in and probe together, and the kernel powers hci0 up the moment
it registers, so Bluetooth enables its function on the chip while mt7663s is starting the N9
firmware. On about one boot in three (unit 1, 2026-10-15) the chip then answered neither driver:
"mt7663s ...: Message 00000002 (seq 10) timeout", "Failed to start N9 firmware", and "Bluetooth:
hci0: Failed to send wmt func ctrl (-110)": no Wi-Fi and no Bluetooth until the next boot. On the
good boots Wi-Fi's firmware is up by about 2 s.

A first try slept 8 s inside the driver's open (patch-btmtksdio-wait-wifi.py, now dropped): Wi-Fi came
up on 10 boots out of 10, but hci0 then never appeared in mgmt's index list. Registering the driver
late keeps every Bluetooth step exactly as upstream has it, only later: nothing touches function 2
until 8 s after boot. TECHO5 brings Bluetooth up at ~10 s anyway (t5_bt_native waits for hci0).

Expects an unpatched or bdaddr/p192-patched btmtksdio.c (the build checks the file out first).

Usage: patch-btmtksdio-late-register.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: register late"
p = tree + "/drivers/bluetooth/btmtksdio.c"
s = open(p).read()
if MARK in s:
    print(p + ": already patched")
    sys.exit(0)
assert "athena: wait for Wi-Fi" not in s, "check the file out first: the open-sleep patch is in it"

a = "module_sdio_driver(btmtksdio_driver);\n"
assert s.count(a) == 1, a
s = s.replace(a, """/* athena: register late. Wi-Fi (mt7663s) starts the shared MT7663 in the first seconds of boot;
 * Bluetooth enabling its function at the same time left the chip answering neither driver on some
 * boots. The driver registers this long after boot, so it binds and sets hci0 up after Wi-Fi.
 */
#define BTMTKSDIO_REGISTER_DELAY_S	8

static void btmtksdio_register_work(struct work_struct *work)
{
	int err = sdio_register_driver(&btmtksdio_driver);

	if (err)
		pr_err("btmtksdio: registering the driver failed: %d\\n", err);
}

static DECLARE_DELAYED_WORK(btmtksdio_reg_work, btmtksdio_register_work);

static int __init btmtksdio_init(void)
{
	pr_info("btmtksdio: registering in %ds, after Wi-Fi has started the chip\\n",
		BTMTKSDIO_REGISTER_DELAY_S);
	schedule_delayed_work(&btmtksdio_reg_work, BTMTKSDIO_REGISTER_DELAY_S * HZ);
	return 0;
}
module_init(btmtksdio_init);

static void __exit btmtksdio_exit(void)
{
	cancel_delayed_work_sync(&btmtksdio_reg_work);
	sdio_unregister_driver(&btmtksdio_driver);
}
module_exit(btmtksdio_exit);
""")
if "#include <linux/workqueue.h>" not in s:
    s = s.replace("#include <linux/gpio/consumer.h>\n", "#include <linux/gpio/consumer.h>\n#include <linux/workqueue.h>\n", 1)
open(p, "w").write(s)
print(p + ": patched")
