# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Agreement of OSMViews with other data about where people are.

Brings population (GHSL), night lights (VIIRS) and the camera locations
of Wikimedia Commons photos onto the pixels of one overview level and
computes Spearman's rank correlation with OSMViews over land, with each
pixel weighted by its ground area so that Web Mercator does not inflate
high latitudes. Antarctica is left out, since none of the references
covers it. For a sense of scale, the references are also correlated with
each other.
"""

from pathlib import Path

import numpy as np

from . import cog, grid, regions, wikimedia
from .results import HEADER
from .stats import weighted_spearman

LEVEL = 4096  # pixels across the world; about 10 km at the equator

# Latitudes covered by each reference; outside, a zero means "no data".
COVERAGE = {"viirs": (-65.0, 75.0)}

NAMES = {
    "osmviews": "OSMViews",
    "osmviewsFiltered": "Filtered",
    "ghsl": "Population",
    "viirs": "Night lights",
    "commons": "Photos",
}


def _cached(cache, name, build):
    """Returns build(), memoized as an .npy file in the cache directory."""
    path = Path(cache) / f"grid-{name}-{LEVEL}.npy"
    if path.exists():
        return np.load(path)
    a = build()
    np.save(path, a)
    return a


def references(cache, ghsl, viirs, commons):
    """Returns the reference grids by name, building each once."""
    def commons_grid():
        _, lat, lon = wikimedia.geo_tags(commons)
        return grid.count_points(LEVEL, lon, lat)

    return {
        "ghsl": _cached(cache, "ghsl",
                        lambda: grid.maxpool_lonlat(ghsl, LEVEL, per_area=True)),
        "viirs": _cached(cache, "viirs",
                         lambda: grid.maxpool_lonlat(viirs, LEVEL, per_area=True)),
        "commons": _cached(cache, "commons", commons_grid),
    }


def _row_lat():
    rows = np.arange(LEVEL)
    return np.degrees(np.arctan(np.sinh(np.pi * (1 - 2 * (rows + 0.5) / LEVEL))))


def run(rasters, refs, countries, out_dir):
    """Computes the correlations and writes the paper's tables.

    rasters: {"osmviews": path, ["osmviewsFiltered": path]}
    refs:    {name: grid} from references()
    """
    grids = {k: cog.read_level(p, LEVEL) for k, p in rasters.items()}
    grids.update(refs)
    land = countries.land & ~countries.mask("continent", "Antarctica")
    area = np.broadcast_to(grid.pixel_area_km2(LEVEL)[:, None], (LEVEL, LEVEL))
    lat = _row_lat()

    def mask(a, b, region=None):
        m = land if region is None else region & land
        for k in (a, b):
            if k in COVERAGE:
                lo, hi = COVERAGE[k]
                m = m & ((lat > lo) & (lat < hi))[:, None]
        return m

    def rho(a, b, region=None):
        m = mask(a, b, region)
        return weighted_spearman(grids[a][m], grids[b][m], area[m])

    names = list(grids)
    matrix = {(a, b): rho(a, b) for i, a in enumerate(names) for b in names[i + 1:]}

    continents = sorted({c["continent"] for c in countries.info} -
                        {"Antarctica", "Seven seas (open ocean)"})
    by_continent = {
        cont: {r: rho("osmviews", r, countries.mask("continent", cont)) for r in refs}
        for cont in continents
    }

    out_dir = Path(out_dir)
    _write_matrix(names, matrix, out_dir / "external-matrix.tex")
    _write_continents(list(refs), by_continent, out_dir / "external-continents.tex")

    res = {"level": LEVEL, "landKm": int(round(float(area[land].sum())))}
    for r in refs:
        res[r] = matrix[("osmviews", r)]
        if "osmviewsFiltered" in grids:
            res[r + "Filtered"] = matrix[("osmviewsFiltered", r)]
    res["ghslViirs"] = matrix[("ghsl", "viirs")]
    res["ghslCommons"] = matrix[("ghsl", "commons")]
    res["viirsCommons"] = matrix[("viirs", "commons")]
    res["byContinent"] = by_continent
    return res




def _write_matrix(names, matrix, path):
    lines = [HEADER, "\\begin{tabular}{l" + "r" * (len(names) - 1) + "}\n",
             "\\hline\n",
             " & " + " & ".join(NAMES[n] for n in names[1:]) + " \\\\\n",
             "\\hline\n"]
    for i, a in enumerate(names[:-1]):
        cells = []
        for j, b in enumerate(names[1:], start=1):
            cells.append(f"{matrix[(a, b)]:.2f}" if j > i else "")
        lines.append(NAMES[a] + " & " + " & ".join(cells) + " \\\\\n")
    lines += ["\\hline\n", "\\end{tabular}\n"]
    path.write_text("".join(lines))


def _write_continents(refs, by_continent, path):
    lines = [HEADER, "\\begin{tabular}{l" + "r" * len(refs) + "}\n",
             "\\hline\n",
             " & " + " & ".join(NAMES[r] for r in refs) + " \\\\\n",
             "\\hline\n"]
    for cont, row in by_continent.items():
        lines.append(cont + " & " + " & ".join(f"{row[r]:.2f}" for r in refs) + " \\\\\n")
    lines += ["\\hline\n", "\\end{tabular}\n"]
    path.write_text("".join(lines))
