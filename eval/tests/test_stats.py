# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT

import unittest

from osmviews_eval.stats import ranks, spearman, weighted_spearman


class StatsTest(unittest.TestCase):
    def test_ranks_ties(self):
        self.assertEqual(list(ranks([10, 20, 10, 30])), [0.5, 2.0, 0.5, 3.0])

    def test_spearman(self):
        self.assertAlmostEqual(spearman([1, 2, 3, 4], [10, 20, 30, 40]), 1.0)
        self.assertAlmostEqual(spearman([1, 2, 3, 4], [4, 3, 2, 1]), -1.0)
        self.assertAlmostEqual(spearman([1, 2, 3, 4], [1, 3, 2, 4]), 0.8)

    def test_weighted_spearman_equal_weights(self):
        a, b = [1, 2, 3, 4, 4, 9], [3, 1, 2, 5, 4, 4]
        self.assertAlmostEqual(weighted_spearman(a, b, [2] * 6), spearman(a, b))

    def test_weighted_spearman_is_like_repeating(self):
        # Weight 2 on a pair is the same as listing that pair twice.
        a, b = [1, 2, 3, 4], [2, 1, 4, 3]
        self.assertAlmostEqual(
            weighted_spearman(a, b, [1, 2, 1, 1]),
            spearman([1, 2, 2, 3, 4], [2, 1, 1, 4, 3]))


if __name__ == "__main__":
    unittest.main()
