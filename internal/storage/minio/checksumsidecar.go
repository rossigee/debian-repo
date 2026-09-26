package minio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// ChecksumSidecarStore handles checksum sidecar persistence to MinIO
type ChecksumSidecarStore struct {
	client *Client
}

// NewChecksumSidecarStore creates a new checksum sidecar store
func NewChecksumSidecarStore(client *Client) *ChecksumSidecarStore {
	return &ChecksumSidecarStore{client: client}
}

// PutChecksumSidecar writes a checksum sidecar to MinIO alongside a .deb file
// Key format: <debObjectKey>.checksums.json (e.g. pool/main/foo_1.0_amd64.deb.checksums.json)
// Returns nil error on success; errors are best-effort (sidecar write failure doesn't block registration)
func (cs *ChecksumSidecarStore) PutChecksumSidecar(ctx context.Context, debKey string, sidecar *model.ChecksumSidecarV1) error {
	if sidecar == nil {
		return fmt.Errorf("sidecar cannot be nil")
	}

	sidecarKey := debKey + ".checksums.json"

	// Marshal to JSON
	data, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal checksum sidecar: %w", err)
	}

	// Upload to MinIO (no atomic temp-key dance needed — this is a best-effort cache)
	if err := cs.client.PutObject(ctx, sidecarKey, bytes.NewReader(data), int64(len(data))); err != nil {
		return fmt.Errorf("failed to write checksum sidecar to %s: %w", sidecarKey, err)
	}

	return nil
}

// GetChecksumSidecar reads a checksum sidecar from MinIO
// Returns (nil, nil) if sidecar does not exist (cache miss)
// Returns error only if retrieval fails (network, permission, corruption)
func (cs *ChecksumSidecarStore) GetChecksumSidecar(ctx context.Context, debKey string) (*model.ChecksumSidecarV1, error) {
	sidecarKey := debKey + ".checksums.json"

	// Try to get the object
	obj, err := cs.client.GetObject(ctx, sidecarKey)
	if err != nil {
		// Check if it's a "not found" error
		if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "does not exist") || strings.Contains(err.Error(), "NoSuchKey") {
			return nil, nil // Cache miss — not an error
		}
		return nil, fmt.Errorf("failed to read checksum sidecar from %s: %w", sidecarKey, err)
	}
	defer func() {
		_ = obj.Close()
	}()

	// Unmarshal JSON
	var sidecar model.ChecksumSidecarV1
	if err := json.NewDecoder(obj).Decode(&sidecar); err != nil {
		return nil, fmt.Errorf("failed to parse checksum sidecar from %s: %w", sidecarKey, err)
	}

	return &sidecar, nil
}
