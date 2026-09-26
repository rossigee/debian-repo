---
title: Upload Flows
description: Deep dive into how debian-repo handles package uploads in different modes
---


Deep dive into how debian-repo handles package uploads in different modes.

## Direct Upload Mode (Default)

The CI pipeline uploads the `.deb` file directly to debian-repo. The service validates, stores, and registers the package in a single operation.

### Flow

```
CI Pipeline
    ↓ POST /api/v1/upload?suite=stable&component=main
debian-repo Service:
  1. Receive streaming .deb file
  2. Validate structure (ar archive, control metadata)
  3. Compute MD5/SHA1/SHA256 checksums
  4. Stream to MinIO pool storage
  5. Add to in-memory index
  6. Render Release/Packages/InRelease
  7. Return success with metadata
    ↓
Package available for installation
```

### Implementation

**Validation** (streaming):
- AR archive structure check
- Control.tar extraction
- Field parsing via go-debian
- Checksum computation during streaming (no temp files)

**Storage** (atomic):
- Stream directly to MinIO
- Fail fast if validation fails (before upload completes)
- No intermediate staging

**Index Update** (under distribution lock):
- Add PackageVersion to in-memory index
- Re-render Packages/Release/InRelease
- Swap atomic pointer (non-blocking for readers)
- Persist snapshot to MinIO (async by default)

### Example

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary "@my-package_1.0.0_amd64.deb" \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
```

### Use Cases

- Single-file packages
- Small to medium files (<100MB)
- CI pipelines with good network
- Private registries with low upload volume

### Advantages

- Single request (no presign step)
- Atomic from client perspective
- Fast for typical packages
- Simple error handling

### Disadvantages

- debian-repo handles streaming (CPU/memory impact)
- Large files tie up debian-repo process
- Not ideal for many concurrent uploads

---

## Presigned URL Mode

The CI pipeline requests a temporary URL, uploads directly to MinIO, then registers the package with debian-repo.

### Flow

```
CI Pipeline
    ↓ POST /api/v1/presign (body: suite, component, filename)
debian-repo Service: return temporary MinIO URL
    ↓
CI Pipeline
    ↓ PUT <presigned-url> (direct to MinIO)
MinIO: accept file directly
    ↓
CI Pipeline
    ↓ POST /api/v1/register (body: suite, component, staging_key)
debian-repo Service:
  1. Fetch from MinIO
  2. Validate structure
  3. Compute checksums
  4. Add to index
  5. Render/persist metadata
    ↓
Package available for installation
```

### Implementation

**Presign Request**:
- Returns temporary URL (expires in 15min by default)
- URL is direct MinIO access, bypasses debian-repo

**Direct Upload**:
- CI uploads directly to MinIO
- debian-repo not involved in streaming
- Fast for large files

**Registration**:
- Fetch from MinIO
- Validate & compute checksums
- Same atomic index update as direct mode

### Example

```bash
# 1. Request presigned URL
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"filename":"my-package_1.0.0_amd64.deb"}' \
  https://debs.myorgname.com/api/v1/dists/stable/main/presign

# Response: presigned_url, expires_in

# 2. Upload directly to MinIO
curl -X PUT --data-binary "@my-package_1.0.0_amd64.deb" \
  "https://minio.internal/..."

# 3. Register with debian-repo
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"filename":"my-package_1.0.0_amd64.deb"}' \
  https://debs.myorgname.com/api/v1/dists/stable/main/register
```

### Use Cases

- Large packages (>100MB)
- Many concurrent uploads
- CI systems with high throughput
- Network bandwidth constraints

### Advantages

- debian-repo not involved in streaming
- Faster for large files
- Better concurrency (MinIO handles direct uploads)
- Lower CPU/memory on debian-repo

### Disadvantages

- Three requests instead of one
- More complex error handling
- URL expiration handling needed
- Validation happens after upload

---

## Comparison

| Aspect | Direct | Presigned |
|--------|--------|-----------|
| Requests | 1 | 3 |
| Best for | Small/medium (<100MB) | Large (>100MB) |
| debian-repo load | Handles streaming | Minimal |
| Concurrency | Limited | High |
| Error handling | Simple | Complex |
| Setup | Simple | Extra presign step |

## Choosing a Mode

**Use Direct Upload when:**
- Packages <100MB
- Occasional uploads
- Simple CI/CD pipelines
- Low upload volume

**Use Presigned URLs when:**
- Packages >100MB
- High upload concurrency
- Heavy CI/CD pipelines
- Want to reduce debian-repo load

## See Also

- **[Uploading Packages](uploading.md)** — How to upload in practice
- **[API Reference](../api/_index.md)** — Endpoint details
