package minio

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// AptUserStore handles atomic apt-user persistence to MinIO
type AptUserStore struct {
	client   *Client
	storeKey string
}

// NewAptUserStore creates a new apt-user store
func NewAptUserStore(client *Client, storeKey string) *AptUserStore {
	return &AptUserStore{
		client:   client,
		storeKey: storeKey,
	}
}

// PutUsers persists apt users to MinIO with atomic semantics
// Writes to a temp key first, then server-side copies to the final key
func (aus *AptUserStore) PutUsers(ctx context.Context, store *model.AptUserStoreV1) (string, error) {
	if aus.client.Bucket() == "" {
		return "", fmt.Errorf("apt user store client has no bucket set: use WithBucketAndPrefix first")
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal apt users: %w", err)
	}

	// Gzip compress
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(data); err != nil {
		return "", fmt.Errorf("failed to gzip apt users: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("failed to close gzip: %w", err)
	}

	compressed := gzBuf.Bytes()

	// Upload to temp key
	tempKey := aus.storeKey + ".tmp-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := aus.client.PutObject(ctx, tempKey, bytes.NewReader(compressed), int64(len(compressed))); err != nil {
		return "", fmt.Errorf("failed to upload temp apt users: %w", err)
	}

	// Atomic server-side copy to final key
	if err := aus.client.CopyObject(ctx, tempKey, aus.storeKey); err != nil {
		_ = aus.client.RemoveObject(ctx, tempKey)
		return "", fmt.Errorf("failed to copy apt users to final location: %w", err)
	}

	// Clean up temp object
	_ = aus.client.RemoveObject(ctx, tempKey)

	return aus.storeKey, nil
}

// GetUsers retrieves and decompresses apt users from MinIO
// Returns an empty store (not an error) if the object doesn't exist yet
func (aus *AptUserStore) GetUsers(ctx context.Context) (*model.AptUserStoreV1, error) {
	if aus.client.Bucket() == "" {
		return nil, fmt.Errorf("apt user store client has no bucket set: use WithBucketAndPrefix first")
	}

	reader, err := aus.client.GetObject(ctx, aus.storeKey)
	if err != nil {
		// Check if it's a not-found error
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "does not exist") {
			return &model.AptUserStoreV1{FormatVersion: 1, GeneratedAt: time.Now().UTC(), Users: []model.AptUser{}}, nil
		}
		return nil, fmt.Errorf("failed to get apt users object: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	// Check if object is empty
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, reader)
	if err != nil {
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "NoSuchKey") {
			return &model.AptUserStoreV1{FormatVersion: 1, GeneratedAt: time.Now().UTC(), Users: []model.AptUser{}}, nil
		}
		return nil, fmt.Errorf("failed to read apt users object: %w", err)
	}
	if buf.Len() == 0 {
		return &model.AptUserStoreV1{FormatVersion: 1, GeneratedAt: time.Now().UTC(), Users: []model.AptUser{}}, nil
	}
	if strings.Contains(buf.String(), "NoSuchKey") || strings.Contains(buf.String(), "does not exist") {
		return &model.AptUserStoreV1{FormatVersion: 1, GeneratedAt: time.Now().UTC(), Users: []model.AptUser{}}, nil
	}

	// Decompress
	gz, err := gzip.NewReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "NoSuchKey") {
			return &model.AptUserStoreV1{FormatVersion: 1, GeneratedAt: time.Now().UTC(), Users: []model.AptUser{}}, nil
		}
		return nil, fmt.Errorf("failed to read gzip: %w", err)
	}
	defer func() {
		_ = gz.Close()
	}()

	data, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("failed to read decompressed data: %w", err)
	}

	// Unmarshal JSON
	var store model.AptUserStoreV1
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("failed to unmarshal apt users: %w", err)
	}

	return &store, nil
}
