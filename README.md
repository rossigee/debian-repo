# debian-repo — Golang Debian Package Repository Service

A native Go microservice replacing the Argo-Workflows-based Debian package repository pipeline for `debs.myorgname.com`.

## Overview

- **In-memory index**: Fast serving of package metadata (Packages, Release, InRelease) from an in-memory cache
- **Atomic registration**: CI pipelines upload packages and register them atomically — no race conditions
- **MinIO backend**: Pool files and dists metadata stored in MinIO S3 bucket
- **Runtime-configurable**: Pool serving mode (proxy vs. passthrough) and upload mode (direct vs. presigned) are runtime flags
- **Generalized**: Supports arbitrary distributions/components/architectures, not just hardcoded `stable`/`main`
- **Native GPG**: In-process OpenPGP signing via `go-crypto`, no external `gpg` binary

## Building

```bash
make build       # Builds debian-repo and repoctl binaries
make test        # Run tests with race detector and coverage
make lint        # Run golangci-lint
make docker      # Build Docker image
```

## Configuration

See `config.example.yaml`. Key runtime flags:
- `pool_serve_mode`: `proxy` (service streams pool files) or `passthrough` (HAProxy routes pool directly to MinIO)
- `upload_mode`: `direct` (CI POSTs to service) or `presigned` (CI gets presigned URLs and uploads directly)
- `signing.passphrase`: GPG key passphrase (from env var `GPG_PASSPHRASE`)

## Authentication

The service implements three-tier authentication for different consumer classes:

### 1. apt clients (HTTP Basic Auth)
Machines authenticate via `/etc/apt/auth.conf.d/` credentials. Passwords are bcrypt-hashed and stored in MinIO. Manage with `repoctl users`:

```bash
repoctl users add <username>              # Create user (prompts for password)
repoctl users add <username> --password <pw>  # Create user with password
repoctl users passwd <username>           # Change password
repoctl users list                        # List users
repoctl users remove <username>           # Delete user
```

Users are reloaded from MinIO every 60 seconds (configurable). The service returns `WWW-Authenticate: Basic realm="debian-repo"` on 401 to trigger credential prompts.

### 2. Web UI (OIDC via Keycloak)
Humans browsing `/`, `/install.html`, `/gpg-key.html` authenticate via Keycloak. Sessions are stateless signed cookies with 12-hour TTL (configurable). Requires:
- `auth.oidc.issuer_url`: Keycloak issuer URL
- `auth.oidc.client_id` / `auth.oidc.client_secret`: Confidential client credentials
- `auth.oidc.redirect_url`: Service's callback URL (e.g., `https://debs.myorgname.com/auth/callback`)
- `auth.oidc.cookie_secret`: 32-byte key for encrypting session cookies

If OIDC is not configured, HTML pages serve unauthenticated (development mode).

### 3. CI/CD (Bearer Tokens)
CI pipelines authenticate to `/api/v1/*` endpoints via bearer tokens in the `Authorization` header. Tokens are compared using constant-time comparison to prevent timing attacks. Configure via `auth.ci_tokens`.

### 4. Metrics (Shared-Secret Bearer Token)
The `/metrics` endpoint (served on a separate port) requires bearer token authentication. Configure via `auth.metrics.token`. Token comparison is constant-time.

## Architecture

- `internal/model`: Data structures (Index, Distribution, Package, PackageVersion, Snapshot)
- `internal/index`: In-memory index manager with concurrency control
- `internal/config`: Configuration loading and defaults
- `internal/storage/minio`: MinIO client and snapshot persistence
- `internal/gpgsign`: OpenPGP signing and key info derivation
- `internal/aptmeta`: Packages/Release file rendering
- `internal/validate`: `.deb` structural validation
- `internal/htmlpages`: HTML template rendering (index.html, install.html, gpg-key.html, CSS theming)
- `internal/apiserver`: HTTP router and handlers
- `internal/aptauth`: HTTP Basic Auth for apt clients with bcrypt password verification
- `internal/webauth`: OIDC authentication for web UI via Keycloak
- `internal/logging`: Structured logging with correlation IDs and request tracing
- `cmd/debian-repo`: Server entrypoint
- `cmd/repoctl`: Operator CLI (import, reconcile, snapshot inspection, user management)

## API Endpoints

### Metadata (HTTP Basic Auth)
- `GET /dists/{suite}/Release` — Plaintext Release metadata
- `GET /dists/{suite}/Release.gpg` — Detached signature
- `GET /dists/{suite}/InRelease` — Clearsigned combined file
- `GET /dists/{suite}/{component}/binary-{arch}/Packages` — Uncompressed package index
- `GET /dists/{suite}/{component}/binary-{arch}/Packages.gz` — Gzipped variant
- `GET /pool/{component}/{filename}` — Package file (only in proxy mode)
- `GET /pubkey.gpg` — Armored public key

### HTML Pages (OIDC, unauthenticated if OIDC not configured)
- `GET /` — Landing page with package index
- `GET /install.html` — Installation instructions with Basic Auth setup
- `GET /gpg-key.html` — GPG key information
- `GET /login` — OIDC login initiation (redirects to `/auth/login`)
- `GET /auth/login` — OIDC authorization endpoint (Keycloak redirect)
- `GET /auth/callback` — OIDC callback (session cookie creation)
- `GET /auth/logout` — Logout (clears session cookie)

### Data APIs (Unauthenticated)
- `GET /index.json` — Package index as JSON (suite, component, latest versions per arch)

### CI/CD (Bearer Token Auth)
- `POST /api/v1/dists/{suite}/{component}/upload` — Direct upload (direct mode)
- `POST /api/v1/dists/{suite}/{component}/presign` — Request presigned URL (presigned mode)
- `POST /api/v1/dists/{suite}/{component}/register` — Register a presigned upload (presigned mode)
- `POST /api/v1/admin/reconcile` — Trigger reconciliation
- `POST /api/v1/dists/{suite}/remove/{package}[/{version}/{arch}]` — Remove package version
- `GET /api/v1/dists` — Discover distributions/components/architectures

### Health & Observability (Unauthenticated)
- `GET /healthz` — Liveness probe
- `GET /readyz` — Readiness probe
- `GET /metrics` — Prometheus metrics (served on separate port, bearer token required)

## Package Management

### Upload (Direct Mode)

Direct upload allows CI pipelines to POST `.deb` files directly to the repository:

```bash
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  --data-binary "@package.deb" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/upload"
```

Response on success (200):
```json
{
  "status": "registered",
  "package": "my-package",
  "version": "1.0.0",
  "architecture": "amd64",
  "filename": "pool/main/my-package_1.0.0_amd64.deb",
  "checksums": {
    "md5": "...",
    "sha1": "...",
    "sha256": "...",
    "size": 12345
  }
}
```

### Remove Package

Remove a package version from a distribution. Supports granular removal by version and/or architecture:

```bash
# Remove all versions of a package
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/remove/my-package"

# Remove a specific version (all architectures)
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/remove/my-package/1.0.0"

# Remove a specific version and architecture
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/remove/my-package/1.0.0/amd64"
```

Response on success (200):
```json
{
  "status": "removed",
  "package": "my-package",
  "suite": "stable"
}
```

The remove endpoint:
- Validates that the package/version/architecture exists
- Atomically removes from the index
- Re-renders Packages/Release/InRelease metadata
- Persists the updated snapshot to MinIO
- Cleans up empty components/packages automatically

## CI/CD Integration

### Automated Uploads with GitHub Actions

The [`rossigee/debian-repo-upload-action`](https://github.com/rossigee/debian-repo-upload-action) GitHub Action provides a simple way to automatically push `.deb` packages to your repository from CI/CD workflows:

```yaml
- name: Push Debian package to repository
  uses: rossigee/debian-repo-upload-action@v1
  with:
    file: dist/my-package_1.0.0_amd64.deb
    base-url: https://debs.myorgname.com
    token: ${{ secrets.DEBIAN_REPO_TOKEN }}
    suite: stable
    component: main
```

**Key features:**
- Wildcard file matching — upload multiple packages at once (`dist/*.deb`)
- Matrix-friendly — works with GitHub Actions matrix builds for multiple architectures
- Automatic verification — validates checksums and confirms package registration
- Bearer token authentication — uses secure token-based auth configured as a secret

### Manual Uploads with curl

For non-GitHub CI systems (GitLab CI, Jenkins, Gitea, etc.), use `curl`:

```bash
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  --data-binary "@my-package_1.0.0_amd64.deb" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/upload"
```

## Development

### Pre-commit checklist
```bash
go build ./...
gofmt -l .
go vet ./...
go test -race ./...
golangci-lint run --timeout=5m
```

### Logging & Observability
The service uses structured JSON logging via `log/slog` with the following features:
- **Correlation IDs** — Each request gets a unique correlation ID (from `X-Correlation-ID` header or auto-generated)
- **Request Tracing** — All HTTP requests logged with method, path, remote address, auth user, status, latency, and bytes written
- **Skipped Endpoints** — Health checks (`/healthz`, `/readyz`) and metrics endpoint are not logged to reduce noise
- **Log Levels** — Configured via `log_level` config (debug, info, warn, error)

Log output includes:
```json
{
  "time": "2026-09-24T...",
  "level": "INFO",
  "msg": "request completed",
  "correlation_id": "req-1234567890",
  "method": "GET",
  "path": "/dists/stable/Release",
  "status": 200,
  "duration_ms": 45,
  "bytes_written": 1234,
  "user": "jenkins"
}
```

See `AGENTS.md` for detailed architecture notes.

## Migration from Argo Workflows

The service replaces the Argo-Events-triggered `scripts/process-package.sh` pipeline with a direct HTTP API. Key differences:

- **No hardcoded `stable`/`main`** — distributions/components are dynamic
- **No metadata drift** — package versions are derived from `.deb` control data, not hand-maintained JSON
- **No hardcoded fingerprint** — GPG key info is rendered dynamically from the live key
- **Atomic registration** — uploads and index updates happen in a single lock-held transaction
- **Streaming validation** — `.deb` files validated while streaming (no temp disk buffering of entire file)

See `/home/rossg/infrastructure/debian-repo/scripts/` for reference on current implementation.
