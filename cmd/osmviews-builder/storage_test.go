// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestNewRemoteStorage(t *testing.T) {
	set := func(m map[string]string) {
		for _, k := range []string{"T_ENDPOINT", "T_KEY", "T_SECRET", "T_BUCKET", "T_REGION"} {
			t.Setenv(k, m[k])
		}
	}

	set(map[string]string{"T_ENDPOINT": "https://s3.example.com", "T_KEY": "k", "T_SECRET": "s", "T_BUCKET": "b"})
	s, bucket, err := newRemoteStorage("T_", true)
	if err != nil {
		t.Fatalf("newRemoteStorage: %v", err)
	}
	if bucket != "b" {
		t.Errorf("bucket = %q, want %q", bucket, "b")
	}
	// The https:// scheme in the endpoint must be stripped for minio.New.
	if got := s.(*remoteStorage).client.EndpointURL().Host; got != "s3.example.com" {
		t.Errorf("endpoint host = %q, want s3.example.com", got)
	}

	set(map[string]string{"T_ENDPOINT": "s3.example.com", "T_KEY": "k", "T_SECRET": "s"})
	if _, _, err := newRemoteStorage("T_", false); err == nil || !strings.Contains(err.Error(), "T_BUCKET") {
		t.Errorf("missing T_BUCKET: got err %v, want one naming T_BUCKET", err)
	}
}

func TestCleanup(t *testing.T) {
	ctx := context.Background()

	localpath := filepath.Join(t.TempDir(), "testcleanup")
	if err := os.WriteFile(localpath, []byte("foo"), 0644); err != nil {
		t.Fatal(err)
	}

	internal := NewFakeStorage()
	public := NewFakeStorage()
	put := func(s *FakeStorage, key string) {
		if err := s.PutFile(ctx, "b", key, localpath, "application/octet-stream"); err != nil {
			t.Fatal(err)
		}
	}

	// Internal bucket: a rolling year of weekly tile-log aggregates (keep 60),
	// plus an unrelated object that must be left alone.
	put(internal, "internal/otherproject’s_data_should/not/be/touched.txt")
	for year := 2021; year <= 2022; year++ {
		for week := 1; week <= 52; week++ {
			if year == 2022 && week > 40 {
				break
			}
			put(internal, fmt.Sprintf("internal/osmviews-builder/tilelogs-%d-W%02d.br", year, week))
		}
	}

	// Public bucket: 5 dated builds. Only the 3 most recent .tiff survive;
	// every .cdx.json is kept forever (issue #110). Decoys must be left alone.
	put(public, "data/osmviews-not-matching-pattern.txt")
	put(public, "data/quxfoo-20210830.csv.gz")
	for _, date := range []string{"20211205", "20211212", "20211226", "20220102", "20220109"} {
		put(public, fmt.Sprintf("data/osmviews-%s.tiff", date))
		put(public, fmt.Sprintf("data/osmviews-%s.cdx.json", date))
	}

	if err := Cleanup(internal, "internal-bucket", public, "public-bucket"); err != nil {
		t.Fatal(err)
	}

	assertKeys := func(name string, s *FakeStorage, want []string) {
		t.Helper()
		got := make([]string, 0, len(s.Files))
		for k := range s.Files {
			got = append(got, k)
		}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("%s bucket after Cleanup:\n got %v\nwant %v", name, got, want)
		}
	}

	wantInternal := []string{"internal/otherproject’s_data_should/not/be/touched.txt"}
	for week := 33; week <= 52; week++ { // 2021-W33..W52
		wantInternal = append(wantInternal, fmt.Sprintf("internal/osmviews-builder/tilelogs-2021-W%02d.br", week))
	}
	for week := 1; week <= 40; week++ { // 2022-W01..W40
		wantInternal = append(wantInternal, fmt.Sprintf("internal/osmviews-builder/tilelogs-2022-W%02d.br", week))
	}
	assertKeys("internal", internal, wantInternal)

	assertKeys("public", public, []string{
		"data/osmviews-not-matching-pattern.txt",
		"data/quxfoo-20210830.csv.gz",
		"data/osmviews-20211205.cdx.json",
		"data/osmviews-20211212.cdx.json",
		"data/osmviews-20211226.cdx.json",
		"data/osmviews-20211226.tiff",
		"data/osmviews-20220102.cdx.json",
		"data/osmviews-20220102.tiff",
		"data/osmviews-20220109.cdx.json",
		"data/osmviews-20220109.tiff",
	})
}

func TestDownload(t *testing.T) {
	remotePath := "remote/path.txt"
	srcPath := filepath.Join(t.TempDir(), "test_download_src.txt")
	destPath := filepath.Join(t.TempDir(), "test_download_dest.txt")
	if err := os.WriteFile(srcPath, []byte("foo"), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewFakeStorage()
	ctx := context.Background()
	if err := s.PutFile(ctx, "osmviews", remotePath, srcPath, "text/plain"); err != nil {
		t.Fatal(err)
	}

	if err := Download(s, "osmviews", remotePath, destPath); err != nil {
		t.Fatal(err)
	}

	gotBytes, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(gotBytes)

	if got != "foo" {
		t.Errorf("expected \"foo\", got \"%s\"", got)
	}
}

type FakeStorageObject struct {
	Content []byte
	Info    ObjectInfo
}

type FakeStorage struct {
	Files map[string]*FakeStorageObject
}

func (s *FakeStorage) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return bucket == "osmviews", nil
}

func (s *FakeStorage) PutFile(ctx context.Context, bucket string, remotepath string, localpath string, contentType string) error {
	content, err := os.ReadFile(localpath)
	if err != nil {
		return err
	}

	digest := md5.Sum(content)
	etag := base64.RawStdEncoding.EncodeToString(digest[0:len(digest)])
	info := ObjectInfo{
		Key:         remotepath,
		ContentType: contentType,
		ETag:        etag,
	}

	s.Files[remotepath] = &FakeStorageObject{content, info}
	return nil
}

func (s *FakeStorage) Get(ctx context.Context, bucket, path string) (io.Reader, error) {
	f, present := s.Files[path]
	if !present {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	return bytes.NewReader(f.Content), nil
}

func (s *FakeStorage) List(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error) {
	result := make([]ObjectInfo, 0, len(s.Files))
	for _, f := range s.Files {
		result = append(result, f.Info)
	}
	return result, nil
}

func (s *FakeStorage) Remove(ctx context.Context, bucketName, path string) error {
	delete(s.Files, path)
	return nil
}

func (s *FakeStorage) Stat(ctx context.Context, bucket string, path string) (ObjectInfo, error) {
	if f, present := s.Files[path]; present {
		return f.Info, nil
	} else {
		return ObjectInfo{}, fmt.Errorf("no such file: %s", path)
	}
}

func NewFakeStorage() *FakeStorage {
	return &FakeStorage{Files: make(map[string]*FakeStorageObject)}
}
