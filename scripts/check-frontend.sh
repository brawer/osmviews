#!/bin/sh
# SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
# SPDX-License-Identifier: MIT
#
# Supply-chain and size guards for the frontend (frontend/, internal/webui).
# Run it after "npm ci && npm run build", with dev dependencies still installed.
# Used by CI (.github/workflows/build-test.yml) and "make check".

set -eu

echo "==> JS bundle size budget"
budget=2097152 # 2 MiB, gzipped
total=0
for f in internal/webui/dist/assets/*.js; do
	[ -e "$f" ] || { echo "FAIL: no built JS in internal/webui/dist/assets/ (run npm run build)"; exit 1; }
	s=$(gzip -9 -c "$f" | wc -c | tr -d ' ')
	printf '    %-32s %8s B gz\n' "$(basename "$f")" "$s"
	total=$((total + s))
done
printf '    %-32s %8s B gz   (budget %s)\n' "total" "$total" "$budget"
[ "$total" -le "$budget" ] || { echo "FAIL: JS bundle over budget"; exit 1; }

echo "==> npm audit (fail on high or critical)"
npm audit --audit-level=high

echo "==> npm registry signatures and provenance attestations"
npm audit signatures

echo "==> dependency sources (must all come from the npm registry)"
npm query "*" | node -e '
let d = "";
process.stdin.on("data", (c) => (d += c)).on("end", () => {
  const pkgs = JSON.parse(d);
  const bad = pkgs
    .filter((p) => p.name !== "osmviews-webapp")
    .filter((p) => !(p.resolved || "").startsWith("https://registry.npmjs.org/"));
  if (bad.length) {
    console.error("FAIL: dependencies not resolved from registry.npmjs.org:");
    for (const p of bad) console.error("    " + p.name + "@" + p.version + "  " + (p.resolved || "(no resolved URL)"));
    process.exit(1);
  }
  console.log("    " + pkgs.length + " packages, all from the npm registry");
});
'

echo "==> npm dependency licenses"
npm query "*" | node -e '
let d = "";
process.stdin.on("data", (c) => (d += c)).on("end", () => {
  const pkgs = JSON.parse(d);
  const deny = /GPL|SSPL|CC-BY-SA|UNLICENSED/i; // strong copyleft / proprietary
  const licenseOf = (p) => {
    let l = p.license;
    if (Array.isArray(p.licenses)) l = p.licenses.map((x) => x.type || x).join(" OR ");
    if (l && typeof l === "object") l = l.type || "";
    return typeof l === "string" && l.trim() ? l.trim() : "MISSING";
  };
  const bad = pkgs
    .filter((p) => p.name !== "osmviews-webapp")
    .map((p) => [p.name + "@" + p.version, licenseOf(p)])
    .filter(([, l]) => l === "MISSING" || deny.test(l));
  if (bad.length) {
    console.error("FAIL: disallowed or missing licenses:");
    for (const [n, l] of bad) console.error("    " + n + "  " + l);
    process.exit(1);
  }
  console.log("    " + pkgs.length + " packages, all permissive");
});
'

echo "==> frontend checks passed"
