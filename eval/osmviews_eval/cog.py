# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Minimal reader for the OSMViews Cloud-Optimized GeoTIFF.

Reads one resolution level into a numpy array. The file has one image per
level, 256x256 float32 tiles, deflate compression, little-endian classic
TIFF. Overview levels are max-pooled from the level below (see
cmd/osmviews-builder/raster.go), so sums over an overview are not view
counts; the evaluation compares ranks and densities instead.
"""

import math
import struct
import zlib

import numpy as np

_SIZES = {1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 11: 4, 12: 8, 16: 8}
_FORMATS = {1: "B", 3: "H", 4: "I", 11: "f", 12: "d", 16: "Q"}

WIDTH, TILE_WIDTH, OFFSETS, COUNTS, COMPRESSION = 256, 322, 324, 325, 259


def _ifds(f):
    f.seek(0)
    header = f.read(8)
    if header[:4] != b"II*\x00":
        raise ValueError("not a little-endian classic TIFF")
    offset = struct.unpack("<I", header[4:])[0]
    while offset:
        f.seek(offset)
        (n,) = struct.unpack("<H", f.read(2))
        tags = {}
        for _ in range(n):
            tag, typ, count, value = struct.unpack("<HHI4s", f.read(12))
            if typ not in _FORMATS:
                continue
            size = _SIZES[typ] * count
            if size > 4:
                pos = f.tell()
                f.seek(struct.unpack("<I", value)[0])
                data = f.read(size)
                f.seek(pos)
            else:
                data = value[:size]
            tags[tag] = struct.unpack(f"<{count}{_FORMATS[typ]}", data)
        (offset,) = struct.unpack("<I", f.read(4))
        yield tags


def levels(path):
    """Returns the pixel widths of the levels in the file."""
    with open(path, "rb") as f:
        return [t[WIDTH][0] for t in _ifds(f)]


def read_level(path, width, window=None):
    """Returns a level of the given pixel width as a float32 array.

    window = (x0, y0, x1, y1) in pixels restricts the result to that box,
    reading only the tiles it touches.
    """
    x0, y0, x1, y1 = window or (0, 0, width, width)
    with open(path, "rb") as f:
        for tags in _ifds(f):
            if tags[WIDTH][0] == width:
                break
        else:
            raise ValueError(f"{path}: no level of width {width}")
        tw = tags[TILE_WIDTH][0]
        n = width // tw
        out = np.zeros((y1 - y0, x1 - x0), np.float32)
        for row in range(y0 // tw, (y1 - 1) // tw + 1):
            for col in range(x0 // tw, (x1 - 1) // tw + 1):
                i = row * n + col
                f.seek(tags[OFFSETS][i])
                data = f.read(tags[COUNTS][i])
                if tags[COMPRESSION][0] == 8:
                    data = zlib.decompress(data)
                tile = np.frombuffer(data, np.float32).reshape(tw, tw)
                ya, yb = max(y0, row * tw), min(y1, (row + 1) * tw)
                xa, xb = max(x0, col * tw), min(x1, (col + 1) * tw)
                out[ya - y0:yb - y0, xa - x0:xb - x0] = (
                    tile[ya - row * tw:yb - row * tw, xa - col * tw:xb - col * tw])
        return out


def pixel(width, lon, lat):
    """Returns the (column, row) of lon/lat at a level of the given width."""
    x = (lon + 180) / 360 * width
    r = math.radians(lat)
    y = (1 - math.log(math.tan(r) + 1 / math.cos(r)) / math.pi) / 2 * width
    return int(x), int(y)


def lonlat(width, col, row):
    """Returns lon/lat arrays of pixel centers for column and row indices."""
    lon = (np.asarray(col) + 0.5) / width * 360 - 180
    lat = np.degrees(np.arctan(np.sinh(np.pi * (1 - 2 * (np.asarray(row) + 0.5) / width))))
    return lon, lat
