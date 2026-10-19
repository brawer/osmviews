# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT

import gzip
import tempfile
import unittest
from pathlib import Path

from osmviews_eval import wikimedia

GEO_TAGS = b"""\
INSERT INTO `geo_tags` VALUES
(1,10,'earth',1,47.37000000,8.54000000,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
(2,10,'earth',1,1.00000000,2.00000000,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
(3,11,'earth',0,5.00000000,6.00000000,NULL,NULL,'a,b',NULL,NULL,NULL,NULL),
(4,12,'moon',1,5.00000000,6.00000000,NULL,NULL,NULL,NULL,NULL,NULL,NULL),
(5,13,'earth',1,-33.80000000,151.20000000,NULL,NULL,NULL,NULL,NULL,NULL,NULL);
"""

PAGES = b"""\
INSERT INTO `page` VALUES
(10,0,'Q72',0,0,0.5,'20261003181442','20261003181442',1,1,'wikibase-item',NULL),
(11,0,'Q3',0,0,0.5,'20261003181442','20261003181442',1,1,'wikibase-item',NULL),
(13,120,'P625',0,0,0.5,'20261003181442','20261003181442',1,1,'wikibase-property',NULL),
(14,4,'Isn\\'t_it',0,0,0.5,'20261003181442','20261003181442',1,1,'wikitext',NULL);
"""


def _gz(d, name, data):
    path = Path(d) / name
    with gzip.open(path, "wb") as f:
        f.write(data)
    return path


class WikimediaTest(unittest.TestCase):
    def test_geo_tags(self):
        with tempfile.TemporaryDirectory() as d:
            pages, lat, lon = wikimedia.geo_tags(_gz(d, "g.sql.gz", GEO_TAGS))
        self.assertEqual(list(pages), [10, 13])
        self.assertEqual(list(lat), [47.37, -33.8])
        self.assertEqual(list(lon), [8.54, 151.2])

    def test_item_pages(self):
        with tempfile.TemporaryDirectory() as d:
            got = wikimedia.item_pages(_gz(d, "p.sql.gz", PAGES), [10, 13, 14])
        self.assertEqual(got, {10: 72})


if __name__ == "__main__":
    unittest.main()
