// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/*.golden files")

// bomTestInputs is a fully-populated, deterministic set of inputs matching
// testdata/bom.golden.cdx.json. Each digest is the real SHA-256/512 of a
// sentinel string standing in for the file it describes (the GeoTIFF, the
// stats JSON), so the values are valid-length hex and stable without
// pretending to be digests of real files.
func bomTestInputs() bomInputs {
	digest256 := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}
	digest512 := func(s string) string {
		h := sha512.Sum512([]byte(s))
		return hex.EncodeToString(h[:])
	}
	return bomInputs{
		Date:        time.Date(2025, 8, 30, 0, 0, 0, 0, time.UTC),
		FirstDay:    time.Date(2024, 9, 2, 0, 0, 0, 0, time.UTC),
		Weeks:       52,
		SHA256:      digest256("osmviews golden GeoTIFF fixture"),
		SHA512:      digest512("osmviews golden GeoTIFF fixture"),
		Software:    "OSMViews/v0.1.2+1a2b3c4d5e6f",
		Revision:    "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b",
		Modified:    false,
		Release:     "0.1.2",
		MaxZoom:     18,
		StatsSHA256: digest256("osmviews golden stats fixture"),
		StatsSHA512: digest512("osmviews golden stats fixture"),
	}
}

func TestWriteBOM_Golden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "osmviews-20250830.cdx.json")
	if err := writeBOM(path, bomTestInputs()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	golden := filepath.Join("testdata", "bom.golden.cdx.json")
	if *updateGolden {
		if err := os.WriteFile(golden, got, 0644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("generated BOM does not match %s.\n--- got ---\n%s", golden, got)
	}

	// The golden file must be valid JSON and a CycloneDX 1.7 document.
	var doc map[string]any
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("golden BOM is not valid JSON: %v", err)
	}
	if doc["bomFormat"] != "CycloneDX" || doc["specVersion"] != "1.7" {
		t.Errorf("bomFormat/specVersion = %v/%v, want CycloneDX/1.7", doc["bomFormat"], doc["specVersion"])
	}
}

func TestWriteBOM_Deterministic(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.cdx.json")
	b := filepath.Join(dir, "b.cdx.json")
	if err := writeBOM(a, bomTestInputs()); err != nil {
		t.Fatal(err)
	}
	if err := writeBOM(b, bomTestInputs()); err != nil {
		t.Fatal(err)
	}
	ba, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if string(ba) != string(bb) {
		t.Error("writeBOM is not deterministic for identical inputs")
	}
}

func TestSoftwarePURL(t *testing.T) {
	for _, tt := range []struct {
		name             string
		revision, releas string
		want             string
	}{
		{"commit wins", "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b", "0.1.2",
			"pkg:github/brawer/osmviews@1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"},
		{"release fallback", "", "0.1.2", "pkg:github/brawer/osmviews@v0.1.2"},
		{"neither", "", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			in := bomInputs{Revision: tt.revision, Release: tt.releas}
			if got := in.softwarePURL(); got != tt.want {
				t.Errorf("softwarePURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDataPURL(t *testing.T) {
	in := bomTestInputs()
	want := "pkg:generic/osmviews@2025-08-30?checksum=sha256:" + in.SHA256 +
		"&download_url=https%3A%2F%2Fosmviews.toolforge.org%2Fdownload%2Fosmviews.tiff"
	if got := in.dataPURL(); got != want {
		t.Errorf("dataPURL() = %q, want %q", got, want)
	}
}

// TestBOM_MatchesPaintedFile paints a tiny GeoTIFF and checks that the BOM
// records the same SHA-256 that hashing the file yields, and that the file is
// byte-reproducible across two paints (so the recorded hash stays valid).
func TestBOM_MatchesPaintedFile(t *testing.T) {
	dir := t.TempDir()
	meta := TiffMetadata{
		Description: "OSMViews test. Tile logs 2026-01-05..2026-08-23.",
		DateTime:    time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
	}
	paths := [2]string{filepath.Join(dir, "a.tif"), filepath.Join(dir, "b.tif")}
	var digests [2]string
	for i, p := range paths {
		if err := paint(p, 11, []io.Reader{strings.NewReader("3/1/1 3\n")}, nil, meta, context.Background()); err != nil {
			t.Fatal(err)
		}
		sha256hex, _, err := hashFile(p)
		if err != nil {
			t.Fatal(err)
		}
		digests[i] = sha256hex
	}
	if digests[0] != digests[1] {
		t.Fatalf("painted GeoTIFF is not byte-reproducible: %s != %s", digests[0], digests[1])
	}

	sha256hex, sha512hex, err := hashFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	bomPath := filepath.Join(dir, "out.cdx.json")
	if err := writeBOM(bomPath, bomInputs{
		Date:     meta.DateTime,
		FirstDay: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		Weeks:    1,
		SHA256:   sha256hex,
		SHA512:   sha512hex,
		Software: "OSMViews",
		MaxZoom:  11,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(bomPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), sha256hex) {
		t.Errorf("BOM does not contain the GeoTIFF's SHA-256 %s", sha256hex)
	}
	if !strings.Contains(string(raw), "pkg:generic/osmviews@2026-08-23") {
		t.Errorf("BOM is missing the expected data purl:\n%s", raw)
	}
}

// TestBOM_NTIASupplier guards the NTIA "minimum elements": a supplier on the
// document and on every component (see FOSSA NTIA SBOM validator).
func TestBOM_NTIASupplier(t *testing.T) {
	bom := buildBOM(bomTestInputs())
	if bom.Metadata.Supplier == nil || bom.Metadata.Supplier.Name == "" {
		t.Error("metadata.supplier is missing")
	}
	if bom.Metadata.Component.Supplier == nil {
		t.Error("metadata.component.supplier is missing")
	}
	for i, c := range bom.Metadata.Tools.Components {
		if c.Supplier == nil || c.Supplier.Name == "" {
			t.Errorf("metadata.tools.components[%d] (%s) has no supplier", i, c.Name)
		}
	}
	for i, c := range bom.Components {
		if c.Supplier == nil || c.Supplier.Name == "" {
			t.Errorf("components[%d] (%s) has no supplier", i, c.Name)
		}
	}
}

// TestBOM_Provenance checks that the GeoTIFF depends only on its source data
// (the OSM tile logs), while the producing pipeline is modelled as a tool and
// a formulation workflow — never as a dependency.
func TestBOM_Provenance(t *testing.T) {
	bom := buildBOM(bomTestInputs())

	if len(bom.Components) != 1 || bom.Components[0].BOMRef != "osm-tilelogs" {
		t.Fatalf("components[] = %+v, want just the OSM tile logs", bom.Components)
	}
	deps := map[string][]string{}
	for _, d := range bom.Dependencies {
		deps[d.Ref] = d.DependsOn
	}
	if got := deps["osmviews-geotiff-20250830"]; len(got) != 1 || got[0] != "osm-tilelogs" {
		t.Errorf("geotiff dependsOn %v, want [osm-tilelogs]", got)
	}
	if got, ok := deps["osm-tilelogs"]; !ok || len(got) != 0 {
		t.Errorf("osm-tilelogs dependency entry = %v (present=%v), want an explicit empty list", got, ok)
	}
	for ref, on := range deps {
		for _, d := range on {
			if d == "osmviews-builder" {
				t.Errorf("%s dependsOn osmviews-builder; the builder is a tool, not a dependency", ref)
			}
		}
	}
	if len(bom.Metadata.Tools.Components) != 1 || bom.Metadata.Tools.Components[0].BOMRef != "osmviews-builder" {
		t.Fatalf("metadata.tools.components = %+v, want the osmviews-builder tool", bom.Metadata.Tools.Components)
	}

	if len(bom.Formulation) != 1 || len(bom.Formulation[0].Workflows) != 1 {
		t.Fatalf("formulation = %+v, want one workflow", bom.Formulation)
	}
	wf := bom.Formulation[0].Workflows[0]
	if len(wf.TaskTypes) != 1 || wf.TaskTypes[0] != "build" {
		t.Errorf("workflow taskTypes = %v, want [build]", wf.TaskTypes)
	}
	if len(wf.ResourceReferences) != 1 || wf.ResourceReferences[0].Ref != "osmviews-builder" {
		t.Errorf("workflow resourceReferences = %+v, want a ref to osmviews-builder", wf.ResourceReferences)
	}
	if len(wf.Outputs) != 1 || wf.Outputs[0].Resource == nil ||
		wf.Outputs[0].Resource.Ref != "osmviews-geotiff-20250830" {
		t.Errorf("workflow outputs = %+v, want the GeoTIFF as the artifact", wf.Outputs)
	}
	if len(wf.Inputs) != 1 || wf.Inputs[0].Resource == nil ||
		wf.Inputs[0].Resource.Ref != "osm-tilelogs" {
		t.Errorf("workflow inputs = %+v, want a ref to the OSM tile logs", wf.Inputs)
	}
}

// TestBOM_TileLogsComponent checks the OSM tile-logs component: an NTIA-valid
// version and identifier, plus a public-domain license substantiated with the
// OSMF LWG minutes.
func TestBOM_TileLogsComponent(t *testing.T) {
	tl := buildBOM(bomTestInputs()).Components[0]

	if tl.Version != "2025-08-30" {
		t.Errorf("tile logs version = %q, want the ingested slice's last day", tl.Version)
	}
	if want := "pkg:generic/openstreetmap/tile-logs@2025-08-30?download_url="; !strings.HasPrefix(tl.PURL, want) {
		t.Errorf("tile logs purl = %q, want prefix %q", tl.PURL, want)
	}
	var firstDay string
	for _, p := range tl.Properties {
		if p.Name == "osmviews:tileLogs:firstDay" {
			firstDay = p.Value
		}
		if p.Name == "osmviews:tileLogs:weeks" {
			t.Error("tile-logs component carries an :weeks property; ISO-week aggregation is our pipeline's, not upstream's")
		}
	}
	if firstDay != "2024-09-02" {
		t.Errorf("tile logs firstDay property = %q, want the first ingested day", firstDay)
	}
	if strings.Contains(tl.Description, "week") {
		t.Errorf("tile logs description mentions weeks (%q); upstream files are per day", tl.Description)
	}

	if len(tl.Licenses) != 1 {
		t.Fatalf("tile logs licenses = %+v, want one", tl.Licenses)
	}
	lic := tl.Licenses[0].License
	if lic.ID != "CC0-1.0" {
		t.Errorf("tile logs license id = %q, want CC0-1.0", lic.ID)
	}
	if lic.Acknowledgement != "concluded" {
		t.Errorf("tile logs license acknowledgement = %q, want concluded", lic.Acknowledgement)
	}
	const evidence = "osmfoundation.org/wiki/Licensing_Working_Group/Minutes/2022-05-12"
	if !strings.Contains(lic.URL, evidence) {
		t.Errorf("tile logs license url = %q, want the LWG minutes", lic.URL)
	}
	var hasLicenseRef bool
	for _, ref := range tl.ExternalReferences {
		if ref.Type == "license" && strings.Contains(ref.URL, evidence) {
			hasLicenseRef = true
		}
	}
	if !hasLicenseRef {
		t.Errorf("tile logs has no 'license' external reference to the LWG minutes: %+v", tl.ExternalReferences)
	}
}

// TestBOM_StatsReference checks that the GeoTIFF component points at the
// dated statistics JSON, with digests, so the BOM is the discovery hub.
func TestBOM_StatsReference(t *testing.T) {
	in := bomTestInputs()
	geotiff := buildBOM(in).Metadata.Component

	var ref *cdxExternalRef
	for i := range geotiff.ExternalReferences {
		if strings.Contains(geotiff.ExternalReferences[i].URL, "osmviews-stats-") {
			ref = &geotiff.ExternalReferences[i]
		}
	}
	if ref == nil {
		t.Fatalf("GeoTIFF has no stats external reference: %+v", geotiff.ExternalReferences)
	}
	if want := "https://osmviews.toolforge.org/download/osmviews-stats-20250830.json"; ref.URL != want {
		t.Errorf("stats ref URL = %q, want %q", ref.URL, want)
	}
	if ref.Type != "other" {
		t.Errorf("stats ref type = %q, want other", ref.Type)
	}
	if len(ref.Hashes) != 2 || ref.Hashes[0].Alg != "SHA-256" || ref.Hashes[0].Content != in.StatsSHA256 {
		t.Errorf("stats ref hashes = %+v, want SHA-256/512 of the stats file", ref.Hashes)
	}

	// Without a hashed stats file the reference is still emitted, sans hashes.
	in.StatsSHA256, in.StatsSHA512 = "", ""
	for _, r := range buildBOM(in).Metadata.Component.ExternalReferences {
		if strings.Contains(r.URL, "osmviews-stats-") && len(r.Hashes) != 0 {
			t.Errorf("stats ref should have no hashes when the file was not hashed: %+v", r)
		}
	}
}

func TestBuildBOM_NoVCS(t *testing.T) {
	in := bomTestInputs()
	in.Revision = ""
	in.Software = "OSMViews/v0.1.2"
	tool := buildBOM(in).Metadata.Tools.Components[0]

	if got := tool.PURL; got != "pkg:github/brawer/osmviews@v0.1.2" {
		t.Errorf("builder purl = %q, want the release fallback", got)
	}
	for _, p := range tool.Properties {
		if strings.HasPrefix(p.Name, "osmviews:vcs:") {
			t.Errorf("unexpected VCS property %q when no revision was recorded", p.Name)
		}
	}
}
