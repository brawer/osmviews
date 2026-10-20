# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Readers for the Wikimedia database dumps used by the evaluation.

The dumps are MySQL scripts with one row per line after each INSERT, so
a line-by-line regular expression is enough and keeps the setup light.
"""

import csv
import gzip
import re

import numpy as np

# gt_id, gt_page_id, gt_globe, gt_primary, gt_lat, gt_lon, ...
_GEO_TAG = re.compile(rb"^\((\d+),(\d+),'([^']*)',(\d),(-?[\d.]+),(-?[\d.]+),")

# page_id, page_namespace, page_title, ...
_PAGE = re.compile(rb"^\((\d+),(-?\d+),'((?:[^'\\]|\\.)*)',")


def geo_tags(path):
    """Returns page ids, latitudes and longitudes of primary Earth coordinates.

    In Wikimedia Commons, the primary coordinate of a file is where the
    camera stood; in Wikidata, it is the item's coordinate location (P625).
    """
    pages, lats, lons = [], [], []
    with gzip.open(path, "rb") as f:
        for line in f:
            m = _GEO_TAG.match(line)
            if not m or m.group(3) != b"earth" or m.group(4) != b"1":
                continue
            pages.append(int(m.group(2)))
            lats.append(float(m.group(5)))
            lons.append(float(m.group(6)))
    pages = np.asarray(pages, np.int64)
    # A page can carry several primary tags; keep the first of each.
    _, first = np.unique(pages, return_index=True)
    first.sort()
    return pages[first], np.asarray(lats)[first], np.asarray(lons)[first]


def item_pages(path, wanted):
    """Returns {page_id: Q-number} for the Wikidata items among `wanted`."""
    wanted = set(int(p) for p in wanted)
    out = {}
    with gzip.open(path, "rb") as f:
        for line in f:
            m = _PAGE.match(line)
            if not m or m.group(2) != b"0":
                continue
            page = int(m.group(1))
            if page in wanted and m.group(3).startswith(b"Q"):
                out[page] = int(m.group(3)[1:])
    return out


def qrank(path):
    """Returns {Q-number: QRank} from qrank.csv.gz."""
    out = {}
    with gzip.open(path, "rt") as f:
        reader = csv.reader(f)
        next(reader)
        for entity, rank in reader:
            out[int(entity[1:])] = int(rank)
    return out
