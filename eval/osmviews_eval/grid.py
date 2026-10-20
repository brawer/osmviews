# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Brings other data onto the pixel grid of an OSMViews overview level.

An OSMViews overview pixel holds the maximum density over the zoom-18
pixels it covers (cog.py). To compare like with like, a gridded reference
such as population is turned into a density per source cell and then
max-pooled onto the same Web Mercator pixels. Point data, such as photo
locations, is counted per pixel and divided by the pixel's ground area.
"""

import math

import numpy as np
import rasterio
from rasterio.windows import Window

EARTH_RADIUS_KM = 6371.0088
MAX_LAT = math.degrees(math.atan(math.sinh(math.pi)))  # edge of Web Mercator


def mercator_row(width, lat):
    """Returns the (fractional) pixel row of latitude lat at a level."""
    r = np.radians(np.asarray(lat, dtype=np.float64))
    return (1 - np.log(np.tan(r) + 1 / np.cos(r)) / np.pi) / 2 * width


def mercator_col(width, lon):
    return (np.asarray(lon, dtype=np.float64) + 180) / 360 * width


def pixel_area_km2(width):
    """Returns the ground area of each row of pixels at a level, in km2."""
    rows = np.arange(width)
    north = np.arctan(np.sinh(np.pi * (1 - 2 * rows / width)))
    south = np.arctan(np.sinh(np.pi * (1 - 2 * (rows + 1) / width)))
    return EARTH_RADIUS_KM ** 2 * (2 * np.pi / width) * (np.sin(north) - np.sin(south))


def _groups(index):
    """Returns the start positions of runs of equal values in a sorted index."""
    starts = np.flatnonzero(np.r_[True, index[1:] != index[:-1]])
    return starts, index[starts]


def maxpool_lonlat(path, width, per_area, strip=512):
    """Max-pools a raster in plain longitude/latitude onto a Mercator level.

    The source must be north-up in EPSG:4326, with cells much smaller than
    the target pixels. If per_area is true, each cell value is a count (such
    as people) and is divided by the cell's ground area first, giving a
    density per km2; otherwise the value already is a density (such as
    radiance). Negative and non-finite values count as zero.
    """
    out = np.zeros((width, width), np.float32)
    with rasterio.open(path) as src:
        t = src.transform
        if t.b != 0 or t.d != 0 or t.e >= 0:
            raise ValueError(f"{path}: not a north-up lon/lat raster")
        lon = t.c + (np.arange(src.width) + 0.5) * t.a
        cols = np.clip(mercator_col(width, lon).astype(np.int64), 0, width - 1)
        col_starts, col_ids = _groups(cols)
        cell_km2_at_equator = (EARTH_RADIUS_KM ** 2 * math.radians(t.a) *
                               math.radians(-t.e))
        for y0 in range(0, src.height, strip):
            h = min(strip, src.height - y0)
            lat = t.f + (y0 + np.arange(h) + 0.5) * t.e
            inside = np.abs(lat) < MAX_LAT
            if not inside.any():
                continue
            data = src.read(1, window=Window(0, y0, src.width, h)).astype(np.float32)
            data[~np.isfinite(data) | (data < 0)] = 0
            if per_area:
                area = cell_km2_at_equator * np.cos(np.radians(lat))
                data /= area[:, None].astype(np.float32)
            data, lat = data[inside], lat[inside]
            rows = np.clip(mercator_row(width, lat).astype(np.int64), 0, width - 1)
            pooled = np.maximum.reduceat(data, col_starts, axis=1)
            row_starts, row_ids = _groups(rows)
            pooled = np.maximum.reduceat(pooled, row_starts, axis=0)
            block = out[row_ids[:, None], col_ids[None, :]]
            out[row_ids[:, None], col_ids[None, :]] = np.maximum(block, pooled)
    return out


def count_points(width, lon, lat, weights=None):
    """Returns the number (or summed weight) of points per pixel per km2."""
    lon, lat = np.asarray(lon, np.float64), np.asarray(lat, np.float64)
    ok = np.isfinite(lon) & np.isfinite(lat) & (np.abs(lat) < MAX_LAT) & (np.abs(lon) <= 180)
    cols = np.clip(mercator_col(width, lon[ok]).astype(np.int64), 0, width - 1)
    rows = np.clip(mercator_row(width, lat[ok]).astype(np.int64), 0, width - 1)
    w = None if weights is None else np.asarray(weights, np.float64)[ok]
    counts = np.bincount(rows * width + cols, weights=w, minlength=width * width)
    counts = counts.reshape(width, width)
    return (counts / pixel_area_km2(width)[:, None]).astype(np.float32)
