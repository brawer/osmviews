# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: CC-BY-4.0
#
# Builds defensive-publication.pdf from defensive-publication.tex.
#   make        -> defensive-publication.pdf  (LuaLaTeX, PDF 2.0: PDF/UA-2 + PDF/A-4f)
#   make check  -> validate the PDF with veraPDF (accessibility + archival profiles)
#   make clean  -> remove build intermediates (keeps the .pdf)

SHELL := /bin/bash

# Publication date. Keep in sync with \pubdate in defensive-publication.tex.
# Drives the embedded PDF creation/modification timestamp (reproducible build).
PUB_DATE := 2026-09-02
export SOURCE_DATE_EPOCH := $(shell date -j -f '%Y-%m-%d %H:%M:%S' '$(PUB_DATE) 12:00:00' +%s)
export FORCE_SOURCE_DATE := 1

SRC := defensive-publication.tex
PDF := defensive-publication.pdf
AUX := defensive-publication.aux defensive-publication.log \
       defensive-publication-luamml-mathml.html

.PHONY: all check clean

all: $(PDF)

# Two passes: luamml writes the MathML sidecar on pass 1; pass 2 embeds it.
$(PDF): $(SRC)
	lualatex --interaction=nonstopmode --halt-on-error $(SRC)
	lualatex --interaction=nonstopmode --halt-on-error $(SRC)
	@echo "wrote $@"

check: $(PDF)
	verapdf --flavour ua2 $(PDF)
	verapdf --flavour 4f  $(PDF)

clean:
	rm -f $(AUX)
