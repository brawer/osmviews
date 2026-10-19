# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: CC-BY-4.0
#
# Builds dataset-paper.pdf from dataset-paper.tex and generated/.
#   make        -> dataset-paper.pdf  (LuaLaTeX, PDF 2.0: PDF/UA-2 + PDF/A-4f)
#   make eval   -> rerun the evaluation (../../eval), which rewrites generated/
#   make check  -> validate the PDF with veraPDF (accessibility + archival profiles)
#   make clean  -> remove build intermediates (keeps the .pdf)

SHELL := /bin/bash

# Draft date. Keep in sync with \pubdate in dataset-paper.tex.
# Drives the embedded PDF creation/modification timestamp (reproducible build).
# GNU date first, BSD date (macOS) as fallback.
PUB_DATE := 2026-10-19
export SOURCE_DATE_EPOCH := $(shell date -u -d '$(PUB_DATE) 12:00:00' +%s 2>/dev/null || \
	date -u -j -f '%Y-%m-%d %H:%M:%S' '$(PUB_DATE) 12:00:00' +%s)
export FORCE_SOURCE_DATE := 1

SRC := dataset-paper.tex
PDF := dataset-paper.pdf
AUX := dataset-paper.aux dataset-paper.log dataset-paper.out \
       dataset-paper-luamml-mathml.html

.PHONY: all eval check clean

all: $(PDF)

# Two passes: luamml writes the MathML sidecar on pass 1; pass 2 embeds it.
$(PDF): $(SRC) $(wildcard generated/*)
	lualatex --interaction=nonstopmode --halt-on-error $(SRC)
	lualatex --interaction=nonstopmode --halt-on-error $(SRC)
	@echo "wrote $@"

eval:
	$(MAKE) -C ../../eval

check: $(PDF)
	verapdf --flavour ua2 $(PDF)
	verapdf --flavour 4f  $(PDF)

clean:
	rm -f $(AUX)
