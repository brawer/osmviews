# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Side-by-side crops of the regular and the filtered GeoTIFF."""

import numpy as np
from PIL import Image

from . import cog


def compare(regular, filtered, width, bbox, out):
    """Writes a PNG with both GeoTIFFs over bbox = (lon0, lat0, lon1, lat1)."""
    lon0, lat0, lon1, lat1 = bbox
    x0, y0 = cog.pixel(width, lon0, lat1)
    x1, y1 = cog.pixel(width, lon1, lat0)
    crops = [cog.read_level(p, width, (x0, y0, x1, y1)) for p in (regular, filtered)]
    vmax = max(float(np.percentile(c, 99.5)) for c in crops) or 1.0
    imgs = [np.clip(c / vmax, 0, 1) ** 0.7 for c in crops]
    gap = np.ones((imgs[0].shape[0], 6))
    im = np.hstack([imgs[0], gap, imgs[1]])
    Image.fromarray((255 * (1 - im)).astype(np.uint8)).save(out, optimize=True)
