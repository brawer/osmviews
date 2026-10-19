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
