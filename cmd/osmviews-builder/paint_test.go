// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

func TestPaint(t *testing.T) {
	file, err := os.Open(filepath.Join("testdata", "zurich-2021-W47.br"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	readers := []io.Reader{brotli.NewReader(file)}
	path := filepath.Join(t.TempDir(), "zurich.tif")
	if err := paint(path, 9, readers, nil, TiffMetadata{}, context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPaint_Metadata(t *testing.T) {
	readers := []io.Reader{strings.NewReader("3/1/1 3\n")}
	path := filepath.Join(t.TempDir(), "meta.tif")
	meta := TiffMetadata{
		Description: "OSMViews test. Tile logs 2026-01-05..2026-08-23.",
		DateTime:    time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC),
	}
	if err := paint(path, 11, readers, nil, meta, context.Background()); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewTiffReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if r.imageDescription != meta.Description {
		t.Errorf("ImageDescription = %q, want %q", r.imageDescription, meta.Description)
	}
	if want := "2026:08:23 00:00:00"; r.dateTime != want {
		t.Errorf("DateTime = %q, want %q", r.dateTime, want)
	}
}

func TestPaint_NoMetadata(t *testing.T) {
	readers := []io.Reader{strings.NewReader("3/1/1 3\n")}
	path := filepath.Join(t.TempDir(), "nometa.tif")
	if err := paint(path, 11, readers, nil, TiffMetadata{}, context.Background()); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(path)
	defer f.Close()
	r, err := NewTiffReader(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.imageDescription, "OpenStreetMap view density") {
		t.Errorf("ImageDescription = %q, want the default", r.imageDescription)
	}
	if r.dateTime != "" {
		t.Errorf("DateTime = %q, want empty (tag omitted)", r.dateTime)
	}
}

// TestPaint_Histogram checks that the painted GeoTIFF carries the pixel-value
// histogram in its GDAL_METADATA tag, that it is a well-formed linear-binning
// Raster Attribute Table, and that its bucket counts add up to every pixel of
// the main image (bucket 0, holding the unviewed world, dominates).
func TestPaint_Histogram(t *testing.T) {
	const zoom = 11 // pixel zoom; main image is 2^zoom pixels on a side
	readers := []io.Reader{strings.NewReader("3/1/1 3\n18/137341/91897 1\n")}
	path := filepath.Join(t.TempDir(), "hist.tif")
	if err := paint(path, zoom, readers, nil, TiffMetadata{}, context.Background()); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := NewTiffReader(f)
	if err != nil {
		t.Fatal(err)
	}

	md := r.gdalMetadata
	for _, want := range []string{
		`<GDALMetadata>`,
		`name="DEFAULT_RASTER_ATTRIBUTE_TABLE" sample="0" role="rat"`,
		`<GDALRasterAttributeTable Row0Min="0" BinSize="0.0625" tableType="athematic">`,
		`<Usage>1</Usage>`, // PixelCount column
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("GDAL_METADATA tag missing %q; got:\n%s", want, md)
		}
	}

	// Sum the <F> at the end of every <Row>: the count column.
	var total int64
	var bucket0 int64
	rows := strings.Count(md, "<Row ")
	for i, seg := range strings.Split(md, "<Row ")[1:] {
		fs := strings.Split(seg, "<F>")
		if len(fs) < 4 {
			t.Fatalf("row %d has %d F values, want 3: %s", i, len(fs)-1, seg)
		}
		countStr := strings.SplitN(fs[3], "</F>", 2)[0]
		n, err := strconv.ParseInt(countStr, 10, 64)
		if err != nil {
			t.Fatalf("row %d count %q: %v", i, countStr, err)
		}
		if i == 0 {
			bucket0 = n
		}
		total += n
	}

	wantTotal := int64(1) << (2 * zoom) // (2^zoom)^2 pixels in the main image
	if total != wantTotal {
		t.Errorf("histogram counts sum to %d over %d buckets, want %d (every main-image pixel)", total, rows, wantTotal)
	}
	if bucket0 <= wantTotal/2 {
		t.Errorf("bucket 0 holds %d of %d pixels; the unviewed world should dominate", bucket0, wantTotal)
	}
}

// Make sure we can handle view counts at deep zoom levels even if not all
// parent tiles have been viewed.
func TestPaint_ParentNotLogged(t *testing.T) {
	readers := []io.Reader{strings.NewReader("3/1/1 3\n18/137341/91897 1\n")}
	path := filepath.Join(t.TempDir(), "notlogged.tif")
	if err := paint(path, 11, readers, nil, TiffMetadata{}, context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPaint_TooManyCountsForSameTile(t *testing.T) {
	readers := []io.Reader{
		// TODO: Uncomment once k-way merging is implemented.
		//strings.NewReader("4/4/10 3\n7/39/87 11\n"),
		strings.NewReader("4/2/1 2\n7/39/87 22\n7/39/87 33\n7/39/87 44\n"),
	}
	path := filepath.Join(t.TempDir(), "toomanycounts.tif")
	var got string
	if err := paint(path, 16, readers, nil, TiffMetadata{}, context.Background()); err != nil {
		got = err.Error()
	}
	want := "tile 7/39/87 appears more than 1 times in input"
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}
