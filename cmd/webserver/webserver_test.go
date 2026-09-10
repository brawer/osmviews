// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sendRequest(method, path string, reqHeader http.Header) (status int, h http.Header, body []byte, err error) {
	req := httptest.NewRequest(method, path, nil)
	req.Header = reqHeader
	w := httptest.NewRecorder()
	testWebserver.HandleDownload(w, req)
	res := w.Result()
	defer res.Body.Close()
	body, err = io.ReadAll(res.Body)
	if err != nil {
		return res.StatusCode, res.Header, body, err
	}
	return res.StatusCode, res.Header, body, nil
}

// testWebserver knows the current version is 2026-09-06.
var testWebserver *Webserver = &Webserver{manifest: &Manifest{date: "20260906"}}

const cdn = "https://osmviews.dandelis.ch/data"

func TestWebserver_DownloadLatestRedirect(t *testing.T) {
	status, header, body, err := sendRequest("GET", "/download/osmviews.tiff", make(http.Header))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusFound {
		t.Errorf("status = %d, want 302", status)
	}
	if got, want := header.Get("Location"), cdn+"/osmviews-20260906.tiff"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if got := header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if !strings.Contains(string(body), "osmviews-20260906.tiff") {
		t.Errorf("redirect body = %q, want it to name the target", string(body))
	}
}

func TestWebserver_DownloadLatestHEAD(t *testing.T) {
	status, header, body, err := sendRequest("HEAD", "/download/osmviews.tiff", make(http.Header))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusFound {
		t.Errorf("status = %d, want 302", status)
	}
	if got, want := header.Get("Location"), cdn+"/osmviews-20260906.tiff"; got != want {
		t.Errorf("Location = %q, want %q", got, want)
	}
	if len(body) != 0 {
		t.Errorf("HEAD body = %q, want empty", string(body))
	}
}

func TestWebserver_DownloadLatestUnknownVersion(t *testing.T) {
	ws := &Webserver{manifest: &Manifest{}} // no successful poll yet
	req := httptest.NewRequest("GET", "/download/osmviews.tiff", nil)
	w := httptest.NewRecorder()
	ws.HandleDownload(w, req)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Result().StatusCode)
	}
}

func TestWebserver_DownloadDatedRedirects(t *testing.T) {
	for _, name := range []string{
		"osmviews-20260830.tiff",
		"osmviews-20260830.cdx.json",
		"osmviews-20250101.tiff",
		"datapackage.json",
	} {
		status, header, _, err := sendRequest("GET", "/download/"+name, make(http.Header))
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusMovedPermanently {
			t.Errorf("%s: status = %d, want 301", name, status)
		}
		if got, want := header.Get("Location"), cdn+"/"+name; got != want {
			t.Errorf("%s: Location = %q, want %q", name, got, want)
		}
		if got := header.Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("%s: Access-Control-Allow-Origin = %q, want *", name, got)
		}
	}
}

func TestWebserver_DownloadNotFound(t *testing.T) {
	for _, name := range []string{
		"unknown",
		"osmviews-2026.tiff",       // date too short
		"osmviews-20260830.txt",    // wrong extension
		"osmviews-20260830.tiff/x", // trailing junk
		"osmviews.tiff.bak",
		"../secrets",
	} {
		status, _, _, err := sendRequest("GET", "/download/"+name, make(http.Header))
		if err != nil {
			t.Fatal(err)
		}
		if status != http.StatusNotFound {
			t.Errorf("%q: status = %d, want 404", name, status)
		}
	}
}

func TestWebserver_DownloadOptions(t *testing.T) {
	status, header, body, err := sendRequest("OPTIONS", "/download/osmviews.tiff", make(http.Header))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusNoContent {
		t.Errorf("status = %d, want 204", status)
	}
	if len(body) > 0 {
		t.Errorf("body = %q, want empty", string(body))
	}
	for k, want := range map[string]string{
		"Allow":                        "GET, HEAD, OPTIONS",
		"Access-Control-Allow-Methods": "GET, HEAD, OPTIONS",
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Max-Age":       "86400",
	} {
		if got := header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got := header.Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "range") {
		t.Errorf("Access-Control-Allow-Headers = %q, want it to include Range", got)
	}
}

func TestWebserver_DownloadMethodNotAllowed(t *testing.T) {
	status, header, body, err := sendRequest("DELETE", "/download/osmviews.tiff", make(http.Header))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", status)
	}
	if len(body) > 0 {
		t.Errorf("body = %q, want empty", string(body))
	}
	if got := header.Get("Allow"); got != "GET, HEAD, OPTIONS" {
		t.Errorf("Allow = %q, want GET, HEAD, OPTIONS", got)
	}
}

// sendToHandler drives an arbitrary Webserver handler, like sendRequest does for
// HandleDownload.
func sendToHandler(handler http.HandlerFunc, method, path string) (int, http.Header, []byte) {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	handler(w, req)
	res := w.Result()
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, body
}

// The test binary is usually built without running "npm run build", so
// internal/webui/dist holds only the placeholder and HandleBeta serves the
// "not built" page. These assertions hold either way.
func TestWebserver_Beta(t *testing.T) {
	status, header, body := sendToHandler(testWebserver.HandleBeta, "GET", "/beta/")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if got := header.Get("X-Robots-Tag"); got != "noindex" {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
	if got := header.Get("Server"); got == "" {
		t.Error("Server header not set")
	}
	if ct := header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if len(body) == 0 {
		t.Error("empty body")
	}
}

func TestWebserver_BetaClientRouteFallsBackToIndex(t *testing.T) {
	// An unknown path with no extension is a client-side route: serve the app.
	status, header, _ := sendToHandler(testWebserver.HandleBeta, "GET", "/beta/some/deep/route")
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if got := header.Get("X-Robots-Tag"); got != "noindex" {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
}

func TestWebserver_BetaMissingAssetIs404(t *testing.T) {
	status, _, _ := sendToHandler(testWebserver.HandleBeta, "GET", "/beta/assets/app-deadbeef.js")
	if status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", status)
	}
}

// When the frontend has actually been built (CI, or a local "npm run build"),
// a content-hashed asset must be served with a long, immutable Cache-Control.
func TestWebserver_BetaAssetIsCachedHard(t *testing.T) {
	entries, err := fs.ReadDir(betaFS, "assets")
	if err != nil {
		t.Skip("frontend not built into this binary; skipping asset check")
	}
	var asset string
	for _, e := range entries {
		if !e.IsDir() {
			asset = e.Name()
			break
		}
	}
	if asset == "" {
		t.Skip("no built assets")
	}
	status, header, _ := sendToHandler(testWebserver.HandleBeta, "GET", "/beta/assets/"+asset)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want immutable", got)
	}
	if got := header.Get("X-Robots-Tag"); got != "noindex" {
		t.Errorf("X-Robots-Tag = %q, want noindex", got)
	}
}

func TestWebserver_RobotsTxtDisallowsBeta(t *testing.T) {
	_, _, body := sendToHandler(testWebserver.HandleRobotsTxt, "GET", "/robots.txt")
	if !strings.Contains(string(body), "Disallow: /beta/") {
		t.Errorf("robots.txt = %q, want it to disallow /beta/", string(body))
	}
}
