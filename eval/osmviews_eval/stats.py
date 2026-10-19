# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Small statistics helpers, kept to numpy so the setup stays light."""

import numpy as np


def ranks(a):
    """Returns the ranks of a, with ties getting their average rank."""
    a = np.asarray(a)
    order = np.argsort(a, kind="stable")
    sorted_a = a[order]
    first = np.r_[True, sorted_a[1:] != sorted_a[:-1]]
    group = np.cumsum(first) - 1
    starts = np.flatnonzero(first)
    ends = np.r_[starts[1:], len(a)]
    avg = (starts + ends - 1) / 2.0
    out = np.empty(len(a))
    out[order] = avg[group]
    return out


def spearman(a, b):
    """Returns Spearman's rank correlation of a and b."""
    ra, rb = ranks(a), ranks(b)
    ra -= ra.mean()
    rb -= rb.mean()
    return float((ra * rb).sum() / np.sqrt((ra * ra).sum() * (rb * rb).sum()))


def weighted_ranks(a, w):
    """Returns weighted mid-ranks of a, scaled to (0, 1).

    Each value's rank is the total weight of smaller values plus half the
    weight of equal values, divided by the total weight. With equal weights
    this is an affine function of ranks().
    """
    a, w = np.asarray(a), np.asarray(w, np.float64)
    order = np.argsort(a, kind="stable")
    sorted_a, sorted_w = a[order], w[order]
    first = np.r_[True, sorted_a[1:] != sorted_a[:-1]]
    group = np.cumsum(first) - 1
    group_w = np.bincount(group, weights=sorted_w)
    below = np.r_[0.0, np.cumsum(group_w)[:-1]]
    mid = (below + group_w / 2) / w.sum()
    out = np.empty(len(a))
    out[order] = mid[group]
    return out


def weighted_spearman(a, b, w):
    """Returns Spearman's rank correlation of a and b with weights w.

    Used with each pixel's ground area as its weight, so that pixels near
    the poles, which Web Mercator makes small, count for their true area.
    """
    w = np.asarray(w, np.float64)
    ra, rb = weighted_ranks(a, w), weighted_ranks(b, w)
    ra -= np.average(ra, weights=w)
    rb -= np.average(rb, weights=w)
    return float((w * ra * rb).sum() / np.sqrt((w * ra * ra).sum() * (w * rb * rb).sum()))
