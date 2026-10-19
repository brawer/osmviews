# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Runs the evaluation for docs/dataset-paper.
#   make        -> download pinned inputs (inputs.toml) into CACHE, run every
#                  evaluation, write ../docs/dataset-paper/generated/
#   make test   -> unit tests of the evaluation code
#   make clean  -> remove the virtual environment (keeps the download cache)

CACHE ?= cache
OUT   := ../docs/dataset-paper/generated
VENV  := .venv
PY    := $(VENV)/bin/python3

.PHONY: all test clean

all: $(VENV)/.ok
	$(PY) -m osmviews_eval $(CACHE) $(OUT)

test: $(VENV)/.ok
	$(PY) -m unittest discover -s tests -t .

$(VENV)/.ok: requirements.txt
	python3 -m venv $(VENV)
	$(PY) -m pip install --quiet -r requirements.txt
	touch $@

clean:
	rm -rf $(VENV)
