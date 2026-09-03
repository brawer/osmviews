// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/base32"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Storage struct {
	client  storageClient
	workdir string
	mutex   sync.RWMutex
	files   map[string]*localFile
}

// LocalFile represents a file in the local working directory,
// which is a cached copy of a servable file in remote storage.
type localFile struct {
	Path         string
	ContentType  string
	ETag         string
	LastModified time.Time
	Version      string // the YYYYMMDD from the object key, "" if none
}

// bomContentType is the media type registered for CycloneDX JSON.
const bomContentType = "application/vnd.cyclonedx+json"

// contentType returns the media type to serve a file of the given name with.
func contentType(name string) string {
	switch {
	case strings.HasSuffix(name, ".cdx.json"):
		return bomContentType
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".gz"):
		return "application/gzip"
	case strings.HasSuffix(name, ".tiff"):
		return "image/tiff"
	case strings.HasSuffix(name, ".txt"):
		return "text/plain"
	default:
		return "application/octet-stream"
	}
}

// StorageClient is the subset of minio.Client used in this program.
// For testing, struct fakeStorageClient provides a fake implementation.
type storageClient interface {
	ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo
	FGetObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.GetObjectOptions) error
}

// NewStorage sets up a client for accessing S3-compatible object storage.
func NewStorage(workdir string) (*Storage, error) {
	if err := os.MkdirAll(workdir, 0755); err != nil {
		return nil, err
	}

	var config struct{ Endpoint, Key, Secret string }
	config.Endpoint = os.Getenv("S3_ENDPOINT")
	config.Key = os.Getenv("S3_KEY")
	config.Secret = os.Getenv("S3_SECRET")
	for name, value := range map[string]string{
		"S3_ENDPOINT": config.Endpoint, "S3_KEY": config.Key, "S3_SECRET": config.Secret,
	} {
		if value == "" {
			return nil, fmt.Errorf("environment variable %s is not set", name)
		}
	}

	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(config.Key, config.Secret, ""),
		Secure: true,
	})
	if err != nil {
		return nil, err
	}

	client.SetAppInfo("osmviews-webserver", "0.1")
	return &Storage{
		client:  client,
		workdir: workdir,
		files:   make(map[string]*localFile, 10),
	}, nil
}

var objRegexp = regexp.MustCompile(`public/([a-z\-]+)\-(2[0-9]{7})\.([a-z0-9\.]+)`)

// Reload caches public content from remote object storage to local disk.
// Any old content (which is not live anymore) is deleted from local disk.
func (s *Storage) Reload(ctx context.Context) error {
	// Find the most recent version of each file in storage.
	objects := s.client.ListObjects(ctx, "osmviews", minio.ListObjectsOptions{
		Prefix:    "public/",
		Recursive: false,
	})
	// Most files are served under a de-dated name ("osmviews.tiff",
	// "osmviews-stats.json") that always points at the most recent version.
	// Bills of materials are the exception: they are served only under their
	// dated name, "osmviews-<YYYYMMDD>.cdx.json" (the object's own basename),
	// so a BOM URL is immutable and names the exact GeoTIFF it describes. The
	// three most recent are kept, matching the builder's own retention.
	inStorage := make(map[string]minio.ObjectInfo, 5)
	var bomNames []string // dated BOM names sort chronologically
	for obj := range objects {
		m := objRegexp.FindStringSubmatch(obj.Key)
		if m == nil {
			continue
		}
		if m[3] == "cdx.json" {
			dated := filepath.Base(obj.Key)
			inStorage[dated] = obj
			bomNames = append(bomNames, dated)
			continue
		}
		if latest := m[1] + "." + m[3]; obj.LastModified.After(inStorage[latest].LastModified) {
			inStorage[latest] = obj
		}
	}
	if len(bomNames) > 3 {
		sort.Sort(sort.Reverse(sort.StringSlice(bomNames)))
		for _, n := range bomNames[3:] {
			delete(inStorage, n)
		}
	}

	files := make(map[string]*localFile, len(inStorage))
	for filename, obj := range inStorage {
		mangled := base32.HexEncoding.EncodeToString([]byte(obj.ETag))
		path, err := filepath.Abs(filepath.Join(
			s.workdir,
			fmt.Sprintf("%s-%s", mangled, filename)))
		if err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			tmpPath := path + ".tmp"
			if err := s.client.FGetObject(ctx, "osmviews", obj.Key, tmpPath, minio.GetObjectOptions{}); err != nil {
				return err
			}
			if err := os.Chtimes(tmpPath, time.Now(), obj.LastModified); err != nil {
				return err
			}
			if err := os.Rename(tmpPath, path); err != nil {
				return err
			}
		}

		loc := &localFile{
			LastModified: obj.LastModified.UTC(),
			ContentType:  contentType(filename),
			ETag:         obj.ETag,
			Path:         path,
		}
		if m := objRegexp.FindStringSubmatch(obj.Key); m != nil {
			loc.Version = m[2]
		}
		files[filename] = loc
	}

	live := make(map[string]bool, len(files))
	for _, f := range files {
		live[f.Path] = true
	}

	s.mutex.Lock()
	s.files = files
	s.mutex.Unlock()

	// Clean up workdir so it only contains live files. If we have a new
	// version for a file that is still getting served to an in-flight
	// request, it’s not a problem: In Linux, it is perfectly fine to
	// delete (unlink) a file while there’s still open file handles.
	// The file handle will remain open and can be used for reading;
	// the underlying file only gets deleted once there’s no open handles
	// anymore.
	ff, err := os.ReadDir(s.workdir)
	if err != nil {
		return err
	}
	for _, f := range ff {
		fp, err := filepath.Abs(filepath.Join(s.workdir, f.Name()))
		if err != nil {
			return err
		}
		if !live[fp] {
			log.Printf("deleting obsolete local file: %s", fp)
			if err := os.Remove(fp); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Storage) Watch(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := s.Reload(ctx); err != nil {
				if err == ctx.Err() {
					return err
				} else {
					log.Println(err)
				}
			}
		}
	}
}

type Content struct {
	f            *os.File
	ContentType  string
	ETag         string
	LastModified time.Time
	Version      string // the YYYYMMDD from the object key, "" if none
}

func (c *Content) Read(p []byte) (int, error) {
	return c.f.Read(p)
}

func (c *Content) Seek(offset int64, whence int) (int64, error) {
	return c.f.Seek(offset, whence)
}

func (c *Content) Close() error {
	return c.f.Close()
}

// ErrNotFound is returned by Retrieve when no servable file matches.
// A different error means the file is known but could not be read.
var ErrNotFound = errors.New("not found")

// Version returns the YYYYMMDD stamp of the currently served file, or "" if
// it is unknown (e.g. before the first successful Reload).
func (s *Storage) Version(filename string) string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if loc, ok := s.files[filename]; ok {
		return loc.Version
	}
	return ""
}

func (s *Storage) Retrieve(filename string) (*Content, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	loc, found := s.files[filename]
	if !found {
		return nil, ErrNotFound
	}

	f, err := os.Open(loc.Path)
	if err != nil {
		return nil, fmt.Errorf("opening cached %s: %w", filename, err)
	}

	c := &Content{
		f:            f,
		ContentType:  loc.ContentType,
		ETag:         loc.ETag,
		LastModified: loc.LastModified,
		Version:      loc.Version,
	}
	return c, nil
}
