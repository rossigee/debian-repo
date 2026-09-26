---
title: API Reference
description: Complete HTTP API documentation for debian-repo
---


Complete HTTP API documentation for debian-repo with endpoint details, authentication, and examples.

## Quick Start

The API provides three main endpoint groups:

1. **Metadata Endpoints** — For apt clients (GET-only)
   - `/dists/` — Release metadata
   - `/pool/` — Package files
   - `/pubkey.gpg` — GPG public key

2. **CI/CD Endpoints** — For CI pipelines (requires bearer token)
   - `POST /api/v1/upload?suite={suite}&component={component}` — Upload package
   - `DELETE /api/v1/dists/{suite}/{component}/remove/{pkg}/{ver}/{arch}` — Remove package
   - `POST /api/v1/admin/move` — Move packages between suites
   - `POST /api/v1/admin/reconcile` — Rebuild index from pool

3. **Data APIs** — For tools and dashboards
   - `GET /index.json` — Package index as JSON
   - `GET /dists/{suite}/feed.atom` — Atom feed of recent packages

## OpenAPI Specification

**View interactive API documentation:**

- **[Swagger UI](./swagger-ui.html)** — Interactive endpoint explorer
- **[ReDoc](./redoc.html)** — Clean, readable API reference
- **[Raw OpenAPI YAML](./openapi.yaml)** — Machine-readable specification

## Authentication

### 1. HTTP Basic Auth (Metadata endpoints)

For apt clients and metadata retrieval:

```bash
curl -u username:password https://debs.myorgname.com/dists/stable/Release
```

Or using header:

```bash
curl -H "Authorization: Basic $(echo -n 'username:password' | base64)" \
  https://debs.myorgname.com/dists/stable/Release
```

### 2. Bearer Token (CI/CD endpoints)

For uploading and managing packages:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  -X POST --data-binary @package.deb \
  https://debs.myorgname.com/api/v1/upload?suite=stable&component=main
```

### 3. No Auth (Data APIs)

Some endpoints are public:

```bash
curl https://debs.myorgname.com/index.json
curl https://debs.myorgname.com/pubkey.gpg
curl https://debs.myorgname.com/
```

## Common Patterns

### Upload a package

```bash
TOKEN="your-bearer-token"
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @nginx_1.20.0_amd64.deb \
  https://debs.myorgname.com/api/v1/upload?suite=stable&component=main
```

**Response (200):**
```json
{
  "status": "registered",
  "package": "nginx",
  "version": "1.20.0",
  "architecture": "amd64",
  "suite": "stable",
  "component": "main",
  "filename": "pool/main/n/nginx/nginx_1.20.0_amd64.deb"
}
```

### Remove a package

Removes a package version from the repository and deletes its `.deb` file from pool storage:

```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx/1.20.0/amd64
```

**Response (200):**
```json
{
  "status": "removed",
  "package": "nginx",
  "suite": "stable"
}
```

**What happens:**
1. Package is removed from the index
2. Repository metadata (Release, Packages files) is re-rendered
3. `.deb` file is deleted from pool storage (async, non-blocking)
4. Updated snapshot is persisted to MinIO

Supports partial removal by version or architecture (see [Managing Packages](../user-guide/managing.md) for details).

### Move packages between suites

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "packages": ["nginx*"],
    "dry_run": false
  }' \
  https://debs.myorgname.com/api/v1/admin/move
```

**Response (200):**
```json
{
  "status": "moved",
  "from_suite": "testing",
  "to_suite": "stable",
  "count": 1,
  "moved": [
    {
      "package": "nginx",
      "version": "1.20.0",
      "architecture": "amd64",
      "filename": "pool/main/n/nginx/nginx_1.20.0_amd64.deb"
    }
  ]
}
```

### Get package metadata (apt)

```bash
# List all packages for amd64
curl -u user:pass https://debs.myorgname.com/dists/stable/main/binary-amd64/Packages

# Get Release file
curl -u user:pass https://debs.myorgname.com/dists/stable/Release

# Get GPG signature
curl -u user:pass https://debs.myorgname.com/dists/stable/InRelease
```

### Query package index

```bash
curl https://debs.myorgname.com/index.json | jq '.packages.stable'
```

**Response:**
```json
{
  "nginx": {
    "latest_version": "1.20.0",
    "latest_architecture": "amd64",
    "available_architectures": ["amd64", "arm64"],
    "latest_size_bytes": 567890,
    "uploaded_at": "2024-09-26T15:00:00Z",
    "description": "High performance web server",
    "maintainer": "Debian Developers"
  }
}
```

### Subscribe to package updates (Atom feed)

```bash
# Get latest package uploads for stable suite
curl https://debs.myorgname.com/dists/stable/feed.atom
```

**Response:** Atom XML feed with recent package uploads

The feed includes:
- **Title:** Package name, version, architecture
- **Summary:** Package description from control metadata
- **Published/Updated:** Upload timestamp
- **Link:** Direct download URL to the .deb file

Compatible with any feed reader (RSS readers, monitoring tools, CI/CD pipelines).

**Configurable limits** per repository (default: 20 items, configurable via `max_items` in config)

## Error Handling

### Common Error Codes

| Code | Meaning | Example |
|------|---------|---------|
| 400 | Invalid request | Malformed .deb file |
| 401 | Unauthorized | Missing credentials |
| 403 | Forbidden | Token lacks permission |
| 404 | Not found | Suite/package doesn't exist |
| 409 | Conflict | Package exists, or suite is protected |
| 413 | Too large | Package exceeds size limit |
| 500 | Server error | Internal failure |

### Example Error Response

```json
{
  "status": "error",
  "message": "invalid .deb file: missing control file"
}
```

### Protected Suite Error

```bash
# Try to remove the last package from stable (protected)
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/last-pkg/1.0.0/amd64
```

**Response (409):**
```json
{
  "status": "conflict",
  "message": "suite 'stable' is protected and cannot be fully emptied; retry with ?force=true"
}
```

**Solution:**
```bash
# Add ?force=true to override protection
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/last-pkg/1.0.0/amd64?force=true
```

## Presigned URL Upload

For large packages, use presigned URLs to avoid streaming through debian-repo:

### Step 1: Request presigned URL

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"suite":"stable","component":"main","filename": "nginx_1.20.0_amd64.deb"}' \
  https://debs.myorgname.com/api/v1/presign
```

**Response:**
```json
{
  "presigned_url": "https://minio.minio.internal/debs-myorgname/...",
  "expires_in": 900
}
```

### Step 2: Upload to MinIO

```bash
PRESIGNED_URL="https://minio.minio.internal/debs-myorgname/..."
curl -X PUT \
  -H "Content-Type: application/octet-stream" \
  --data-binary @nginx_1.20.0_amd64.deb \
  "$PRESIGNED_URL"
```

### Step 3: Register the package

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"suite":"stable","component":"main","staging_key":"<staging-uuid-from-presign>"}' \
  https://debs.myorgname.com/api/v1/register
```

## Reconciliation (Async)

Rebuild the package index from pool contents:

### Start reconciliation job

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/admin/reconcile
```

**Response (202):**
```json
{
  "job_id": "job-abc123def456",
  "status": "running"
}
```

### Poll job status

```bash
curl -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/admin/reconcile/job-abc123def456
```

**Response (while running):**
```json
{
  "job_id": "job-abc123def456",
  "status": "running",
  "progress": 45
}
```

**Response (when done):**
```json
{
  "job_id": "job-abc123def456",
  "status": "completed",
  "progress": 100,
  "result": {
    "packages_found": 1234,
    "packages_changed": 5,
    "elapsed": "15.2s"
  }
}
```

## Health Checks

### Liveness Probe (service is alive)

```bash
curl -s https://debs.myorgname.com/health/live && echo "UP" || echo "DOWN"
```

### Readiness Probe (service is ready)

```bash
curl -s https://debs.myorgname.com/health/ready && echo "READY" || echo "NOT READY"
```

## Metrics (Prometheus)

```bash
curl -H "Authorization: Bearer $METRICS_TOKEN" \
  https://debs.myorgname.com:9090/metrics
```

Sample metrics:
- `debian_repo_packages_total{suite="stable"}` — Total packages in suite
- `debian_repo_upload_duration_seconds` — Upload timing
- `debian_repo_index_rebuild_duration_seconds` — Reconciliation timing

## Code Examples

### Python

```python
import requests

TOKEN = "your-bearer-token"
API_URL = "https://debs.myorgname.com/api/v1"

# Upload a package
with open("nginx_1.20.0_amd64.deb", "rb") as f:
    resp = requests.post(
        f"{API_URL}/dists/stable/main/upload",
        data=f,
        headers={"Authorization": f"Bearer {TOKEN}"}
    )
    print(resp.json())

# Move package to stable
resp = requests.post(
    f"{API_URL}/dists/move",
    json={
        "from_suite": "testing",
        "to_suite": "stable",
        "packages": ["nginx*"]
    },
    headers={"Authorization": f"Bearer {TOKEN}"}
)
print(resp.json())
```

### Bash

```bash
#!/bin/bash
TOKEN="your-bearer-token"
API="https://debs.myorgname.com/api/v1"

# Upload
curl -X POST -H "Authorization: Bearer $TOKEN" \
  --data-binary @nginx_1.20.0_amd64.deb \
  "$API/dists/stable/main/upload"

# Remove
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  "$API/dists/stable/main/remove/nginx/1.20.0/amd64"
```

### Go

```go
package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
)

func uploadPackage(token, filepath string) error {
	data, _ := ioutil.ReadFile(filepath)
	req, _ := http.NewRequest("POST",
		"https://debs.myorgname.com/api/v1/dists/stable/main/upload",
		bytes.NewReader(data))
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	fmt.Println(string(body))
	return nil
}
```

## Rate Limiting

No per-user rate limits. However:

- **Concurrent uploads** are supported
- **Large packages** (>512MB) may be rejected
- **Timeout** is typically 30 seconds per request

For best practices, see [CI Integration — Performance Tips](../user-guide/ci-integration.md#performance-tips).

## Pagination

The API does not support pagination. All results are returned at once.

## Webhooks

Not currently supported. Poll `/api/v1/admin/reconcile/{job_id}` for async operations.

## See Also

- **[Getting Started](../getting-started/)** — 5-minute overview
- **[User Guide](../user-guide/)** — Practical upload examples
- **[Admin Guide](../admin-guide/)** — Server configuration
