// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/brawer/osmviews/v2/internal/version"
	//"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ServerVersion is returned to HTTP clients as the Server header. main()
// resolves it via internal/version: a released build reports its version,
// any other build the source revision. A linker flag
// (-ldflags "-X main.ServerVersion=…") still overrides it if set.
var ServerVersion = "OSMViews"

func main() {
	ServerVersion = version.Resolve(ServerVersion)
	port := flag.Int("port", 0, "port for serving HTTP requests")
	showVersion := flag.Bool("version", false, "print version and exit")
	dev := flag.Bool("dev", false, "local development: don't poll datapackage.json, so /download/osmviews.tiff returns 503; /, /robots.txt and the dated /download/ redirects still work")
	flag.Parse()
	if *showVersion {
		fmt.Println(ServerVersion)
		return
	}

	if *port == 0 {
		*port, _ = strconv.Atoi(os.Getenv("PORT"))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The webserver no longer stores anything: it redirects /download/… to the
	// CDN (issue #110) and only needs the current version date, which it reads
	// from datapackage.json.
	manifest := NewManifest(dataBaseURL)
	if *dev {
		log.Print("dev mode: not polling datapackage.json; /download/osmviews.tiff will return 503")
	} else {
		if err := manifest.refresh(ctx); err != nil {
			log.Printf("initial datapackage.json fetch failed, continuing: %v", err)
		}
		go manifest.Watch(ctx, 60*time.Second)
	}

	server := &Webserver{manifest: manifest}
	http.HandleFunc("/", server.HandleMain)
	http.HandleFunc("/robots.txt", server.HandleRobotsTxt)
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/download/", server.HandleDownload)
	log.Printf("%s, built with %s, listening for HTTP requests on port %d",
		ServerVersion, runtime.Version(), *port)
	err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
	log.Fatalf("HTTP server stopped: %v", err)
}

type Webserver struct {
	manifest *Manifest
}

// homeURL is the OSMViews homepage: an interactive map of the current data,
// maintained in github.com/brawer/osmviews-app.
const homeURL = "https://osmviews.brawer.ch/"

// HandleMain permanently redirects the root to homeURL. Because it is
// registered for "/", it also receives every path that no other handler
// claims; those are 404.
func (ws *Webserver) HandleMain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Server", ServerVersion)
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, homeURL, http.StatusMovedPermanently)
}

// HandleDownload redirects the legacy /download/ URLs to the CDN (issue #110).
// This webserver holds no data of its own any more.
//
//	/download/osmviews.tiff          302 → <cdn>/data/osmviews-<latest>.tiff
//	/download/osmviews-<date>.tiff   301 → <cdn>/data/osmviews-<date>.tiff
//	/download/osmviews-<date>.cdx.json  301 → same
//	/download/datapackage.json       301 → <cdn>/data/datapackage.json
//
// The dated objects are immutable, so those redirects are permanent; the
// de-dated "latest" one is temporary and its target is the current version
// from datapackage.json.
func (ws *Webserver) HandleDownload(w http.ResponseWriter, req *http.Request) {
	h := w.Header()
	h.Set("Server", ServerVersion)
	h.Set("Access-Control-Allow-Origin", "*")

	name := strings.TrimPrefix(req.URL.Path, "/download/")

	switch req.Method {
	case http.MethodGet, http.MethodHead:
		// handled below
	case http.MethodOptions: // CORS pre-flight
		h.Set("Allow", "GET, HEAD, OPTIONS")
		h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Range, If-Match, If-None-Match, If-Modified-Since, If-Range")
		h.Set("Access-Control-Max-Age", "86400") // 1 day
		w.WriteHeader(http.StatusNoContent)
		return
	default:
		h.Set("Allow", "GET, HEAD, OPTIONS")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if name == "osmviews.tiff" {
		date, ok := ws.manifest.Date()
		if !ok {
			http.Error(w, "current version is not known yet", http.StatusServiceUnavailable)
			return
		}
		h.Set("Cache-Control", "no-store")
		http.Redirect(w, req, dataBaseURL+"/osmviews-"+date+".tiff", http.StatusFound)
		return
	}

	// name is fully validated before it reaches the Location header, so this
	// can only ever redirect to a fixed-shape path under dataBaseURL.
	if name == "datapackage.json" || datedObjectRegexp.MatchString(name) {
		http.Redirect(w, req, dataBaseURL+"/"+name, http.StatusMovedPermanently)
		return
	}

	http.NotFound(w, req)
}

// HandleRobotsTxt sends a constant robots.txt file back to the
// client, allowing web crawlers to access our entire site.  If we
// didn't handle /robots.txt ourselves, Wikimedia's proxy would inject
// a deny-all response and return that to the caller.
func (ws *Webserver) HandleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Server", ServerVersion)

	// https://wikitech.wikimedia.org/wiki/Help:Toolforge/Web#/robots.txt
	h.Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "%s", "User-Agent: *\nAllow: /\n")
}
