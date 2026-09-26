---
title: Maintenance & Backup
description: Snapshots, reconciliation, and recovery
---


Guide for backing up your repository, recovering from failures, and rebuilding the index when needed.

## Snapshots

The package index is periodically serialized to MinIO as a snapshot, enabling recovery if the in-memory index is corrupted or lost.

### Snapshot Format

Snapshots are stored as gzip-compressed JSON in MinIO at `storage.snapshot_key` (default: `_meta/index-snapshot.json.gz`).

Each snapshot contains:
- **Metadata:** timestamp, generation number, who generated it (e.g., "api-move", "repoctl-import")
- **Full index:** all distributions, components, packages, versions, architectures
- **Control fields:** from each package's `.deb` metadata

### Snapshot Storage

Configure snapshot settings in `config.yaml`:

```yaml
storage:
  snapshot_key: "_meta/index-snapshot.json.gz"
  snapshot_history_keep: 20           # Retain last 20 snapshots
```

Old snapshots are retained at `_meta/snapshots/index-snapshot-<gen>-<timestamp>.json.gz` for recovery purposes.

### Persistence Modes

#### Sync Mode (default, safest)

Each package registration triggers an immediate snapshot write to MinIO:

```yaml
index:
  persist_mode: "sync"
```

**Pros:** Near-zero data loss on crash
**Cons:** MinIO write latency on every upload

#### Debounced Mode (for high-volume scenarios)

Snapshot writes are batched: waits 30 seconds (configurable) after the last change before writing:

```yaml
index:
  persist_mode: "debounced"
  persist_debounce: 30s
```

**Pros:** Fewer MinIO writes, better throughput
**Cons:** Loss of recent uploads if service crashes (up to 30 seconds of data loss)

## Reconciliation

Reconciliation rebuilds the package index from pool contents, useful for:
- Recovery after index corruption
- Fixing discrepancies between pool and index
- Initial setup from existing pool
- Verifying pool integrity

### CLI: repoctl reconcile

```bash
# Dry run: preview what would change
repoctl reconcile --dry-run

# Actually rebuild and load into index
repoctl reconcile --apply

# Reconcile a specific suite only
repoctl reconcile --suite stable --apply

# Reconcile with custom config
repoctl reconcile --config /etc/debian-repo/config.yaml --apply
```

### HTTP API: Async Reconciliation

Start an async reconciliation job:

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/admin/reconcile
```

Response (202 Accepted):
```json
{
  "job_id": "job-abc123def456",
  "status": "running"
}
```

Poll for completion:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/admin/reconcile/job-abc123def456
```

While running:
```json
{
  "job_id": "job-abc123def456",
  "status": "running",
  "processed": 450,
  "total": 1000,
  "percent": 45
}
```

When complete:
```json
{
  "job_id": "job-abc123def456",
  "status": "completed",
  "processed": 1000,
  "total": 1000,
  "percent": 100,
  "discrepancies": [
    {
      "kind": "missing_from_pool",
      "message": "package nginx 1.20.0 (amd64) in index but not in pool"
    }
  ],
  "snapshot_gen": 42
}
```

### Discrepancy Types

| Type | Meaning | Action |
|------|---------|--------|
| `invalid_pool_path` | File in pool has wrong naming convention | Review pool structure |
| `invalid_deb_file` | File is not a valid .deb | Delete from pool |
| `missing_from_pool` | Index references file not in pool | Remove from index or restore from backup |
| `checksum_mismatch` | File contents changed on disk | Investigate pool corruption |

### Sidecar Cache Optimization

Reconciliation caches checksums in MinIO (`_meta/checksums/`) to speed up repeat runs:

- **Cache hit** (ETag unchanged): reuses cached checksums, ~100x faster
- **Cache miss** (file changed): recomputes all hashes (MD5, SHA1, SHA256)

On first reconciliation of a large pool, budget 15-30 minutes. Subsequent runs with unchanged pool take seconds.

## Backup Strategy

### What to Back Up

1. **Snapshots** — Retained in MinIO by `snapshot_history_keep` (default: 20 versions)
   - Located at: `_meta/snapshots/index-snapshot-*.json.gz`
   - Contains: complete index metadata
   - Frequency: automatic (after each registration)

2. **Pool** — `.deb` files in MinIO
   - Located at: `pool/**/*.deb`
   - Size: all your package files
   - Frequency: on-demand (no auto-backup)

3. **Configuration** — `config.yaml`
   - Credentials: auth tokens, MinIO keys
   - Signing key: GPG private key for signing Release files
   - Store safely (version control, secrets manager)

### Backup Examples

**Export snapshots from MinIO:**

```bash
# List all snapshots
mc ls minio/debian-packages/_meta/snapshots/

# Download the latest
mc cp minio/debian-packages/_meta/index-snapshot.json.gz ./backup/

# Download all snapshots
mc cp --recursive minio/debian-packages/_meta/snapshots/ ./backup/
```

**Export pool from MinIO:**

```bash
# Mirror entire pool
mc mirror minio/debian-packages/pool/ ./backup/pool/

# This can take a while for large repositories
```

**Backup configuration:**

```bash
# Commit to Git (in a private repo)
git add config.yaml signing-key.asc
git commit -m "Backup: config and signing key"
git push
```

## Recovery Procedures

### Recover from Snapshot

If the in-memory index is corrupted but MinIO snapshots exist:

1. **Restart the service** — debian-repo automatically loads the latest snapshot on startup
2. **Verify recovery** — Check that packages are restored:
   ```bash
   curl https://debs.myorgname.com/index.json | jq '.packages | length'
   ```

### Recover from Pool

If snapshots are lost but the pool is intact:

1. **Run reconciliation:**
   ```bash
   repoctl reconcile --apply
   ```
   This rebuilds the entire index from pool contents (slow, but complete).

2. **Verify reconstruction:**
   ```bash
   curl https://debs.myorgname.com/index.json | jq '.packages | length'
   ```

### Full Repository Restore

If both snapshot and pool are lost:

1. **Restore pool from backup** (your external backup system)
2. **Run reconciliation** to rebuild index from restored pool
3. **Verify** all packages are present

## Disaster Checklist

- [ ] MinIO bucket is backed up to external storage (S3, GCS, etc.)
- [ ] Snapshots are retained for at least 30 days
- [ ] Configuration and signing key are stored in version control
- [ ] Recovery procedures are documented and tested
- [ ] Team has practiced a recovery drill
- [ ] Monitoring alerts are set for index generation failures

## See Also

- **[Operations Guide](operations.md)** — Monitoring and troubleshooting
- **[Configuration Reference](configuration.md)** — Snapshot settings
- **[Admin Guide](_index.md)** — Overview of all admin topics
