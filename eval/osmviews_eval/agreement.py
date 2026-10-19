# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""How much the zoom filter changes the map.

Compares the regular and the filtered GeoTIFF at one overview level:
the share of pixels the filter changes, and the rank correlation of all
pixels with views. Pixels within 3 degrees of Null Island are reported
separately, since the filter replaces them on purpose.
"""

import numpy as np

from . import cog
from .stats import spearman

LEVEL = 4096          # pixels across the world; about 10 km at the equator
NULL_ISLAND_DEG = 3.0


def run(regular, filtered):
    a = cog.read_level(regular, LEVEL)
    b = cog.read_level(filtered, LEVEL)
    rows, cols = np.indices(a.shape)
    lon, lat = cog.lonlat(LEVEL, cols, rows)
    island = np.hypot(lon, lat) <= NULL_ISLAND_DEG
    seen = (a > 0) | (b > 0)
    outside = seen & ~island
    changed = outside & (np.abs(a - b) > 1e-6)
    return {
        "level": LEVEL,
        "pixels": int(outside.sum()),
        "changed": int(changed.sum()),
        "changedPct": 100.0 * changed.sum() / max(outside.sum(), 1),
        "spearman": spearman(a[outside], b[outside]),
    }
