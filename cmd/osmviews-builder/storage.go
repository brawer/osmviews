// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ObjectInfo struct {
	Key         string
	ContentType string
	ETag        string
}

type Storage interface {
	BucketExists(ctx context.Context, bucket string) (bool, error)
	List(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)
	Stat(ctx context.Context, bucket, path string) (ObjectInfo, error)
	Get(ctx context.Context, bucket, path string) (io.Reader, error)
	PutFile(ctx context.Context, bucket string, remotepath string, localpath string, contentType string) error
	Remove(ctx context.Context, bucketName, path string) error
}

// RemoteStorage is an implementation of interface Storage that talks
// to a remote S3-compatible server. The other implementation is FakeStorage,
// which is used for testing.
type remoteStorage struct {
	client *minio.Client
}

func (s *remoteStorage) BucketExists(ctx context.Context, bucket string) (bool, error) {
	return s.client.BucketExists(ctx, bucket)
}

func (s *remoteStorage) List(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error) {
	opts := minio.ListObjectsOptions{Prefix: prefix, Recursive: true}
	result := make([]ObjectInfo, 0)
	for f := range s.client.ListObjects(ctx, bucket, opts) {
		o := ObjectInfo{Key: f.Key, ContentType: f.ContentType, ETag: f.ETag}
		result = append(result, o)
	}
	return result, nil
}

func (s *remoteStorage) Stat(ctx context.Context, bucket, path string) (ObjectInfo, error) {
	st, err := s.client.StatObject(ctx, bucket, path, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	info := ObjectInfo{Key: st.Key, ContentType: st.ContentType, ETag: st.ETag}
	return info, nil
}

func (s *remoteStorage) Get(ctx context.Context, bucket, path string) (io.Reader, error) {
	return s.client.GetObject(ctx, bucket, path, minio.GetObjectOptions{})
}

func (s *remoteStorage) PutFile(ctx context.Context, bucket string, remotepath string, localpath string, contentType string) error {
	opts := minio.PutObjectOptions{ContentType: contentType}
	_, err := s.client.FPutObject(ctx, bucket, remotepath, localpath, opts)
	return err
}

func (s *remoteStorage) Remove(ctx context.Context, bucket, path string) error {
	return s.client.RemoveObject(ctx, bucket, path, minio.RemoveObjectOptions{})
}

// NewInternalStorage connects to the S3-compatible bucket that holds the
// build's private tile-log aggregates, configured through INTERNAL_S3_ENDPOINT
// / _KEY / _SECRET / _BUCKET (and an optional _REGION). It returns the client
// and the bucket name. The public bucket the CDN serves has its own client
// (issue #110).
func NewInternalStorage() (Storage, string, error) {
	return newRemoteStorage("INTERNAL_S3_", false)
}

// NewPublicStorage connects to the S3-compatible bucket the CDN serves at
// <host>/data/*, configured through PUBLIC_S3_ENDPOINT / _KEY / _SECRET /
// _BUCKET / _REGION. Bunny's S3 gateway requires path-style addressing.
func NewPublicStorage() (Storage, string, error) {
	return newRemoteStorage("PUBLIC_S3_", true)
}

// newRemoteStorage builds a minio client from "<prefix>ENDPOINT", "<prefix>KEY",
// "<prefix>SECRET" and "<prefix>BUCKET" (all required) plus an optional
// "<prefix>REGION". pathStyle forces path-style bucket addressing, which some
// providers (Bunny) require and others reject.
func newRemoteStorage(prefix string, pathStyle bool) (Storage, string, error) {
	endpoint := strings.TrimPrefix(strings.TrimPrefix(os.Getenv(prefix+"ENDPOINT"), "https://"), "http://")
	key := os.Getenv(prefix + "KEY")
	secret := os.Getenv(prefix + "SECRET")
	bucket := os.Getenv(prefix + "BUCKET")
	for name, value := range map[string]string{
		prefix + "ENDPOINT": endpoint, prefix + "KEY": key,
		prefix + "SECRET": secret, prefix + "BUCKET": bucket,
	} {
		if value == "" {
			return nil, "", fmt.Errorf("environment variable %s is not set", name)
		}
	}
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(key, secret, ""),
		Secure: true,
		Region: os.Getenv(prefix + "REGION"), // "" lets minio decide
	}
	if pathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, opts)
	if err != nil {
		return nil, "", err
	}

	client.SetAppInfo("OSMViewsBuilder", "0.1")
	return &remoteStorage{client: client}, bucket, nil
}

// Cleanup garbage-collects old objects. Each rule runs against exactly one
// bucket; keep the internal and public arguments straight, or a keep-N sweep
// on one bucket deletes from the other.
//
// The public bucket keeps only the 3 most recent dated GeoTIFFs (each build
// supersedes the last). Dated CycloneDX BOMs there are never deleted — ~3 KB
// each, permanent provenance records (issue #110). The internal bucket keeps a
// rolling year of weekly tile-log aggregates.
func Cleanup(internal Storage, internalBucket string, public Storage, publicBucket string) error {
	if err := cleanupPath(internalBucket,
		"internal/osmviews-builder/tilelogs-",
		`internal/osmviews-builder/tilelogs-\d{4}-W\d{2}(-\d+d)?\.br`, 60, internal); err != nil {
		return err
	}
	if err := cleanupPath(publicBucket,
		"data/osmviews-", `data/osmviews-\d{8}\.tiff`, 3, public); err != nil {
		return err
	}
	return nil
}

func cleanupPath(bucket, prefix, pattern string, keep int, s Storage) error {
	ctx := context.Background()
	logger := log.Default()
	re := regexp.MustCompile(pattern)

	found := make([]string, 0, keep+10)
	files, err := s.List(ctx, bucket, prefix)
	if err != nil {
		return err
	}
	for _, f := range files {
		if re.MatchString(f.Key) {
			found = append(found, f.Key)
		}
	}

	if len(found) > keep {
		sort.Strings(found)
		for _, path := range found[0 : len(found)-keep] {
			logger.Printf("Deleting from storage: %s/%s", bucket, path)
			if err := s.Remove(ctx, bucket, path); err != nil {
				return err
			}
		}
	}

	return nil
}

func Download(s Storage, bucket string, remotePath string, localPath string) error {
	ctx := context.Background()
	logger := log.Default()
	out, err := os.CreateTemp(filepath.Dir(localPath), "*.tmp")
	if err != nil {
		return err
	}

	r, err := s.Get(ctx, bucket, remotePath)
	errMsg := fmt.Sprintf("download of s3://%s/%s failed", bucket, remotePath)
	if err != nil {
		out.Close()
		os.Remove(out.Name())
		logger.Printf("%s: %v", errMsg, err)
		return err
	}

	if _, err = io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(out.Name())
		logger.Printf("%s: %v", errMsg, err)
		return err
	}

	if err = out.Close(); err != nil {
		os.Remove(out.Name())
		logger.Printf("%s: %v", errMsg, err)
		return err
	}

	if err = os.Rename(out.Name(), localPath); err != nil {
		os.Remove(out.Name())
		logger.Printf("%s: %v", errMsg, err)
		return err
	}
	return nil
}
