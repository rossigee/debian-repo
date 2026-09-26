// Package minio provides S3/MinIO storage operations for Debian packages and snapshots.
package minio

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	minios3 "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Client wraps the MinIO S3 client with bucket and key prefix scoping
type Client struct {
	mc     *minios3.Client
	bucket string
	prefix string // normalized: "" or "prefix/" (with trailing slash, no leading slash)
}

// ClientConfig holds MinIO connection configuration (for back-compat with NewClient wrapper)
type ClientConfig struct {
	Endpoint   string
	AccessKey  string
	SecretKey  string
	Bucket     string
	UseTLS     bool
	CACertPath string
}

// BaseClientConfig holds MinIO connection configuration without bucket binding
type BaseClientConfig struct {
	Endpoint   string
	AccessKey  string
	SecretKey  string
	UseTLS     bool
	CACertPath string
}

// NewBaseClient creates a new MinIO client without binding to a specific bucket or prefix.
// Use WithBucketAndPrefix() to scope the client after creation.
func NewBaseClient(cfg BaseClientConfig) (*Client, error) {
	opts := &minios3.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseTLS,
	}

	// Load custom CA certificate if provided
	if cfg.CACertPath != "" {
		caCert, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA cert: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert")
		}

		// Create a custom HTTP transport with the CA cert
		tlsConfig := &tls.Config{
			RootCAs: caCertPool,
		}
		opts.Transport = &http.Transport{
			TLSClientConfig: tlsConfig,
		}
	}

	mc, err := minios3.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	return &Client{mc: mc, bucket: "", prefix: ""}, nil
}

// WithBucketAndPrefix derives a scoped client with a specific bucket and optional key prefix.
// The underlying MinIO connection is shared; only the bucket and prefix are different.
func (c *Client) WithBucketAndPrefix(bucket, prefix string) *Client {
	// Normalize prefix: ensure trailing slash if non-empty, no leading slash
	if prefix != "" {
		if prefix[0] == '/' {
			prefix = prefix[1:]
		}
		if prefix[len(prefix)-1] != '/' {
			prefix = prefix + "/"
		}
	}
	return &Client{
		mc:     c.mc,
		bucket: bucket,
		prefix: prefix,
	}
}

// Bucket returns the bucket this client is scoped to ("" if unscoped).
func (c *Client) Bucket() string {
	return c.bucket
}

// EnsureBucket verifies the bucket exists, creating it if necessary.
func (c *Client) EnsureBucket(ctx context.Context) error {
	if c.bucket == "" {
		return fmt.Errorf("bucket not set on client")
	}
	_, err := c.mc.BucketExists(ctx, c.bucket)
	if err != nil {
		// Try to create the bucket if it doesn't exist
		err := c.mc.MakeBucket(ctx, c.bucket, minios3.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create/access bucket: %w", err)
		}
	}
	return nil
}

// key prefixes the given object name with the client's configured prefix.
func (c *Client) key(name string) string {
	return c.prefix + name
}

// NewClient creates a new MinIO client bound to a specific bucket (back-compat wrapper).
func NewClient(cfg ClientConfig) (*Client, error) {
	base, err := NewBaseClient(BaseClientConfig{
		Endpoint:   cfg.Endpoint,
		AccessKey:  cfg.AccessKey,
		SecretKey:  cfg.SecretKey,
		UseTLS:     cfg.UseTLS,
		CACertPath: cfg.CACertPath,
	})
	if err != nil {
		return nil, err
	}

	c := base.WithBucketAndPrefix(cfg.Bucket, "")
	if err := c.EnsureBucket(context.Background()); err != nil {
		return nil, err
	}

	return c, nil
}

// PutObject uploads an object to MinIO
func (c *Client) PutObject(ctx context.Context, objectName string, reader io.Reader, size int64) error {
	_, err := c.mc.PutObject(ctx, c.bucket, c.key(objectName), reader, size, minios3.PutObjectOptions{})
	return err
}

// GetObject downloads an object from MinIO
func (c *Client) GetObject(ctx context.Context, objectName string) (io.ReadCloser, error) {
	return c.mc.GetObject(ctx, c.bucket, c.key(objectName), minios3.GetObjectOptions{})
}

// StatObject gets object info
func (c *Client) StatObject(ctx context.Context, objectName string) (minios3.ObjectInfo, error) {
	return c.mc.StatObject(ctx, c.bucket, c.key(objectName), minios3.StatObjectOptions{})
}

// RemoveObject deletes an object
func (c *Client) RemoveObject(ctx context.Context, objectName string) error {
	return c.mc.RemoveObject(ctx, c.bucket, c.key(objectName), minios3.RemoveObjectOptions{})
}

// ListObjects lists objects with a prefix, stripping the client's configured prefix from returned keys
// so callers always see repo-relative paths.
func (c *Client) ListObjects(ctx context.Context, prefix string) <-chan minios3.ObjectInfo {
	prefixedPrefix := c.prefix + prefix
	ch := c.mc.ListObjects(ctx, c.bucket, minios3.ListObjectsOptions{Prefix: prefixedPrefix, Recursive: true})

	// Wrap the channel to strip the prefix from each returned key
	result := make(chan minios3.ObjectInfo)
	go func() {
		defer close(result)
		for obj := range ch {
			obj.Key = strings.TrimPrefix(obj.Key, c.prefix)
			result <- obj
		}
	}()
	return result
}

// CopyObject copies an object server-side (atomic from client perspective)
func (c *Client) CopyObject(ctx context.Context, source, destination string) error {
	_, err := c.mc.CopyObject(ctx, minios3.CopyDestOptions{
		Bucket: c.bucket,
		Object: c.key(destination),
	}, minios3.CopySrcOptions{
		Bucket: c.bucket,
		Object: c.key(source),
	})
	return err
}

// PresignedGetObject creates a presigned GET URL
func (c *Client) PresignedGetObject(ctx context.Context, objectName string, _ int) (string, error) {
	url, err := c.mc.PresignedGetObject(ctx, c.bucket, c.key(objectName), 0, nil)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}

// PresignedPutObject creates a presigned PUT URL
func (c *Client) PresignedPutObject(ctx context.Context, objectName string, _ int) (string, error) {
	url, err := c.mc.PresignedPutObject(ctx, c.bucket, c.key(objectName), 0)
	if err != nil {
		return "", err
	}
	return url.String(), nil
}
