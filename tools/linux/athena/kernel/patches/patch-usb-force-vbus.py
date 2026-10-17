#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline mtu3 + T-PHY for athena's USB device mode: a "mediatek,force-vbus" property.

athena senses VBUS on a GPIO (GPIO165), not through the USB PHY, so neither the U2 PHY nor the
mtu3 MAC ever sees a session and the host never sees the gadget. Early test builds forced it by
hand (devmem U2PHYDTM1 = 0x2c2c, U3D_MISC_CTRL |= VBUS_FRC_EN | VBUS_ON). This does the same from the
drivers, when the device tree asks for it:

  - T-PHY (phy-mtk-tphy.c), on a U2 port with mediatek,force-vbus: at power-on, also set the force
    bits for VBUSVALID, BVALID and AVALID in U2PHYDTM1 along with their values (0x2c2c);
  - mtu3 (mtu3_core.c), on the controller with mediatek,force-vbus: VBUS forced on instead of
    "vbus detected by HW".

Usage: patch-usb-force-vbus.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: force-vbus"


def edit(path, pairs):
    p = tree + "/" + path
    s = open(p).read()
    if MARK in s:
        return
    for a, b in pairs:
        assert s.count(a) == 1, (path, a[:70], s.count(a))
        s = s.replace(a, b)
    open(p, "w").write(s)


edit("drivers/phy/mediatek/phy-mtk-tphy.c", [
    ("#define P2C_RG_VBUSVALID		BIT(5)\n",
     "/* athena: force-vbus, the force bits for the session signals in U2PHYDTM1 */\n"
     "#define P2C_FORCE_VBUSVALID		BIT(13)\n"
     "#define P2C_FORCE_BVALID		BIT(11)\n"
     "#define P2C_FORCE_AVALID		BIT(10)\n"
     "#define P2C_RG_BVALID			BIT(3)\n"
     "#define P2C_RG_VBUSVALID		BIT(5)\n"),
    ("	bool type_force_mode;\n",
     "	bool type_force_mode;\n"
     "	bool force_vbus;\n"),
    ("""	if (instance->type == PHY_TYPE_USB3)
		instance->type_force_mode = device_property_read_bool(dev, "mediatek,force-mode");
""",
     """	if (instance->type == PHY_TYPE_USB3)
		instance->type_force_mode = device_property_read_bool(dev, "mediatek,force-mode");
	if (instance->type == PHY_TYPE_USB2)
		instance->force_vbus = device_property_read_bool(dev, "mediatek,force-vbus");
"""),
    ("""	mtk_phy_clear_bits(com + U3P_U2PHYDTM1, P2C_RG_SESSEND);

	if (tphy->pdata->avoid_rx_sen_degradation && index) {
		mtk_phy_set_bits(com + U3D_U2PHYDCR0, P2C_RG_SIF_U2PLL_FORCE_ON);
""",
     """	mtk_phy_clear_bits(com + U3P_U2PHYDTM1, P2C_RG_SESSEND);

	/* VBUS sensed elsewhere (a GPIO): hold the session valid */
	if (instance->force_vbus)
		mtk_phy_set_bits(com + U3P_U2PHYDTM1,
				 P2C_FORCE_VBUSVALID | P2C_FORCE_BVALID | P2C_FORCE_AVALID |
				 P2C_RG_VBUSVALID | P2C_RG_BVALID | P2C_RG_AVALID);

	if (tphy->pdata->avoid_rx_sen_degradation && index) {
		mtk_phy_set_bits(com + U3D_U2PHYDCR0, P2C_RG_SIF_U2PLL_FORCE_ON);
"""),
])

edit("drivers/usb/mtu3/mtu3_core.c", [
    ("""	/* vbus detected by HW */
	mtu3_clrbits(mbase, U3D_MISC_CTRL, VBUS_FRC_EN | VBUS_ON);
""",
     """	/* vbus detected by HW, or forced on where the board senses it elsewhere (athena: force-vbus) */
	if (of_property_read_bool(mtu->dev->of_node, "mediatek,force-vbus"))
		mtu3_setbits(mbase, U3D_MISC_CTRL, VBUS_FRC_EN | VBUS_ON);
	else
		mtu3_clrbits(mbase, U3D_MISC_CTRL, VBUS_FRC_EN | VBUS_ON);
"""),
])
print("patched")
