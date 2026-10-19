// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/brawer/osmviews/v2/internal/version"
)

// maxZoom is the deepest zoom level painted into the output GeoTIFF; one
// tile view at this level becomes one pixel. It is also recorded in the BOM.
const maxZoom = 18

func main() {
	SoftwareVersion = version.Resolve(SoftwareVersion)
	ctx := context.Background()

	workdir := flag.String("workdir", "osmviews-builder-workdir", "path to working directory")
	showVersion := flag.Bool("version", false, "print version and exit")
	zoomFilter := flag.Bool("zoom-filter", false,
		"paint both GeoTIFFs (regular and with the experimental zoom filter) "+
			"to workdir only, and exit without publishing anything")
	flag.Parse()
	if *showVersion {
		fmt.Println(SoftwareVersion)
		return
	}

	logger := log.Default()
	logger.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lshortfile)
	logger.Printf("starting %s, built with %s", SoftwareVersion, runtime.Version())

	if *workdir != "" {
		if err := os.MkdirAll(*workdir, 0755); err != nil {
			logger.Fatalf("creating workdir %s: %v", *workdir, err)
		}
	}

	storage, bucket, err := NewInternalStorage()
	if err != nil {
		logger.Fatalf("connecting to internal object storage: %v", err)
	}
	bucketExists, err := storage.BucketExists(ctx, bucket)
	if err != nil {
		logger.Fatalf("checking for bucket %q: %v", bucket, err)
	}
	if !bucketExists {
		logger.Fatalf("storage bucket %q does not exist", bucket)
	}

	// The GeoTIFF, BOM and (later) datapackage.json go to a separate public
	// bucket the CDN serves; the tile-log aggregates stay on the internal one.
	// Bunny's S3 gateway has no HeadBucket, so there is no existence check here.
	pub, pubBucket, err := NewPublicStorage()
	if err != nil {
		logger.Fatalf("connecting to public object storage: %v", err)
	}

	maxWeeks := 52 // 1 year
	logs, err := fetchWeeklyLogs(*workdir, storage, bucket, maxWeeks)
	if err != nil {
		logger.Fatalf("fetching weekly tile logs: %v", err)
	}

	// Construct a file path for the output file. As part of the file name,
	// we use the date of the last day of the last week whose data is being
	// painted. That needs less explanation to users than some file name
	// convention involving ISO weeks, which are less commonly known.
	year, week, err := ParseWeek(logs.lastWeek)
	if err != nil {
		logger.Fatalf("parsing last week %q: %v", logs.lastWeek, err)
	}
	lastDay := weekStart(year, week).AddDate(0, 0, 6)
	date := lastDay.Format("20060102")
	localpath := filepath.Join(*workdir, fmt.Sprintf("osmviews-%s.tiff", date))
	remotepath := fmt.Sprintf("data/osmviews-%s.tiff", date)
	localBomPath := filepath.Join(*workdir, fmt.Sprintf("osmviews-%s.cdx.json", date))
	remoteBomPath := fmt.Sprintf("data/osmviews-%s.cdx.json", date)

	filteredPath := filepath.Join(*workdir, fmt.Sprintf("osmviews-%s-filtered.tiff", date))
	remoteFilteredPath := fmt.Sprintf("data/experimental/osmviews-%s-filtered.tiff", date)
	filteredMeta := TiffMetadata{
		Description: fmt.Sprintf(
			"EXPERIMENTAL, zoom filter v%d against tile-log lanes. OpenStreetMap view density: "+
				"ln(1 + weekly user views per km2) per pixel. Tile logs %s..%s. https://osmviews.brawer.ch",
			zoomFilterVersion, logs.firstDay.Format("2006-01-02"), logs.lastDay.Format("2006-01-02")),
		DateTime: logs.lastDay,
	}
	meta := TiffMetadata{
		Description: fmt.Sprintf(
			"OpenStreetMap view density: ln(1 + weekly user views per km2) "+
				"per pixel. Tile logs %s..%s. https://osmviews.brawer.ch",
			logs.firstDay.Format("2006-01-02"), logs.lastDay.Format("2006-01-02")),
		DateTime: logs.lastDay,
	}

	// For comparison, paint both GeoTIFFs into workdir and stop there.
	if *zoomFilter {
		corrections, err := openZoomFilters(*workdir, logs.names, nil, "", ctx)
		if err != nil {
			logger.Fatalf("computing zoom filter: %v", err)
		}
		outputs := []PaintOutput{{Path: localpath, Meta: meta}, {Path: filteredPath, Meta: filteredMeta, Filtered: true}}
		if err := paint(outputs, maxZoom, logs.readers, logs.weights, corrections, ctx); err != nil {
			logger.Fatalf("painting: %v", err)
		}
		logger.Printf("painted %s and %s; not publishing because of -zoom-filter", localpath, filteredPath)
		return
	}

	// If every published object for this week is already in the public bucket,
	// there is nothing to do. datapackage.json is written last, so requiring it
	// too means a build interrupted mid-publish is retried on the next run (the
	// paint is deterministic, so the rebuild is byte-identical).
	published := true
	for _, key := range []string{remotepath, remoteBomPath, "data/datapackage.json"} {
		if _, err := pub.Stat(ctx, pubBucket, key); err != nil {
			published = false
			break
		}
	}
	filteredPublished := false
	if _, err := pub.Stat(ctx, pubBucket, remoteFilteredPath); err == nil {
		filteredPublished = true
	}
	if published && filteredPublished {
		logger.Printf("already published: %s/%s and siblings; nothing to do", pubBucket, remotepath)
		return
	}
	if !published {
		logger.Printf("painting the regular GeoTIFF; the filtered one follows in a later run")
	}

	// The experimental zoom filter (zoomfilter.go) yields a second GeoTIFF,
	// published outside the BOM and datapackage.json until it has been
	// evaluated. Painting it takes about as long as the regular GeoTIFF, so
	// it is painted in a later run than the regular one, to stay within the
	// job's time limit; the job runs daily but a new week comes only weekly.
	// The filter's corrections are cached per week in object storage. On a
	// first run, computing them for a whole year would not fit into the time
	// limit either, so each run works on them for a limited time, and the
	// filtered GeoTIFF is painted once all weeks are ready. Since the filter
	// is experimental, its failures do not stop the regular output.
	paintFiltered := false
	if published {
		ready, err := prepareZoomFilters(*workdir, logs.names, storage, bucket, 40*time.Minute, ctx)
		switch {
		case err != nil:
			logger.Printf("zoom filter failed: %v", err)
		case !ready:
			logger.Printf("zoom filter not ready for all weeks yet; continuing on the next run")
		default:
			paintFiltered = true
		}
		if !paintFiltered {
			return
		}
	}

	// Paint the output GeoTIFF files. The description deliberately carries no
	// generation timestamp, so a rebuild of the same week is byte-identical
	// and the BOM's recorded hashes stay valid.
	var outputs []PaintOutput
	var corrections []io.Reader
	if !published {
		outputs = append(outputs, PaintOutput{Path: localpath, Meta: meta})
	}
	if paintFiltered {
		corrections, err = openZoomFilters(*workdir, logs.names, storage, bucket, ctx)
		if err != nil {
			logger.Printf("zoom filter failed: %v", err)
			return
		}
		outputs = append(outputs, PaintOutput{Path: filteredPath, Meta: filteredMeta, Filtered: true})
	}
	if err := paint(outputs, maxZoom, logs.readers, logs.weights, corrections, ctx); err != nil {
		logger.Fatalf("painting: %v", err)
	}
	if !published {
		publishRegular(ctx, pub, pubBucket, logs, localpath, remotepath, localBomPath, remoteBomPath, *workdir)
	}
	if paintFiltered {
		if err := pub.PutFile(ctx, pubBucket, remoteFilteredPath, filteredPath, "image/tiff"); err != nil {
			logger.Fatalf("uploading %s/%s: %v", pubBucket, remoteFilteredPath, err)
		}
		logger.Printf("uploaded experimental %s/%s", pubBucket, remoteFilteredPath)
	}

	if err := Cleanup(storage, bucket, pub, pubBucket); err != nil {
		logger.Fatalf("garbage-collecting old files in storage: %v", err)
	}
}

// publishRegular builds the BOM and datapackage.json for the regular
// GeoTIFF at localpath and uploads all three to the public bucket.
func publishRegular(ctx context.Context, pub Storage, pubBucket string, logs *weeklyLogs,
	localpath, remotepath, localBomPath, remoteBomPath, workdir string) {
	logger := log.Default()

	// Build the CycloneDX Bill of Materials for the GeoTIFF: the exact bytes
	// (SHA-256/512) and the software revision that produced them. See bom.go
	// and https://github.com/brawer/osmviews/issues/87.
	sha256hex, sha512hex, err := hashFile(localpath)
	if err != nil {
		logger.Fatalf("hashing %s: %v", localpath, err)
	}
	revision, modified := version.Revision()
	if err := writeBOM(localBomPath, bomInputs{
		Date:     logs.lastDay,
		FirstDay: logs.firstDay,
		Weeks:    len(logs.readers),
		SHA256:   sha256hex,
		SHA512:   sha512hex,
		Software: SoftwareVersion,
		Revision: revision,
		Modified: modified,
		Release:  version.Release,
		MaxZoom:  maxZoom,
	}); err != nil {
		logger.Fatalf("building BOM %s: %v", localBomPath, err)
	}

	// Build the Frictionless datapackage.json: the version date and every
	// distribution by relative path, byte size and SHA-256. See datapackage.go
	// and https://github.com/brawer/osmviews/issues/110.
	bomSHA256, _, err := hashFile(localBomPath)
	if err != nil {
		logger.Fatalf("hashing %s: %v", localBomPath, err)
	}
	rasterInfo, err := os.Stat(localpath)
	if err != nil {
		logger.Fatalf("stat %s: %v", localpath, err)
	}
	bomInfo, err := os.Stat(localBomPath)
	if err != nil {
		logger.Fatalf("stat %s: %v", localBomPath, err)
	}
	localDPPath := filepath.Join(workdir, "datapackage.json")
	if err := writeDatapackage(localDPPath, datapackageInputs{
		Date:         logs.lastDay,
		RasterPath:   filepath.Base(remotepath),
		RasterBytes:  rasterInfo.Size(),
		RasterSHA256: sha256hex,
		BOMPath:      filepath.Base(remoteBomPath),
		BOMBytes:     bomInfo.Size(),
		BOMSHA256:    bomSHA256,
	}); err != nil {
		logger.Fatalf("building datapackage.json: %v", err)
	}

	// Upload to the public bucket, and garbage-collect old files. Order is
	// BOM -> GeoTIFF -> datapackage.json: each object is written before the
	// object that references it, so whatever a consumer can reach is already
	// there. datapackage.json last means "I can see version N" implies all of
	// N's files exist.
	const remoteDPPath = "data/datapackage.json"
	uploads := []struct{ remote, local, contentType string }{
		{remoteBomPath, localBomPath, "application/vnd.cyclonedx+json"},
		{remotepath, localpath, "image/tiff"},
		{remoteDPPath, localDPPath, "application/json"},
	}
	for _, u := range uploads {
		if err := pub.PutFile(ctx, pubBucket, u.remote, u.local, u.contentType); err != nil {
			logger.Fatalf("uploading %s/%s: %v", pubBucket, u.remote, err)
		}
	}
	logger.Printf("uploaded %s/%s, %s/%s and %s/%s; %s",
		pubBucket, remoteBomPath, pubBucket, remotepath, pubBucket, remoteDPPath, memStats())
}

// newHTTPClient returns the HTTP client used to fetch tile logs from
// planet.openstreetmap.org. Its timeouts bound the cases where a
// connection stalls without ever failing on its own. A hung fetch would
// otherwise block the run indefinitely, and because the Toolforge job
// runs with concurrencyPolicy: Forbid, every following scheduled run
// would be skipped too. There is deliberately no overall Client.Timeout:
// it would also abort a slow but healthy download of a large weekly log.
// Individual daily-log requests additionally get a generous per-attempt
// deadline and are retried with backoff (see fetchPolicy).
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   30 * time.Second,
			ResponseHeaderTimeout: 2 * time.Minute,
			IdleConnTimeout:       90 * time.Second,
			ExpectContinueTimeout: 10 * time.Second,
		},
	}
}

// Fetch log data for up to `maxWeeks` weeks from planet.openstreetmap.org.
// For each week, the available daily log files are fetched from OpenStreetMap
// and combined into a single compressed file, stored on local disk and in
// object storage. If this weekly file already exists, we return its content
// directly without re-fetching that week from the server. Therefore, if this
// tool is run periodically, it will only fetch content that has not been
// downloaded before.
//
// weeklyLogs is the result of fetchWeeklyLogs.
type weeklyLogs struct {
	readers  []io.Reader // one per week, oldest first
	weights  []float64   // parallel to readers: 7/NumDays for each week
	names    []string    // parallel to readers: weekly log file names in workdir
	lastWeek string      // ISO week of the most recent week, eg. "2021-W28"
	firstDay time.Time   // earliest ingested daily log
	lastDay  time.Time   // most recent ingested daily log
}

func fetchWeeklyLogs(workdir string, storage Storage, bucket string, maxWeeks int) (*weeklyLogs, error) {
	logger := log.Default()
	client := newHTTPClient()
	weeks, err := GetAvailableWeeks(client, time.Now())
	if err != nil {
		return nil, err
	}
	if len(weeks) == 0 {
		return nil, fmt.Errorf("OpenStreetMap has no tile logs")
	}

	if len(weeks) > maxWeeks {
		weeks = weeks[len(weeks)-maxWeeks:]
	}

	partial, fewestDays := 0, 7
	for _, w := range weeks {
		if w.NumDays < 7 {
			partial++
		}
		if w.NumDays < fewestDays {
			fewestDays = w.NumDays
		}
	}
	logger.Printf(
		"found %d weeks with OpenStreetMap tile logs, from %s to %s; "+
			"%d are partial (fewest %d/7 days), their counts are scaled up to a full week",
		len(weeks), weeks[0].Week, weeks[len(weeks)-1].Week, partial, fewestDays)

	readers := make([]io.Reader, 0, len(weeks))
	weights := make([]float64, 0, len(weeks))
	names := make([]string, 0, len(weeks))
	for _, week := range weeks {
		r, err := GetTileLogs(week.Week, week.NumDays, client, workdir, storage, bucket)
		if err != nil {
			return nil, fmt.Errorf("week %s: %w", week.Week, err)
		}
		readers = append(readers, r)
		weights = append(weights, 7.0/float64(week.NumDays))
		names = append(names, weekLogName(week.Week, week.NumDays))
	}
	logger.Printf("all %d weeks ready, %s", len(weeks), memStats())

	return &weeklyLogs{
		readers:  readers,
		weights:  weights,
		names:    names,
		lastWeek: weeks[len(weeks)-1].Week,
		firstDay: weeks[0].FirstDay,
		lastDay:  weeks[len(weeks)-1].LastDay,
	}, nil
}
