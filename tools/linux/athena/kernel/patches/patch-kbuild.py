#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Hook athena's new files into Kbuild: the board DTS and the machine driver.

  - arch/arm64/boot/dts/mediatek/Makefile: build mt8183-amazon-athena.dtb
    (the DTS itself is copied in by build.sh from ../dts);
  - sound/soc/mediatek/Kconfig: SND_SOC_MT8183_ATHENA, which selects the amp and mic ADC codecs;
  - sound/soc/mediatek/mt8183/Makefile: build mt8183-athena.o (copied in from ../src).

Usage: patch-kbuild.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]


def edit(rel, mark, anchor, new):
    p = tree + "/" + rel
    s = open(p).read()
    if mark in s:
        return
    assert s.count(anchor) == 1, (rel, anchor, s.count(anchor))
    s = s.replace(anchor, new)
    open(p, "w").write(s)


edit("arch/arm64/boot/dts/mediatek/Makefile", "mt8183-amazon-athena.dtb",
     "dtb-$(CONFIG_ARCH_MEDIATEK) += mt8183-evb.dtb\n",
     "dtb-$(CONFIG_ARCH_MEDIATEK) += mt8183-amazon-athena.dtb\n"
     "dtb-$(CONFIG_ARCH_MEDIATEK) += mt8183-evb.dtb\n")

edit("sound/soc/mediatek/Kconfig", "SND_SOC_MT8183_ATHENA",
     "config SND_SOC_MT8183_MT6358_TS3A227E_MAX98357A\n",
     """config SND_SOC_MT8183_ATHENA
	tristate "ASoC Audio driver for the Echo Show 8 2nd gen (MT8183 + TAS5805M + TLV320ADC5140)"
	depends on SND_SOC_MT8183 && I2C
	select SND_SOC_TAS5805M
	select SND_SOC_TLV320ADCX140
	help
	  Machine driver for the Amazon Echo Show 8 2nd gen (athena).

config SND_SOC_MT8183_MT6358_TS3A227E_MAX98357A
""")

edit("sound/soc/mediatek/mt8183/Makefile", "mt8183-athena.o",
     "obj-$(CONFIG_SND_SOC_MT8183_DA7219_MAX98357A) += mt8183-da7219-max98357.o\n",
     "obj-$(CONFIG_SND_SOC_MT8183_DA7219_MAX98357A) += mt8183-da7219-max98357.o\n"
     "obj-$(CONFIG_SND_SOC_MT8183_ATHENA) += mt8183-athena.o\n")
print("patched")
