// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestWebserver_Download(t *testing.T) {
	rh := make(http.Header)
	status, header, body, err := sendRequest("GET", "/download/c.txt", rh)
	if err != nil {
		t.Error(err)
		return
	}

	if status != http.StatusOK {
		t.Errorf("want StatusCode %d, got %d", http.StatusOK, status)
	}

	want := "Content"
	if string(body) != want {
		t.Errorf(`want body="%s", got "%s"`, want, string(body))
	}

	want = "text/plain"
	if got := header.Get("Content-Type"); got != want {
		t.Errorf(`want "Content-Type: %s", got "%s"`, want, got)
	}

	want = "Tue, 21 Nov 2023 19:20:21 GMT"
	if got := header.Get("Last-Modified"); got != want {
		t.Errorf(`expected "Last-Modified: %s", got "%s"`, want, got)
	}

	want = `"ETag-123"`
	if got := header.Get("ETag"); got != want {
		t.Errorf(`expected "ETag: %s", got "%s"`, want, got)
	}

	want = "*"
	if got := header.Get("Access-Control-Allow-Origin"); got != want {
		t.Errorf(`expected "Access-Control-Allow-Origin: %s", got "%s"`, want, got)
	}
}

func TestWebserver_DownloadETagMatch(t *testing.T) {
	rh := make(http.Header)
	rh.Set("If-None-Match", `"ETag-123"`)
	status, header, body, err := sendRequest("GET", "/download/c.txt", rh)
	if err != nil {
		t.Error(err)
		return
	}

	if status != http.StatusNotModified {
		t.Errorf("want StatusCode %d, got %d", http.StatusNotModified, status)
	}

	if len(body) > 0 {
		t.Errorf(`want empty body, got "%s"`, string(body))
	}

	want := `"ETag-123"`
	if got := header.Get("ETag"); got != want {
		t.Errorf(`expected "ETag: %s", got "%s"`, want, got)
	}
}

func TestWebserver_DownloadNotFound(t *testing.T) {
	rh := make(http.Header)
	status, _, _, err := sendRequest("GET", "/download/unkown", rh)
	if err != nil {
		t.Error(err)
		return
	}

	if status != http.StatusNotFound {
		t.Errorf("want StatusCode %d, got %d", http.StatusNotFound, status)
	}
}

func TestWebserver_DownloadOptions(t *testing.T) {
	rh := make(http.Header)
	status, header, body, err := sendRequest("OPTIONS", "/download/c.txt", rh)
	if err != nil {
		t.Error(err)
		return
	}

	if status != http.StatusNoContent {
		t.Errorf("want StatusCode %d, got %d", http.StatusNoContent, status)
	}

	if len(body) > 0 {
		t.Errorf(`want empty body, got "%s"`, string(body))
	}

	want := "GET, HEAD, OPTIONS"
	if got := header.Get("Allow"); got != want {
		t.Errorf(`expected "Allow: %s", got "%s"`, want, got)
	}
	if got := header.Get("Access-Control-Allow-Methods"); got != want {
		t.Errorf(`expected "Access-Control-Allow-Methods: %s", got "%s"`, want, got)
	}

	want = "*"
	if got := header.Get("Access-Control-Allow-Origin"); got != want {
		t.Errorf(`expected "Access-Control-Allow-Origin: %s", got "%s"`, want, got)
	}

	want = "ETag, If-Match, If-None-Match, If-Modified-Since, If-Range, Range"
	if got := header.Get("Access-Control-Allow-Headers"); got != want {
		t.Errorf(`expected "Access-Control-Allow-Headers: %s", got "%s"`, want, got)
	}

	want = "ETag"
	if got := header.Get("Access-Control-Expose-Headers"); got != want {
		t.Errorf(`expected "Access-Control-Expose-Headers: %s", got "%s"`, want, got)
	}

	want = "86400"
	if got := header.Get("Access-Control-Max-Age"); got != want {
		t.Errorf(`expected "Access-Control-Max-Age: %s", got "%s"`, want, got)
	}

}

func TestWebserver_DownloadOptionsNotFound(t *testing.T) {
	rh := make(http.Header)
	status, _, _, err := sendRequest("OPTIONS", "/download/unkown", rh)
	if err != nil {
		t.Error(err)
		return
	}

	if status != http.StatusNotFound {
		t.Errorf("want StatusCode %d, got %d", http.StatusNotFound, status)
	}
}

func TestWebserver_DownloadMethodNotAllowed(t *testing.T) {
	rh := make(http.Header)
	status, header, body, err := sendRequest("DELETE", "/download/c.txt", rh)
	if err != nil {
		t.Error(err)
		return
	}

	if status != http.StatusMethodNotAllowed {
		t.Errorf("want StatusCode %d, got %d", http.StatusMethodNotAllowed, status)
	}

	if len(body) > 0 {
		t.Errorf(`want empty body, got "%s"`, string(body))
	}

	want := "GET, HEAD, OPTIONS"
	if got := header.Get("Allow"); got != want {
		t.Errorf(`expected "Allow: %s", got "%s"`, want, got)
	}
}

var testWebserver *Webserver = makeTestWebserver()

func makeTestWebserver() *Webserver {
	storage := &Storage{
		client:  &fakeStorageClient{},
		workdir: os.TempDir(),
		files:   make(map[string]*localFile, 10),
	}

	path := filepath.Join(storage.workdir, "c.txt")
	if err := os.WriteFile(path, []byte("Content"), 0644); err != nil {
		log.Fatal(err)
	}

	lastmod, _ := time.Parse(time.RFC3339, "2023-11-21T19:20:21Z")
	storage.files["c.txt"] = &localFile{
		Path:         path,
		ContentType:  "text/plain",
		ETag:         "ETag-123",
		LastModified: lastmod,
	}

	tiffPath := filepath.Join(storage.workdir, "t.tiff")
	if err := os.WriteFile(tiffPath, []byte("II*\x00 fake geotiff"), 0644); err != nil {
		log.Fatal(err)
	}
	storage.files["osmviews.tiff"] = &localFile{
		Path:         tiffPath,
		ContentType:  "image/tiff",
		ETag:         "ETag-tiff",
		LastModified: lastmod,
		Version:      "20260830",
	}
	bomPath := filepath.Join(storage.workdir, "b.cdx.json")
	if err := os.WriteFile(bomPath, []byte(`{"bomFormat":"CycloneDX"}`), 0644); err != nil {
		log.Fatal(err)
	}
	storage.files["osmviews-20260830.cdx.json"] = &localFile{
		Path:         bomPath,
		ContentType:  bomContentType,
		ETag:         "ETag-bom",
		LastModified: lastmod,
		Version:      "20260830",
	}

	return &Webserver{storage: storage}
}

func TestWebserver_DownloadGeoTIFFLinksBOM(t *testing.T) {
	_, header, _, err := sendRequest("GET", "/download/osmviews.tiff", make(http.Header))
	if err != nil {
		t.Fatal(err)
	}
	want := `</download/osmviews-20260830.cdx.json>; rel="describedby"; type="application/vnd.cyclonedx+json"`
	if got := header.Get("Link"); got != want {
		t.Errorf("Link header = %q, want %q", got, want)
	}
	if got := header.Get("Access-Control-Expose-Headers"); got != "ETag, Link" {
		t.Errorf("Access-Control-Expose-Headers = %q, want %q", got, "ETag, Link")
	}
}

func TestWebserver_DownloadBOMContentType(t *testing.T) {
	status, header, _, err := sendRequest("GET", "/download/osmviews-20260830.cdx.json", make(http.Header))
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if got := header.Get("Content-Type"); got != "application/vnd.cyclonedx+json" {
		t.Errorf("Content-Type = %q, want application/vnd.cyclonedx+json", got)
	}
	if header.Get("Link") != "" {
		t.Errorf("BOM response should carry no Link header, got %q", header.Get("Link"))
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
