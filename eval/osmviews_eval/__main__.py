# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Runs every evaluation and writes the paper's generated files.

Usage: python3 -m osmviews_eval CACHE_DIR OUT_DIR
"""

import json
import sys
from pathlib import Path

import numpy as np

from . import (agreement, cog, coverage, external, figures, inputs, qrank,
               regions, results, stability)


def _in_zip(path, member):
    return f"/vsizip/{Path(path).resolve()}/{member}"


def main(cache, out):
    out = Path(out)
    out.mkdir(parents=True, exist_ok=True)
    regular = inputs.resolve("osmviews", cache)
    filtered = inputs.resolve("osmviews-filtered", cache)
    rasters = {"osmviews": regular}
    if filtered:
        rasters["osmviewsFiltered"] = filtered

    res = {}
    if filtered:
        res["agreement"] = agreement.run(regular, filtered)
        # Lanes fanning out from Null Island over Africa and Europe.
        figures.compare(regular, filtered, 8192, (-10, -8, 45, 50), out / "fan.png")
        # Null Island itself.
        figures.compare(regular, filtered, 16384, (-12, -10, 12, 10), out / "null-island.png")
    else:
        print("no filtered GeoTIFF; skipping the evaluations of the filter")

    countries = regions.load(inputs.resolve("ne-countries", cache), external.LEVEL)
    refs = external.references(
        cache,
        ghsl=_in_zip(inputs.resolve("ghsl-pop", cache),
                     "GHS_POP_E2025_GLOBE_R2023A_4326_30ss_V1_0.tif"),
        viirs=_in_zip(inputs.resolve("viirs-flux", cache), "radiantflux2024.tif"),
        commons=inputs.resolve("commons-geotags", cache),
    )
    res["external"] = external.run(rasters, refs, countries, out)

    previous = [inputs.resolve(f"osmviews-previous-{i}", cache) for i in (2, 1)]
    res["stability"] = stability.run(previous + [regular])

    views_path = Path(cache) / f"views-{Path(regular).stem}-{external.LEVEL}.npy"
    if not views_path.exists():
        np.save(views_path, cog.views(regular, external.LEVEL))
    users = coverage.internet_users(inputs.resolve("wb-internet", cache),
                                    inputs.resolve("wb-population", cache))
    res["coverage"] = coverage.run(np.load(views_path), countries, users, out)

    items = qrank.items(
        cache,
        geo_tags=inputs.resolve("wikidata-geotags", cache),
        pages=inputs.resolve("wikidata-pages", cache),
        qrank_csv=inputs.resolve("qrank", cache),
    )
    res["qrank"] = qrank.run(rasters, *items)

    # Keep the results of evaluations skipped this time.
    old = out / "results.json"
    if old.exists():
        for k, v in json.loads(old.read_text()).items():
            res.setdefault(k, v)
    results.write(res, out / "results.tex", out / "results.json")


if __name__ == "__main__":
    main(*sys.argv[1:3])
