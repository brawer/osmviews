# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Ranking functions evaluated on sample sets.

A ranking function maps a longitude and latitude to a score (functions.py);
a sample set is the points that get scored (samples.py). For every sample
set, this module scores all points with every function and reports how
each function behaves (how many points it cannot tell apart, how skewed
its scores are) and how much each pair of functions agrees on the order.

A density function scored on points of its own source leaves each point
out of its own count; otherwise every point would score at least one.
"""

from pathlib import Path

import numpy as np

from . import agreement_stats
from .results import HEADER

FUNCTION_NAMES = {
    "osmviews": "OSMViews",
    "population": "Population",
    "lights": "Night lights",
    "photos": "Photos",
    "items": "Wikidata",
    "places": "Places",
    "observations": "Nature",
    "random": "Random",
}

SET_NAMES = {
    "land": "Random land",
    "people": "Random people",
    "wikidata": "Wikidata items",
    "commons": "Commons photos",
    "places": "Overture places",
    "brands": "ATP brands",
    "infrastructure": "ATP infrastructure",
    "trees": "ATP trees",
    "governments": "ATP governments",
    "observations": "iNaturalist",
}

# Sample set drawn from the same source as a density function.
OWN = {"photos": "commons", "items": "wikidata", "places": "places",
       "observations": "observations"}

# The sample set whose full agreement matrix goes into the paper.
MATRIX_SET = "brands"


def score(sets, functions, path):
    """Scores every sample set with every function, cached at path.

    sets:      {name: (lon, lat)}
    functions: {name: callable returning an object with .score(lon, lat)};
               built one at a time, since point densities hold large indexes
    Returns {set: {function: scores}}.
    """
    names = list(sets)
    split = np.cumsum([0] + [len(sets[k][0]) for k in names])
    path = Path(path)
    cached = dict(np.load(path)) if path.exists() else {}
    if list(cached.get("sets", [])) != names or not np.array_equal(cached.get("split"), split):
        cached = {}
    lon = np.concatenate([np.asarray(sets[k][0], np.float64) for k in names])
    lat = np.concatenate([np.asarray(sets[k][1], np.float64) for k in names])
    for f, build in functions.items():
        if f in cached:
            continue
        fn = build()
        s = fn.score(lon, lat)
        if OWN.get(f) in names:
            i = names.index(OWN[f])
            a, b = split[i], split[i + 1]
            s[a:b] = fn.score(lon[a:b], lat[a:b], own=True)
        cached[f] = np.asarray(s, np.float64)
        del fn
        np.savez(path, sets=np.array(names), split=split,
                 **{k: v for k, v in cached.items() if k not in ("sets", "split")})
    return {k: {f: cached[f][split[i]:split[i + 1]] for f in functions}
            for i, k in enumerate(names)}


def run(scores, out_dir):
    """Computes the statistics and writes the paper's tables."""
    res = {}
    for k, by_fn in scores.items():
        fns = list(by_fn)
        r = {"points": len(next(iter(by_fn.values())))}
        tau = {}
        for i, f in enumerate(fns):
            for g in fns[i + 1:]:
                tau[f, g] = tau[g, f] = agreement_stats.kendall(by_fn[f], by_fn[g])
        for f in fns:
            d = agreement_stats.describe(by_fn[f])
            d["kendall"] = {g: tau[f, g] for g in fns if g != f}
            others = [tau[f, g] for g in fns if g not in (f, "random")]
            d["kendallMean"] = float(np.mean(others)) if others else float("nan")
            d["spearman"] = agreement_stats.spearman(by_fn[f], by_fn["osmviews"])
            d["topOverlap"] = agreement_stats.top_overlap(by_fn["osmviews"], by_fn[f])
            r[f] = d
        res[k] = r
    out_dir = Path(out_dir)
    _write_agreement(res, out_dir / "ranking-agreement.tex")
    _write_ties(res, out_dir / "ranking-ties.tex")
    _write_matrix(res[MATRIX_SET], out_dir / "ranking-matrix.tex")
    return res


def _fmt(x, digits=2):
    if x != x:  # NaN
        return "--"
    s = f"{x:.{digits}f}"
    if float(s) == 0:
        s = s.lstrip("-")
    return "$-$" + s[1:] if s.startswith("-") else s


def _table(columns, rows):
    lines = [HEADER, "\\begin{tabular}{l" + "r" * len(columns) + "}\n", "\\hline\n",
             " & " + " & ".join(columns) + " \\\\\n", "\\hline\n"]
    lines += [name + " & " + " & ".join(cells) + " \\\\\n" for name, cells in rows]
    lines += ["\\hline\n", "\\end{tabular}\n"]
    return "".join(lines)


def _write_agreement(res, path):
    """Kendall's tau of each function with OSMViews, per sample set."""
    sets = list(res)
    fns = [f for f in res[sets[0]] if f not in ("points", "osmviews")]
    rows = []
    for k in sets:
        cells = [_fmt(res[k][f]["kendall"]["osmviews"]) if f in res[k] else "--"
                 for f in fns]
        rows.append((SET_NAMES[k], cells))
    path.write_text(_table([FUNCTION_NAMES[f] for f in fns], rows))


def _write_ties(res, path):
    """Share of points each function cannot tell from another point."""
    sets = list(res)
    fns = [f for f in res[sets[0]] if f not in ("points", "random")]
    rows = [(SET_NAMES[k], [f"{res[k][f]['tiedPct']:.0f}" for f in fns]) for k in sets]
    path.write_text(_table([FUNCTION_NAMES[f] for f in fns], rows))


def _write_matrix(r, path):
    fns = [f for f in r if f not in ("points", "random")]
    rows = []
    for i, a in enumerate(fns[:-1]):
        cells = [_fmt(r[a]["kendall"][b]) if j > i else "" for j, b in enumerate(fns[1:], 1)]
        rows.append((FUNCTION_NAMES[a], cells))
    path.write_text(_table([FUNCTION_NAMES[f] for f in fns[1:]], rows))
