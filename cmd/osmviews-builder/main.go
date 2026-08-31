// SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
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
)

func main() {
	ctx := context.Background()

	workdir := flag.String("workdir", "osmviews-builder-workdir", "path to working directory")
	flag.Parse()

	logger := log.Default()
	logger.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lshortfile)

	if *workdir != "" {
		if err := os.MkdirAll(*workdir, 0755); err != nil {
			logger.Fatal(err)
		}
	}

	storage, err := NewStorage()
	if err != nil {
		logger.Fatal(err)
	}
	bucketExists, err := storage.BucketExists(ctx, "osmviews")
	if err != nil {
		logger.Fatal(err)
	}
	if !bucketExists {
		logger.Fatal("storage bucket \"osmviews\" does not exist")
	}

	maxWeeks := 52 // 1 year
	tilecounts, weights, lastWeek, err := fetchWeeklyLogs(*workdir, storage, maxWeeks)
	if err != nil {
		logger.Fatal(err)
	}

	// Construct a file path for the output file. As part of the file name,
	// we use the date of the last day of the last week whose data is being
	// painted. That needs less explanation to users than some file name
	// convention involving ISO weeks, which are less commonly known.
	year, week, err := ParseWeek(lastWeek)
	if err != nil {
		logger.Fatal(err)
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
			msg := fmt.Sprintf("Already in storage: %s/%s and %s/%s", bucket, remotepath, bucket, remoteStatsPath)
			logger.Println(msg)
			return
		}
	}

	// Paint the output GeoTIFF file.
	if err := paint(localpath, 18, tilecounts, weights, ctx); err != nil {
		logger.Fatal(err)
	}

	if err := BuildStats(localpath, localStatsPath, localStatsPlotPath); err != nil {
		logger.Fatal(err)
	}

	// Upload the output file to storage, and garbage-collect old files.
	if storage != nil {
		err := storage.PutFile(ctx, bucket, remotepath, localpath, "image/tiff")
		if err != nil {
			logger.Fatal(err)
		}

		err = storage.PutFile(ctx, bucket, remoteStatsPath, localStatsPath, "application/json")
		if err != nil {
			logger.Fatal(err)
		}

		msg := fmt.Sprintf("Uploaded to storage: %s/%s and %s/%s\n", bucket, remotepath, bucket, remoteStatsPath)
		logger.Println(msg)

		if err := Cleanup(storage); err != nil {
			logger.Fatal(err)
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
// The results are an array of readers (one per week), a parallel array of
// weights (7/NumDays, to extrapolate weeks with missing days to a full week),
// and the ISO week string (like "2021-W28") for the last available week.
func fetchWeeklyLogs(workdir string, storage Storage, maxWeeks int) ([]io.Reader, []float64, string, error) {
	logger := log.Default()
	client := newHTTPClient()
	weeks, err := GetAvailableWeeks(client, time.Now())
	if err != nil {
		return nil, nil, "", err
	}
	if len(weeks) == 0 {
		return nil, nil, "", fmt.Errorf("OpenStreetMap has no tile logs")
	}

	if len(weeks) > maxWeeks {
		weeks = weeks[len(weeks)-maxWeeks:]
	}

	logger.Printf(
		"found %d weeks with OpenStreetMap tile logs, from %s to %s",
		len(weeks), weeks[0].Week, weeks[len(weeks)-1].Week)

	readers := make([]io.Reader, 0, len(weeks))
	weights := make([]float64, 0, len(weeks))
	for _, week := range weeks {
		r, err := GetTileLogs(week.Week, week.NumDays, client, workdir, storage)
		if err != nil {
			return nil, nil, "", err
		}
		readers = append(readers, r)
		weights = append(weights, 7.0/float64(week.NumDays))
	}

	return readers, weights, weeks[len(weeks)-1].Week, nil
}
