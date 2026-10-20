# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT

import gzip
import struct
import tempfile
import unittest
from pathlib import Path

import numpy as np

from osmviews_eval import nominatim
from osmviews_eval.functions import LonLatRaster


def _tile(x0, y0, data):
    h, w = data.shape
    b = struct.pack("<BHH6diHH", 1, 0, 1, 0.5, -0.25, x0, y0, 0, 0, 4326, w, h)
    b += bytes([0x46]) + b"\0\0" + data.astype("<u2").tobytes()
    return b.hex().upper()


class NominatimTest(unittest.TestCase):
    def test_secondary_tiff(self):
        a = np.arange(8, dtype=np.uint16).reshape(2, 4)
        sql = "\n".join([
            "BEGIN;",
            'COPY "public"."secondary_importance" ("rast") FROM stdin;',
            _tile(10.0, 50.0, a),
            _tile(12.0, 50.0, a + 100),  # east of the first tile
            "\\.", "COMMIT;", ""])
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "secondary.sql.gz"
            with gzip.open(path, "wt") as f:
                f.write(sql)
            tif = nominatim.secondary_tiff(tmp, path)
            f = LonLatRaster(tif, per_area=False)
            # Cells are 0.5 by 0.25 degrees; the first row starts at 50 N.
            lon = np.array([10.1, 11.6, 12.1, 13.9, 10.1, 20.0])
            lat = np.array([49.9, 49.9, 49.9, 49.6, 49.6, 49.9])
            self.assertEqual(list(f.score(lon, lat)), [0, 3, 100, 107, 4, 0])

    def test_importance(self):
        csv = ("language\ttype\ttitle\timportance\twikidata_id\n"
               "en\ta\tParis\t0.9\tQ90\n"
               "fr\ta\tParis\t0.8\tQ90\n"
               "en\ta\tZurich\t0.7\tQ72\n"
               "en\ta\tNothing\t0.1\t\n")
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "importance.csv.gz"
            with gzip.open(path, "wt") as f:
                f.write(csv)
            self.assertEqual(nominatim.importance(path), {90: 0.9, 72: 0.7})


if __name__ == "__main__":
    unittest.main()
