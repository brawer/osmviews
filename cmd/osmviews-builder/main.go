// SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	ctx := context.Background()

	workdir := flag.String("workdir", "", "path to working directory")
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
	tilecounts, lastWeek, err := fetchWeeklyLogs(*workdir, storage, maxWeeks)
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
	if err := paint(localpath, 18, tilecounts, ctx); err != nil {
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

// Fetch log data for up to `maxWeeks` weeks from planet.openstreetmap.org.
// For each week, the seven daily log files are fetched from OpenStreetMap,
// and combined into a one single compressed file, stored on local disk.
// If this weekly file already exists on disk, we return its content directly
// without re-fetching that week from the server. Therefore, if this tool
// is run periodically, it will only fetch the content that has not been
// downloaded before. The result is an array of readers (one for each week),
// and the ISO week string (like "2021-W28") for the last available week.
func fetchWeeklyLogs(workdir string, storage Storage, maxWeeks int) ([]io.Reader, string, error) {
	logger := log.Default()
	client := &http.Client{}
	weeks, err := GetAvailableWeeks(client)
	if err != nil {
		return nil, "", err
	}

	if len(weeks) > maxWeeks {
		weeks = weeks[len(weeks)-maxWeeks:]
	}

	logger.Printf(
		"found %d weeks with OpenStreetMap tile logs, from %s to %s",
		len(weeks), weeks[0], weeks[len(weeks)-1])

	readers := make([]io.Reader, 0, len(weeks))
	for _, week := range weeks {
		if r, err := GetTileLogs(week, client, workdir, storage); err == nil {
			readers = append(readers, r)
		} else {
			return nil, "", err
		}
	}

	return readers, weeks[len(weeks)-1], nil
}
