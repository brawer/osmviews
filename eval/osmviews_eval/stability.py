# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""How much the published map changes from one weekly release to the next.

Each release covers a trailing window of 52 weeks, so consecutive
releases share 51 weeks of tile logs; this measures what a user of the
weekly file sees change, not how stable a single week of logs is. For
each pair of consecutive releases, over pixels with views in either, it
reports the area-weighted rank correlation and how many of the top 1% of
pixels stay in the top 1%.
"""

import numpy as np

from . import cog, grid
from .stats import weighted_spearman

LEVEL = 4096
TOP_PCT = 1


def _top(a, mask, pct):
    cut = np.percentile(a[mask], 100 - pct)
    return mask & (a >= cut)


def run(releases):
    """releases: list of GeoTIFF paths, oldest first."""
    area = np.broadcast_to(grid.pixel_area_km2(LEVEL)[:, None], (LEVEL, LEVEL))
    grids = [cog.read_level(p, LEVEL) for p in releases]
    rhos, overlaps = [], []
    for a, b in zip(grids, grids[1:]):
        m = (a > 0) | (b > 0)
        rhos.append(weighted_spearman(a[m], b[m], area[m]))
        ta, tb = _top(a, m, TOP_PCT), _top(b, m, TOP_PCT)
        overlaps.append(100.0 * float((ta & tb).sum() / ta.sum()))
    return {
        "level": LEVEL,
        "releases": len(releases),
        "spearmanMin": min(rhos),
        "topPct": TOP_PCT,
        "topKeptPctMin": min(overlaps),
    }
