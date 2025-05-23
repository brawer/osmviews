// SPDX-FileCopyrightText: 2022 Sascha Brawer <sascha@brawer.ch>
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

// NewStorage sets up a client for accessing S3-compatible object storage.
func NewStorage() (Storage, error) {
	endpoint := os.Getenv("S3_ENDPOINT")
	key := os.Getenv("S3_KEY")
	secret := os.Getenv("S3_SECRET")
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(key, secret, ""),
		Secure: true,
	})
	if err != nil {
		return nil, err
	}

	client.SetAppInfo("OSMViewsBuilder", "0.1")
	return &remoteStorage{client: client}, nil
}

func Cleanup(s Storage) error {
	for _, p := range []struct {
		prefix, pattern string
		keep            int
	}{
		{"internal/osmviews-builder/tilelogs-", `internal/osmviews-builder/tilelogs-\d{4}-W\d{2}\.br`, 60},
		{"public/osmviews-", `public/osmviews-\d{8}\.tiff`, 3},
		{"public/osmviews-stats-", `public/osmviews-stats-\d{8}\.json`, 3},
	} {
		if err := cleanupPath("osmviews", p.prefix, p.pattern, p.keep, s); err != nil {
			return err
		}
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
	}
	return nil
}
