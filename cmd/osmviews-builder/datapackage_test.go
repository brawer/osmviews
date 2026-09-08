// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// datapackageTestInputs is a deterministic set of inputs matching
// testdata/datapackage.golden.json. The digests are the SHA-256 of sentinel
// strings, so they are valid-length hex without pretending to hash real files.
func datapackageTestInputs() datapackageInputs {
	digest := func(s string) string {
		h := sha256.Sum256([]byte(s))
		return hex.EncodeToString(h[:])
	}
	return datapackageInputs{
		Date:         time.Date(2025, 8, 30, 0, 0, 0, 0, time.UTC),
		RasterPath:   "osmviews-20250830.tiff",
		RasterBytes:  608174080,
		RasterSHA256: digest("osmviews golden GeoTIFF fixture"),
		BOMPath:      "osmviews-20250830.cdx.json",
		BOMBytes:     3072,
		BOMSHA256:    digest("osmviews golden BOM fixture"),
	}
}

func TestWriteDatapackage_Golden(t *testing.T) {
	path := filepath.Join(t.TempDir(), "datapackage.json")
	if err := writeDatapackage(path, datapackageTestInputs()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	golden := filepath.Join("testdata", "datapackage.golden.json")
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
		t.Errorf("generated datapackage.json does not match %s.\n--- got ---\n%s", golden, got)
	}

	var doc struct {
		Schema    string `json:"$schema"`
		Name      string `json:"name"`
		Version   string `json:"version"`
		Resources []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Hash string `json:"hash"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(got, &doc); err != nil {
		t.Fatalf("golden datapackage.json is not valid JSON: %v", err)
	}
	if !strings.HasPrefix(doc.Schema, "https://datapackage.org/profiles/2.0/") {
		t.Errorf("$schema = %q, want a Frictionless v2 profile", doc.Schema)
	}
	if doc.Name != "osmviews" || doc.Version != "2025-08-30" {
		t.Errorf("name/version = %q/%q, want osmviews/2025-08-30", doc.Name, doc.Version)
	}
	if len(doc.Resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(doc.Resources))
	}
	for _, r := range doc.Resources {
		if !strings.HasPrefix(r.Hash, "sha256:") || len(r.Hash) != len("sha256:")+64 {
			t.Errorf("resource %q hash = %q, want sha256:<64 hex>", r.Name, r.Hash)
		}
		// Relative paths only — they must resolve next to datapackage.json.
		if strings.ContainsAny(r.Path, "/:") {
			t.Errorf("resource %q path = %q, want a bare relative filename", r.Name, r.Path)
		}
	}
}

func TestWriteDatapackage_Deterministic(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	if err := writeDatapackage(a, datapackageTestInputs()); err != nil {
		t.Fatal(err)
	}
	if err := writeDatapackage(b, datapackageTestInputs()); err != nil {
		t.Fatal(err)
	}
	ba, _ := os.ReadFile(a)
	bb, _ := os.ReadFile(b)
	if string(ba) != string(bb) {
		t.Error("writeDatapackage is not deterministic for identical inputs")
	}
}

func TestDatapackageID_Stable(t *testing.T) {
	// The dataset identity must be the same for every build, forever. If this
	// changes, downstream catalogs treat it as a different dataset.
	const want = "0c231884-c307-56cd-a796-787ff9fceb08"
	if datapackageID != want {
		t.Errorf("datapackageID = %q, want %q (dataset identity must never change)", datapackageID, want)
	}
}
