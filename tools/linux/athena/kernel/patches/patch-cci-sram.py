#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline mtk-cci-devfreq: give MT8183 the SRAM voltage limits it needs to track an SRAM
supply, the same ones mediatek-cpufreq already has for MT8183.

The CPU SRAM on athena is powered by its own PMIC rails (vsram_proc11, vsram_proc12), and Fire OS
moves them along with the CPU rails in software. TECHO5 does the same: the device tree gives each
CPU and the CCI an sram-supply, and the drivers keep SRAM above the logic voltage.
The CCI shares vproc12 with the little cores, so it must track vsram_proc12 too; otherwise it can
raise vproc12 while the little cores hold their SRAM low.

Mainline's MT8183 data for the CCI driver has no sram_min_volt/sram_max_volt (Chromebooks give the
CCI no sram-supply), so with one it asks for an SRAM voltage between about 1 V and 0 V, and the
regulator core's BUG_ON(min > max) stops the kernel at boot.

Usage: patch-cci-sram.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: sram limits"
p = tree + "/drivers/devfreq/mtk-cci-devfreq.c"
s = open(p).read()
if MARK in s:
    print(p + ": already patched")
    sys.exit(0)

a = """static const struct mtk_ccifreq_platform_data mt8183_platform_data = {
	.min_volt_shift = 100000,
	.max_volt_shift = 200000,
	.proc_max_volt = 1150000,
};
"""
b = """static const struct mtk_ccifreq_platform_data mt8183_platform_data = {
	.min_volt_shift = 100000,
	.max_volt_shift = 200000,
	.proc_max_volt = 1150000,
	/* athena: sram limits, as mediatek-cpufreq's mt8183 data */
	.sram_min_volt = 0,
	.sram_max_volt = 1150000,
};
"""
assert s.count(a) == 1, s.count(a)
s = s.replace(a, b)
open(p, "w").write(s)
print(p + ": patched")
