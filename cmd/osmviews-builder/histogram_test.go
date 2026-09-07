// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestHistogram_Add(t *testing.T) {
	var h Histogram

	// Negative and zero both land in bucket 0.
	h.Add(-1, 3)
	h.Add(0, 5)
	// 0.1 / 0.0625 = 1.6 -> bucket 1.
	h.Add(0.1, 2)
	// 0.5 / 0.0625 = 8 -> bucket 8.
	h.Add(0.5, 1)

	if got := h.Total(); got != 11 {
		t.Errorf("Total() = %d, want 11", got)
	}
	if got := h.counts[0]; got != 8 {
		t.Errorf("counts[0] = %d, want 8", got)
	}
	if got := h.counts[1]; got != 2 {
		t.Errorf("counts[1] = %d, want 2", got)
	}
	if got := h.counts[8]; got != 1 {
		t.Errorf("counts[8] = %d, want 1", got)
	}
}

func TestHistogram_numBins(t *testing.T) {
	var empty Histogram
	if got := empty.numBins(); got != 1 {
		t.Errorf("empty numBins() = %d, want 1", got)
	}

	var h Histogram
	h.Add(0, 1)   // bucket 0
	h.Add(0.5, 1) // bucket 8
	h.Add(1.0, 0) // bucket 16, but zero count
	if got := h.numBins(); got != 9 {
		t.Errorf("numBins() = %d, want 9 (trailing empty buckets trimmed)", got)
	}
}

func TestHistogram_RATXML(t *testing.T) {
	var h Histogram
	h.Add(0, 4_000_000_000) // bucket 0, exercises a count beyond int32
	h.Add(0.1, 7)           // bucket 1

	xml := h.RATXML()

	for _, want := range []string{
		`<GDALMetadata><Item name="DEFAULT_RASTER_ATTRIBUTE_TABLE" sample="0" role="rat">`,
		`<GDALRasterAttributeTable Row0Min="0" BinSize="0.0625" tableType="athematic">`,
		`<FieldDefn index="0"><Name>min</Name><Type>1</Type><Usage>3</Usage></FieldDefn>`,
		`<FieldDefn index="2"><Name>count</Name><Type>0</Type><Usage>1</Usage></FieldDefn>`,
		`<Row index="0"><F>0</F><F>0.0625</F><F>4000000000</F></Row>`,
		`<Row index="1"><F>0.0625</F><F>0.125</F><F>7</F></Row>`,
		`</GDALRasterAttributeTable></Item></GDALMetadata>`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("RATXML() missing %q\ngot: %s", want, xml)
		}
	}

	if got := strings.Count(xml, "<Row "); got != 2 {
		t.Errorf("RATXML() has %d rows, want 2", got)
	}
}
