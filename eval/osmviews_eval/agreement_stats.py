# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""How ranking functions behave, and how much two of them agree."""

import numpy as np
from scipy import optimize, special
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


def power_law(x, candidates=50, min_tail=100):
    """Fits a power law to the upper tail of x, after Clauset et al. (2009).

    For each candidate lower bound xmin (quantiles of the positive values),
    fits the continuous power law p(x) ~ x^-alpha above xmin by maximum
    likelihood and keeps the xmin whose fit is closest to the data by the
    Kolmogorov-Smirnov distance. Then compares it with a lognormal fitted
    to the same tail, by the normalized log-likelihood ratio of Vuong's
    test: positive values favour the power law, negative the lognormal.

    Returns {alpha, xmin, tailPct, ks, lognormalR, lognormalP}, or None
    if there are too few positive values.
    """
    x = np.sort(np.asarray(x, np.float64))
    pos = x[x > 0]
    if len(pos) < min_tail:
        return None
    best = None
    for xmin in np.unique(np.quantile(pos, np.linspace(0, 0.99, candidates))):
        tail = pos[pos >= xmin]
        n = len(tail)
        if n < min_tail:
            break
        logs = np.log(tail / xmin)
        if logs.sum() <= 0:
            continue
        alpha = 1 + n / logs.sum()
        emp = np.arange(n) / n
        fit = 1 - (tail / xmin) ** (1 - alpha)
        ks = float(np.max(np.abs(emp - fit)))
        if best is None or ks < best[0]:
            best = (ks, xmin, alpha, tail)
    if best is None:
        return None
    ks, xmin, alpha, tail = best
    n = len(tail)
    lp = np.log(alpha - 1) - np.log(xmin) - alpha * np.log(tail / xmin)
    ll = _lognormal_tail(tail, xmin)
    d = lp - ll
    sd = d.std()
    r = float(d.sum() / (np.sqrt(n) * sd)) if sd > 0 else 0.0
    return {
        "alpha": float(alpha),
        "xmin": float(xmin),
        "tailPct": 100.0 * n / len(x),
        "ks": ks,
        "lognormalR": r,
        "lognormalP": float(special.erfc(abs(r) / np.sqrt(2))),
    }


def _lognormal_tail(tail, xmin):
    """Per-point log-likelihood of a lognormal truncated below at xmin."""
    logs = np.log(tail)

    def nll(p):
        mu, sigma = p[0], np.exp(p[1])
        z0 = (np.log(xmin) - mu) / sigma
        lsf = special.log_ndtr(-z0)
        z = (logs - mu) / sigma
        return -(np.sum(-0.5 * z * z - np.log(sigma) - logs) - len(tail) * (
            0.5 * np.log(2 * np.pi) + lsf))

    res = optimize.minimize(nll, [logs.mean(), np.log(logs.std() + 1e-9)],
                            method="Nelder-Mead")
    mu, sigma = res.x[0], np.exp(res.x[1])
    z = (logs - mu) / sigma
    z0 = (np.log(xmin) - mu) / sigma
    return (-0.5 * z * z - np.log(sigma) - logs - 0.5 * np.log(2 * np.pi)
            - special.log_ndtr(-z0))


def describe(x):
    """Properties of one function's scores over a sample set.

    zeroPct:     share of points scored zero, which cannot be ranked
    tiedPct:     share of points that share their score with another point
    zipf:        exponent s of score ~ rank^-s in the power-law tail, which
                 is 1 / (alpha - 1) for the fitted density exponent alpha
    powerLaw:    the tail fit of power_law()
    """
    x = np.asarray(x, np.float64)
    _, inverse, counts = np.unique(x, return_inverse=True, return_counts=True)
    fit = power_law(x)
    res = {
        "zeroPct": 100.0 * float((x == 0).mean()),
        "tiedPct": 100.0 * float((counts[inverse] > 1).mean()),
        "zipf": 1 / (fit["alpha"] - 1) if fit else float("nan"),
    }
    if fit:
        res["powerLaw"] = fit
    return res
