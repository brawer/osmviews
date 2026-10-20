# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT

import tempfile
import unittest
from pathlib import Path

import numpy as np

from osmviews_eval import agreement_stats, cities, grid, ranking, samples
from osmviews_eval.functions import PointDensity, Random


class AgreementStatsTest(unittest.TestCase):
    def test_kendall(self):
        self.assertAlmostEqual(agreement_stats.kendall([1, 2, 3], [10, 20, 30]), 1.0)
        self.assertAlmostEqual(agreement_stats.kendall([1, 2, 3], [3, 2, 1]), -1.0)

    def test_top_overlap(self):
        a = np.arange(1000.0)
        self.assertEqual(agreement_stats.top_overlap(a, a), 1.0)
        self.assertEqual(agreement_stats.top_overlap(a, -a), 0.0)

    def test_top_overlap_ties_are_chance(self):
        # A function that scores almost every point zero must not agree
        # perfectly with everything.
        a = np.random.default_rng(3).random(100_000)
        b = np.zeros(100_000)
        self.assertLess(agreement_stats.top_overlap(a, b), 0.05)

    def test_describe(self):
        x = np.r_[np.arange(1.0, 1001), 0, 0]
        d = agreement_stats.describe(x)
        self.assertAlmostEqual(d["zeroPct"], 200 / 1002)
        self.assertAlmostEqual(d["tiedPct"], 200 / 1002)

    def test_power_law(self):
        rng = np.random.default_rng(1)
        pareto = (1 - rng.random(20000)) ** (-1 / 1.5)  # alpha = 2.5
        fit = agreement_stats.power_law(pareto)
        self.assertAlmostEqual(fit["alpha"], 2.5, delta=0.05)
        self.assertGreater(fit["tailPct"], 50)
        self.assertAlmostEqual(agreement_stats.describe(pareto)["zipf"],
                               1 / (fit["alpha"] - 1))

    def test_power_law_prefers_lognormal(self):
        x = np.random.default_rng(2).lognormal(0, 1, 50000)
        fit = agreement_stats.power_law(x, candidates=10)
        self.assertLess(fit["lognormalR"], 0)

    def test_power_law_too_few(self):
        self.assertIsNone(agreement_stats.power_law(np.zeros(1000)))


class PointDensityTest(unittest.TestCase):
    def test_leave_one_out(self):
        lon, lat = np.array([8.5, 8.5, 20.0]), np.array([47.4, 47.4, 0.0])
        d = PointDensity(lon, lat)
        area = grid.pixel_area_km2(65536)
        row = int(grid.mercator_row(65536, 47.4))
        self.assertAlmostEqual(d.score(lon[:1], lat[:1])[0], 2 / area[row])
        self.assertAlmostEqual(d.score(lon[:1], lat[:1], own=True)[0], 1 / area[row])
        self.assertEqual(d.score(np.array([-70.0]), np.array([10.0]))[0], 0)


class SamplesTest(unittest.TestCase):
    def test_draw_is_stable(self):
        lon = np.linspace(-170, 170, 1000)
        lat = np.linspace(-80, 80, 1000)
        a = samples.draw(lon, lat, n=10)
        b = samples.draw(lon, lat, n=10)
        self.assertEqual(list(a[0]), list(b[0]))
        self.assertTrue(np.all(np.abs(a[1]) < grid.MAX_LAT))


class RankingTest(unittest.TestCase):
    def test_score_and_run(self):
        rng = np.random.default_rng(1)
        src = (rng.uniform(0, 1, 500), rng.uniform(0, 1, 500))
        sets = {"land": (rng.uniform(0, 1, 300), rng.uniform(0, 1, 300)),
                "commons": (src[0][:200], src[1][:200])}

        class Lon:
            def score(self, lon, lat):
                return np.asarray(lon) * 10

        fns = {"osmviews": Lon, "photos": lambda: PointDensity(*src),
               "random": Random}
        with tempfile.TemporaryDirectory() as tmp:
            scores = ranking.score(sets, fns, Path(tmp) / "scores.npz")
            # Every commons point is in its own source, but not counted.
            own = PointDensity(*src).score(*sets["commons"], own=True)
            self.assertTrue(np.array_equal(scores["commons"]["photos"], own))
            again = ranking.score(sets, fns, Path(tmp) / "scores.npz")
            self.assertTrue(np.array_equal(again["land"]["random"], scores["land"]["random"]))
            ranking.MATRIX_SET, old = "land", ranking.MATRIX_SET
            try:
                res = ranking.run(scores, tmp)
            finally:
                ranking.MATRIX_SET = old
            self.assertEqual(res["land"]["points"], 300)
            self.assertAlmostEqual(res["land"]["osmviews"]["kendall"]["random"],
                                   res["land"]["random"]["kendall"]["osmviews"])
            table = (Path(tmp) / "ranking-agreement.tex").read_text()
            self.assertIn("Random land &", table)


class CitiesTest(unittest.TestCase):
    def test_window_sums(self):
        lon, lat = np.array([8.54]), np.array([47.37])
        r = int(grid.mercator_row(cities.LEVEL, lat[0]))
        c = int(grid.mercator_col(cities.LEVEL, lon[0]))
        cells = {(r, c): 5, (r + 1, c): 2,  # about 1.6 km apart: inside
                 (r, c + 10): 100}          # about 16 km away: outside

        class Grid:  # sparse, since the level has 16384 x 16384 pixels
            def __getitem__(self, idx):
                return np.array([cells.get(k, 0) for k in zip(*idx)])

        self.assertEqual(cities.window_sums(Grid(), lon, lat)[0], 7)

if __name__ == "__main__":
    unittest.main()
