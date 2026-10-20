# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Sample sets: the geographic points that the ranking functions score.

Which points we rank changes what agreement between ranking functions
means. Random points on land mostly test whether a function separates
towns from wilderness; points from a dataset that people want to rank,
such as places of interest or nature observations, test whether it
separates busy from quiet places among the ones that matter.

Every loader returns (lon, lat) arrays, cached in the cache directory,
and every sample set is drawn from them with a fixed seed.
"""

import csv
import io
import math
import re
import shutil
import urllib.request
import zipfile
from pathlib import Path

import numpy as np
import rasterio
from rasterio.windows import Window

from . import grid, wikimedia

SEED = 20261020
SIZE = 200_000  # points per sample set


def _cached(cache, name, build):
    path = Path(cache) / f"points-{name}.npz"
    if path.exists():
        z = np.load(path)
        return z["lon"], z["lat"]
    lon, lat = build()
    np.savez(path, lon=np.asarray(lon), lat=np.asarray(lat))
    return lon, lat


def draw(lon, lat, n=SIZE, seed=SEED):
    """Returns a random subset of n points, the same on every run."""
    lon, lat = np.asarray(lon), np.asarray(lat)
    ok = np.isfinite(lon) & np.isfinite(lat) & (np.abs(lat) < grid.MAX_LAT)
    idx = np.flatnonzero(ok)
    if len(idx) > n:
        idx = np.sort(np.random.default_rng(seed).choice(idx, n, replace=False))
    return lon[idx], lat[idx]


def random_land(cache, countries, n=SIZE, seed=SEED):
    """Points spread evenly by area over land outside Antarctica."""
    return _cached(cache, "land", lambda: _random_land(countries, n, seed))


def _random_land(countries, n, seed):
    rng = np.random.default_rng(seed)
    land = countries.land & ~countries.mask("continent", "Antarctica")
    width = land.shape[0]
    s = math.sin(math.radians(grid.MAX_LAT))
    lons, lats = [], []
    while sum(len(x) for x in lons) < n:
        lon = rng.uniform(-180, 180, 4 * n)
        lat = np.degrees(np.arcsin(rng.uniform(-s, s, 4 * n)))
        col = np.clip(grid.mercator_col(width, lon).astype(int), 0, width - 1)
        row = np.clip(grid.mercator_row(width, lat).astype(int), 0, width - 1)
        keep = land[row, col]
        lons.append(lon[keep])
        lats.append(lat[keep])
    return np.concatenate(lons)[:n], np.concatenate(lats)[:n]


def random_people(cache, ghsl, n=SIZE, seed=SEED):
    """Points drawn with probability proportional to population (GHS-POP).

    Picks population cells at the raster's own resolution, then a uniform
    position within each cell.
    """
    return _cached(cache, "people", lambda: _random_people(ghsl, n, seed))


def _random_people(ghsl, n, seed, strip=512):
    rng = np.random.default_rng(seed)
    with rasterio.open(ghsl) as src:
        t = src.transform
        totals = []
        for y0 in range(0, src.height, strip):
            h = min(strip, src.height - y0)
            d = src.read(1, window=Window(0, y0, src.width, h))
            d[~np.isfinite(d) | (d < 0)] = 0
            lat = t.f + (y0 + np.arange(h) + 0.5) * t.e
            d[np.abs(lat) >= grid.MAX_LAT] = 0
            totals.append(float(d.sum()))
        cum = np.cumsum(totals)
        targets = np.sort(rng.uniform(0, cum[-1], n))
        lons, lats = [], []
        start = 0
        for k, y0 in enumerate(range(0, src.height, strip)):
            end = np.searchsorted(targets, cum[k], side="right")
            if end > start:
                h = min(strip, src.height - y0)
                d = src.read(1, window=Window(0, y0, src.width, h))
                d[~np.isfinite(d) | (d < 0)] = 0
                lat = t.f + (y0 + np.arange(h) + 0.5) * t.e
                d[np.abs(lat) >= grid.MAX_LAT] = 0
                flat = np.cumsum(d.ravel(), dtype=np.float64)
                base = cum[k - 1] if k else 0.0
                cells = np.searchsorted(flat, targets[start:end] - base, side="right")
                cells = np.minimum(cells, len(flat) - 1)
                r, c = np.divmod(cells, src.width)
                lons.append(t.c + (c + rng.uniform(0, 1, len(c))) * t.a)
                lats.append(t.f + (y0 + r + rng.uniform(0, 1, len(r))) * t.e)
            start = end
    return np.concatenate(lons), np.concatenate(lats)


def geo_tags(cache, name, path):
    """All primary coordinates of a Wikimedia geo_tags dump."""
    def build():
        _, lat, lon = wikimedia.geo_tags(path)
        return lon, lat
    return _cached(cache, name, build)


OVERTURE_MIN_CONFIDENCE = 0.5


def overture_places(cache, release_url, parts):
    """Overture Maps places with confidence of at least 0.5.

    Downloads one part at a time into the cache, keeps only the position
    and confidence, and deletes the part again.
    """
    def build():
        import pyarrow.compute as pc
        import pyarrow.parquet as pq
        lons, lats = [], []
        tmp = Path(cache) / "overture-part.parquet"
        for part in parts:
            with urllib.request.urlopen(f"{release_url}/{part}") as r, open(tmp, "wb") as out:
                shutil.copyfileobj(r, out, 1 << 24)
            t = pq.read_table(tmp, columns=["bbox", "confidence"])
            tmp.unlink()
            t = t.filter(pc.greater_equal(t["confidence"], OVERTURE_MIN_CONFIDENCE))
            bbox = t["bbox"].combine_chunks()
            lons.append(np.asarray(bbox.field("xmin")))
            lats.append(np.asarray(bbox.field("ymin")))
        return np.concatenate(lons), np.concatenate(lats)
    return _cached(cache, "overture", build)


def inaturalist(cache, url, etag=None):
    """Locations of iNaturalist observations with an open licence.

    Streams the observations table instead of storing it; the coordinates
    of sensitive species are already obscured by iNaturalist. If etag is
    given, refuses a file that has changed since it was pinned.
    """
    def build():
        import pyarrow as pa
        import pyarrow.csv as pacsv
        lons, lats = [], []
        with urllib.request.urlopen(url) as r:
            if etag and r.headers.get("ETag") != etag:
                raise SystemExit(f"{url} has changed since it was pinned")
            stream = pa.input_stream(r, compression="gzip")
            reader = pacsv.open_csv(
                stream,
                read_options=pacsv.ReadOptions(block_size=64 << 20),
                parse_options=pacsv.ParseOptions(delimiter="\t", quote_char=False),
                convert_options=pacsv.ConvertOptions(
                    include_columns=["latitude", "longitude"],
                    column_types={"latitude": pa.float64(), "longitude": pa.float64()}),
            )
            for batch in reader:
                lats.append(batch.column("latitude").to_numpy(zero_copy_only=False).astype(np.float32))
                lons.append(batch.column("longitude").to_numpy(zero_copy_only=False).astype(np.float32))
        lon, lat = np.concatenate(lons), np.concatenate(lats)
        ok = np.isfinite(lon) & np.isfinite(lat)
        return lon[ok], lat[ok]
    return _cached(cache, "inaturalist", build)


def geonames_cities(zip_path):
    """Returns names, lon, lat and population from GeoNames citiesNNNNN.zip.

    Leaves out sections of cities (feature code PPLX), such as the
    arrondissements of Paris, which would count the same city many times.
    """
    with zipfile.ZipFile(zip_path) as z:
        name = next(n for n in z.namelist() if n.endswith(".txt"))
        text = io.TextIOWrapper(z.open(name), encoding="utf-8")
        names, lons, lats, pops = [], [], [], []
        for row in csv.reader(text, delimiter="\t", quoting=csv.QUOTE_NONE):
            if row[7] == "PPLX":
                continue
            names.append(row[1])
            lats.append(float(row[4]))
            lons.append(float(row[5]))
            pops.append(int(row[14] or 0))
    return names, np.array(lons), np.array(lats), np.array(pops)


ATP_GROUPS = ("brands", "infrastructure", "trees", "governments")
_ATP_LINEAGE = {
    b"S_ATP_BRANDS": 0,
    b"S_ATP_INFRASTRUCTURE": 1,
    b"S_ATP_GOVERNMENTS": 3,
}


def alltheplaces(cache, zip_path):
    """AllThePlaces points by kind of source, with trees on their own.

    Each AllThePlaces spider declares its lineage. brands are chain stores
    and other branded places; infrastructure comes mostly from municipal
    GIS data, such as street lamps, benches and hydrants; governments are
    public offices and services. Trees (natural=tree, from municipal tree
    inventories) form their own group whatever the lineage. Address-only
    spiders are left out.
    Returns {group: (lon, lat)}.
    """
    path = Path(cache) / "points-alltheplaces.npz"
    if not path.exists():
        import orjson
        lons, lats, groups = [], [], []
        with zipfile.ZipFile(zip_path) as z:
            for info in z.infolist():
                if not info.filename.endswith(".geojson"):
                    continue
                with z.open(info) as f:
                    head = f.readline()
                    m = re.search(rb'"spider:lineage":"([^"]*)"', head)
                    group = _ATP_LINEAGE.get(m.group(1) if m else b"")
                    if group is None:
                        continue
                    for line in f:
                        line = line.strip().rstrip(b",")
                        if not line.startswith(b"{"):
                            continue
                        try:
                            feat = orjson.loads(line)
                        except orjson.JSONDecodeError:
                            continue
                        geom = feat.get("geometry") or {}
                        if geom.get("type") != "Point":
                            continue
                        lon, lat = geom["coordinates"][:2]
                        props = feat.get("properties") or {}
                        lons.append(lon)
                        lats.append(lat)
                        groups.append(2 if props.get("natural") == "tree" else group)
        np.savez(path, lon=np.array(lons), lat=np.array(lats), group=np.array(groups, np.int8))
    z = np.load(path)
    return {g: (z["lon"][z["group"] == i], z["lat"][z["group"] == i])
            for i, g in enumerate(ATP_GROUPS)}
