---
title: JSON API
description: Machine-readable package index
---


Reference for the machine-readable package index API, useful for automation, dashboards, and tooling.

## Overview

The JSON API provides a complete, read-only package inventory as JSON. It's public (no authentication required) and cached for 60 seconds.

### Endpoint

```
GET /index.json
```

### Authentication

None required — this endpoint is public.

### Response Format

```json
{
  "packages": {
    "stable": {
      "nginx": {
        "latest_version": "1.20.0",
        "latest_architecture": "amd64",
        "available_architectures": ["amd64", "arm64"],
        "latest_size_bytes": 567890,
        "uploaded_at": "2026-09-26T15:00:00Z",
        "description": "High performance web server",
        "maintainer": "Debian Developers",
        "section": "web"
      }
    }
  },
  "distributions": ["stable", "testing"],
  "components": ["main", "contrib"],
  "generated_at": "2026-09-26T15:30:00Z"
}
```

## Response Fields

### Root Level

| Field | Type | Description |
|-------|------|-------------|
| `packages` | object | Map of `{suite_name → {package_name → package_summary}}` |
| `distributions` | array | Sorted list of all suite names in the repository |
| `components` | array | Sorted list of all components in the repository |
| `generated_at` | string | ISO 8601 timestamp when the index was generated |

### PackageSummary

| Field | Type | Description |
|-------|------|-------------|
| `latest_version` | string | Most recently uploaded version (see "Version Selection" below) |
| `latest_architecture` | string | Architecture of the latest version |
| `available_architectures` | array | All architectures this package has been built for |
| `latest_size_bytes` | number | File size of the latest `.deb` file in bytes |
| `uploaded_at` | string | ISO 8601 timestamp of the latest upload |
| `description` | string | Short description from the package's control metadata |
| `maintainer` | string | Maintainer email from the package metadata |
| `section` | string | Package section (e.g., "web", "development", "misc") |

## Example Usage

### Check what versions are available

```bash
curl https://debs.myorgname.com/index.json | jq '.packages.stable.nginx'
```

Output:
```json
{
  "latest_version": "1.20.0",
  "latest_architecture": "amd64",
  "available_architectures": ["amd64", "arm64"],
  "latest_size_bytes": 567890,
  "uploaded_at": "2026-09-26T15:00:00Z",
  "description": "High performance web server",
  "maintainer": "Debian Developers",
  "section": "web"
}
```

### List all available packages in a suite

```bash
curl https://debs.myorgname.com/index.json | jq '.packages.stable | keys'
```

### Automation: Check if a newer version exists

```bash
#!/bin/bash
CURRENT_VERSION="1.19.0"
LATEST_VERSION=$(curl -s https://debs.myorgname.com/index.json | \
  jq -r '.packages.stable.nginx.latest_version')

if [ "$LATEST_VERSION" != "$CURRENT_VERSION" ]; then
  echo "Upgrade available: $LATEST_VERSION"
  apt update && apt install nginx
fi
```

### Dashboard: Package inventory snapshot

```bash
curl https://debs.myorgname.com/index.json | jq '{
  total_packages: (.packages | map(length) | add),
  suites: .distributions,
  latest_upload: .generated_at
}'
```

## Important Notes

### Version Selection

The "latest version" is determined by:
1. **Most recent upload time** (`uploaded_at` in descending order)
2. **Lexicographic string comparison on tie** — if two versions have the same upload timestamp, the higher ASCII value wins

⚠️ **Caveat**: Lexicographic ordering does **NOT** follow Debian version semantics. For strict version comparison, always use `dpkg --compare-versions` or a Debian-aware library. For example, "1.9.0" > "1.10.0" in lexicographic order.

### Caching

The response is cached for **60 seconds** (`Cache-Control: public, max-age=60`). Between requests within 60 seconds, you may see stale data if packages were recently uploaded.

For real-time package status, use the individual Packages/Release metadata endpoints instead (see **[Metadata Endpoints](../api/index.md#metadata-endpoints)**).

### Size and Performance

The entire response is generated on-demand and includes all packages across all suites. For very large repositories (thousands of packages), this response can be large. Consider:

- Filtering locally after fetching (jq is efficient for this)
- Polling less frequently if bandwidth is a concern
- Using individual suite metadata endpoints for suite-specific checks

## See Also

- **[API Reference](../api/)** — Full endpoint documentation
- **[Getting Started](../getting-started/)** — Quick overview of debian-repo
- **[User Guide](_index.md)** — Overview of all user tasks
