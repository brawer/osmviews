// SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"math/bits"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/lanrat/extsort"
	"github.com/ulikunitz/xz"
	"golang.org/x/sync/errgroup"
)

// WeekAvailability describes the OpenStreetMap tile-log data that is
// available for one ISO week.
type WeekAvailability struct {
	Week    string // ISO 8601 week, eg. "2021-W07"
	NumDays int    // number of days (1..7) that have a log file
}

// Return the weeks for which OpenStreetMap has tile logs, together with
// how many of the week’s seven days actually have a log file. The result
// is sorted from least to most recent week.
//
// Until mid-2026, OpenStreetMap published a log file for every single day,
// and we only accepted weeks with all seven days present. Since then, only
// a few days per week are published, so we accept any week with at least
// one day and let the caller scale the counts by 7/NumDays. Weeks that are
// not over yet (plus a two-day grace period for the last days to appear)
// are skipped: their data would be incomplete, and the output file name
// for the most recent week is keyed only on the week, so we would never
// revisit it once written.
func GetAvailableWeeks(client *http.Client, now time.Time) ([]WeekAvailability, error) {
	url := "https://planet.openstreetmap.org/tile_logs/"
	r, err := client.Get(url)
	if err != nil {
		return nil, err
	}

	// Only accept HTTP responses with status code 200 OK
	// and when the Content-Type header is HTML.
	contentType := r.Header.Get("Content-Type")
	if strings.ContainsRune(contentType, ';') { // text/html;charset=UTF-8
		contentType = strings.Split(contentType, ";")[0]
	}
	if r.StatusCode != 200 || contentType != "text/html" {
		return nil, fmt.Errorf("failed to fetch %s, StatusCode=%d Content-Type=%s", url, r.StatusCode, contentType)
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	// Find out what weeks are available. For each week, we keep a bitmask
	// that tells for which days of that week the OSM Planet server
	// has log files available. For example, if this map contains
	// the entry 202107 → 5 (in binary: 0000101), the server has log files
	// for Tuesday (0000100) and Sunday (0000001) for the 7th week of 2021.
	// That is, Tuesday, February 16, and Sunday, February 21.
	re := regexp.MustCompile(`<a href="tiles-(\d{4}-\d\d-\d\d)\.txt\.xz">`)
	available := make(map[int]uint8) // (year*100+isoweek) → 7 bits
	for _, m := range re.FindAllSubmatch(body, -1) {
		if t, err := time.Parse("2006-01-02", string(m[1])); err == nil {
			year, week := t.ISOWeek()
			available[year*100+week] |= 1 << uint(t.Weekday())
		}
	}

	now = now.UTC()
	result := make([]WeekAvailability, 0, len(available))
	for key, days := range available {
		year, week := key/100, key%100
		// Skip the current week and any week that ended in the last two
		// days, giving OpenStreetMap time to publish that week's last days.
		weekEnd := weekStart(year, week).AddDate(0, 0, 7)
		if !weekEnd.AddDate(0, 0, 2).Before(now) {
			continue
		}
		result = append(result, WeekAvailability{
			Week:    fmt.Sprintf("%04d-W%02d", year, week),
			NumDays: bits.OnesCount8(days),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Week < result[j].Week })
	return result, nil
}

var tileLogRegexp = regexp.MustCompile(`^(\d+)/(\d+)/(\d+)\s+(\d+)$`)

// weekLogName returns the file name (also used as the object-storage key
// suffix) for the merged, sorted tile logs of one ISO week. Weeks with all
// seven days keep the historical name; weeks with fewer days carry the day
// count, so that a later run recomputes the week if OpenStreetMap has
// published additional days in the meantime.
func weekLogName(week string, numDays int) string {
	if numDays >= 7 {
		return fmt.Sprintf("tilelogs-%s.br", week)
	}
	return fmt.Sprintf("tilelogs-%s-%dd.br", week, numDays)
}

// GetTileLogs returns an io.Reader for the sorted log records of a week.
// numDays is how many of the week’s seven days OpenStreetMap has published;
// it selects the cache key and lets a later run pick up additional days.
// If workdir already contains cached records for the requested week,
// the data will be read from local disk. Otherwise, the daily log files
// for the requested week are fetched from the OpenStreetMap planet server
// (missing days are skipped), uncompressed, sorted by TileKey, and stored
// as a compressed file into workdir and object storage.
func GetTileLogs(week string, numDays int, client *http.Client, workdir string, storage Storage) (io.Reader, error) {
	ctx := context.Background()
	logger := log.Default()

	name := weekLogName(week, numDays)
	path := filepath.Join(workdir, name)
	if f, err := os.Open(path); err == nil {
		logger.Printf("for week %s, reading %s from workdir", week, path)
		return brotli.NewReader(f), nil
	}

	remotePath := fmt.Sprintf("internal/osmviews-builder/%s", name)
	remotePathExists := false
	if _, err := storage.Stat(ctx, "osmviews", remotePath); err == nil {
		remotePathExists = true
	}

	if remotePathExists {
		logger.Printf("for week %s, loading s3://osmviews/%s to %s", week, remotePath, path)
		if err := Download(storage, "osmviews", remotePath, path); err != nil {
			logger.Printf("cannot download %s to %s, err=%v", remotePath, path, err)
			return nil, err
		}

		if f, err := os.Open(path); err == nil {
			return brotli.NewReader(f), nil
		} else {
			return nil, err
		}
	}

	logger.Printf("for week %s, computing %s", week, path)
	if err := os.MkdirAll(workdir, os.ModePerm); err != nil {
		return nil, err
	}

	ch := make(chan extsort.SortType, 100000)
	g, subCtx := errgroup.WithContext(ctx)
	config := extsort.DefaultConfig()
	config.NumWorkers = runtime.NumCPU()
	sorter, outChan, errChan := extsort.New(ch, TileCountFromBytes, TileCountLess, config)
	g.Go(func() error {
		return fetchWeeklyTileLogs(week, client, ch, subCtx)
	})
	g.Go(func() error {
		sorter.Sort(ctx) // not subCtx, as per extsort docs
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// We write to a temporary file first, and rename it atomically
	// once it is finished in usable state. This prevents hiccups
	// in case the process crashes (or the machine dies) while the
	// output file is being written.
	tmppath := path + ".tmp"
	tmpfile, err := os.Create(tmppath)
	if err != nil {
		return nil, err
	}
	defer tmpfile.Close()
	writer := brotli.NewWriterLevel(tmpfile, 9)
	defer writer.Close()

	var last TileCount
	for data := range outChan {
		cur := data.(TileCount)
		if cur.Key != last.Key {
			if last.Count > 0 {
				zoom, x, y := last.Key.ZoomXY()
				fmt.Fprintf(writer, "%d/%d/%d %d\n", zoom, x, y, last.Count)
			}
			last = cur
		} else {
			last.Count += cur.Count
		}
	}
	if last.Count > 0 {
		zoom, x, y := last.Key.ZoomXY()
		fmt.Fprintf(writer, "%d/%d/%d %d\n", zoom, x, y, last.Count)
	}

	// Check for errors from the external sorting library.
	if err := <-errChan; err != nil {
		return nil, err
	}

	// Close writer/compressor, ask kernel to ensure temp file is on disk, and close it.
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := tmpfile.Sync(); err != nil {
		return nil, err
	}
	if err := tmpfile.Close(); err != nil {
		return nil, err
	}

	// Now that we have the result on disk, rename it to final path.
	if err := os.Rename(tmppath, path); err != nil {
		return nil, err
	}

	// Upload the file to object storage.
	contentType := "application/x-brotli"
	if err := storage.PutFile(ctx, "osmviews", remotePath, path, contentType); err != nil {
		logger.Printf("upload of %s to s3://osmviews/%s failed: %v", path, remotePath, err)
		return nil, err
	}

	// Open the file for reading and return a reader for it.
	if f, err := os.Open(path); err == nil {
		return brotli.NewReader(f), nil
	} else {
		return nil, err
	}
}

func fetchWeeklyTileLogs(week string, client *http.Client, ch chan<- extsort.SortType, ctx context.Context) error {
	defer close(ch)

	// Fetch the tile logs for each day of this week that OpenStreetMap
	// has published; days without a log file are skipped (see fetchTileLogs).
	parsedYear, parsedWeek, err := ParseWeek(week)
	if err != nil {
		return err
	}

	// Initially we did the fetches in parallel, but planet.openstreetmap.org
	// only seems to accept 1-2 connections from the same IP address.
	firstDay := weekStart(parsedYear, parsedWeek)
	for i := 0; i < 7; i++ {
		day := firstDay.AddDate(0, 0, i)
		if err := fetchTileLogs(day, client, ch, ctx); err != nil {
			return err
		}
	}

	return nil
}

func fetchTileLogs(day time.Time, client *http.Client, ch chan<- extsort.SortType, ctx context.Context) error {
	url := fmt.Sprintf(
		"https://planet.openstreetmap.org/tile_logs/tiles-%04d-%02d-%02d.txt.xz",
		day.Year(), day.Month(), day.Day())
	r, err := client.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()

	// OpenStreetMap no longer publishes a log file for every day of the
	// week. A missing day is expected; skip it. Its absence is accounted
	// for by the 7/NumDays scaling factor (see GetAvailableWeeks).
	if r.StatusCode == http.StatusNotFound {
		log.Default().Printf("no tile logs for %04d-%02d-%02d, skipping",
			day.Year(), day.Month(), day.Day())
		return nil
	}
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch %s, StatusCode=%d", url, r.StatusCode)
	}

	reader, err := xz.NewReader(r.Body)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		// Check if our task has been canceled. Typically this can happen
		// because of an error in another goroutine in the same x.sync.errroup.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if tc := ParseTileCount(scanner.Text()); tc.Count > 0 {
			ch <- tc
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	return nil
}

// Reverse of Go’s time.ISOWeek() function.
func weekStart(year, week int) time.Time {
	// Find the first Monday before July 1 of the given year.
	t := time.Date(year, 7, 1, 0, 0, 0, 0, time.UTC)
	if wd := t.Weekday(); wd == time.Sunday {
		t = t.AddDate(0, 0, -6)
	} else {
		t = t.AddDate(0, 0, -int(wd)+1)
	}

	_, w := t.ISOWeek()
	return t.AddDate(0, 0, (week-w)*7)
}

var isoWeekRegexp = regexp.MustCompile(`(\d{4})-W(\d{2})`)

// ParseWeek gives the year and week for an ISO week string like "2018-W34".
func ParseWeek(s string) (year int, week int, err error) {
	match := isoWeekRegexp.FindStringSubmatch(s)
	if match == nil || len(match) != 3 {
		return 0, 0, fmt.Errorf("week not in ISO 8601 format: %s", s)
	}

	year, _ = strconv.Atoi(match[1])
	week, _ = strconv.Atoi(match[2])
	return year, week, nil
}
