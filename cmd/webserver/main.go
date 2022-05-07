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
	storagekey := flag.String("storage-key", "keys/storage-key", "path to key with storage access credentials")
	workdir := flag.String("workdir", "webserver-workdir", "path to working directory on local disk")
	flag.Parse()

	if *port == 0 {
		*port, _ = strconv.Atoi(os.Getenv("PORT"))
	}

	storage, err := NewStorage(*storagekey, *workdir)
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
		`<html>
<head>
<link href='https://tools-static.wmflabs.org/fontcdn/css?family=Roboto+Slab:400,700' rel='stylesheet' type='text/css'/>
<style>
* {
  font-family: 'Roboto Slab', serif;
}
h1 {
  color: #0066ff;
  margin-left: 1em;
  margin-top: 1em;
}
p {
  margin-left: 5em;
}
</style>
</head>
<body><h1>OSMViews</h1>

<p>Ranking geo locations based on OpenStreetMap views.
Useful for maintaining Wikidata and other data that needs
to order or prioritize geographic locations. For background, see
<a href="https://github.com/brawer/osmviews">source repo</a>.</p>

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
	case http.MethodGet:
		// As per https://tools.ietf.org/html/rfc7232, ETag must have quotes.
		h.Set("ETag", fmt.Sprintf(`"%s"`, c.ETag))
		h.Set("Content-Type", c.ContentType)
		h.Set("Access-Control-Allow-Origin", "*")
		http.ServeContent(w, req, "", c.LastModified, c)

	case http.MethodOptions: // CORS pre-flight
		h.Set("Allow", "GET, OPTIONS")
		h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "ETag, If-Match, If-None-Match, If-Modified-Since, If-Range, Range")
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Expose-Headers", "ETag")
		h.Set("Access-Control-Max-Age", "86400") // 1 day
		w.WriteHeader(http.StatusNoContent)

	default:
		h.Set("Allow", "GET, OPTIONS")
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
