// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// datapackage.go emits data/datapackage.json, a Frictionless Data Package v2
// descriptor served at <host>/data/datapackage.json. It is the primary,
// server-independent way a client learns the current version and finds each
// distribution (raster, BOM) by relative path, byte size and SHA-256.
// See https://github.com/brawer/osmviews/issues/110.
//
// Like the BOM it is hand-rolled and deterministic: encoding/json emits struct
// fields in declaration order and every input is derived from the tile-log
// date, so a rebuild of the same week is byte-identical.

const (
	datapackageSchema = "https://datapackage.org/profiles/2.0/datapackage.json"
	datapackageName   = "osmviews"
	datapackageTitle  = "OSMViews — worldwide ranking of geographic locations"
	datapackageDesc   = "OpenStreetMap view density in weekly user views per km2, " +
		"as a Cloud-Optimized GeoTIFF (EPSG:3857, zoom 0-18). Rebuilt weekly over " +
		"a trailing 52-week window."
)

// datapackageID is the dataset's stable identity: the same value for every
// build, forever. It is a UUIDv5 derived from a fixed name so the derivation
// is self-documenting rather than a frozen literal.
var datapackageID = uuid.NewSHA1(bomNamespace, []byte("osmviews-datapackage")).String()

// datapackageInputs is everything writeDatapackage needs that is not constant.
// Every field is derived from the build, nothing from the wall clock.
type datapackageInputs struct {
	Date         time.Time
	RasterPath   string // dated basename, e.g. "osmviews-20250830.tiff"
	RasterBytes  int64
	RasterSHA256 string // lower-case hex
	BOMPath      string // dated basename, e.g. "osmviews-20250830.cdx.json"
	BOMBytes     int64
	BOMSHA256    string // lower-case hex
}

func buildDatapackage(in datapackageInputs) *fdPackage {
	date := in.Date.UTC().Format("2006-01-02")
	return &fdPackage{
		Schema:      datapackageSchema,
		Name:        datapackageName,
		ID:          datapackageID,
		Title:       datapackageTitle,
		Description: datapackageDesc,
		Version:     date,
		Created:     in.Date.UTC().Format("2006-01-02T15:04:05Z"),
		Homepage:    bomWebsiteURL,
		Licenses: []fdLicense{{
			Name:  "CC0-1.0",
			Path:  "https://creativecommons.org/publicdomain/zero/1.0/",
			Title: "Creative Commons Zero v1.0 Universal",
		}},
		Sources: []fdSource{{
			Title: "OpenStreetMap tile logs",
			Path:  bomTileLogsURL,
		}},
		Contributors: []fdContributor{{
			Title: bomSupplierName,
			Path:  bomSupplierURL,
			Roles: []string{"author", "publisher"},
		}},
		Resources: []fdResource{
			{
				Name:        "osmviews",
				Path:        in.RasterPath,
				Title:       "OSMViews raster",
				Description: fmt.Sprintf("Cloud-Optimized GeoTIFF, %s, zoom 0-%d. This dated object is immutable; the manifest's version is what advances.", bomCRS, maxZoom),
				Format:      "tiff",
				Mediatype:   "image/tiff; application=geotiff",
				Bytes:       in.RasterBytes,
				Hash:        "sha256:" + in.RasterSHA256,
			},
			{
				Name:        "sbom",
				Path:        in.BOMPath,
				Title:       "CycloneDX 1.7 bill of materials",
				Description: "Provenance for the raster: producing software (by purl) and dataset identity, anchored on the raster's SHA-256/512.",
				Format:      "json",
				Mediatype:   "application/vnd.cyclonedx+json; version=1.7",
				Bytes:       in.BOMBytes,
				Hash:        "sha256:" + in.BOMSHA256,
				Describes:   "osmviews",
			},
		},
	}
}

// writeDatapackage builds the descriptor for in and writes it to path atomically.
func writeDatapackage(path string, in datapackageInputs) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildDatapackage(in)); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Frictionless Data Package v2 JSON model, covering only the fields the builder
// emits. Field order is the JSON output order; keep it stable.

type fdPackage struct {
	Schema       string          `json:"$schema"`
	Name         string          `json:"name"`
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	Version      string          `json:"version"`
	Created      string          `json:"created"`
	Homepage     string          `json:"homepage"`
	Licenses     []fdLicense     `json:"licenses"`
	Sources      []fdSource      `json:"sources"`
	Contributors []fdContributor `json:"contributors"`
	Resources    []fdResource    `json:"resources"`
}

type fdLicense struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Title string `json:"title"`
}

type fdSource struct {
	Title string `json:"title"`
	Path  string `json:"path"`
}

type fdContributor struct {
	Title string   `json:"title"`
	Path  string   `json:"path"`
	Roles []string `json:"roles"`
}

type fdResource struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Format      string `json:"format"`
	Mediatype   string `json:"mediatype"`
	Bytes       int64  `json:"bytes"`
	Hash        string `json:"hash"`
	Describes   string `json:"describes,omitempty"`
}
