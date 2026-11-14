# debian-repo — Golang Debian Package Repository Service

A native Go microservice that serves and manages Debian package repositories for `debs.myorgname.com`.

## Overview

- **In-memory index**: Serves package metadata (Packages, Release, InRelease) from pre-rendered artifacts, so apt clients never wait on a registration
- **Atomic registration**: A package is visible only once it is in the index and the metadata is re-rendered
- **MinIO backend**: Pool files, snapshots and apt-user credentials are stored in a MinIO S3 bucket
- **Runtime-configurable**: Pool serving mode (proxy vs. passthrough) and upload mode (direct vs. presigned) are runtime flags
- **Multiple repositories**: One process serves several repositories, each with its own path prefix or vhost, storage and signing key
- **Generalized**: Supports arbitrary distributions, components and architectures, not just `stable`/`main`
- **Native GPG**: In-process OpenPGP signing via `go-crypto`, no external `gpg` binary

## Building

```bash
make build       # Builds debian-repo and repoctl binaries
make test        # Run tests with race detector and coverage
make lint        # Run golangci-lint
make docker      # Build Docker image
```

## Configuration

See `config.example.yaml`. Key settings:

- `pool_serve_mode`: `proxy` (service streams pool files) or `passthrough` (HAProxy routes pool directly to MinIO)
- `upload_mode`: `direct` (CI POSTs to service) or `presigned` (CI gets presigned URLs and uploads directly)
- `signing.passphrase`: GPG key passphrase (from env var `GPG_PASSPHRASE`)
- `public_host`: the public name clients use for repositories without a `vhost`. This is never the storage endpoint. With neither this nor a repository's own `public_host` set, repository URLs are the path alone, and the install and GPG key pages show a `REPOSITORY_URL` placeholder.
- `metrics.per_repo`: `false` by default. Index-state gauges are totals across repositories. Set `true` for one series per repository, labelled `repo`.
- `validation.allowed_distributions`, `allowed_components`, `allowed_architectures`: an empty list places no restriction. A non-empty list is enforced, and a rejected request gets a 400 naming the value.
- `validation.max_deb_size_bytes`: largest accepted package (default 512MB)

Apt-user credentials are stored in the global `storage.minio` bucket, not in any one repository's bucket.

## Authentication

The service has three authentication mechanisms for different consumers, plus a metrics token.

### 1. apt clients (HTTP Basic Auth credentials, not yet enforced)
apt-user credentials are bcrypt-hashed and stored in MinIO, and the service loads them. **The metadata and pool routes do not yet enforce them**: the Basic Auth middleware is not applied to `/dists`, `/pool` or `/pubkey.gpg`, so those routes are public today. Manage credentials with `repoctl users`:

```bash
repoctl users add <username>                  # Create user (prompts for password)
repoctl users add <username> --password <pw>  # Create user with password
repoctl users passwd <username>               # Change password
repoctl users list                            # List users
repoctl users remove <username>               # Delete user
```

Users are reloaded from MinIO every 60 seconds (configurable). The authentication code returns `WWW-Authenticate: Basic realm="debian-repo"` on 401, but it runs only where the middleware is applied, which is the gap noted above.

### 2. Web UI (public pages, optional OIDC login)
The repository pages (`/`, `/install.html`, `/gpg-key.html`, package pages) are public. They are not gated by OIDC.

When OIDC is configured, the user menu offers login through Keycloak, and `/profile` requires a session. Requires:
- `auth.oidc.issuer_url`: Keycloak issuer URL
- `auth.oidc.client_id` / `auth.oidc.client_secret`: Confidential client credentials
- `auth.oidc.redirect_url`: Service's callback URL (e.g., `https://debs.myorgname.com/auth/callback`)
- `auth.oidc.cookie_secret`: 32-byte key for encrypting session cookies
- `auth.oidc.session_ttl`: session lifetime (default 12 hours)

Sessions are stateless signed cookies.

### 3. CI/CD (Bearer Tokens)
CI pipelines authenticate to `/api/v1/*` endpoints with a bearer token in the `Authorization` header. Tokens are compared in constant time. Each token has an identity and grants per repository, suite, component and operation, configured under `auth.ci_tokens`.

### 4. Metrics (Shared-Secret Bearer Token)
The `/metrics` endpoint, served on its own port, requires a bearer token. Configure via `auth.metrics.token`. Token comparison is constant-time.

## Architecture

- `internal/model`: Data structures (Index, Distribution, Package, PackageVersion, Snapshot, rendered artifacts)
- `internal/index`: In-memory index manager with per-distribution locking and atomic publication of rendered artifacts
- `internal/repo`: Per-repository objects (storage, signer, index, routing) and the registry of repositories
- `internal/config`: Configuration loading and defaults
- `internal/storage/minio`: MinIO clients, snapshot and apt-user persistence, presigned uploads
- `internal/gpgsign`: OpenPGP signing and key info derivation
- `internal/aptmeta`: Packages and Release file rendering
- `internal/validate`: `.deb` structural validation
- `internal/templates`: Embedded HTML templates and static assets (see its README)
- `internal/apiserver`: HTTP router and handlers for apt clients, the web UI and CI
- `internal/acl`: Authorization decisions for CI tokens
- `internal/aptauth`: HTTP Basic Auth for apt clients with bcrypt password verification
- `internal/webauth`: OIDC login for the web UI via Keycloak
- `internal/feed`: Atom feeds of recent uploads
- `internal/reconcile`: Rebuilds the index from pool contents
- `internal/metrics`: Prometheus metrics
- `internal/tracing`: OpenTelemetry OTLP tracing
- `internal/logging`: Structured logging with correlation IDs and request tracing
- `internal/version`: Build version information
- `internal/testsupport/gpgtest`: Throwaway PGP keys for tests
- `cmd/debian-repo`: Server entrypoint
- `cmd/repoctl`: Operator CLI (`import`, `reconcile`, `users`)

## API Endpoints

### Metadata (public; Basic Auth not yet enforced)
- `GET /dists/{suite}/Release` — Plaintext Release metadata
- `GET /dists/{suite}/Release.gpg` — Detached signature
- `GET /dists/{suite}/InRelease` — Clearsigned combined file
- `GET /dists/{suite}/{component}/binary-{arch}/Packages` — Uncompressed package index
- `GET /dists/{suite}/{component}/binary-{arch}/Packages.gz` — Gzipped variant
- `GET /pool/{component}/{filename}` — Package file (only in proxy mode)
- `GET /pubkey.gpg` — Armored public key

### Web UI (public)
- `GET /` — Landing page with package index
- `GET /install.html` — Installation instructions with Basic Auth setup
- `GET /gpg-key.html` — GPG key information
- `GET /packages/{name}` — Package detail
- `GET /dists/{suite}/feed.atom` — Atom feed of recent uploads
- `GET /profile` — User profile (requires an OIDC session)
- `GET /login`, `GET /auth/login`, `GET /auth/callback`, `GET /auth/logout` — OIDC login flow (only when OIDC is configured)

### Data APIs (public)
- `GET /index.json` — Package index as JSON (suite, component, latest versions per arch)

### CI/CD (Bearer Token Auth)
- `POST /api/v1/upload?suite={suite}&component={component}` — Direct upload of a `.deb` body (direct mode)
- `POST /api/v1/presign` — Request a presigned upload URL, body `{"suite", "component", "filename"}` (presigned mode)
- `POST /api/v1/register` — Register a presigned upload, body `{"suite", "component", "staging_key"}` (presigned mode)
- `POST|DELETE /api/v1/dists/{suite}/{component}/remove/{package}[/{version}[/{arch}]]` — Remove a package version
- `POST /api/v1/admin/move` — Move packages between suites, body `{"from_suite", "to_suite", "component", "packages", "dry_run", "force"}`
- `POST /api/v1/admin/reconcile` — Start a reconciliation job (202 Accepted, returns a job ID)
- `GET /api/v1/admin/reconcile/{jobID}` — Reconciliation job status
- `GET /api/v1/dists` — Discover distributions, components and architectures

Each route is also served under the repository's path prefix when the repository has one.

### Health & Observability
- `GET /healthz` — Liveness probe (unauthenticated)
- `GET /readyz` — Readiness probe; ready once every repository's snapshot is loaded (unauthenticated)
- `GET /metrics` — Prometheus metrics (separate port, bearer token required)

## Package Management

### Upload (Direct Mode)

Direct upload allows CI pipelines to POST `.deb` files directly to the repository:

```bash
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  --data-binary "@package.deb" \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
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

`suite` and `component` default to the repository's defaults when omitted. The body is limited to `validation.max_deb_size_bytes`.

### Remove Package

Remove a package version from a distribution. Supports granular removal by version and/or architecture:

```bash
# Remove all versions of a package
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/remove/my-package"

# Remove a specific version (all architectures)
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/remove/my-package/1.0.0"

# Remove a specific version and architecture
curl -X POST \
  -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/remove/my-package/1.0.0/amd64"
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
- Validates that the package, version and architecture exist
- Removes the entry from the index and re-renders Packages/Release/InRelease metadata
- Persists the updated snapshot to MinIO
- Cleans up empty components and packages automatically
- Deletes the pool object only when no suite in the repository still references it, since one `.deb` can be shared by several suites

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
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
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
- **Correlation IDs** — Each request gets a correlation ID, taken from the `X-Correlation-ID` header or generated
- **Request Tracing** — HTTP requests are logged with method, path, remote address, auth user, status, latency, and bytes written
- **Skipped Endpoints** — Health checks (`/healthz`, `/readyz`) and the metrics endpoint are not logged, to reduce noise
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

- **No hardcoded `stable`/`main`** — distributions and components are dynamic
- **No metadata drift** — package versions are derived from `.deb` control data, not hand-maintained JSON
- **No hardcoded fingerprint** — GPG key info is rendered dynamically from the live key
- **Registration visibility** — a package is served only after its index entry is registered and the metadata re-rendered. The pool object is written before registration, and the write is serialised with removals in the same repository.
- **Streaming validation** — `.deb` files are validated while streaming, with no temp disk buffering of the entire file
