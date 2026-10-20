# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Runs every evaluation and writes the paper's generated files.

Usage: python3 -m osmviews_eval CACHE_DIR OUT_DIR
"""

import json
import shutil
import sys
import zipfile
from pathlib import Path

import numpy as np

from . import (agreement, cities, cog, coverage, external, figures, functions,
               inputs, qrank, ranking, regions, results, samples, stability)

GHSL_TIF = "GHS_POP_E2025_GLOBE_R2023A_4326_30ss_V1_0.tif"
VIIRS_TIF = "radiantflux2024.tif"


def _in_zip(path, member):
    return f"/vsizip/{Path(path).resolve()}/{member}"


def _extract(path, member, cache):
    """Extracts member of zip file path into the cache, once.

    Reading through /vsizip/ is slow for many small reads.
    """
    dest = Path(cache) / member
    if not dest.exists():
        with zipfile.ZipFile(path) as z, z.open(member) as src, \
                open(dest.with_suffix(".tmp"), "wb") as out:
            shutil.copyfileobj(src, out, 1 << 24)
        dest.with_suffix(".tmp").rename(dest)
    return str(dest)


def _ranking(cache, regular, countries, out):
    """Ranking functions on sample sets, and the ranking of cities."""
    ghsl = _extract(inputs.resolve("ghsl-pop", cache), GHSL_TIF, cache)
    viirs = _in_zip(inputs.resolve("viirs-flux", cache), VIIRS_TIF)
    overture = inputs.entry("overture-places")
    inat = inputs.entry("inaturalist")
    sources = {
        "commons": lambda: samples.geo_tags(
            cache, "commons", inputs.resolve("commons-geotags", cache)),
        "wikidata": lambda: samples.geo_tags(
            cache, "wikidata", inputs.resolve("wikidata-geotags", cache)),
        "places": lambda: samples.overture_places(
            cache, overture["url"], overture["parts"]),
        "observations": lambda: samples.inaturalist(cache, inat["url"], inat["etag"]),
    }
    atp = samples.alltheplaces(cache, inputs.resolve("alltheplaces", cache))
    sets = {
        "land": samples.random_land(cache, countries),
        "people": samples.random_people(cache, ghsl),
    }
    sizes = {}

    def add(name, lon, lat):
        sizes[name] = len(lon)
        sets[name] = samples.draw(lon, lat)

    for name in ("wikidata", "commons", "places"):
        add(name, *sources[name]())
    for g in samples.ATP_GROUPS:
        add(g, *atp[g])
    add("observations", *sources["observations"]())
    del atp

    fns = {
        "osmviews": lambda: functions.OSMViews(regular),
        "population": lambda: functions.LonLatRaster(ghsl, per_area=True),
        "lights": lambda: functions.LonLatRaster(viirs, per_area=True),
    }
    for name, load in sources.items():
        fns[{"commons": "photos", "wikidata": "items"}.get(name, name)] = (
            lambda load=load: functions.PointDensity(*load()))
    fns["random"] = functions.Random
    scores = ranking.score(sets, fns, Path(cache) / f"scores-{Path(regular).stem}.npz")
    res = ranking.run(scores, out)
    for name, n in sizes.items():
        res[name]["sourcePoints"] = n

    _, lon, lat, pop = samples.geonames_cities(inputs.resolve("geonames-cities", cache))
    grids = cities.sum_grids(cache, regular, ghsl, _in_zip(
        inputs.resolve("viirs-flux", cache), VIIRS_TIF), {
            "photos": sources["commons"], "items": sources["wikidata"],
            "places": sources["places"], "observations": sources["observations"]})
    res["cities"], _ = cities.run(grids, lon, lat, pop)
    cities.write_table(res["cities"], Path(out) / "ranking-cities.tex")
    return res


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
        ghsl=_in_zip(inputs.resolve("ghsl-pop", cache), GHSL_TIF),
        viirs=_in_zip(inputs.resolve("viirs-flux", cache), VIIRS_TIF),
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

    res["ranking"] = _ranking(cache, regular, countries, out)

    # Keep the results of evaluations skipped this time.
    old = out / "results.json"
    if old.exists():
        for k, v in json.loads(old.read_text()).items():
            res.setdefault(k, v)
    results.write(res, out / "results.tex", out / "results.json")


if __name__ == "__main__":
    main(*sys.argv[1:3])
