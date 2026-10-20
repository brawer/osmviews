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


def sample(path, width, lon, lat):
    """Returns the pixel values at points lon/lat on a level, as float32.

    Decodes each tile that holds a point once, so it scales to millions of
    points on a fine level that would not fit in memory as a whole.
    """
    lon, lat = np.asarray(lon, np.float64), np.asarray(lat, np.float64)
    lat = np.clip(lat, -85.05112877, 85.05112877)
    col = np.clip(((lon + 180) / 360 * width).astype(np.int64), 0, width - 1)
    r = np.radians(lat)
    row = (1 - np.log(np.tan(r) + 1 / np.cos(r)) / np.pi) / 2 * width
    row = np.clip(row.astype(np.int64), 0, width - 1)
    out = np.zeros(len(lon), np.float32)
    with open(path, "rb") as f:
        for tags in _ifds(f):
            if tags[WIDTH][0] == width:
                break
        else:
            raise ValueError(f"{path}: no level of width {width}")
        tw = tags[TILE_WIDTH][0]
        tile = (row // tw) * (width // tw) + col // tw
        order = np.argsort(tile, kind="stable")
        starts = np.flatnonzero(np.r_[True, tile[order][1:] != tile[order][:-1]])
        ends = np.r_[starts[1:], len(order)]
        for s, e in zip(starts, ends):
            idx = order[s:e]
            i = tile[idx[0]]
            f.seek(tags[OFFSETS][i])
            data = f.read(tags[COUNTS][i])
            if not data:
                continue
            if tags[COMPRESSION][0] == 8:
                data = zlib.decompress(data)
            t = np.frombuffer(data, np.float32).reshape(tw, tw)
            out[idx] = t[row[idx] % tw, col[idx] % tw]
    return out


def views(path, width):
    """Returns weekly user views per pixel at a level, summed from the base.

    Pixels of the base level hold ln(1 + views per km2); undoing the
    logarithm and multiplying by each base pixel's ground area gives views,
    which are then summed (not max-pooled) into pixels of the given width.
    Reads the whole base level, so it takes a few minutes.
    """
    with open(path, "rb") as f:
        base = max(_ifds(f), key=lambda t: t[WIDTH][0])
        bw, tw = base[WIDTH][0], base[TILE_WIDTH][0]
        if bw % width or (bw // width) > tw or tw % (bw // width):
            raise ValueError(f"cannot sum {bw} pixels into {width}")
        k = bw // width            # base pixels per output pixel
        per_tile = tw // k         # output pixels per tile
        n = bw // tw
        rows = np.arange(bw, dtype=np.float64)
        north = np.arctan(np.sinh(np.pi * (1 - 2 * rows / bw)))
        south = np.arctan(np.sinh(np.pi * (1 - 2 * (rows + 1) / bw)))
        area = (6371.0088 ** 2 * (2 * np.pi / bw) * (np.sin(north) - np.sin(south)))
        area = area.astype(np.float32)
        out = np.zeros((width, width), np.float64)
        for i, (offset, count) in enumerate(zip(base[OFFSETS], base[COUNTS])):
            if not count:
                continue
            f.seek(offset)
            data = f.read(count)
            if base[COMPRESSION][0] == 8:
                data = zlib.decompress(data)
            t = np.frombuffer(data, np.float32).reshape(tw, tw)
            if not t.any():
                continue
            row, col = divmod(i, n)
            v = np.expm1(t) * area[row * tw:(row + 1) * tw, None]
            s = v.reshape(per_tile, k, per_tile, k).sum(axis=(1, 3))
            out[row * per_tile:(row + 1) * per_tile, col * per_tile:(col + 1) * per_tile] = s
    return out
