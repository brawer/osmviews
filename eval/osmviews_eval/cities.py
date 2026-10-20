# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Ranking cities: each function summed over a window around the city.

A city is an area, not a point. Its GeoNames coordinate can sit where few
people go, such as the Imperial Palace at the center of Tokyo, so a
fine-grained function read at that one point would rank the city by its
park. Instead, each function is summed over all pixels within RADIUS_KM
of the coordinate, on a level of about 2.4 km per pixel at the equator,
and the sums are compared with each other and with the population that
GeoNames gives for the city.
"""

from pathlib import Path

import numpy as np

from . import agreement_stats, cog, grid
from .results import HEADER

LEVEL = 16384
RADIUS_KM = 5.0


def sum_grids(cache, osmviews, ghsl, viirs, points):
    """Returns {name: per-pixel totals at LEVEL}, cached as .npy files.

    points: {name: callable returning (lon, lat)} of point datasets to count.
    """
    def cached(name, build):
        path = Path(cache) / f"sum-{name}-{LEVEL}.npy"
        if not path.exists():
            np.save(path, build().astype(np.float32))
        return np.load(path, mmap_mode="r")

    out = {
        "osmviews": cached(f"osmviews-{Path(osmviews).stem}", lambda: cog.views(osmviews, LEVEL)),
        "population": cached("population", lambda: grid.maxpool_lonlat(
            ghsl, LEVEL, per_area=False, reduce=np.add)),
        "lights": cached("lights", lambda: grid.maxpool_lonlat(
            viirs, LEVEL, per_area=False, reduce=np.add)),
    }
    for name, load in points.items():
        out[name] = cached(name, lambda: grid.point_counts(LEVEL, *load()))
    return out


def window_sums(g, lon, lat):
    """Sums grid g over the pixels within RADIUS_KM of each point."""
    col = np.clip(grid.mercator_col(LEVEL, lon).astype(np.int64), 0, LEVEL - 1)
    row = np.clip(grid.mercator_row(LEVEL, lat).astype(np.int64), 0, LEVEL - 1)
    px_km = 2 * np.pi * grid.EARTH_RADIUS_KM * np.cos(np.radians(lat)) / LEVEL
    radius = np.ceil(RADIUS_KM / px_km).astype(np.int64)
    out = np.empty(len(lon))
    for i, (r, c, k) in enumerate(zip(row, col, radius)):
        dy, dx = np.mgrid[-k:k + 1, -k:k + 1]
        inside = (dx * dx + dy * dy) <= k * k
        rr = np.clip(r + dy[inside], 0, LEVEL - 1)
        cc = (c + dx[inside]) % LEVEL
        out[i] = float(np.asarray(g[rr, cc], np.float64).sum())
    return out


def run(grids, lon, lat, population):
    """Returns per-function agreement with GeoNames population and OSMViews."""
    keep = np.abs(lat) < grid.MAX_LAT
    lon, lat, population = lon[keep], lat[keep], population[keep]
    scores = {k: window_sums(g, lon, lat) for k, g in grids.items()}
    res = {"cities": int(keep.sum()), "radiusKm": RADIUS_KM}
    for k, s in scores.items():
        res[k] = {
            "kendallPopulation": agreement_stats.kendall(s, population),
            "kendallOsmviews": agreement_stats.kendall(s, scores["osmviews"]),
            "zeroPct": 100.0 * float((s == 0).mean()),
        }
    return res, scores


NAMES = {"osmviews": "OSMViews", "population": "Population", "lights": "Night lights",
         "photos": "Photos", "items": "Wikidata", "places": "Places",
         "observations": "Nature"}


def write_table(res, path):
    """Writes Kendall's tau of each function with GeoNames and OSMViews."""
    lines = [HEADER, "\\begin{tabular}{lrr}\n", "\\hline\n",
             " & GeoNames & OSMViews \\\\\n", "\\hline\n"]
    for k, name in NAMES.items():
        if k in res:
            r = res[k]
            other = "" if k == "osmviews" else f"{r['kendallOsmviews']:.2f}"
            lines.append(f"{name} & {r['kendallPopulation']:.2f} & {other} \\\\\n")
    lines += ["\\hline\n", "\\end{tabular}\n"]
    path.write_text("".join(lines))
