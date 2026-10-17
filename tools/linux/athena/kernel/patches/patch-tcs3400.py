#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Teach mainline drivers/iio/light/tcs3472.c the AMS TCS3400 (athena's light sensor).

The TCS3400 keeps the TCS3472's register addresses (0x80 ENABLE .. 0x9b BDATAH, ID at 0x92,
STATUS at 0x93 with AINT bit 4 and AVALID bit 0), with these differences, all read off the stock
4.4 driver's disassembly (tcs3400_* in Amazon's athena kernel):

  - No command-type field. The TCS3472 sets bit 5 (0x20) for auto-increment; on the TCS3400 the
    address auto-increments by itself, so the data and threshold registers are plain 0x94 and
    0x84 (stock block-reads 8 bytes at 0x94 and block-writes 4 bytes at 0x84). 0xb4/0xa4 would be
    the wrong registers.
  - Interrupts are cleared by writing 0 to AICLEAR (0xe7), not by addressing 0xe6.
  - Gains are 1/4/16/64x (stock table 01 04 10 40), not 1/4/16/60x.
  - ATIME steps 2.78 ms (stock multiplies the cycle count by 2780 us), not 2.4 ms.
  - ID 0x90 (TCS34001/34005) or 0x93 (TCS34003/34007); athena reads 0x93.

The chip is picked from the ID register, so the TCS3472 paths are unchanged. The IIO device is
named "tcs3400" on a TCS3400. Adds the "ams,tcs3400" compatible (and the i2c id "tcs3400") and
documents it in trivial-devices.yaml.

Usage: patch-tcs3400.py KTREE   (idempotent)
"""
import sys

tree = sys.argv[1]
MARK = "athena: TCS3400"


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


edit("drivers/iio/light/tcs3472.c", [
    # header
    (''' * TODO: wait time
 */
''',
     ''' * TODO: wait time
 *
 * athena: TCS3400 support. Same register addresses without the command-type
 * field, its own gains, ATIME step and interrupt clear (see tcs3400_chip).
 */
'''),
    # chip descriptions
    ('''#define TCS3472_CONTROL_AGAIN_MASK (BIT(0) | BIT(1))
''',
     '''#define TCS3472_CONTROL_AGAIN_MASK (BIT(0) | BIT(1))

/* TCS3400: writing this clears all interrupts */
#define TCS3400_AICLEAR (TCS3472_COMMAND | 0x67)

struct tcs3472_chip {
	const char *name;
	const int *agains;		/* 4 entries, by CONTROL.AGAIN */
	const char *agains_avail;
	unsigned int atime_step_us;	/* one ATIME cycle */
	u8 auto_incr;			/* OR'd into word/block register addresses */
	bool aiclear;			/* clear interrupts by writing AICLEAR */
};

static const int tcs3472_agains[] = { 1, 4, 16, 60 };
static const int tcs3400_agains[] = { 1, 4, 16, 64 };

static const struct tcs3472_chip tcs3472_chip = {
	.name = TCS3472_DRV_NAME,
	.agains = tcs3472_agains,
	.agains_avail = "1 4 16 60",
	.atime_step_us = 2400,
	.auto_incr = TCS3472_AUTO_INCR,
};

/*
 * athena: TCS3400. The address auto-increments without a command-type field,
 * so 0xb4 is not CDATA there. Values from the stock 4.4 driver.
 */
static const struct tcs3472_chip tcs3400_chip = {
	.name = "tcs3400",
	.agains = tcs3400_agains,
	.agains_avail = "1 4 16 64",
	.atime_step_us = 2780,
	.auto_incr = 0,
	.aiclear = true,
};
'''),
    ('''struct tcs3472_data {
	struct i2c_client *client;
''',
     '''struct tcs3472_data {
	struct i2c_client *client;
	const struct tcs3472_chip *chip;
'''),
    # the old table moves up into the chip descriptions
    ('''static const int tcs3472_agains[] = { 1, 4, 16, 60 };

static const struct iio_chan_spec''',
     '''static const struct iio_chan_spec'''),
    # register address helper, after req_data's declaration point
    ('''static int tcs3472_req_data(struct tcs3472_data *data)
{''',
     '''/* a word/block register address with the chip's auto-increment bit */
static u8 tcs3472_wreg(struct tcs3472_data *data, u8 reg)
{
	return (reg & ~TCS3472_AUTO_INCR) | data->chip->auto_incr;
}

static int tcs3472_req_data(struct tcs3472_data *data)
{'''),
    # read_raw
    ('''		ret = i2c_smbus_read_word_data(data->client, chan->address);
		iio_device_release_direct(indio_dev);''',
     '''		ret = i2c_smbus_read_word_data(data->client,
					       tcs3472_wreg(data, chan->address));
		iio_device_release_direct(indio_dev);'''),
    ('''		*val = tcs3472_agains[data->control &
			TCS3472_CONTROL_AGAIN_MASK];
		return IIO_VAL_INT;
	case IIO_CHAN_INFO_INT_TIME:
		*val = 0;
		*val2 = (256 - data->atime) * 2400;''',
     '''		*val = data->chip->agains[data->control &
			TCS3472_CONTROL_AGAIN_MASK];
		return IIO_VAL_INT;
	case IIO_CHAN_INFO_INT_TIME:
		*val = 0;
		*val2 = (256 - data->atime) * data->chip->atime_step_us;'''),
    # write_raw
    ('''		for (i = 0; i < ARRAY_SIZE(tcs3472_agains); i++) {
			if (val == tcs3472_agains[i]) {''',
     '''		for (i = 0; i < ARRAY_SIZE(tcs3472_agains); i++) {
			if (val == data->chip->agains[i]) {'''),
    ('''			if (val2 == (256 - i) * 2400) {''',
     '''			if (val2 == (256 - i) * data->chip->atime_step_us) {'''),
    # events
    ('''		period = (256 - data->atime) * 2400 *
			tcs3472_intr_pers[data->apers];''',
     '''		period = (256 - data->atime) * data->chip->atime_step_us *
			tcs3472_intr_pers[data->apers];'''),
    ('''		ret = i2c_smbus_write_word_data(data->client, command, val);''',
     '''		ret = i2c_smbus_write_word_data(data->client,
						tcs3472_wreg(data, command), val);'''),
    ('''			if (period <= (256 - data->atime) * 2400 *
					tcs3472_intr_pers[i])''',
     '''			if (period <= (256 - data->atime) *
					data->chip->atime_step_us *
					tcs3472_intr_pers[i])'''),
    ('''		i2c_smbus_read_byte_data(data->client, TCS3472_INTR_CLEAR);''',
     '''		if (data->chip->aiclear)
			i2c_smbus_write_byte_data(data->client,
						  TCS3400_AICLEAR, 0);
		else
			i2c_smbus_read_byte_data(data->client,
						 TCS3472_INTR_CLEAR);'''),
    # buffer
    ('''		ret = i2c_smbus_read_word_data(data->client,
			TCS3472_CDATA + 2*i);''',
     '''		ret = i2c_smbus_read_word_data(data->client,
			tcs3472_wreg(data, TCS3472_CDATA + 2*i));'''),
    # available attributes depend on the chip
    ('''static ssize_t tcs3472_show_int_time_available(struct device *dev,
					struct device_attribute *attr,
					char *buf)
{
	size_t len = 0;
	int i;

	for (i = 1; i <= 256; i++)
		len += scnprintf(buf + len, PAGE_SIZE - len, "0.%06d ",
			2400 * i);''',
     '''static ssize_t tcs3472_show_int_time_available(struct device *dev,
					struct device_attribute *attr,
					char *buf)
{
	struct tcs3472_data *data = iio_priv(dev_to_iio_dev(dev));
	size_t len = 0;
	int i;

	for (i = 1; i <= 256; i++)
		len += scnprintf(buf + len, PAGE_SIZE - len, "0.%06d ",
			data->chip->atime_step_us * i);'''),
    ('''static IIO_CONST_ATTR(calibscale_available, "1 4 16 60");
static IIO_DEV_ATTR_INT_TIME_AVAIL(tcs3472_show_int_time_available);

static struct attribute *tcs3472_attributes[] = {
	&iio_const_attr_calibscale_available.dev_attr.attr,''',
     '''static ssize_t tcs3472_show_calibscale_available(struct device *dev,
					struct device_attribute *attr,
					char *buf)
{
	struct tcs3472_data *data = iio_priv(dev_to_iio_dev(dev));

	return sysfs_emit(buf, "%s\\n", data->chip->agains_avail);
}

static IIO_DEVICE_ATTR(calibscale_available, 0444,
		       tcs3472_show_calibscale_available, NULL, 0);
static IIO_DEV_ATTR_INT_TIME_AVAIL(tcs3472_show_int_time_available);

static struct attribute *tcs3472_attributes[] = {
	&iio_dev_attr_calibscale_available.dev_attr.attr,'''),
    # probe: pick the chip from the ID
    ('''	indio_dev->info = &tcs3472_info;
	indio_dev->name = TCS3472_DRV_NAME;
''',
     '''	indio_dev->info = &tcs3472_info;
'''),
    ('''	if (ret == 0x44)
		dev_info(&client->dev, "TCS34721/34725 found\\n");
	else if (ret == 0x4d)
		dev_info(&client->dev, "TCS34723/34727 found\\n");
	else
		return -ENODEV;
''',
     '''	data->chip = &tcs3472_chip;
	if (ret == 0x44) {
		dev_info(&client->dev, "TCS34721/34725 found\\n");
	} else if (ret == 0x4d) {
		dev_info(&client->dev, "TCS34723/34727 found\\n");
	} else if (ret == 0x90 || ret == 0x93) {
		/* athena: TCS3400 */
		data->chip = &tcs3400_chip;
		dev_info(&client->dev, "%s found\\n",
			 ret == 0x90 ? "TCS34001/34005" : "TCS34003/34007");
	} else {
		return -ENODEV;
	}
	indio_dev->name = data->chip->name;
'''),
    ('''	ret = i2c_smbus_read_word_data(data->client, TCS3472_AILT);
	if (ret < 0)
		return ret;
	data->low_thresh = ret;

	ret = i2c_smbus_read_word_data(data->client, TCS3472_AIHT);''',
     '''	ret = i2c_smbus_read_word_data(data->client,
				       tcs3472_wreg(data, TCS3472_AILT));
	if (ret < 0)
		return ret;
	data->low_thresh = ret;

	ret = i2c_smbus_read_word_data(data->client,
				       tcs3472_wreg(data, TCS3472_AIHT));'''),
    # ids
    ('''static const struct i2c_device_id tcs3472_id[] = {
	{ "tcs3472" },
	{ }
};
MODULE_DEVICE_TABLE(i2c, tcs3472_id);
''',
     '''static const struct i2c_device_id tcs3472_id[] = {
	{ "tcs3400" },	/* athena: TCS3400 */
	{ "tcs3472" },
	{ }
};
MODULE_DEVICE_TABLE(i2c, tcs3472_id);

static const struct of_device_id tcs3472_of_match[] = {
	{ .compatible = "ams,tcs3400" },
	{ }
};
MODULE_DEVICE_TABLE(of, tcs3472_of_match);
'''),
    ('''		.name	= TCS3472_DRV_NAME,
		.pm	= pm_sleep_ptr(&tcs3472_pm_ops),''',
     '''		.name	= TCS3472_DRV_NAME,
		.of_match_table = tcs3472_of_match,
		.pm	= pm_sleep_ptr(&tcs3472_pm_ops),'''),
])

# dt-binding: a plain I2C sensor with one interrupt fits trivial-devices.
p = tree + "/Documentation/devicetree/bindings/trivial-devices.yaml"
s = open(p).read()
if "ams,tcs3400" not in s:
    a = "          - ams,iaq-core\n"
    assert s.count(a) == 1, "trivial-devices anchor"
    s = s.replace(a, a + "            # AMS TCS3400 color light-to-digital converter\n"
                  "          - ams,tcs3400\n")
    open(p, "w").write(s)

print("patched")
