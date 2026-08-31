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
	"time"

	"github.com/brawer/osmviews/v2/internal/version"
)

func main() {
	SoftwareVersion = version.Resolve(SoftwareVersion)
	ctx := context.Background()

	workdir := flag.String("workdir", "osmviews-builder-workdir", "path to working directory")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(SoftwareVersion)
		return
	}

	logger := log.Default()
	logger.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lshortfile)
	logger.Printf("starting %s", SoftwareVersion)

	if *workdir != "" {
		if err := os.MkdirAll(*workdir, 0755); err != nil {
			logger.Fatalf("creating workdir %s: %v", *workdir, err)
		}
	}

	storage, err := NewStorage()
	if err != nil {
		logger.Fatalf("connecting to object storage: %v", err)
	}
	bucketExists, err := storage.BucketExists(ctx, "osmviews")
	if err != nil {
		logger.Fatalf("checking for bucket \"osmviews\": %v", err)
	}
	if !bucketExists {
		logger.Fatal("storage bucket \"osmviews\" does not exist")
	}

	maxWeeks := 52 // 1 year
	logs, err := fetchWeeklyLogs(*workdir, storage, maxWeeks)
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
	bucket := "osmviews"
	localpath := filepath.Join(*workdir, fmt.Sprintf("osmviews-%s.tiff", date))
	localStatsPath := filepath.Join(*workdir, fmt.Sprintf("osmviews-stats-%s.json", date))
	localStatsPlotPath := filepath.Join(*workdir, fmt.Sprintf("osmviews-statsplot-%s.png", date))
	remotepath := fmt.Sprintf("public/osmviews-%s.tiff", date)
	remoteStatsPath := fmt.Sprintf("public/osmviews-stats-%s.json", date)

	// Check if the output file already exists in storage.
	// If we can retrieve object stats without an error, we don’t need
	// to do anything and are completely done.
	if storage != nil {
		_, err := storage.Stat(ctx, bucket, remotepath)
		hasGeoTiff := err == nil
		_, err = storage.Stat(ctx, bucket, remoteStatsPath)
		hasStats := err == nil
		if hasGeoTiff && hasStats {
			logger.Printf("already in storage: %s/%s and %s/%s; nothing to do",
				bucket, remotepath, bucket, remoteStatsPath)
			return
		}
	}

	// Paint the output GeoTIFF file.
	meta := TiffMetadata{
		Description: fmt.Sprintf(
			"OpenStreetMap view density, in weekly user views per km2. "+
				"Tile logs %s..%s, generated %s. https://osmviews.toolforge.org",
			logs.firstDay.Format("2006-01-02"), logs.lastDay.Format("2006-01-02"),
			time.Now().UTC().Format("2006-01-02 15:04 UTC")),
		DateTime: logs.lastDay,
	}
	if err := paint(localpath, 18, logs.readers, logs.weights, meta, ctx); err != nil {
		logger.Fatalf("painting %s: %v", localpath, err)
	}

	logger.Printf("computing statistics from %s", localpath)
	if err := BuildStats(localpath, localStatsPath, localStatsPlotPath); err != nil {
		logger.Fatalf("computing statistics: %v", err)
	}

	// Upload the output file to storage, and garbage-collect old files.
	if storage != nil {
		if err := storage.PutFile(ctx, bucket, remotepath, localpath, "image/tiff"); err != nil {
			logger.Fatalf("uploading %s/%s: %v", bucket, remotepath, err)
		}
		if err := storage.PutFile(ctx, bucket, remoteStatsPath, localStatsPath, "application/json"); err != nil {
			logger.Fatalf("uploading %s/%s: %v", bucket, remoteStatsPath, err)
		}
		logger.Printf("uploaded %s/%s and %s/%s; done, %s",
			bucket, remotepath, bucket, remoteStatsPath, memStats())

		if err := Cleanup(storage); err != nil {
			logger.Fatalf("garbage-collecting old files in storage: %v", err)
		}
	}
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
	lastWeek string      // ISO week of the most recent week, eg. "2021-W28"
	firstDay time.Time   // earliest ingested daily log
	lastDay  time.Time   // most recent ingested daily log
}

func fetchWeeklyLogs(workdir string, storage Storage, maxWeeks int) (*weeklyLogs, error) {
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
	for _, week := range weeks {
		r, err := GetTileLogs(week.Week, week.NumDays, client, workdir, storage)
		if err != nil {
			return nil, fmt.Errorf("week %s: %w", week.Week, err)
		}
		readers = append(readers, r)
		weights = append(weights, 7.0/float64(week.NumDays))
	}
	logger.Printf("all %d weeks ready, %s", len(weeks), memStats())

	return &weeklyLogs{
		readers:  readers,
		weights:  weights,
		lastWeek: weeks[len(weeks)-1].Week,
		firstDay: weeks[0].FirstDay,
		lastDay:  weeks[len(weeks)-1].LastDay,
	}, nil
}
