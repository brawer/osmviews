# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""How ranking functions behave, and how much two of them agree."""

import numpy as np
from scipy import stats as sps

TOP_PCT = 1.0


def kendall(a, b):
    """Kendall's tau-b, which accounts for ties."""
    return float(sps.kendalltau(a, b, variant="b").statistic)


def spearman(a, b):
    return float(sps.spearmanr(a, b).statistic)


def top_overlap(a, b, pct=TOP_PCT, seed=1):
    """Share of the top pct % of points by a that are also there by b.

    Takes exactly the top pct % of points by each function. Ties at the
    cut-off are broken at random, so a function that scores most points
    zero agrees with any other only by chance, not perfectly.
    """
    a, b = np.asarray(a), np.asarray(b)
    k = max(1, int(round(len(a) * pct / 100)))
    noise = np.random.default_rng(seed).random(len(a))
    ta = np.lexsort((noise, -a))[:k]
    tb = np.lexsort((noise[::-1], -b))[:k]
    return float(len(np.intersect1d(ta, tb)) / k)


def describe(x):
    """Properties of one function's scores over a sample set.

    zeroPct:     share of points scored zero, which cannot be ranked
    tiedPct:     share of points that share their score with another point
    zipf:        exponent s of score ~ rank^-s, fitted over the positive
                 scores from rank 10 to the 90th percentile of ranks
    """
    x = np.asarray(x, np.float64)
    _, inverse, counts = np.unique(x, return_inverse=True, return_counts=True)
    pos = np.sort(x[x > 0])[::-1]
    zipf = float("nan")
    if len(pos) > 100:
        r = np.arange(1, len(pos) + 1)
        sel = slice(9, int(0.9 * len(pos)))
        slope, _ = np.polyfit(np.log(r[sel]), np.log(pos[sel]), 1)
        zipf = float(-slope)
    return {
        "zeroPct": 100.0 * float((x == 0).mean()),
        "tiedPct": 100.0 * float((counts[inverse] > 1).mean()),
        "zipf": zipf,
    }
