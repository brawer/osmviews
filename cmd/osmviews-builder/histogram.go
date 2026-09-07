// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// histogramBinSize is the width of one histogram bucket, in units of
// ln(1 + weekly views per km²) — the same logarithmic space the GeoTIFF
// stores its pixels in. It is a fixed constant, never derived from the data,
// so bucket k denotes the same density range in every weekly release and
// releases stay comparable. 1/16 splits the ln domain (about 0…16 in
// practice) into roughly 256 buckets.
const histogramBinSize = 0.0625

// Histogram counts how many pixels of the GeoTIFF's main image fall into each
// value bucket. Bucket k covers the half-open interval
// [k·histogramBinSize, (k+1)·histogramBinSize) in ln(1 + views per km²) space.
//
// Bucket 0 holds every pixel whose weekly view density rounds to zero, which
// is most of the planet — oceans, ice, and all the land nobody zoomed into.
// That is the honest shape of a view-density distribution; no GDAL_NODATA
// value is set, because zero is a real measurement ("nobody looked here"),
// not missing data.
//
// The zero value is ready to use. Add is not safe for concurrent use.
type Histogram struct {
	counts []int64
}

// Add records n pixels whose stored value is v, in ln(1+views/km²) space.
func (h *Histogram) Add(v float32, n int64) {
	bin := 0
	if v > 0 {
		bin = int(float64(v) / histogramBinSize)
	}
	if bin >= len(h.counts) {
		grown := make([]int64, bin+1)
		copy(grown, h.counts)
		h.counts = grown
	}
	h.counts[bin] += n
}

// Total returns the number of pixels recorded so far.
func (h *Histogram) Total() int64 {
	var n int64
	for _, c := range h.counts {
		n += c
	}
	return n
}

// numBins is the number of buckets to serialize: up to and including the last
// non-empty one, and always at least one.
func (h *Histogram) numBins() int {
	n := len(h.counts)
	for n > 0 && h.counts[n-1] == 0 {
		n--
	}
	if n == 0 {
		return 1
	}
	return n
}

// RATXML renders the histogram as the XML payload of the GDAL_METADATA TIFF
// tag (42112): a band-1 Raster Attribute Table with linear binning, carrying
// the min/max bound of each bucket and its pixel count. GDAL 3.12+ and QGIS
// read this back through GetDefaultRAT(); older readers ignore the tag and
// the raster still opens. Field type/usage integers are GDAL's own codes:
// Real=1, Integer=0; Min=3, Max=4, PixelCount=1.
func (h *Histogram) RATXML() string {
	n := h.numBins()

	var b strings.Builder
	b.Grow(80 * (n + 6))
	b.WriteString(`<GDALMetadata>`)
	b.WriteString(`<Item name="DEFAULT_RASTER_ATTRIBUTE_TABLE" sample="0" role="rat">`)
	fmt.Fprintf(&b, `<GDALRasterAttributeTable Row0Min="0" BinSize="%s" tableType="athematic">`,
		strconv.FormatFloat(histogramBinSize, 'g', -1, 64))
	b.WriteString(`<FieldDefn index="0"><Name>min</Name><Type>1</Type><Usage>3</Usage></FieldDefn>`)
	b.WriteString(`<FieldDefn index="1"><Name>max</Name><Type>1</Type><Usage>4</Usage></FieldDefn>`)
	b.WriteString(`<FieldDefn index="2"><Name>count</Name><Type>0</Type><Usage>1</Usage></FieldDefn>`)
	for i := 0; i < n; i++ {
		var count int64
		if i < len(h.counts) {
			count = h.counts[i]
		}
		fmt.Fprintf(&b, `<Row index="%d"><F>%s</F><F>%s</F><F>%d</F></Row>`, i,
			strconv.FormatFloat(float64(i)*histogramBinSize, 'g', -1, 64),
			strconv.FormatFloat(float64(i+1)*histogramBinSize, 'g', -1, 64),
			count)
	}
	b.WriteString(`</GDALRasterAttributeTable></Item></GDALMetadata>`)
	return b.String()
}
