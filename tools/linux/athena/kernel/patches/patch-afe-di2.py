#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch mainline mt8183 AFE for athena's 4-mic capture (I2S2 DI + DI2 into VUL12/UL1).

From the stock 4.4 kernel's disassembly: the second I2S2 data
input has its own interconnect inputs I27/I28, and VUL12 takes 4 channels with
AFE_MEMIF_PBUF_SIZE bit 17 (VUL12_4CH). There is no I2S2 register change. VUL12's 4 channels
come from O21/O22 (ch1/ch2) and O09/O10 (ch3/ch4).

Also fixes a mainline bug: UL1_CH2's "I2S0_CH2" switch wrote AFE_CONN21 (channel 1's
output) instead of AFE_CONN22.

Usage: patch-afe-di2.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
d = tree + "/sound/soc/mediatek/mt8183/"
MARK = "athena: I2S2 DI2"


def edit(path, pairs):
    p = d + path
    s = open(p).read()
    if MARK in s:
        return
    for a, b in pairs:
        assert s.count(a) == 1, (path, a[:70], s.count(a))
        s = s.replace(a, b)
    open(p, "w").write(s)


edit("mt8183-interconnection.h", [(
    "#define I_I2S2_CH2 26\n",
    "#define I_I2S2_CH2 26\n"
    "/* athena: I2S2 DI2, the second I2S2 data input (stock calls it I2S2_ADC2) */\n"
    "#define I_I2S2_CH3 27\n"
    "#define I_I2S2_CH4 28\n",
)])

edit("mt8183-dai-i2s.c", [(
    '''		.name = "I2S2",
		.id = MT8183_DAI_I2S_2,
		.capture = {
			.stream_name = "I2S2",
			.channels_min = 1,
			.channels_max = 2,''',
    '''		.name = "I2S2",
		.id = MT8183_DAI_I2S_2,
		.capture = {
			.stream_name = "I2S2",
			.channels_min = 1,
			/* athena: I2S2 DI2 carries ch3/ch4 */
			.channels_max = 4,''',
)])

edit("mt8183-afe-pcm.c", [
    # UL1 FE: 4 channels
    ('''		.name = "UL1",
		.id = MT8183_MEMIF_VUL12,
		.capture = {
			.stream_name = "UL1",
			.channels_min = 1,
			.channels_max = 2,''',
     '''		.name = "UL1",
		.id = MT8183_MEMIF_VUL12,
		.capture = {
			.stream_name = "UL1",
			.channels_min = 1,
			/* athena: I2S2 DI2, VUL12 has a 4-channel mode */
			.channels_max = 4,'''),
    # mixers
    ('''static const struct snd_kcontrol_new memif_ul1_ch1_mix[] = {
	SOC_DAPM_SINGLE_AUTODISABLE("ADDA_UL_CH1", AFE_CONN21,
				    I_ADDA_UL_CH1, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S0_CH1", AFE_CONN21,
				    I_I2S0_CH1, 1, 0),
};

static const struct snd_kcontrol_new memif_ul1_ch2_mix[] = {
	SOC_DAPM_SINGLE_AUTODISABLE("ADDA_UL_CH2", AFE_CONN22,
				    I_ADDA_UL_CH2, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S0_CH2", AFE_CONN21,
				    I_I2S0_CH2, 1, 0),
};
''',
     '''static const struct snd_kcontrol_new memif_ul1_ch1_mix[] = {
	SOC_DAPM_SINGLE_AUTODISABLE("ADDA_UL_CH1", AFE_CONN21,
				    I_ADDA_UL_CH1, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S0_CH1", AFE_CONN21,
				    I_I2S0_CH1, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH1", AFE_CONN21,
				    I_I2S2_CH1, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH3", AFE_CONN21,
				    I_I2S2_CH3, 1, 0),
};

static const struct snd_kcontrol_new memif_ul1_ch2_mix[] = {
	SOC_DAPM_SINGLE_AUTODISABLE("ADDA_UL_CH2", AFE_CONN22,
				    I_ADDA_UL_CH2, 1, 0),
	/* athena: I2S2 DI2 patch also fixes this one, which wrote AFE_CONN21 */
	SOC_DAPM_SINGLE_AUTODISABLE("I2S0_CH2", AFE_CONN22,
				    I_I2S0_CH2, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH2", AFE_CONN22,
				    I_I2S2_CH2, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH4", AFE_CONN22,
				    I_I2S2_CH4, 1, 0),
};

/* VUL12 in 4-channel mode takes ch3/ch4 from O09/O10 */
static const struct snd_kcontrol_new memif_ul1_ch3_mix[] = {
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH1", AFE_CONN9,
				    I_I2S2_CH1, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH3", AFE_CONN9,
				    I_I2S2_CH3, 1, 0),
};

static const struct snd_kcontrol_new memif_ul1_ch4_mix[] = {
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH2", AFE_CONN10,
				    I_I2S2_CH2, 1, 0),
	SOC_DAPM_SINGLE_AUTODISABLE("I2S2_CH4", AFE_CONN10,
				    I_I2S2_CH4, 1, 0),
};
'''),
    # widgets
    ('''	SND_SOC_DAPM_MIXER("UL1_CH2", SND_SOC_NOPM, 0, 0,
			   memif_ul1_ch2_mix, ARRAY_SIZE(memif_ul1_ch2_mix)),
''',
     '''	SND_SOC_DAPM_MIXER("UL1_CH2", SND_SOC_NOPM, 0, 0,
			   memif_ul1_ch2_mix, ARRAY_SIZE(memif_ul1_ch2_mix)),
	SND_SOC_DAPM_MIXER("UL1_CH3", SND_SOC_NOPM, 0, 0,
			   memif_ul1_ch3_mix, ARRAY_SIZE(memif_ul1_ch3_mix)),
	SND_SOC_DAPM_MIXER("UL1_CH4", SND_SOC_NOPM, 0, 0,
			   memif_ul1_ch4_mix, ARRAY_SIZE(memif_ul1_ch4_mix)),
'''),
    # routes
    ('''	{"UL1_CH1", "I2S0_CH1", "I2S0"},
	{"UL1_CH2", "I2S0_CH2", "I2S0"},
''',
     '''	{"UL1_CH1", "I2S0_CH1", "I2S0"},
	{"UL1_CH2", "I2S0_CH2", "I2S0"},
	{"UL1", NULL, "UL1_CH3"},
	{"UL1", NULL, "UL1_CH4"},
	{"UL1_CH1", "I2S2_CH1", "I2S2"},
	{"UL1_CH1", "I2S2_CH3", "I2S2"},
	{"UL1_CH2", "I2S2_CH2", "I2S2"},
	{"UL1_CH2", "I2S2_CH4", "I2S2"},
	{"UL1_CH3", "I2S2_CH1", "I2S2"},
	{"UL1_CH3", "I2S2_CH3", "I2S2"},
	{"UL1_CH4", "I2S2_CH2", "I2S2"},
	{"UL1_CH4", "I2S2_CH4", "I2S2"},
'''),
])

# The 4ch fields go in after the table, so the macro stays as upstream has it.
p = d + "mt8183-afe-pcm.c"
s = open(p).read()
if "VUL12_4CH_MASK" not in s:
    a = "static const struct mtk_base_memif_data memif_data[MT8183_MEMIF_NUM] = {"
    assert s.count(a) == 1
    s = s.replace(a, a.replace("static const", "static"), 1)
    hook = "\tafe->memif_size = MT8183_MEMIF_NUM;\n"
    assert s.count(hook) == 1, "memif_size hook"
    s = s.replace(hook, hook + '''	/* athena: I2S2 DI2, VUL12's 4-channel mode (stock writes 0x3d8 bit 17) */
	memif_data[MT8183_MEMIF_VUL12].quad_ch_reg = AFE_MEMIF_PBUF_SIZE;
	memif_data[MT8183_MEMIF_VUL12].quad_ch_mask = VUL12_4CH_MASK;
	memif_data[MT8183_MEMIF_VUL12].quad_ch_shift = VUL12_4CH_SFT;
''', 1)
    open(p, "w").write(s)
print("patched")
