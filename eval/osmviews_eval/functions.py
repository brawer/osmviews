# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Ranking functions: each maps a longitude and latitude to a score.

OSMViews is one ranking function; population, night lights, and the
density of photos, Wikidata items, places or nature observations are
others. Each function is read at its own native resolution, so the
evaluation compares what a user of each dataset would actually get for
a point.
"""

import numpy as np
import rasterio
from rasterio.windows import Window

from . import cog, grid


class OSMViews:
    """Weekly views per km2 at the zoom-18 pixel of each point."""

    def __init__(self, path, width=262144):
        self.path, self.width = path, width

    def score(self, lon, lat):
        return np.expm1(cog.sample(self.path, self.width, lon, lat).astype(np.float64))


class LonLatRaster:
    """The value of the raster cell at each point of a plain lon/lat raster.

    If per_area is true, cell values are counts (people, watts) and are
    divided by the cell's ground area to give a density per km2.
    """

    def __init__(self, path, per_area, strip=256):
        self.path, self.per_area, self.strip = path, per_area, strip

    def score(self, lon, lat):
        lon, lat = np.asarray(lon, np.float64), np.asarray(lat, np.float64)
        out = np.zeros(len(lon))
        with rasterio.open(self.path) as src:
            t = src.transform
            col = np.floor((lon - t.c) / t.a).astype(np.int64)
            row = np.floor((lat - t.f) / t.e).astype(np.int64)
            inside = (col >= 0) & (col < src.width) & (row >= 0) & (row < src.height)
            idx = np.flatnonzero(inside)
            idx = idx[np.argsort(row[idx], kind="stable")]
            strips = row[idx] // self.strip
            bounds = np.flatnonzero(np.r_[True, strips[1:] != strips[:-1], True])
            for s, e in zip(bounds[:-1], bounds[1:]):
                part = idx[s:e]
                y0 = int(strips[s]) * self.strip
                h = min(self.strip, src.height - y0)
                data = src.read(1, window=Window(0, y0, src.width, h))
                out[part] = data[row[part] - y0, col[part]]
            out[~np.isfinite(out) | (out < 0)] = 0
            if self.per_area:
                cell = (grid.EARTH_RADIUS_KM ** 2 * np.radians(t.a) * np.radians(-t.e)
                        * np.cos(np.radians(lat)))
                out = np.where(inside, out / cell, 0)
        return out


class PointDensity:
    """Points of a dataset per km2, counted in Web Mercator pixels.

    The default pixel, 65,536 pixels across the world, is about 600 m wide
    at the equator. When the points being scored come from the same
    dataset, each one is left out of its own count.
    """

    def __init__(self, lon, lat, width=65536, chunk=1 << 24):
        self.width = width
        self.keys = np.concatenate([self._keys(lon[i:i + chunk], lat[i:i + chunk])
                                    for i in range(0, len(lon), chunk)])
        self.keys.sort()

    def _keys(self, lon, lat):
        lon, lat = np.asarray(lon, np.float64), np.asarray(lat, np.float64)
        lat = np.clip(lat, -grid.MAX_LAT + 1e-9, grid.MAX_LAT - 1e-9)
        col = np.clip(grid.mercator_col(self.width, lon).astype(np.int64), 0, self.width - 1)
        row = np.clip(grid.mercator_row(self.width, lat).astype(np.int64), 0, self.width - 1)
        return row * self.width + col

    def score(self, lon, lat, own=False):
        k = self._keys(lon, lat)
        n = (np.searchsorted(self.keys, k, side="right") -
             np.searchsorted(self.keys, k, side="left")).astype(np.float64)
        if own:
            n = np.maximum(n - 1, 0)
        area = grid.pixel_area_km2(self.width)[k // self.width]
        return n / area


class Random:
    """A uniformly random score, as a baseline for no agreement."""

    def __init__(self, seed=1):
        self.seed = seed

    def score(self, lon, lat):
        return np.random.default_rng(self.seed).uniform(size=len(lon))
