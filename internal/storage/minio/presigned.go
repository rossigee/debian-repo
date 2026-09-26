package minio

import (
	"context"
	"fmt"
	"io"
	"time"

	minios3 "github.com/minio/minio-go/v7"
)

// PresignedUploadInfo holds the presigned URL and metadata for an upload
type PresignedUploadInfo struct {
	UploadURL  string    `json:"upload_url"`
	StagingKey string    `json:"staging_key"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// GeneratePresignedUploadURL generates a presigned URL for direct MinIO upload.
// The key is scoped to the client's bucket and key prefix like all other
// operations.
func (c *Client) GeneratePresignedUploadURL(ctx context.Context, stagingKey string, expiry time.Duration) (*PresignedUploadInfo, error) {
	if expiry == 0 {
		expiry = 1 * time.Hour
	}

	// Generate presigned PUT URL
	url, err := c.mc.PresignedPutObject(ctx, c.bucket, c.key(stagingKey), expiry)
	if err != nil {
		return nil, fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return &PresignedUploadInfo{
		UploadURL:  url.String(),
		StagingKey: stagingKey,
		ExpiresAt:  time.Now().Add(expiry),
	}, nil
}

// FetchStagedFile downloads a staged file from MinIO.
// The key is scoped to the client's bucket and key prefix.
func (c *Client) FetchStagedFile(ctx context.Context, stagingKey string) (io.ReadCloser, error) {
	object, err := c.mc.GetObject(ctx, c.bucket, c.key(stagingKey), minios3.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch staged file: %w", err)
	}
	return object, nil
}

// MoveObject atomically moves (copies then deletes) an object from staging to pool.
// Both keys are scoped to the client's bucket and key prefix.
func (c *Client) MoveObject(ctx context.Context, srcKey, dstKey string) error {
	// Copy from staging to pool
	_, err := c.mc.CopyObject(ctx, minios3.CopyDestOptions{
		Bucket: c.bucket,
		Object: c.key(dstKey),
	}, minios3.CopySrcOptions{
		Bucket: c.bucket,
		Object: c.key(srcKey),
	})
	if err != nil {
		return fmt.Errorf("failed to copy object from %s to %s: %w", srcKey, dstKey, err)
	}

	// Delete the source (staging) file
	err = c.mc.RemoveObject(ctx, c.bucket, c.key(srcKey), minios3.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to remove staged file %s: %w", srcKey, err)
	}

	return nil
}

// DeleteObject removes an object from MinIO.
// The key is scoped to the client's bucket and key prefix.
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	err := c.mc.RemoveObject(ctx, c.bucket, c.key(key), minios3.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete object %s: %w", key, err)
	}
	return nil
}

// ObjectExists checks if an object exists in MinIO.
// The key is scoped to the client's bucket and key prefix.
func (c *Client) ObjectExists(ctx context.Context, key string) (bool, error) {
	_, err := c.mc.StatObject(ctx, c.bucket, c.key(key), minios3.StatObjectOptions{})
	if err != nil {
		errResp := minios3.ToErrorResponse(err)
		if errResp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, fmt.Errorf("failed to check object existence: %w", err)
	}
	return true, nil
}
