# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Coverage by country: OSMViews views compared with internet users.

Each pixel's views (summed from the full-resolution level, cog.views) go
to the country it lies in. A country's internet users are its population
times the share of individuals using the internet, both from the World
Bank, for the latest year since 2020 that has both values. The ratio of a country's
share of views to its share of internet users shows which countries the
dataset over- or under-represents.
"""

import csv
import io
import zipfile
from pathlib import Path

import numpy as np

from .results import HEADER
from .stats import spearman

MIN_YEAR = 2020        # older internet-use figures are left out
MIN_USERS = 1_000_000  # countries with fewer internet users are left out of the table
TABLE_ROWS = 8


def world_bank(zip_path):
    """Returns {ISO3: {year: value}} from a World Bank indicator CSV zip."""
    with zipfile.ZipFile(zip_path) as z:
        name = next(n for n in z.namelist() if n.startswith("API_"))
        text = z.read(name).decode("utf-8-sig")
    rows = list(csv.reader(io.StringIO(text)))
    header = next(i for i, r in enumerate(rows) if r and r[0] == "Country Name")
    years = rows[header]
    out = {}
    for r in rows[header + 1:]:
        if len(r) < 5:
            continue
        vals = {int(y): float(v) for y, v in zip(years[4:], r[4:]) if y.isdigit() and v}
        out[r[1]] = vals
    return out


def internet_users(internet_zip, population_zip):
    """Returns {ISO3: (year, internet users)} for the latest year with both."""
    share, pop = world_bank(internet_zip), world_bank(population_zip)
    out = {}
    for iso3, s in share.items():
        years = sorted(y for y in set(s) & set(pop.get(iso3, {})) if y >= MIN_YEAR)
        if years and s[years[-1]] > 0:
            y = years[-1]
            out[iso3] = (y, s[y] / 100 * pop[iso3][y])
    return out


def run(views, countries, users, out_dir):
    """views: weekly views per pixel at the level of `countries`."""
    per_country = np.bincount(countries.ids.ravel(), weights=views.ravel(),
                              minlength=len(countries.info) + 1)
    total = views.sum()
    # Natural Earth splits some countries into several features with the
    # same ISO code, such as overseas islands; add them up.
    by_iso3, names = {}, {}
    for i, c in enumerate(countries.info):
        iso3 = c["iso3"]
        if iso3 in users:
            by_iso3[iso3] = by_iso3.get(iso3, 0.0) + per_country[i + 1]
            if iso3 not in names or c["adm0"] == iso3:
                names[iso3] = c["name"]
    rows = [(names[k], k, by_iso3[k], users[k][1], users[k][0]) for k in sorted(by_iso3)]
    v = np.array([r[2] for r in rows])
    u = np.array([r[3] for r in rows])
    ratio = (v / v.sum()) / (u / u.sum())
    big = [i for i, r in enumerate(rows) if r[3] >= MIN_USERS]
    order = sorted(big, key=lambda i: ratio[i])
    res = {
        "countries": len(rows),
        "spearman": spearman(v, u),
        "inCountriesPct": 100.0 * float(per_country[1:].sum() / total),
        "years": f"{min(r[4] for r in rows)}--{max(r[4] for r in rows)}",
        "medianYear": str(int(np.median([r[4] for r in rows]))),
        "minUsersMillions": MIN_USERS // 1_000_000,
    }
    _write_table([(rows[i][0], ratio[i]) for i in order[-TABLE_ROWS:][::-1]],
                 [(rows[i][0], ratio[i]) for i in order[:TABLE_ROWS]],
                 Path(out_dir) / "coverage-countries.tex")
    return res


def _write_table(over, under, path):
    lines = [
        HEADER,
        "\\begin{tabular}{lrlr}\n\\hline\n",
        "\\multicolumn{2}{l}{Most views per user} & "
        "\\multicolumn{2}{l}{Fewest views per user} \\\\\n\\hline\n",
    ]
    for (a, ra), (b, rb) in zip(over, under):
        lines.append(f"{_tex(a)} & {ra:.2f} & {_tex(b)} & {rb:.2f} \\\\\n")
    lines.append("\\hline\n\\end{tabular}\n")
    path.write_text("".join(lines))


def _tex(s):
    return s.replace("&", "\\&")
