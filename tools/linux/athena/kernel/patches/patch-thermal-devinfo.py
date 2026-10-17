#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline auxadc_thermal: on athena, take the MT8183 thermal calibration from the
bootloader's copy of the efuse when the efuse itself reads as zeros.

Amazon's secure firmware keeps the efuse from Linux: through the efuse driver all 4 KB read as
zeros (Fire OS logs a DEVAPC access violation on efuse_top too). So the thermal driver found no
calibration and used its defaults, which put the readings off by several degrees. LK copies the
efuse into the device tree at boot, as /chosen "atag,devinfo" (little-endian words: a size and a
tag, then the efuse words), which is where MediaTek's own kernels read it. The three calibration
words are words 101-103 of that property: on unit 1 all twelve fields decode to exactly the values
Fire OS logged for that unit ([Thermal/TZ/CPU][cal]), and unit 2 has its own in the same range.

Only used when all three efuse words are zero and the property is there, so a board whose efuse
reads normally is not affected.

Usage: patch-thermal-devinfo.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: devinfo calibration"
p = tree + "/drivers/thermal/mediatek/auxadc_thermal.c"
s = open(p).read()
if MARK in s:
    print(p + ": already patched")
    sys.exit(0)

pairs = [
    ("static int mtk_thermal_get_calibration_data(struct device *dev,\n",
     """/* athena: devinfo calibration. Amazon's secure firmware hides the efuse
 * from Linux (it reads as zeros), but LK copies it into /chosen as
 * "atag,devinfo": little-endian words, a size and a tag, then the efuse.
 * The thermal calibration is in words 101-103 of the property.
 */
#define ATHENA_DEVINFO_THERMAL	101

static void mtk_thermal_devinfo_calibration(struct device *dev, u32 *buf)
{
	const __le32 *w;
	int len;

	if (!of_chosen)
		return;
	w = of_get_property(of_chosen, "atag,devinfo", &len);
	if (!w || len < (ATHENA_DEVINFO_THERMAL + 3) * (int)sizeof(u32))
		return;
	buf[0] = le32_to_cpu(w[ATHENA_DEVINFO_THERMAL]);
	buf[1] = le32_to_cpu(w[ATHENA_DEVINFO_THERMAL + 1]);
	buf[2] = le32_to_cpu(w[ATHENA_DEVINFO_THERMAL + 2]);
	dev_info(dev, "efuse reads as zeros, using the bootloader's devinfo copy\\n");
}

static int mtk_thermal_get_calibration_data(struct device *dev,
"""),
    ("""	if (len < 3 * sizeof(u32)) {
		dev_warn(dev, "invalid calibration data\\n");
		ret = -EINVAL;
		goto out;
	}
""",
     """	if (len < 3 * sizeof(u32)) {
		dev_warn(dev, "invalid calibration data\\n");
		ret = -EINVAL;
		goto out;
	}

	if (!buf[0] && !buf[1] && !buf[2])
		mtk_thermal_devinfo_calibration(dev, buf);
"""),
]
for a, b in pairs:
    assert s.count(a) == 1, (a, s.count(a))
    s = s.replace(a, b)
open(p, "w").write(s)
print(p + ": patched")
