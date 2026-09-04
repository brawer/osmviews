// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/brawer/osmviews/v2/internal/version"
	"github.com/brawer/osmviews/v2/internal/webui"
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
	workdir := flag.String("workdir", "webserver-workdir", "path to working directory on local disk")
	showVersion := flag.Bool("version", false, "print version and exit")
	dev := flag.Bool("dev", false, "local development: skip object storage, so /download/ 404s but /, /beta/ and /robots.txt work without S3 credentials")
	flag.Parse()
	if *showVersion {
		fmt.Println(ServerVersion)
		return
	}

	if *port == 0 {
		*port, _ = strconv.Atoi(os.Getenv("PORT"))
	}

	if *workdir != "" {
		if err := os.MkdirAll(*workdir, 0755); err != nil {
			log.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var storage *Storage
	if *dev {
		log.Print("dev mode: object storage disabled, /download/ will return 404")
		storage = &Storage{files: make(map[string]*localFile)}
	} else {
		var err error
		storage, err = NewStorage(*workdir)
		if err != nil {
			log.Fatal(err)
		}
		if err := storage.Reload(context.Background()); err != nil {
			log.Fatal(err)
		}
		go storage.Watch(ctx)
	}

	server := &Webserver{storage: storage}
	http.HandleFunc("/", server.HandleMain)
	http.HandleFunc("/robots.txt", server.HandleRobotsTxt)
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/download/", server.HandleDownload)
	http.HandleFunc("/beta/", server.HandleBeta)
	log.Printf("%s listening for HTTP requests on port %d", ServerVersion, *port)
	err := http.ListenAndServe(":"+strconv.Itoa(*port), nil)
	log.Fatalf("HTTP server stopped: %v", err)
}

type Webserver struct {
	storage *Storage
}

func (ws *Webserver) HandleMain(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Server", ServerVersion)

	fmt.Fprint(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<link href='https://tools-static.wmflabs.org/fontcdn/css?family=Roboto+Slab:400,700' rel='stylesheet' type='text/css'/>
<link href='https://tools-static.wmflabs.org/fontcdn/css?family=Source+Code+Pro:400' rel='stylesheet' type='text/css'/>
<meta name='viewport' content='width=device-width, initial-scale=1.0'>
<style>
* {
  box-sizing: border-box;
  font-family: 'Roboto Slab', serif;
}
h1 {
  margin-left: 1em;
  margin-top: 1em;
}
.osm { color: #ff0088 }
p { margin-left: 5em }
pre.code {
  margin-left: 9em;
  white-space: pre;
  font-family: 'Source Code Pro', monospace;
}
a:link { color: #ff77bb }
a:hover { color: #ff48a5 }
a:active { color: #ff0088 }
a:visited { color: #ffaed7 }
</style>
</head>
<body><h1><span class="osm">OSM</span>Views</h1>

<p>World-wide ranking of geographic locations based on OpenStreetMap tile logs.
<br/>Updated weekly. Aggregated over the past 52 weeks to smoothen seasonal effects.
<br/>For any location on the planet, up to ~150m/z18 resolution.</p>

<p><b>Use from Python:</b></p>

<pre class="code"># pip install osmviews
import osmviews

# Fetch the GeoTIFF (~594 MB, updated weekly) from osmviews.DOWNLOAD_URL
# to a local file, then look up locations by (longitude, latitude):
with osmviews.open('osmviews.tiff') as o:
    print(f'Tokyo, Shibuya:      {o.rank(139.7013,  35.6586):.2f}')
    print(f'Zürich, Altstetten:  {o.rank(  8.4889,  47.3915):.2f}')
    print(f'Ushuaia:             {o.rank(-68.3030, -54.8019):.2f}')
    print(f'Sahara:              {o.rank( 13.0000,  23.0000):.2f}')

Tokyo, Shibuya:      0.69
Zürich, Altstetten:  0.66
Ushuaia:             0.56
Sahara:              0.00
</pre>

<p>Ranks range from 0.0 (never viewed) to 1.0 (most viewed). For
high-throughput lookups, use the
<a href="https://github.com/brawer/osmviews-rs">Rust client</a>, which is
considerably faster than the Python one.</p>

<p>
<b>Author:</b> <a href="https://brawer.ch/">Sascha Brawer</a>
<br/><b>Backend:</b>
<a href="https://github.com/brawer/osmviews">github.com/brawer/osmviews</a>
<br/><b>Clients:</b>
<a href="https://github.com/brawer/osmviews-py">Python</a>,
<a href="https://github.com/brawer/osmviews-rs">Rust</a>
<br/><b>Download:</b> <a href="download/osmviews.tiff">Cloud-Optimized GeoTIFF</a>
<br/><b>Provenance:</b> <a href="https://github.com/brawer/osmviews/blob/main/docs/downloads.md">bill of materials &amp; verification</a>
<br/><b>License:</b> <a href="https://creativecommons.org/publicdomain/zero/1.0/">CC0-1.0</a> (data), <a href="https://en.wikipedia.org/wiki/MIT_License">MIT</a> (code)
</p>

<p><img src="https://mirrors.creativecommons.org/presskit/buttons/88x31/svg/cc-zero.svg"
width="88" height="31" alt="Public Domain" style="float:left"/></p>

</body></html>`)
}

func (ws *Webserver) HandleDownload(w http.ResponseWriter, req *http.Request) {
	if !strings.HasPrefix(req.URL.Path, "/download/") {
		http.NotFound(w, req)
		return
	}

	path := strings.TrimPrefix(req.URL.Path, "/download/")
	c, err := ws.storage.Retrieve(path)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			// The request path, and the error text that echoes it, are
			// attacker-influenced: strip line breaks so a crafted path
			// cannot forge or split log lines.
			log.Printf("serving /download/%s: %s", stripLineBreaks(path), stripLineBreaks(err.Error()))
		}
		http.NotFound(w, req)
		return
	}
	defer c.Close()

	h := w.Header()
	h.Set("Server", ServerVersion)

	switch req.Method {
	case http.MethodHead:
		fallthrough

	case http.MethodGet:
		// As per https://tools.ietf.org/html/rfc7232, ETag must have quotes.
		h.Set("ETag", fmt.Sprintf(`"%s"`, c.ETag))
		h.Set("Content-Type", c.ContentType)
		h.Set("Access-Control-Allow-Origin", "*")
		// Point consumers at the CycloneDX BOM for this exact GeoTIFF
		// version, so a supply-chain client need not parse a TIFF tag.
		if c.Version != "" && strings.HasSuffix(path, ".tiff") {
			stem := strings.TrimSuffix(path, ".tiff")
			h.Set("Link", fmt.Sprintf(
				`</download/%s-%s.cdx.json>; rel="describedby"; type="%s"`,
				stem, c.Version, bomContentType))
			h.Set("Access-Control-Expose-Headers", "ETag, Link")
		}
		http.ServeContent(w, req, "", c.LastModified, c)

	case http.MethodOptions: // CORS pre-flight
		h.Set("Allow", "GET, HEAD, OPTIONS")
		h.Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "ETag, If-Match, If-None-Match, If-Modified-Since, If-Range, Range")
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Expose-Headers", "ETag")
		h.Set("Access-Control-Max-Age", "86400") // 1 day
		w.WriteHeader(http.StatusNoContent)

	default:
		h.Set("Allow", "GET, HEAD, OPTIONS")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// stripLineBreaks removes the CR and LF characters that could otherwise let
// an attacker-controlled string forge or split entries in a plain-text log.
func stripLineBreaks(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// betaFS holds the built frontend single-page app (internal/webui/dist),
// embedded at compile time. On a checkout where the frontend has not been
// built it contains only a placeholder and no index.html.
var betaFS = webui.FS()

// betaNotBuiltHTML is served for /beta/ when the binary was built without
// running "npm run build" first (e.g. a bare `go run ./cmd/webserver`).
const betaNotBuiltHTML = `<!doctype html><meta charset="utf-8">` +
	`<title>OSMViews beta</title>` +
	`<p>The <code>/beta/</code> app is not built into this binary. ` +
	`Run <code>npm ci &amp;&amp; npm run build</code>, then rebuild.`

// HandleBeta serves the embedded single-page app under /beta/. Content-hashed
// assets are cached hard; every other path falls back to index.html so the
// client-side router can handle it. The whole tree is marked noindex while the
// app is a moving target (there is also a Disallow in robots.txt).
func (ws *Webserver) HandleBeta(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Server", ServerVersion)
	h.Set("X-Robots-Tag", "noindex")

	name := strings.TrimPrefix(r.URL.Path, "/beta/")
	if name == "" || name == "index.html" || !fs.ValidPath(name) {
		ws.serveBetaIndex(w)
		return
	}

	f, err := betaFS.Open(name)
	if err != nil {
		// A path with no file extension is a client-side route: serve the app.
		// A missing asset (has an extension) is a genuine 404.
		if path.Ext(name) == "" {
			ws.serveBetaIndex(w)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		ws.serveBetaIndex(w)
		return
	}

	if strings.HasPrefix(name, "assets/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}

	// embed.FS files implement io.ReadSeeker, which http.ServeContent needs.
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, info.Name(), info.ModTime(), rs)
		return
	}
	http.Error(w, "internal error", http.StatusInternalServerError)
}

func (ws *Webserver) serveBetaIndex(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	if b, err := fs.ReadFile(betaFS, "index.html"); err == nil {
		w.Write(b)
		return
	}
	w.Write([]byte(betaNotBuiltHTML))
}

// HandleRobotsTxt sends a constant robots.txt file back to the
// client, allowing web crawlers to access our entire site.  If we
// didn't handle /robots.txt ourselves, Wikimedia's proxy would inject
// a deny-all response and return that to the caller.
func (ws *Webserver) HandleRobotsTxt(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Server", ServerVersion)

	// https://wikitech.wikimedia.org/wiki/Help:Toolforge/Web#/robots.txt
	// /beta/ is the in-progress web app: keep it out of search indexes for now
	// (HandleBeta also sends X-Robots-Tag: noindex).
	h.Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "%s", "User-Agent: *\nAllow: /\nDisallow: /beta/\n")
}
