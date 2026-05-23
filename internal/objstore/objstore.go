// Package objstore wraps an S3-compatible object store (default MinIO) for
// sandbox snapshots and any other binary payloads. Kept slim and free of any
// sandbox/audit imports so future callers (audit export, file uploads) can
// depend on it without dragging the sandbox graph.
package objstore

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config configures a Client. Endpoint is host:port without scheme; UseSSL
// flips http/https. Bucket is required.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

// Client wraps a minio.Client + the bucket name. Construct once at boot and
// pass to callers; safe for concurrent use.
type Client struct {
	mc     *minio.Client
	bucket string
	region string
}

// PutResult is the surface returned by Put. Size is the actual uploaded size
// reported by minio-go (may differ from the caller's hint when streaming).
type PutResult struct {
	Bucket string
	Key    string
	Size   int64
	ETag   string
}

// New constructs a Client from cfg. Bucket must be non-empty; Endpoint must
// be non-empty. Does not perform any network I/O; call EnsureBucket
// separately during boot.
func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" {
		return nil, errors.New("objstore: endpoint required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("objstore: bucket required")
	}
	mc, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("objstore: new minio client: %w", err)
	}
	return &Client{mc: mc, bucket: cfg.Bucket, region: cfg.Region}, nil
}

// Bucket returns the configured bucket name.
func (c *Client) Bucket() string { return c.bucket }

// EnsureBucket creates the bucket if it does not already exist. Idempotent.
// Call once during server boot.
func (c *Client) EnsureBucket(ctx context.Context) error {
	exists, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("objstore: bucket exists check: %w", err)
	}
	if exists {
		return nil
	}
	if err := c.mc.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{Region: c.region}); err != nil {
		return fmt.Errorf("objstore: make bucket %s: %w", c.bucket, err)
	}
	return nil
}

// Put streams reader into object at key. When size is unknown pass -1 (the
// caller usually does, since docker save returns a stream with no fixed
// length). PartSize controls multipart chunk size; 64 MiB is a reasonable
// default for tar payloads.
func (c *Client) Put(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (*PutResult, error) {
	if key == "" {
		return nil, errors.New("objstore: key required")
	}
	opts := minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    64 << 20, // 64 MiB
	}
	info, err := c.mc.PutObject(ctx, c.bucket, key, reader, size, opts)
	if err != nil {
		return nil, fmt.Errorf("objstore: put %s/%s: %w", c.bucket, key, err)
	}
	return &PutResult{
		Bucket: info.Bucket,
		Key:    info.Key,
		Size:   info.Size,
		ETag:   info.ETag,
	}, nil
}

// Open returns a streaming reader for key. Caller must Close the reader.
func (c *Client) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if key == "" {
		return nil, errors.New("objstore: key required")
	}
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("objstore: get %s/%s: %w", c.bucket, key, err)
	}
	return obj, nil
}

// Stat returns the size + etag of an object. Returns ErrNotExists on miss.
func (c *Client) Stat(ctx context.Context, key string) (int64, error) {
	info, err := c.mc.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("objstore: stat %s/%s: %w", c.bucket, key, err)
	}
	return info.Size, nil
}
