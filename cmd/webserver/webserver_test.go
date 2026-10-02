// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"io"
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

const cdn = "https://osmviews.brawer.ch/data"

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

func TestWebserver_RobotsTxt(t *testing.T) {
	_, _, body := sendToHandler(testWebserver.HandleRobotsTxt, "GET", "/robots.txt")
	if got, want := string(body), "User-Agent: *\nAllow: /\n"; got != want {
		t.Errorf("robots.txt = %q, want %q", got, want)
	}
}

func TestWebserver_HomeRedirect(t *testing.T) {
	for _, method := range []string{"GET", "HEAD"} {
		status, header, _ := sendToHandler(testWebserver.HandleMain, method, "/")
		if status != http.StatusMovedPermanently {
			t.Errorf("%s /: status = %d, want 301", method, status)
		}
		if got := header.Get("Location"); got != "https://osmviews.brawer.ch/" {
			t.Errorf("%s /: Location = %q, want https://osmviews.brawer.ch/", method, got)
		}
		if got := header.Get("Server"); got == "" {
			t.Errorf("%s /: Server header not set", method)
		}
	}
}

func TestWebserver_UnknownPathNotFound(t *testing.T) {
	for _, path := range []string{"/index.html", "/beta/", "/foo/bar"} {
		status, _, _ := sendToHandler(testWebserver.HandleMain, "GET", path)
		if status != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", path, status)
		}
	}
}
