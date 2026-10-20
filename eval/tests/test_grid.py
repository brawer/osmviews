# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT

import math
import tempfile
import unittest
from pathlib import Path

import numpy as np
import rasterio
from rasterio.transform import from_origin

from osmviews_eval import grid


class GridTest(unittest.TestCase):
    def test_pixel_area(self):
        # The level covers the Earth between the Mercator latitude limits.
        area = grid.pixel_area_km2(64).sum() * 64
        band = 4 * math.pi * grid.EARTH_RADIUS_KM ** 2 * math.sin(math.radians(grid.MAX_LAT))
        self.assertAlmostEqual(area / band, 1.0, places=9)

    def test_count_points(self):
        g = grid.count_points(4, [10.0, 11.0, -100.0], [1.0, 2.0, 1.0])
        self.assertEqual(int(round((g * grid.pixel_area_km2(4)[:, None]).sum())), 3)
        self.assertGreater(g[1, 2], g[1, 0])  # two points vs. one, same row

    def test_maxpool_lonlat(self):
        # One-degree cells; a single cell of 100 people near (5E, 45.5N).
        data = np.zeros((180, 360), np.float32)
        data[44, 185] = 100
        with tempfile.TemporaryDirectory() as d:
            path = Path(d) / "pop.tif"
            with rasterio.open(path, "w", driver="GTiff", width=360, height=180,
                               count=1, dtype="float32", crs="EPSG:4326",
                               transform=from_origin(-180, 90, 1, 1)) as f:
                f.write(data, 1)
            out = grid.maxpool_lonlat(path, 16, per_area=True)
        col, row = int(grid.mercator_col(16, 5.5)), int(grid.mercator_row(16, 45.5))
        self.assertEqual(int(np.argmax(out)), row * 16 + col)
        cell_km2 = grid.EARTH_RADIUS_KM ** 2 * math.radians(1) ** 2 * math.cos(math.radians(45.5))
        self.assertAlmostEqual(float(out.max()), 100 / cell_km2, places=4)


if __name__ == "__main__":
    unittest.main()
