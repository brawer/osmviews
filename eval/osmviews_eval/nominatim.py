# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Importance as computed by Nominatim, the geocoder of OpenStreetMap.

Nominatim ranks search results by a primary importance per Wikidata item,
derived from Wikipedia, and breaks ties with a secondary importance raster
that it looks up at a place's centroid. The raster distributed since
Nominatim 4.5 holds, for each cell of about 0.02 degrees, the highest
importance of any area (usually an administrative boundary) covering it.

Both files are read from the data that nominatim.org publishes: the
primary importance as CSV, the raster as a PostGIS SQL dump, which is
converted once into a GeoTIFF so it can be read like the other rasters.
"""

import gzip
import struct
from pathlib import Path

import numpy as np
import rasterio
from rasterio.transform import from_origin

from . import agreement_stats, samples
from .results import HEADER

# PostGIS raster pixel type 16BUI with a nodata value.
_PIXTYPE_16BUI_NODATA = 0x46


def _tiles(sql_gz):
    """Yields (scale_x, scale_y, x0, y0, width, height, uint16 array) per
    raster tile in a PostGIS dump made by raster2pgsql."""
    with gzip.open(sql_gz, "rt") as f:
        for line in f:
            if not line.startswith("01"):
                continue
            b = bytes.fromhex(line.strip())
            sx, sy, x0, y0, kx, ky = struct.unpack("<6d", b[5:53])
            _, w, h = struct.unpack("<iHH", b[53:61])
            if kx or ky or b[61] != _PIXTYPE_16BUI_NODATA:
                raise ValueError(f"{sql_gz}: unexpected raster tile layout")
            data = np.frombuffer(b[64:64 + 2 * w * h], "<u2").reshape(h, w)
            yield sx, sy, x0, y0, w, h, data


def secondary_tiff(cache, sql_gz):
    """Converts the secondary importance dump into a GeoTIFF, once."""
    out = Path(cache) / "nominatim-secondary-importance.tif"
    if out.exists():
        return str(out)
    tiles = list(_tiles(sql_gz))
    sx, sy = tiles[0][0], tiles[0][1]
    west = min(t[2] for t in tiles)
    north = max(t[3] for t in tiles)
    east = max(t[2] + t[4] * sx for t in tiles)
    south = min(t[3] + t[5] * sy for t in tiles)
    width = int(round((east - west) / sx))
    height = int(round((south - north) / sy))
    a = np.zeros((height, width), np.uint16)
    for _, _, x0, y0, w, h, data in tiles:
        c = int(round((x0 - west) / sx))
        r = int(round((y0 - north) / sy))
        a[r:r + h, c:c + w] = data
    tmp = out.with_suffix(".tmp")
    with rasterio.open(tmp, "w", driver="GTiff", width=width, height=height,
                       count=1, dtype="uint16", crs="EPSG:4326",
                       transform=from_origin(west, north, sx, -sy),
                       tiled=True, compress="deflate") as dst:
        dst.write(a, 1)
    tmp.rename(out)
    return str(out)


def importance(csv_gz):
    """Returns {Q-id number: highest importance over all languages}."""
    import pyarrow.compute as pc
    import pyarrow.csv as pacsv
    t = pacsv.read_csv(
        csv_gz,
        parse_options=pacsv.ParseOptions(delimiter="\t", quote_char=False),
        convert_options=pacsv.ConvertOptions(
            include_columns=["importance", "wikidata_id"]))
    ids = t["wikidata_id"]
    t = t.filter(pc.fill_null(pc.match_substring_regex(ids, r"^Q[0-9]+$"), False))
    qid = pc.cast(pc.utf8_slice_codeunits(t["wikidata_id"], 1), "int64",
                  safe=False).to_numpy(zero_copy_only=False)
    imp = t["importance"].to_numpy(zero_copy_only=False)
    ok = np.isfinite(imp) & (qid > 0)
    qid, imp = qid[ok], imp[ok]
    order = np.lexsort((imp, qid))
    qid, imp = qid[order], imp[order]
    last = np.r_[qid[1:] != qid[:-1], True]
    return dict(zip(qid[last].tolist(), imp[last].tolist()))


NAMES = {
    "qrank": "QRank",
    "importance": "Nominatim",
    "osmviews": "OSMViews",
    "secondary": "Area",
}


def run(qid, qrank, lon, lat, imp, osmviews, secondary, out_dir):
    """Ranks the same Wikidata items by QRank, Nominatim's primary
    importance, OSMViews and Nominatim's secondary importance.

    qid, qrank, lon, lat: the items with coordinates and a QRank
    imp:       {Q-id: importance}; items without one score zero
    osmviews, secondary: ranking functions (functions.py)
    """
    idx = np.flatnonzero(np.abs(lat) < 85)
    if len(idx) > samples.SIZE:
        idx = np.sort(np.random.default_rng(samples.SEED).choice(idx, samples.SIZE, replace=False))
    qid, lon, lat = qid[idx], lon[idx], lat[idx]
    scores = {
        "qrank": np.asarray(qrank[idx], np.float64),
        "importance": np.array([imp.get(int(q), 0.0) for q in qid]),
        "osmviews": osmviews.score(lon, lat),
        "secondary": secondary.score(lon, lat),
    }
    names = list(scores)
    res = {"items": len(qid),
           "withImportancePct": 100.0 * float((scores["importance"] > 0).mean())}
    for i, a in enumerate(names):
        for b in names[i + 1:]:
            res.setdefault(a, {})[b] = agreement_stats.kendall(scores[a], scores[b])
        res.setdefault(a, {})["tiedPct"] = agreement_stats.describe(scores[a])["tiedPct"]
    _write(res, names, Path(out_dir) / "nominatim-items.tex")
    return res


def _write(res, names, path):
    lines = [HEADER, "\\begin{tabular}{l" + "r" * (len(names) - 1) + "}\n", "\\hline\n",
             " & " + " & ".join(NAMES[n] for n in names[1:]) + " \\\\\n", "\\hline\n"]
    for i, a in enumerate(names[:-1]):
        cells = [f"{res[a][b]:.2f}" if j > i else "" for j, b in enumerate(names[1:], 1)]
        lines.append(NAMES[a] + " & " + " & ".join(cells) + " \\\\\n")
    lines += ["\\hline\n", "\\end{tabular}\n"]
    path.write_text("".join(lines))
