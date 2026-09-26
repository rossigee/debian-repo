package minio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"compress/gzip"
	"git.golder.lan/rossgolderltd/debian-repo/internal/model"
)

// SnapshotStore handles atomic snapshot persistence to MinIO
type SnapshotStore struct {
	client        *Client
	snapshotKey   string
	historyPrefix string
	historyKeep   int
}

// NewSnapshotStore creates a new snapshot store
func NewSnapshotStore(client *Client, snapshotKey, historyPrefix string, historyKeep int) *SnapshotStore {
	return &SnapshotStore{
		client:        client,
		snapshotKey:   snapshotKey,
		historyPrefix: historyPrefix,
		historyKeep:   historyKeep,
	}
}

// DefaultHistoryPrefix derives the snapshot history prefix from the snapshot
// key: history lives in a "history/" directory next to the snapshot object,
// never inside the staging namespace used for presigned uploads.
func DefaultHistoryPrefix(snapshotKey string) string {
	dir := path.Dir(snapshotKey)
	if dir == "." || dir == "/" || dir == "" {
		return "history/"
	}
	return strings.TrimSuffix(dir, "/") + "/history/"
}

// PutSnapshot persists a snapshot to MinIO with atomic semantics
// Writes to a temp key first, then server-side copies to the final key
func (ss *SnapshotStore) PutSnapshot(ctx context.Context, snap *model.SnapshotV1) (string, error) {
	// Marshal snapshot to JSON
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Gzip compress
	var gzBuf bytes.Buffer
	gz := gzip.NewWriter(&gzBuf)
	if _, err := gz.Write(data); err != nil {
		return "", fmt.Errorf("failed to gzip snapshot: %w", err)
	}
	if err := gz.Close(); err != nil {
		return "", fmt.Errorf("failed to close gzip: %w", err)
	}

	compressed := gzBuf.Bytes()

	// Upload to temp key
	tempKey := ss.snapshotKey + ".tmp-" + fmt.Sprintf("%d", time.Now().UnixNano())
	if err := ss.client.PutObject(ctx, tempKey, bytes.NewReader(compressed), int64(len(compressed))); err != nil {
		return "", fmt.Errorf("failed to upload temp snapshot: %w", err)
	}

	// Atomic server-side copy to final key
	if err := ss.client.CopyObject(ctx, tempKey, ss.snapshotKey); err != nil {
		// Clean up temp object on copy failure
		_ = ss.client.RemoveObject(ctx, tempKey)
		return "", fmt.Errorf("failed to copy snapshot to final location: %w", err)
	}

	// Clean up temp object
	_ = ss.client.RemoveObject(ctx, tempKey)

	// Optionally save to history
	if ss.historyKeep > 0 {
		historyKey := fmt.Sprintf("%s/index-snapshot-%d-%d.json.gz", ss.historyPrefix, snap.SnapshotGen, time.Now().Unix())
		_ = ss.client.PutObject(ctx, historyKey, bytes.NewReader(compressed), int64(len(compressed)))
		// Cleanup old history entries (best effort)
		_ = ss.pruneHistory(ctx)
	}

	return ss.snapshotKey, nil
}

// GetSnapshot retrieves and decompresses a snapshot from MinIO
func (ss *SnapshotStore) GetSnapshot(ctx context.Context, key string) (*model.SnapshotV1, error) {
	if key == "" {
		key = ss.snapshotKey
	}

	// Retrieve object
	reader, err := ss.client.GetObject(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot object: %w", err)
	}
	defer func() {
		_ = reader.Close()
	}()

	// Decompress
	gz, err := gzip.NewReader(reader)
	if err != nil {
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
	var snap model.SnapshotV1
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal snapshot: %w", err)
	}

	return &snap, nil
}

// GetLatestSnapshot retrieves the latest snapshot
func (ss *SnapshotStore) GetLatestSnapshot(ctx context.Context) (*model.SnapshotV1, error) {
	return ss.GetSnapshot(ctx, ss.snapshotKey)
}

// ListHistorySnapshots lists available history snapshots
func (ss *SnapshotStore) ListHistorySnapshots(ctx context.Context) ([]string, error) {
	var keys []string
	for obj := range ss.client.ListObjects(ctx, ss.historyPrefix) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

// pruneHistory removes old history snapshots, keeping only the most recent N
func (ss *SnapshotStore) pruneHistory(ctx context.Context) error {
	if ss.historyKeep <= 0 {
		return nil
	}

	// List history objects
	var keys []string
	for obj := range ss.client.ListObjects(ctx, ss.historyPrefix) {
		if obj.Err != nil {
			return obj.Err
		}
		keys = append(keys, obj.Key)
	}

	// Keep only the most recent N. History keys embed "<gen>-<unix>"
	// (see PutSnapshot), so sort by (unix, gen) numerically — plain
	// lexicographic order is wrong across digit widths and S3 list order
	// is undefined.
	if len(keys) > ss.historyKeep {
		sort.Slice(keys, func(i, j int) bool {
			ui, gi := historyKeyOrder(keys[i])
			uj, gj := historyKeyOrder(keys[j])
			if ui != uj {
				return ui < uj
			}
			return gi < gj
		})
		for i := 0; i < len(keys)-ss.historyKeep; i++ {
			_ = ss.client.RemoveObject(ctx, keys[i])
		}
	}

	return nil
}

// historyKeyOrder extracts (unix, gen) from a history key of the form
// "<prefix>/index-snapshot-<gen>-<unix>.json.gz" for oldest-first sorting.
// Unparseable keys sort before parseable ones so they are pruned first.
func historyKeyOrder(key string) (unix, gen int64) {
	base := path.Base(key)
	base = strings.TrimSuffix(base, ".json.gz")
	base = strings.TrimSuffix(base, ".gz")
	parts := strings.Split(base, "-")
	if len(parts) < 2 {
		return 0, 0
	}
	unix, _ = strconv.ParseInt(parts[len(parts)-1], 10, 64)
	gen, _ = strconv.ParseInt(parts[len(parts)-2], 10, 64)
	return unix, gen
}

// SnapshotExists checks if a snapshot exists
func (ss *SnapshotStore) SnapshotExists(ctx context.Context, key string) (bool, error) {
	if key == "" {
		key = ss.snapshotKey
	}

	_, err := ss.client.StatObject(ctx, key)
	if err != nil {
		// Check if it's a 404 (not found) vs. other error
		if strings.Contains(err.Error(), "NotFound") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
