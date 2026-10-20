# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Countries on the pixel grid of an OSMViews overview level.

Rasterizes the Natural Earth 1:10m country polygons, so evaluations can
restrict themselves to land and break results down by country or region.
"""

import zipfile
from pathlib import Path

import numpy as np
import shapefile
from rasterio import features
from rasterio.transform import Affine

from .grid import MAX_LAT, mercator_col, mercator_row


class Countries:
    """Countries rasterized at one level.

    ids:    int16 array, 0 outside any country, else 1 + index into `info`.
    info:   list of dicts with iso3, adm0, name, continent and subregion.
    """

    def __init__(self, ids, info):
        self.ids = ids
        self.info = info

    @property
    def land(self):
        return self.ids > 0

    def mask(self, key, value):
        """Returns the pixels whose country has info[key] == value."""
        hit = [i + 1 for i, c in enumerate(self.info) if c[key] == value]
        return np.isin(self.ids, hit)


def _iso3(rec):
    for key in ("ISO_A3_EH", "ISO_A3", "ADM0_A3"):
        v = rec.get(key, "")
        if v and v != "-99":
            return v
    return ""


def _project(width, geom):
    """Projects a GeoJSON-like polygon geometry into pixel coordinates."""
    def ring(coords):
        a = np.asarray(coords, np.float64)
        lat = np.clip(a[:, 1], -MAX_LAT, MAX_LAT)
        return np.c_[mercator_col(width, a[:, 0]), mercator_row(width, lat)].tolist()

    if geom["type"] == "Polygon":
        return {"type": "Polygon", "coordinates": [ring(r) for r in geom["coordinates"]]}
    return {"type": "MultiPolygon",
            "coordinates": [[ring(r) for r in p] for p in geom["coordinates"]]}


def load(zip_path, width):
    """Rasterizes ne_10m_admin_0_countries.zip at a level of the given width."""
    with zipfile.ZipFile(zip_path) as z:
        stem = Path([n for n in z.namelist() if n.endswith(".shp")][0]).stem
        files = {ext: z.open(f"{stem}.{ext}") for ext in ("shp", "shx", "dbf")}
        reader = shapefile.Reader(**files, encoding="utf-8")
        info, shapes = [], []
        for i, sr in enumerate(reader.iterShapeRecords()):
            rec = sr.record.as_dict()
            info.append({
                "iso3": _iso3(rec),
                "adm0": rec.get("ADM0_A3", ""),
                "name": rec.get("NAME", ""),
                "continent": rec.get("CONTINENT", ""),
                "subregion": rec.get("SUBREGION", ""),
            })
            shapes.append((_project(width, sr.shape.__geo_interface__), i + 1))
    ids = features.rasterize(shapes, out_shape=(width, width), fill=0,
                             transform=Affine.identity(), dtype="int16")
    return Countries(ids, info)
