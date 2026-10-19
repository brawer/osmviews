# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Runs every evaluation and writes the paper's generated files.

Usage: python3 -m osmviews_eval CACHE_DIR OUT_DIR
"""

import sys
from pathlib import Path

from . import agreement, figures, inputs, results


def main(cache, out):
    out = Path(out)
    out.mkdir(parents=True, exist_ok=True)
    regular = inputs.resolve("osmviews", cache)
    filtered = inputs.resolve("osmviews-filtered", cache)

    res = {"agreement": agreement.run(regular, filtered)}

    # Lanes fanning out from Null Island over Africa and Europe.
    figures.compare(regular, filtered, 8192, (-10, -8, 45, 50), out / "fan.png")
    # Null Island itself.
    figures.compare(regular, filtered, 16384, (-12, -10, 12, 10), out / "null-island.png")

    results.write(res, out / "results.tex", out / "results.json")


if __name__ == "__main__":
    main(*sys.argv[1:3])
