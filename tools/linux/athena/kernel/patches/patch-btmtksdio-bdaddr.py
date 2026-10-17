#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline btmtksdio so a public address set through mgmt survives power cycles.

athena's MT7663 Bluetooth comes up on whatever address its firmware or eFuse holds; TECHO5 gives it
the factory one from idme with `btmgmt public-addr` (techo5-lib.sh t5_bt_up, mainline path). The
kernel programs that address (vendor command 0xFC1A, btmtk_set_bdaddr) only once, on the power-on
that follows the mgmt command. btmtksdio sets HCI_QUIRK_NON_PERSISTENT_SETUP and switches the
Bluetooth function off in its shutdown, so a later power-off/power-on (bluetoothd, btmgmt) runs the
setup again on a controller that may have dropped the address. This programs it again at the end
of every setup, whenever one has been set. Nothing changes until userspace sets one.

Usage: patch-btmtksdio-bdaddr.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: keep public address"


def edit(path, pairs):
    p = tree + "/" + path
    s = open(p).read()
    if MARK in s:
        print(path + ": already patched")
        return
    for a, b in pairs:
        assert s.count(a) == 1, (path, a[:70], s.count(a))
        s = s.replace(a, b)
    open(p, "w").write(s)
    print(path + ": patched")


edit("drivers/bluetooth/btmtksdio.c", [
    ("""	default:
		return -ENODEV;
	}

	rettime = ktime_get();
""",
     """	default:
		return -ENODEV;
	}

	/* athena: keep public address. One set through mgmt is programmed
	 * again on every setup: the controller may lose it while the
	 * Bluetooth function is off.
	 */
	if (bacmp(&hdev->public_addr, BDADDR_ANY)) {
		err = btmtk_set_bdaddr(hdev, &hdev->public_addr);
		if (err < 0)
			bt_dev_warn(hdev, "Failed to restore public address (%d)",
				    err);
	}

	rettime = ktime_get();
"""),
])
