// SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	//"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ServerVersion is returned to HTTP clients as the Server header.
// In released server binaries, the value of this variable is
// overwritten to a string that includes the tagged release version,
// for example "OSMViews/0.7". The release process does this by passing
// the -X flag to the Go compiler/linker.
var ServerVersion = "OSMViews"

func main() {
	port := flag.Int("port", 0, "port for serving HTTP requests")
	workdir := flag.String("workdir", "webserver-workdir", "path to working directory on local disk")
	flag.Parse()

	if *port == 0 {
		*port, _ = strconv.Atoi(os.Getenv("PORT"))
	}

	if *workdir != "" {
		if err := os.MkdirAll(*workdir, 0755); err != nil {
			log.Fatal(err)
		}
	}

	storage, err := NewStorage(*workdir)
	if err != nil {
		log.Fatal(err)
	}

	if err := storage.Reload(context.Background()); err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go storage.Watch(ctx)
	server := &Webserver{storage: storage}
	http.HandleFunc("/", server.HandleMain)
	http.HandleFunc("/robots.txt", server.HandleRobotsTxt)
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/download/", server.HandleDownload)
	log.Printf("Listening for HTTP requests on port %d", *port)
	http.ListenAndServe(":"+strconv.Itoa(*port), nil)
	cancel()
}

type Webserver struct {
	storage *Storage
}

func (ws *Webserver) HandleMain(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Server", ServerVersion)

	fmt.Fprintf(w, "%s",
		`<!DOCTYPE html>
<html>
<head>
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
p.code {
  margin-left: 9em;
  display: block;
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

<p><b>Use in Python:</b></p>

<p class="code"># pip install osmviews
import osmviews
osmviews.download('/tmp/osmviews.tiff')
with osmviews.open('/tmp/osmviews.tiff') as o:
    print(f'Tokyo, Shibuya:      {o.rank( 35.658514, 139.701330):>9.2f}')
    print(f'Tokyo, Sumida:       {o.rank( 35.710719, 139.801547):>9.2f}')
    print(f'Zürich, Altstetten:  {o.rank( 47.391485,   8.488945):>9.2f}')
    print(f'Zürich, Witikon:     {o.rank( 47.358651,   8.590251):>9.2f}')
    print(f'Ushuaia, Costa Este: {o.rank(-54.794395, -68.251958):>9.2f}')
    print(f'Ushuaia, Las Reinas: {o.rank(-54.769225, -68.279174):>9.2f}')

Tokyo, Shibuya:      227437.98
Tokyo, Sumida:        60537.62
Zürich, Altstetten:   37883.31
Zürich, Witikon:      11711.94
Ushuaia, Costa Este:   2697.14
Ushuaia, Las Reinas:    257.89
</pre>

<p>
<b>Author:</b> <a href="https://brawer.ch/">Sascha Brawer</a>
<br/><b>Backend:</b>
<a href="https://github.com/brawer/osmviews">github.com/brawer/osmviews</a>
<br/><b>Clients:</b>
<a href="https://github.com/brawer/osmviews-py">Python</a>
<br/><b>Download:</b> <a href="download/osmviews.tiff">Cloud-Optimized GeoTIFF</a> (data),
<a href="download/osmviews-stats.json">JSON</a> (histogram)
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
