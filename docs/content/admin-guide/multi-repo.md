---
title: Multi-Repo Setup
description: Configuring and deploying multiple independent repositories
---


Complete guide to configuring and deploying multiple independent repositories in debian-repo.

## Overview

debian-repo supports multiple independently-configured repositories with complete isolation. Each repository has its own:

- **Index** — Separate package database for each repo
- **Storage** — Independent MinIO bucket or prefix per repo
- **Signing** — Optional per-repo GPG key for Release signatures
- **Reconciliation Jobs** — Async jobs per repo
- **Authentication** — Per-repo access control via ACL grants

## Configuration

Define multiple repos in `config.yaml`:

```yaml
required_repo_id: "main"

repos:
  - id: "main"
    # Serves at root (vhost="", path_prefix="")
    # Uses global storage/signing by default
    default_suite: "stable"
    protect_default_suite: true

  - id: "testing"
    path_prefix: "/testing"
    # Serves at /testing/* on same vhost
    default_suite: "testing"
    protect_default_suite: false
    repo_metadata:
      origin: "myorgname.com"
      label: "myorgname.com Testing Repository"
      description: "Pre-release packages"
```

### Configuration Options

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | required | Unique repository identifier |
| `vhost` | string | empty | Virtual host for routing (e.g., "debs2.example.com") |
| `path_prefix` | string | empty | URL path prefix (e.g., "/testing") |
| `default_suite` | string | "stable" | Default suite if not specified in request |
| `default_component` | string | "main" | Default component if not specified |
| `protect_default_suite` | bool | true | Prevent draining default suite to empty |
| `storage.*` | object | inherits | Override storage endpoint/bucket/prefix |
| `signing.*` | object | inherits | Override signing key/passphrase |
| `repo_metadata.*` | object | inherits | Override Origin/Label/Description |

## Routing

Go 1.22 `ServeMux` natively supports multi-repo routing via **vhost-based** and **path-prefix** patterns:

### Example 1: Vhost-based routing

```yaml
repos:
  - id: "main"
    vhost: "debs.myorgname.com"      # Serves at debs.myorgname.com/*

  - id: "testing"
    vhost: "debs-test.myorgname.com"  # Serves at debs-test.myorgname.com/*
```

Access:
```bash
curl https://debs.myorgname.com/dists/stable/Release          # main repo
curl https://debs-test.myorgname.com/dists/testing/Release    # testing repo
```

### Example 2: Path-prefix routing

```yaml
repos:
  - id: "main"
    # vhost empty, path_prefix empty = serves at root

  - id: "testing"
    path_prefix: "/testing"         # Serves at /testing/*
```

Access:
```bash
curl https://debs.myorgname.com/dists/stable/Release          # main repo
curl https://debs.myorgname.com/testing/dists/testing/Release # testing repo
```

### Example 3: Mixed routing

```yaml
repos:
  - id: "main"
    vhost: "debs.myorgname.com"
    path_prefix: ""

  - id: "clients"
    vhost: "debs.myorgname.com"
    path_prefix: "/clients"

  - id: "partners"
    vhost: "debs-partners.example.com"
    path_prefix: ""
```

**Validation:** No two repos can have the same `(vhost, path_prefix)` pair.

## Storage per Repository

### Option 1: Shared bucket with prefixes (default)

All repos use the same MinIO bucket but different key prefixes:

```yaml
storage:
  minio:
    endpoint: "minio.minio.internal:443"
    bucket: "debs-myorg"
    access_key: "${MINIO_ACCESS_KEY}"
    secret_key: "${MINIO_SECRET_KEY}"

repos:
  - id: "main"
    # Uses bucket: debs-myorg, prefix: (empty or "main/")

  - id: "testing"
    # Uses bucket: debs-myorg, prefix: "testing/"
```

### Option 2: Separate buckets per repository

Each repo has its own MinIO bucket:

```yaml
repos:
  - id: "main"
    storage:
      bucket: "debs-main"
      access_key: "${MINIO_ACCESS_KEY}"
      secret_key: "${MINIO_SECRET_KEY}"

  - id: "testing"
    storage:
      bucket: "debs-testing"
      access_key: "${MINIO_ACCESS_KEY_TESTING}"
      secret_key: "${MINIO_SECRET_KEY_TESTING}"
```

### Option 3: Mixed approach

Global defaults with per-repo overrides:

```yaml
storage:
  minio:
    endpoint: "minio.minio.internal:443"
    bucket: "debs-shared"
    access_key: "${MINIO_ACCESS_KEY}"
    secret_key: "${MINIO_SECRET_KEY}"

repos:
  - id: "main"
    # Uses global: debs-shared bucket

  - id: "partners"
    storage:
      bucket: "debs-partners"  # Override bucket
      access_key: "${PARTNER_MINIO_KEY}"
      secret_key: "${PARTNER_MINIO_SECRET}"
```

## Signing Keys per Repository

### Option 1: Shared key (default)

All repos signed with the same GPG key:

```yaml
signing:
  key_path: "/etc/debian-repo/signing-key.asc"
  passphrase: "${SIGNING_KEY_PASSPHRASE}"

repos:
  - id: "main"
    # Uses global signing key
  - id: "testing"
    # Uses global signing key
```

### Option 2: Per-repo keys

Each repo has its own signing key:

```yaml
signing:
  key_path: "/etc/debian-repo/main-key.asc"
  passphrase: "${MAIN_KEY_PASSPHRASE}"

repos:
  - id: "main"
    # Uses global: /etc/debian-repo/main-key.asc

  - id: "partners"
    signing:
      key_path: "/etc/debian-repo/partners-key.asc"
      passphrase: "${PARTNERS_KEY_PASSPHRASE}"
```

## Authentication & ACL

### Per-Repo Bearer Tokens

Configure CI tokens with per-repo grants:

```yaml
auth:
  ci_tokens:
    - token: "${CI_TOKEN}"
      identity: "ci-pipeline"
      grants:
        - repos: ["main"]           # Only main repo
          suites: ["stable"]
          components: ["main"]
          operations: ["upload"]

    - token: "${PARTNER_TOKEN}"
      identity: "partner-ci"
      grants:
        - repos: ["partners"]        # Only partners repo
          suites: ["*"]
          components: ["*"]
          operations: ["upload"]

    - token: "${ADMIN_TOKEN}"
      identity: "admin"
      grants:
        - repos: ["*"]               # All repos
          suites: ["*"]
          components: ["*"]
          operations: ["upload", "remove", "move"]
```

### Protected Suites

Each repo can protect its default suite from being fully drained:

```yaml
repos:
  - id: "main"
    default_suite: "stable"
    protect_default_suite: true    # Cannot remove last package

  - id: "testing"
    default_suite: "testing"
    protect_default_suite: false   # Can fully drain
```

Attempt to drain a protected suite returns **409 Conflict**:

```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/last-pkg/1.0.0/amd64

# Response: 409 Conflict
# "suite 'stable' is protected and cannot be fully emptied; retry with ?force=true"
```

## Deployment Checklist

- ✅ Define repos in `config.yaml` with unique vhost/path pairs
- ✅ Configure per-repo storage if needed (separate buckets)
- ✅ Configure per-repo signing keys if needed
- ✅ Set up CI tokens with per-repo grants
- ✅ Update apt sources to use correct vhost/path
- ✅ Test upload/download for each repo independently
- ✅ Verify ACL isolation (token for repo A cannot access repo B)
- ✅ Monitor metrics per repo (if configured)

## Testing Multi-Repo Setup

### 1. Verify routing

```bash
# Upload to main repo
curl -X POST -H "Authorization: Bearer $MAIN_TOKEN" \
  --data-binary @pkg_1.0.0_amd64.deb \
  https://debs.myorgname.com/api/v1/dists/stable/main/upload

# Upload to testing repo (different path)
curl -X POST -H "Authorization: Bearer $TEST_TOKEN" \
  --data-binary @pkg_2.0.0_amd64.deb \
  https://debs.myorgname.com/testing/api/v1/dists/testing/main/upload

# Verify isolation
curl https://debs.myorgname.com/index.json | jq '.packages | keys'
curl https://debs.myorgname.com/testing/index.json | jq '.packages | keys'
```

### 2. Verify ACL isolation

```bash
# Token for main repo cannot access testing
curl -X POST -H "Authorization: Bearer $MAIN_TOKEN" \
  --data-binary @pkg.deb \
  https://debs.myorgname.com/testing/api/v1/dists/testing/main/upload

# Should return 403 Forbidden
```

### 3. Verify storage isolation

```bash
# Check MinIO buckets/prefixes
mc ls minio/debs-myorg/main/
mc ls minio/debs-myorg/testing/
```

## Backward Compatibility

Single-repo deployments work unchanged:

```yaml
repos:
  - id: "stable"
    # Empty vhost and path_prefix = serves at root
    # All packages at /dists/stable/*
```

- All routes served at same paths as before
- All metadata byte-identical
- Zero operational changes

## Troubleshooting

### "duplicate (vhost, path_prefix)"

Two repos have the same routing pattern. Fix by:
- Assigning different vhosts, or
- Assigning different path_prefixes

```yaml
# ❌ Invalid
repos:
  - id: "main"
    vhost: "debs.example.com"
  - id: "testing"
    vhost: "debs.example.com"    # Same vhost, both have empty path_prefix

# ✅ Fixed
repos:
  - id: "main"
    vhost: "debs.example.com"
  - id: "testing"
    vhost: "debs.example.com"
    path_prefix: "/testing"
```

### "required_repo_id not found"

Configured `required_repo_id` is missing from `repos` list. Add it or remove the `required_repo_id` setting.

### Token cannot access repo

Token's grants don't include the target repo. Check:
- `repos: ["*"]` or `repos: ["target-repo"]` in the grant
- Token identity matches the grant
- Repo ID is spelled correctly

## Performance Considerations

- **One repo:** Minimal overhead, all features available
- **Two repos:** Negligible overhead per repo (separate index, same storage if using prefixes)
- **Many repos (10+):** Consider separate MinIO buckets per repo to avoid single-bucket bottlenecks

## See Also

- **[Installation Guide](installation.md)** — Deploying debian-repo
- **[Configuration Reference](configuration.md)** — All config options
- **[Admin Guide](_index.md)** — Operations and monitoring
