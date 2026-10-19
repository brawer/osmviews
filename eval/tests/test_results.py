# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT

import json
import tempfile
import unittest
from pathlib import Path

from osmviews_eval import results


class ResultsTest(unittest.TestCase):
    def test_write(self):
        with tempfile.TemporaryDirectory() as d:
            tex, js = Path(d) / "r.tex", Path(d) / "r.json"
            results.write({"agreement": {"changedPct": 1.5, "pixels": 1234567, "spearman": 0.99876}}, tex, js)
            text = tex.read_text()
            self.assertIn(r"\newcommand{\resAgreementChangedPct}{1.5}", text)
            self.assertIn(r"\newcommand{\resAgreementPixels}{1{,}234{,}567}", text)
            self.assertIn(r"\newcommand{\resAgreementSpearman}{0.999}", text)
            self.assertEqual(json.loads(js.read_text())["agreement"]["pixels"], 1234567)


if __name__ == "__main__":
    unittest.main()
