#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline btmtksdio: report the MT7663's P-256 link keys as their P-192 kind.

athena's MT7663 makes P-256 (Secure Connections) link keys with a phone even with the host's
Secure Connections support off, but encrypts the link with E0, not AES-CCM. The kernel then drops
the connection a moment after pairing: "Invalid security: Missing AES-CCM usage" (seen on unit 1,
2026-10-15, btmon: Link Key Notification key type 0x08, Encryption Change "Enabled with E0", local
Disconnect "Authentication Failure"). TECHO5's btbridge did the same rewrite in userspace on the
other Shows (cmd/btbridge/h4.go p192Key, issue #105): in a Link Key Notification, authenticated
P-256 (8) becomes authenticated P-192 (5) and unauthenticated P-256 (7) becomes unauthenticated
P-192 (4). The kernel then holds the link to the level below, which A2DP never needs more than.
The controller is never handed the type back: a Link Key Request Reply carries address and key only.

Only for the MT7663; the same driver serves the MT7921, which is left alone.

Usage: patch-btmtksdio-p192.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: P-192 link keys"
p = tree + "/drivers/bluetooth/btmtksdio.c"
s = open(p).read()
if MARK in s:
    print(p + ": already patched")
    sys.exit(0)

a = """	/* When someone waits for the WMT event, the skb is being cloned
	 * and being processed the events from there then.
	 */
	if (test_bit(BTMTKSDIO_TX_WAIT_VND_EVT, &bdev->tx_state)) {"""
assert s.count(a) == 1, a
s = s.replace(a, """	/* athena: P-192 link keys. The MT7663 makes P-256 keys but encrypts
	 * with E0, so the core would drop the link ("Missing AES-CCM usage").
	 * Report the key as its P-192 kind (8 -> 5, 7 -> 4).
	 */
	if (evt == HCI_EV_LINK_KEY_NOTIFY && bdev->data->chipid == 0x7663 &&
	    skb->len == HCI_EVENT_HDR_SIZE + sizeof(struct hci_ev_link_key_notify)) {
		struct hci_ev_link_key_notify *ev = (void *)(skb->data + HCI_EVENT_HDR_SIZE);

		if (ev->key_type == HCI_LK_AUTH_COMBINATION_P256)
			ev->key_type = HCI_LK_AUTH_COMBINATION_P192;
		else if (ev->key_type == HCI_LK_UNAUTH_COMBINATION_P256)
			ev->key_type = HCI_LK_UNAUTH_COMBINATION_P192;
	}

""" + a)
open(p, "w").write(s)
print(p + ": patched")
