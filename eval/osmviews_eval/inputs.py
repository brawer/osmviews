# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
"""Resolves the inputs listed in inputs.toml to local files."""

import hashlib
import os
import shutil
import tomllib
import urllib.request
from pathlib import Path

MANIFEST = Path(__file__).resolve().parent.parent / "inputs.toml"


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for block in iter(lambda: f.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def resolve(name, cache):
    """Returns the local path of input `name`, downloading it if needed.

    Returns None for an optional input that is neither pinned nor given.
    """
    with open(MANIFEST, "rb") as f:
        entry = tomllib.load(f)[name]
    override = os.environ.get(entry["env"], "")
    if override:
        return Path(override)
    if not entry["url"]:
        if entry.get("optional"):
            return None
        raise SystemExit(
            f"input {name!r} has no pinned url yet; "
            f"set {entry['env']} to a local file"
        )
    path = Path(cache) / entry.get("file", Path(entry["url"]).name)
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        tmp = path.with_suffix(path.suffix + ".tmp")
        with urllib.request.urlopen(entry["url"]) as r, open(tmp, "wb") as out:
            shutil.copyfileobj(r, out)
        tmp.rename(path)
    if entry["sha256"] and sha256(path) != entry["sha256"]:
        raise SystemExit(f"checksum mismatch for {path}")
    return path
