#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline tas5805m: take the DSP configuration from a device-tree slot, and keep the sound
card alive without one.

The mainline driver loads its PPC3 register blob with request_firmware("tas5805m_dsp_<name>.bin")
at probe and fails the probe without it, which takes the whole athena sound card down with it (the
microphones too). The amp probes before the root file system is mounted, so a file there comes too
late. The blob is the speaker tuning from the unit's own stock firmware and is not ours to ship, so
the published kernel carries an empty, fixed size slot for it in the device tree instead, which
the installer fills from the unit's own copy:

  techo5,dsp-config = 8192 bytes:
    0..7     magic, ASCII "T5DSPCF1"
    8..11    N, u32 little-endian: length of the data (0 = empty slot)
    12..43   SHA-256 of the data (zeros when empty)
    44..     N bytes of data in the mainline blob format (register, value byte pairs), then zeros

At probe:
  1. a slot with the right size and magic, 0 < N <= 8148, N even and a matching SHA-256: its N bytes
     are the DSP configuration;
  2. otherwise the upstream path: request_firmware("tas5805m_dsp_<ti,dsp-config-name>.bin");
  3. neither: the codec still registers (so the card and its other links come up), but the amp is
     never started: no DSP configuration is sent and it is never put in play mode, so the speaker
     stays silent. Upstream fails the probe here.

The magic is kept XOR'd in the driver so that the string occurs exactly once in a boot image, in
the slot itself; the installer finds the slot by searching for it.

Also adds "select CRYPTO_LIB_SHA256" to SND_SOC_TAS5805M.

Usage: patch-tas5805m-dsp-slot.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: DSP slot"


def edit(rel, pairs):
    p = tree + "/" + rel
    s = open(p).read()
    if MARK in s:
        return
    for a, b in pairs:
        assert s.count(a) == 1, (rel, a[:70], s.count(a))
        s = s.replace(a, b)
    assert MARK in s, rel
    open(p, "w").write(s)


edit("sound/soc/codecs/Kconfig", [(
    '''config SND_SOC_TAS5805M
	tristate "Texas Instruments TAS5805M speaker amplifier"
	depends on I2C
''',
    '''config SND_SOC_TAS5805M
	tristate "Texas Instruments TAS5805M speaker amplifier"
	depends on I2C
	# athena: DSP slot, checked with SHA-256
	select CRYPTO_LIB_SHA256
'''),
])

edit("sound/soc/codecs/tas5805m.c", [
    ('''#include <linux/workqueue.h>
''',
     '''#include <linux/workqueue.h>
#include <linux/unaligned.h>
#include <crypto/sha2.h>
'''),
    # do_work: an amp without a DSP configuration is never started
    ('''	mutex_lock(&tas5805m->lock);
	/* We mustn't issue any I2C transactions until the I2S
''',
     '''	mutex_lock(&tas5805m->lock);
	/* athena: DSP slot. Without a configuration the amp stays off. */
	if (!tas5805m->dsp_cfg_len) {
		mutex_unlock(&tas5805m->lock);
		return;
	}

	/* We mustn't issue any I2C transactions until the I2S
'''),
    # helpers before probe
    ('''static int tas5805m_i2c_probe(struct i2c_client *i2c)
{''',
     '''/*
 * athena: DSP slot. A fixed-size device-tree property that an installer fills
 * in place with the DSP configuration (same format as the firmware file):
 *   0..7 magic, 8..11 data length N (le32, 0 = empty), 12..43 SHA-256 of the
 *   data, 44.. the data, zero padded to TAS5805M_SLOT_SIZE.
 */
#define TAS5805M_SLOT_SIZE	8192
#define TAS5805M_SLOT_HDR	44
#define TAS5805M_SLOT_XOR	0x5a

/* The magic XOR'd, so it appears in the boot image only in the slot itself */
static const u8 tas5805m_slot_magic_x[8] = {
	0x0e, 0x6f, 0x1e, 0x09, 0x0a, 0x19, 0x1c, 0x6b,
};

static int tas5805m_dsp_from_slot(struct device *dev,
				  struct tas5805m_priv *tas5805m)
{
	u8 digest[SHA256_DIGEST_SIZE];
	const u8 *slot;
	int len, i;
	u32 n;

	slot = of_get_property(dev->of_node, "techo5,dsp-config", &len);
	if (!slot)
		return -ENOENT;

	if (len != TAS5805M_SLOT_SIZE) {
		dev_warn(dev, "DSP slot: %d bytes, not %d\\n", len,
			 TAS5805M_SLOT_SIZE);
		return -EINVAL;
	}

	for (i = 0; i < ARRAY_SIZE(tas5805m_slot_magic_x); i++) {
		if ((slot[i] ^ TAS5805M_SLOT_XOR) != tas5805m_slot_magic_x[i]) {
			dev_warn(dev, "DSP slot: bad magic\\n");
			return -EINVAL;
		}
	}

	n = get_unaligned_le32(slot + 8);
	if (!n)
		return -ENOENT;

	if (n > TAS5805M_SLOT_SIZE - TAS5805M_SLOT_HDR || (n & 1)) {
		dev_warn(dev, "DSP slot: bad length %u\\n", n);
		return -EINVAL;
	}

	sha256(slot + TAS5805M_SLOT_HDR, n, digest);
	if (memcmp(digest, slot + 12, SHA256_DIGEST_SIZE)) {
		dev_warn(dev, "DSP slot: SHA-256 mismatch\\n");
		return -EINVAL;
	}

	tas5805m->dsp_cfg_data = devm_kmemdup(dev, slot + TAS5805M_SLOT_HDR,
					      n, GFP_KERNEL);
	if (!tas5805m->dsp_cfg_data)
		return -ENOMEM;
	tas5805m->dsp_cfg_len = n;

	dev_info(dev, "DSP configuration from the device tree slot (%u bytes)\\n",
		 n);
	return 0;
}

/* The upstream way: a firmware file named after ti,dsp-config-name */
static int tas5805m_dsp_from_firmware(struct device *dev,
				      struct tas5805m_priv *tas5805m)
{
	char filename[128];
	const char *config_name;
	const struct firmware *fw;
	int ret;

	/* This configuration must be generated by PPC3. The file loaded
	 * consists of a sequence of register writes, where bytes at
	 * even indices are register addresses and those at odd indices
	 * are register values.
	 *
	 * The fixed portion of PPC3's output prior to the 5ms delay
	 * should be omitted.
	 */
	if (device_property_read_string(dev, "ti,dsp-config-name",
					&config_name))
		config_name = "default";

	snprintf(filename, sizeof(filename), "tas5805m_dsp_%s.bin",
		 config_name);
	ret = firmware_request_nowarn(&fw, filename, dev);
	if (ret)
		return ret;

	if ((fw->size < 2) || (fw->size & 1)) {
		dev_err(dev, "firmware is invalid\\n");
		release_firmware(fw);
		return -EINVAL;
	}

	tas5805m->dsp_cfg_len = fw->size;
	tas5805m->dsp_cfg_data = devm_kmemdup(dev, fw->data, fw->size, GFP_KERNEL);
	if (!tas5805m->dsp_cfg_data) {
		tas5805m->dsp_cfg_len = 0;
		release_firmware(fw);
		return -ENOMEM;
	}

	release_firmware(fw);
	return 0;
}

static int tas5805m_i2c_probe(struct i2c_client *i2c)
{'''),
    ('''	struct tas5805m_priv *tas5805m;
	char filename[128];
	const char *config_name;
	const struct firmware *fw;
	int ret;
''',
     '''	struct tas5805m_priv *tas5805m;
	int ret;
'''),
    ('''	/* This configuration must be generated by PPC3. The file loaded
	 * consists of a sequence of register writes, where bytes at
	 * even indices are register addresses and those at odd indices
	 * are register values.
	 *
	 * The fixed portion of PPC3's output prior to the 5ms delay
	 * should be omitted.
	 */
	if (device_property_read_string(dev, "ti,dsp-config-name",
					&config_name))
		config_name = "default";

	snprintf(filename, sizeof(filename), "tas5805m_dsp_%s.bin",
		 config_name);
	ret = request_firmware(&fw, filename, dev);
	if (ret)
		return ret;

	if ((fw->size < 2) || (fw->size & 1)) {
		dev_err(dev, "firmware is invalid\\n");
		release_firmware(fw);
		return -EINVAL;
	}

	tas5805m->dsp_cfg_len = fw->size;
	tas5805m->dsp_cfg_data = devm_kmemdup(dev, fw->data, fw->size, GFP_KERNEL);
	if (!tas5805m->dsp_cfg_data) {
		release_firmware(fw);
		return -ENOMEM;
	}

	release_firmware(fw);
''',
     '''	/* athena: DSP slot first, then the firmware file. With neither the
	 * codec still registers, so the rest of the card works, but the amp
	 * is never started (do_work) and stays silent.
	 */
	ret = tas5805m_dsp_from_slot(dev, tas5805m);
	if (ret == -ENOMEM)
		return ret;
	if (ret) {
		ret = tas5805m_dsp_from_firmware(dev, tas5805m);
		if (ret == -ENOMEM)
			return ret;
		if (ret)
			dev_warn(dev, "no DSP configuration (%d): speaker output stays off\\n",
				 ret);
	}
'''),
])
print("patched")
