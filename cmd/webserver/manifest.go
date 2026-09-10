// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// dataBaseURL is where the CDN serves the published dataset. `/download/…` on
// this webserver redirects there. Flip this to osmviews.brawer.ch at the
// dandelis.ch → brawer.ch cutover (issue #110).
const dataBaseURL = "https://osmviews.dandelis.ch/data"

// datedObjectRegexp matches the immutable per-build object names the CDN serves
// under /data/ (and that /download/ redirects to unchanged).
var datedObjectRegexp = regexp.MustCompile(`^osmviews-\d{8}\.(tiff|cdx\.json)$`)

var isoDateRegexp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Manifest tracks the newest published build by polling data/datapackage.json.
// The webserver needs only the version date, to build the /download/osmviews.tiff
// → dated-object redirect.
type Manifest struct {
	url    string
	client *http.Client

	mu   sync.RWMutex
	date string // "20260906"; "" until the first successful poll
}

func NewManifest(baseURL string) *Manifest {
	return &Manifest{
		url:    baseURL + "/datapackage.json",
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// Date returns the newest build's date as YYYYMMDD and whether it is known.
func (m *Manifest) Date() (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.date, m.date != ""
}

// refresh fetches datapackage.json once and updates the cached date. On error
// the previously cached date is kept.
func (m *Manifest) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.url, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", m.url, resp.Status)
	}

	var doc struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return fmt.Errorf("parsing %s: %w", m.url, err)
	}
	if !isoDateRegexp.MatchString(doc.Version) {
		return fmt.Errorf("%s: version %q is not an ISO date", m.url, doc.Version)
	}

	date := doc.Version[0:4] + doc.Version[5:7] + doc.Version[8:10]
	m.mu.Lock()
	changed := m.date != date
	m.date = date
	m.mu.Unlock()
	if changed {
		log.Printf("current version is %s (from %s)", doc.Version, m.url)
	}
	return nil
}

// Watch polls datapackage.json every interval until ctx is done.
func (m *Manifest) Watch(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := m.refresh(ctx); err != nil {
				log.Printf("manifest refresh: %v", err)
			}
		}
	}
}
