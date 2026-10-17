#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-2.0
"""Patch a mainline 6.18 tree for athena's real display: MediaTek DRM + DSI + the ILI9881C/JD9365 panels.

What it does:
  1. Applies the panel patches from panel/ (written 2026-10-14 against 6.18.56, still apply cleanly):
       0001/0002  ilitek,ili9881c binding + driver: 5 "amazon,athena-ili9881c-*" panels
                  (unit 1 = amazon,athena-ili9881c-boe)
       0003/0004  jadard,jd9365da-h3 binding + driver: 5 "amazon,athena-jd9365-*" panels (other units)
  2. mediatek-drm (mtk_drm_drv.c): evict the bootloader's simple-framebuffer when the DRM device
     binds (athena: evict-simplefb). Upstream mediatek-drm never does this (ChromeOS has no simplefb),
     so without it simplefb keeps /dev/fb0 and the DRM fbdev emulation lands on /dev/fb1, while the
     daemon draws to fb0. With it, simplefb is unregistered first and the DRM fbdev takes fb0; if
     mediatek-drm never binds, simplefb stays and the screen works as before. Same call as
     rockchip/sun4i/meson make.

Usage: patch-display.py KTREE   (idempotent: each step is skipped when its marker is present)
"""
import os
import subprocess
import sys

tree = sys.argv[1]
here = os.path.dirname(os.path.abspath(__file__))
panel = os.path.join(here, "panel")


def read(path):
    with open(os.path.join(tree, path)) as f:
        return f.read()


def apply_patch(name, path, marker):
    """Apply panel/<name> unless <path> already holds <marker>."""
    if marker in read(path):
        return
    subprocess.run(["patch", "-p1", "-N", "-s", "--no-backup-if-mismatch",
                    "-i", os.path.join(panel, name)], cwd=tree, check=True)
    assert marker in read(path), (name, "marker missing after patch")


def edit(path, mark, pairs):
    p = os.path.join(tree, path)
    s = open(p).read()
    if mark in s:
        return
    for a, b in pairs:
        assert s.count(a) == 1, (path, a[:70], s.count(a))
        s = s.replace(a, b)
    open(p, "w").write(s)


BIND = "Documentation/devicetree/bindings/display/panel/"
apply_patch("0001-dt-bindings-ili9881c-add-amazon-athena-panels.patch",
            BIND + "ilitek,ili9881c.yaml", "amazon,athena-ili9881c-boe")
# mediatek-dsi finds its panel only through the OF graph (dsi port@1 -> panel port), which the
# ili9881c binding does not list yet (jd9365da-h3 does): allow it, or dtbs_check flags the node.
edit(BIND + "ilitek,ili9881c.yaml", "  port: true\n", [
    ("  backlight: true\n  power-supply: true\n",
     "  backlight: true\n  port: true\n  power-supply: true\n"),
])
apply_patch("0002-drm-panel-ili9881c-add-amazon-athena-panels.patch",
            "drivers/gpu/drm/panel/panel-ilitek-ili9881c.c", "amazon,athena-ili9881c-boe")
apply_patch("0003-dt-bindings-jd9365da-add-amazon-athena-panels.patch",
            BIND + "jadard,jd9365da-h3.yaml", "amazon,athena-jd9365-boe")
apply_patch("0004-drm-panel-jd9365da-add-amazon-athena-panels.patch",
            "drivers/gpu/drm/panel/panel-jadard-jd9365da-h3.c", "amazon,athena-jd9365-boe")

edit("drivers/gpu/drm/mediatek/mtk_drm_drv.c", "athena: evict-simplefb", [
    ("#include <linux/component.h>\n",
     "#include <linux/aperture.h>\n"
     "#include <linux/component.h>\n"),
    ("""	if (!mtk_drm_get_all_drm_priv(dev)) {
		put_device(private->mutex_dev);
		return 0;
	}

	drm = drm_dev_alloc(&mtk_drm_driver, dev);
""",
     """	if (!mtk_drm_get_all_drm_priv(dev)) {
		put_device(private->mutex_dev);
		return 0;
	}

	/*
	 * athena: evict-simplefb. The bootloader's picture is shown through a
	 * simple-framebuffer until now; unregister it so that the fbdev
	 * emulation below takes its place (and /dev/fb0).
	 */
	ret = aperture_remove_all_conflicting_devices(mtk_drm_driver.name);
	if (ret)
		goto err_put_dev;

	drm = drm_dev_alloc(&mtk_drm_driver, dev);
"""),
])
print("patched")
