<!--
SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
SPDX-License-Identifier: CC-BY-4.0
-->

# Dataset paper (working draft)

A paper describing the OSMViews dataset: how it is built from the
OpenStreetMap tile logs, how the zoom filter removes artifacts, and how well
the result tracks other measures of where people look. Not yet submitted;
once a venue is chosen, the text moves into its template.

- **`dataset-paper.tex`** — the single source of the text, laid out like
  [`../defensive-publication`](../defensive-publication).
- **`generated/`** — every number and figure that comes from data:
  `results.tex` defines one `\res...` macro per value, `results.json` holds
  the same values for scripts, and the PNG files are figures. These files
  are written by the evaluation in [`../../eval`](../../eval) and committed,
  so the paper builds without rerunning it. Never edit them by hand.
- **`Makefile`** — `make` builds the PDF, `make eval` reruns the
  evaluation first, `make check` validates the PDF.

The text is licensed **CC BY 4.0**; the evaluation code is MIT.
