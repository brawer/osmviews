# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Agreement of OSMViews with QRank for Wikidata items with coordinates.

QRank ranks Wikidata items by their page views across Wikimedia projects.
For each item with a coordinate location and a QRank, the OSMViews value
is read at the item's coordinates, and the two are rank-correlated. The
coordinates come from the Wikidata geo_tags dump, joined to Q-ids through
the page dump, so no query service is involved.
"""

from pathlib import Path

import numpy as np

from . import cog, wikimedia
from .stats import spearman

LEVEL = 65536  # pixels across the world; about 600 m at the equator


def items(cache, geo_tags, pages, qrank_csv):
    """Returns Q-ids, QRank, lat and lon of items with both, cached."""
    path = Path(cache) / "qrank-items.npz"
    if path.exists():
        z = np.load(path)
        return z["qid"], z["qrank"], z["lat"], z["lon"]
    page, lat, lon = wikimedia.geo_tags(geo_tags)
    qids = wikimedia.item_pages(pages, page)
    ranks = wikimedia.qrank(qrank_csv)
    keep = [i for i, p in enumerate(page) if qids.get(int(p)) in ranks]
    qid = np.array([qids[int(page[i])] for i in keep], np.int64)
    qrank = np.array([ranks[q] for q in qid], np.int64)
    lat, lon = lat[keep], lon[keep]
    np.savez_compressed(path, qid=qid, qrank=qrank, lat=lat, lon=lon)
    return qid, qrank, lat, lon


def run(rasters, qid, qrank, lat, lon):
    res = {"level": LEVEL, "items": len(qid)}
    for name, path in rasters.items():
        views = cog.sample(path, LEVEL, lon, lat)
        key = "" if name == "osmviews" else "Filtered"
        res["spearman" + key] = spearman(qrank, views)
        if not key:
            res["withViewsPct"] = 100.0 * float((views > 0).mean())
    return res
