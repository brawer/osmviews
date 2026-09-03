// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestStorage_Reload(t *testing.T) {
	lastmod, _ := time.Parse(time.RFC3339, "2021-12-29T13:14:15Z")
	storage := &Storage{
		client: &fakeStorageClient{
			objects: []minio.ObjectInfo{{
				Key: "public/hello-20211229.txt", Size: 5,
				ETag: "Test-ETag", LastModified: lastmod,
			}},
			blobs: map[string][]byte{"public/hello-20211229.txt": []byte("Hello")},
		},
		workdir: t.TempDir(),
		files:   make(map[string]*localFile, 10),
	}

	old := filepath.Join(storage.workdir, "obsolete")
	if err := os.WriteFile(old, []byte("Old content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := storage.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(old); err == nil {
		t.Errorf("Storage.Reload() should delete old file %s", old)
	}

	if len(storage.files) != 1 {
		t.Errorf("got %d files in %v, expected 1", len(storage.files), storage.files)
	}

	loc := storage.files["hello.txt"]
	if loc.ETag != "Test-ETag" {
		t.Errorf("got ETag=%v, want %v", loc.ETag, "testetag")
	}

	gotLastmod := loc.LastModified.Format(time.RFC3339)
	wantLastmod := "2021-12-29T13:14:15Z"
	if gotLastmod != wantLastmod {
		t.Errorf("got LastMod=%s, want %s", gotLastmod, wantLastmod)
	}

	if loc.ContentType != "text/plain" {
		t.Errorf("got ContentType=%s, want text/plain", loc.ContentType)
	}

	gotContent, err := os.ReadFile(loc.Path)
	if err != nil {
		t.Error(err)
	}
	wantContent := "Hello"
	if string(gotContent) != wantContent {
		t.Errorf("got content=%v, want %v", string(gotContent), wantContent)
	}
}

func TestStorage_Retrieve(t *testing.T) {
	storage := &Storage{
		client:  &fakeStorageClient{},
		workdir: t.TempDir(),
		files:   make(map[string]*localFile, 10),
	}

	path := filepath.Join(storage.workdir, "c.txt")
	if err := os.WriteFile(path, []byte("Content"), 0644); err != nil {
		t.Fatal(err)
	}

	lastmod, _ := time.Parse(time.RFC3339, "2023-11-21T19:20:21Z")
	storage.files["c.txt"] = &localFile{
		Path:         path,
		ContentType:  "text/plain",
		ETag:         "ETag-123",
		LastModified: lastmod,
	}

	c, err := storage.Retrieve("c.txt")
	if err != nil {
		t.Fatal(err)
	}

	if c.ContentType != "text/plain" {
		t.Errorf("got ContentType=%v, want %v", c.ContentType, "text/plain")
	}

	if c.ETag != "ETag-123" {
		t.Errorf("got ETag=%v, want %v", c.ETag, "ETag-123")
	}

	if c.LastModified != lastmod {
		t.Errorf("got LastModified=%v, want %v", c.LastModified, lastmod)
	}

	buf := make([]byte, 2)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != len(buf) {
		t.Errorf("got n=%d, want %d", n, len(buf))
	}
	if string(buf) != "Co" {
		t.Errorf(`got %v, want "Co"`, string(buf))
	}

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStorage_RetrieveErrors(t *testing.T) {
	storage := &Storage{
		client:  &fakeStorageClient{},
		workdir: t.TempDir(),
		files:   make(map[string]*localFile, 10),
	}
	// Known to storage, but the cached file is missing from disk.
	storage.files["gone.txt"] = &localFile{Path: filepath.Join(storage.workdir, "gone.txt")}

	if _, err := storage.Retrieve("unknown.txt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown file: got %v, want ErrNotFound", err)
	}
	if _, err := storage.Retrieve("gone.txt"); err == nil || errors.Is(err, ErrNotFound) {
		t.Errorf("missing cache file: got %v, want a non-ErrNotFound error", err)
	}
}

// fakeStorageClient serves the objects it is given. Tests that do not call
// Reload can leave objects and blobs nil.
type fakeStorageClient struct {
	storageClient
	objects []minio.ObjectInfo
	blobs   map[string][]byte
}

func (s *fakeStorageClient) ListObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	ch := make(chan minio.ObjectInfo)
	go func() {
		for _, o := range s.objects {
			ch <- o
		}
		close(ch)
	}()
	return ch
}

func (s *fakeStorageClient) FGetObject(ctx context.Context, bucketName, objectName, filePath string, opts minio.GetObjectOptions) error {
	if body, ok := s.blobs[objectName]; ok && bucketName == "osmviews" {
		return os.WriteFile(filePath, body, 0644)
	}
	return fmt.Errorf("object not found: %s/%s", bucketName, objectName)
}

func TestStorage_Reload_BOM(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse("20060102", s); return d }
	var objs []minio.ObjectInfo
	blobs := map[string][]byte{}
	for _, d := range []string{"20260808", "20260815", "20260822", "20260830"} {
		key := "public/osmviews-" + d + ".cdx.json"
		objs = append(objs, minio.ObjectInfo{Key: key, ETag: "etag-" + d, LastModified: day(d)})
		blobs[key] = []byte(`{"bomFormat":"CycloneDX","specVersion":"1.7"}`)
	}
	tiff := "public/osmviews-20260830.tiff"
	objs = append(objs, minio.ObjectInfo{Key: tiff, ETag: "etag-tiff", LastModified: day("20260830")})
	blobs[tiff] = []byte("II*\x00 fake geotiff")

	s := &Storage{
		client:  &fakeStorageClient{objects: objs, blobs: blobs},
		workdir: t.TempDir(),
		files:   map[string]*localFile{},
	}
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}

	// BOMs are served only under their dated name, and only the newest three.
	if s.files["osmviews.cdx.json"] != nil {
		t.Error("BOMs must not be served under a de-dated name")
	}
	for _, d := range []string{"20260815", "20260822", "20260830"} {
		bom := s.files["osmviews-"+d+".cdx.json"]
		if bom == nil {
			t.Errorf("missing dated BOM osmviews-%s.cdx.json", d)
			continue
		}
		if bom.Version != d {
			t.Errorf("osmviews-%s.cdx.json Version = %q, want %q", d, bom.Version, d)
		}
		if bom.ContentType != bomContentType {
			t.Errorf("BOM content type = %q, want %q", bom.ContentType, bomContentType)
		}
	}
	if s.files["osmviews-20260808.cdx.json"] != nil {
		t.Error("osmviews-20260808.cdx.json should have been dropped (keep-3)")
	}

	if tf := s.files["osmviews.tiff"]; tf == nil || tf.Version != "20260830" {
		t.Fatalf("osmviews.tiff = %+v, want version 20260830", tf)
	}
	if got := s.Version("osmviews.tiff"); got != "20260830" {
		t.Errorf("Version(osmviews.tiff) = %q, want 20260830", got)
	}
}

func TestStorage_objRegexp(t *testing.T) {
	for _, s := range []string{
		"public/osmviews-stats-20220631.json",
		"public/osmviews-20220631.tiff",
		"public/osmviews-20260830.cdx.json",
	} {
		if !objRegexp.MatchString(s) {
			t.Errorf("should match but does not: %v", s)
		}
	}

	for _, s := range []string{
		"internal/osmviews-builder/foobar.tiff",
		"public/foobar.csv.gz",
	} {
		if objRegexp.MatchString(s) {
			t.Errorf("should not match but does: %v", s)
		}
	}
}
