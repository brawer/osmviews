// SPDX-FileCopyrightText: 2026 Sascha Brawer <sascha@brawer.ch>
// SPDX-License-Identifier: MIT

//go:build ignore

// Throwaway connectivity probe for the public (Bunny) S3 bucket. Excluded from
// every normal build/test by the ignore tag; run it explicitly:
//
//	PUBLIC_S3_ENDPOINT=de-s3.storage.bunnycdn.com \
//	PUBLIC_S3_KEY=osmviews-data \
//	PUBLIC_S3_SECRET=... \
//	PUBLIC_S3_BUCKET=osmviews-data \
//	PUBLIC_S3_REGION=de \
//	go run ./cmd/osmviews-builder/probe_ignore.go
//
// It PUTs, STATs, LISTs, GETs and DELETEs a tiny object at data/_probe.txt,
// then tells you to check the CDN URL. Delete this file once the builder's
// real public client is in place.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func main() {
	endpoint := strings.TrimPrefix(strings.TrimPrefix(os.Getenv("PUBLIC_S3_ENDPOINT"), "https://"), "http://")
	key := os.Getenv("PUBLIC_S3_KEY")
	secret := os.Getenv("PUBLIC_S3_SECRET")
	bucket := os.Getenv("PUBLIC_S3_BUCKET")
	region := os.Getenv("PUBLIC_S3_REGION")

	fmt.Printf("endpoint=%q bucket=%q region=%q key=%q\n", endpoint, bucket, region, key)

	cl, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(key, secret, ""),
		Secure:       true,
		Region:       region,
		BucketLookup: minio.BucketLookupPath, // Bunny is path-style only
	})
	must("minio.New", err)

	ctx := context.Background()
	obj := "data/_probe.txt"
	body := fmt.Sprintf("osmviews storage probe %s\n", time.Now().UTC().Format(time.RFC3339))

	_, err = cl.PutObject(ctx, bucket, obj, strings.NewReader(body), int64(len(body)),
		minio.PutObjectOptions{ContentType: "text/plain"})
	must("PUT", err)

	st, err := cl.StatObject(ctx, bucket, obj, minio.StatObjectOptions{})
	must("STAT", err)
	fmt.Printf("STAT ok: etag=%s size=%d contentType=%s\n", st.ETag, st.Size, st.ContentType)

	n := 0
	for o := range cl.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: "data/", Recursive: true}) {
		must("LIST", o.Err)
		n++
	}
	fmt.Printf("LIST ok: %d object(s) under data/\n", n)

	r, err := cl.GetObject(ctx, bucket, obj, minio.GetObjectOptions{})
	must("GET(open)", err)
	got, err := io.ReadAll(r)
	must("GET(read)", err)
	if string(got) != body {
		fmt.Printf("GET mismatch: %q != %q\n", string(got), body)
		os.Exit(1)
	}
	fmt.Println("GET ok: round-trip byte-identical")

	must("DELETE", cl.RemoveObject(ctx, bucket, obj, minio.RemoveObjectOptions{}))
	fmt.Println("DELETE ok")

	fmt.Println("\nAll S3 ops passed. Now check the CDN edge rule (object was deleted,")
	fmt.Println("so re-PUT by hand if you want a live URL to curl):")
	fmt.Println("  curl -sSI https://osmviews.dandelis.ch/data/_probe.txt")
}

func must(op string, err error) {
	if err != nil {
		fmt.Printf("%s FAILED: %v\n", op, err)
		os.Exit(1)
	}
	fmt.Printf("%s ok\n", op)
}
