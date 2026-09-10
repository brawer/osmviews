// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestManifest_Refresh(t *testing.T) {
	var body string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/datapackage.json" {
			t.Errorf("unexpected request path %q", r.URL.Path)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	m := NewManifest(srv.URL)
	ctx := context.Background()

	// Good response.
	status, body = 200, `{"version": "2026-09-06", "resources": []}`
	if err := m.refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if d, ok := m.Date(); !ok || d != "20260906" {
		t.Errorf("Date() = %q, %v; want 20260906, true", d, ok)
	}

	// A later, broken response must not clobber the known-good date.
	for _, tc := range []struct {
		name         string
		status, want int
		body         string
	}{
		{"http 500", 500, 0, "nope"},
		{"not json", 200, 0, "<html>"},
		{"version not a date", 200, 0, `{"version": "latest"}`},
	} {
		status, body = tc.status, tc.body
		if err := m.refresh(ctx); err == nil {
			t.Errorf("%s: refresh returned nil error", tc.name)
		}
		if d, _ := m.Date(); d != "20260906" {
			t.Errorf("%s: Date() = %q, want the last good value 20260906", tc.name, d)
		}
	}

	// A new build advances the date.
	status, body = 200, `{"version": "2026-09-13"}`
	if err := m.refresh(ctx); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if d, _ := m.Date(); d != "20260913" {
		t.Errorf("Date() = %q, want 20260913", d)
	}
}

func TestManifest_DateUnknownBeforeFirstPoll(t *testing.T) {
	m := NewManifest("https://example.invalid")
	if d, ok := m.Date(); ok || d != "" {
		t.Errorf("Date() = %q, %v; want \"\", false", d, ok)
	}
}

func TestDatedObjectRegexp(t *testing.T) {
	for _, s := range []string{
		"osmviews-20260906.tiff",
		"osmviews-20250101.cdx.json",
	} {
		if !datedObjectRegexp.MatchString(s) {
			t.Errorf("should match: %q", s)
		}
	}
	for _, s := range []string{
		"osmviews.tiff",
		"osmviews-2026.tiff",
		"osmviews-20260906.txt",
		"osmviews-20260906.tiff.bak",
		"x/osmviews-20260906.tiff",
		"datapackage.json",
	} {
		if datedObjectRegexp.MatchString(s) {
			t.Errorf("should not match: %q", s)
		}
	}
}
