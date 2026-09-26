---
title: Configuration Reference
description: Complete config.yaml options reference
---


Complete reference for all `config.yaml` configuration options in debian-repo.

## Quick Start

Minimal working configuration:

```yaml
listen:
  http: ":8080"
  metrics: ":9090"

storage:
  minio:
    endpoint: "minio.internal:9000"
    bucket: "debian-packages"
    access_key: "minioadmin"
    secret_key: "minioadmin"
    use_tls: true

auth:
  metrics:
    token: "your-metrics-token"

signing:
  key_path: "/etc/debian-repo/signing-key.asc"
  passphrase: "key-passphrase"

repos:
  - id: "default"
    vhost: "debs.myorgname.com"
```

## Server Configuration

### listen

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `http` | string | `:8080` | HTTP server listen address (port or :port) |
| `metrics` | string | `:9090` | Metrics server port (deprecated; use http) |

```yaml
listen:
  http: ":8080"
  metrics: ":9090"    # Metrics endpoint runs on same HTTP listener
```

## Storage Configuration

### storage.minio

MinIO S3 bucket for storing packages and metadata.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `endpoint` | string | required | MinIO server address (host:port) |
| `bucket` | string | required | Bucket name |
| `access_key` | string | required | MinIO access key |
| `secret_key` | string | required | MinIO secret key |
| `use_tls` | bool | true | Use TLS for MinIO connection |
| `ca_cert` | string | (none) | Custom CA cert for MinIO (PEM format) |
| `key_prefix` | string | (none) | Prefix for all objects (e.g., "myrepo/") |

### storage (Snapshot Settings)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `snapshot_key` | string | `_meta/index-snapshot.json.gz` | MinIO key for storing index snapshots |
| `snapshot_history_keep` | int | 20 | Number of old snapshots to retain |
| `staging_prefix` | string | `_staging/` | Prefix for temporary upload staging |

### index (Persistence)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `persist_mode` | string | `"sync"` | Snapshot strategy: `"sync"` (immediate) or `"debounced"` (batched) |
| `persist_debounce` | duration | 30s | Wait time before batching snapshots (only for debounced mode) |

```yaml
storage:
  minio:
    endpoint: "minio.internal:9000"
    bucket: "debian-packages"
    access_key: "minioadmin"
    secret_key: "minioadmin"
    use_tls: true
  snapshot_key: "_meta/index-snapshot.json.gz"
  snapshot_history_keep: 20
  staging_prefix: "_staging/"

index:
  persist_mode: "sync"
  persist_debounce: 30s
```

## Package Upload Configuration

### pool_serve_mode

How to serve `.deb` files to apt clients.

| Value | Description |
|-------|-------------|
| `"proxy"` | Stream through debian-repo (default) |
| `"passthrough"` | Redirect to MinIO with optional HAProxy routing |

### upload_mode

How CI/CD uploads are handled.

| Value | Description |
|-------|-------------|
| `"direct"` | Stream through debian-repo (default) |
| `"presigned"` | Return MinIO presigned URLs for direct client upload |

### presign_expiry

Time limit for presigned URLs (only used if `upload_mode: "presigned"`).

```yaml
pool_serve_mode: "proxy"          # proxy | passthrough
upload_mode: "direct"             # direct | presigned
presign_expiry: 15m               # Duration for presigned URL validity
```

## GPG Signing Configuration

### signing

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `key_path` | string | required | Path to armored GPG private key |
| `passphrase` | string | required | Passphrase to decrypt the key |
| `keep_decrypted` | bool | true | Cache decrypted key in memory (buggy default; all values become true) |

```yaml
signing:
  key_path: "/etc/debian-repo/signing-key.asc"
  passphrase: "key-passphrase"
```

## Validation Configuration

### validation

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `max_deb_size_bytes` | int64 | 536870912 (512MB) | Maximum allowed .deb file size |
| `allowed_architectures` | []string | (empty = all) | Restrict to these architectures (e.g., amd64, arm64) |
| `allowed_distributions` | []string | (empty = all) | Restrict to these distributions |
| `allowed_components` | []string | (empty = all) | Restrict to these components |

```yaml
validation:
  max_deb_size_bytes: 536870912    # 512 MB
  allowed_architectures: []        # Empty = all allowed
  allowed_components: ["main", "contrib"]
```

## Authentication Configuration

### auth.ci_tokens

CI/CD bearer tokens for uploading and managing packages.

```yaml
auth:
  ci_tokens:
    - token: "token-value"
      identity: "github-ci"
      grants:
        - repos: ["*"]              # Wildcard or specific repo IDs
          suites: ["testing"]       # Which suites
          components: ["main"]      # Empty = all components
          operations:
            - upload
            - remove
```

### auth.apt_users

HTTP Basic Auth store for apt clients.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `store_key` | string | `_meta/apt-users.json.gz` | MinIO key for user store |
| `reload_interval` | duration | 60s | How often to reload credentials |

### auth.metrics

Bearer token for Prometheus metrics endpoint.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `token` | string | required | Token for `/metrics` endpoint auth |

### auth.oidc (Web UI)

OIDC configuration for web dashboard.

```yaml
auth:
  metrics:
    token: "metrics-token"
  apt_users:
    store_key: "_meta/apt-users.json.gz"
    reload_interval: 60s
  oidc:
    issuer_url: "https://login.example.com/oidc"
    client_id: "client-id"
    client_secret: "client-secret"
    redirect_url: "https://debs.myorgname.com/auth/callback"
    cookie_secret: "base64-encoded-secret"
    session_ttl: 12h
```

## Repository Configuration

Each entry in `repos` defines an independent repository with its own signing key, storage settings, and defaults.

### repos[] (Per-Repository)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `id` | string | required | Unique repo identifier (used in CLI, URLs, etc.) |
| `vhost` | string | (optional) | Virtual host (subdomain-based multi-tenancy) |
| `path_prefix` | string | (optional) | URL path prefix (path-based multi-tenancy) |
| `default_suite` | string | `"stable"` | Suite used if not specified in upload |
| `default_component` | string | `"main"` | Component used if not specified in upload |
| `protect_default_suite` | bool | true | Prevent removing all packages from default_suite |
| `feed.enabled` | bool | true | Enable Atom feed endpoint for this repo |
| `feed.max_items` | int | 20 | Max items in Atom feed |

Storage, signing, and auth can be overridden per-repo; see repo-level storage fields.

```yaml
repos:
  - id: "default"
    vhost: "debs.myorgname.com"
    default_suite: "stable"
    default_component: "main"
    protect_default_suite: true
    feed:
      enabled: true
      max_items: 20
```

## Repository Metadata

### repo_metadata

Package metadata that appears in Release files and client queries.

| Field | Type | Default |
|-------|------|---------|
| `origin` | string | `"myorgname.com"` |
| `label` | string | `"golder.tech Debian Repository"` |
| `description` | string | `"Debian package repository for golder.tech infrastructure"` |

```yaml
repo_metadata:
  origin: "myorgname.com"
  label: "My Organization Repository"
  description: "Official Debian packages for My Organization"
```

## Tracing Configuration

### tracing

OpenTelemetry OTLP tracing for observability.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | bool | false | Enable tracing |
| `otlp_host` | string | `"localhost"` | OTLP collector host |
| `otlp_port` | int | 4317 | OTLP collector port |
| `service_name` | string | `"debian-repo"` | Service name in traces |
| `sampler` | string | `"probabilistic"` | always / never / probabilistic |
| `sample_rate` | float | 0.1 | Sampling rate (0.0-1.0) for probabilistic sampler |

```yaml
tracing:
  enabled: true
  otlp_host: "localhost"
  otlp_port: 4317
  service_name: "debian-repo"
  sampler: "probabilistic"
  sample_rate: 0.1
```

## Logging Configuration

### log_level

| Value | Description |
|-------|-------------|
| `"debug"` | All messages including request/response details |
| `"info"` | Normal operation (default) |
| `"warn"` | Warnings and errors only |
| `"error"` | Errors only |

```yaml
log_level: "info"
```

## See Also

- **[Authentication](authentication.md)** — Detailed auth configuration
- **[Multi-Repo Setup](multi-repo.md)** — Multiple repository configuration examples
- **[Admin Guide](_index.md)** — Overview of all admin topics
