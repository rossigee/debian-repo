# Architecture and Development Guidelines

## System Overview

`debian-repo` is a native Go microservice that replaces the Argo-Workflows-based Debian package repository pipeline. It serves as:

1. **A fast, atomic registry**: Maintains an in-memory index of all packages, versions, and architectures
2. **An HTTP API**: Serves package metadata (Packages/Release/InRelease) to apt clients and accepts package uploads from CI pipelines
3. **A multi-distribution repository**: Supports arbitrary suites/components/architectures, not hardcoded `stable`/`main`

### Key Design Principles

- **Atomicity over eventual consistency**: A package is either fully registered (in index + rendered Packages + signed Release) or not at all
- **No external tools**: No shelling out to `dpkg-deb`, `gpg`, `apt-ftparchive`, `mc`  — everything is implemented natively in Go
- **Runtime configurability**: Pool serving, upload modes, and authentication are runtime flags, not compile-time choices
- **Source-of-truth is pool contents**: The index is rebuilt from `.deb` control metadata in MinIO if the snapshot is missing or corrupted
- **Non-blocking reads**: apt clients never block on CI uploads; GET handlers read an atomic pointer to pre-rendered artifacts
- **Three-tier authentication**: Different consumer classes use different mechanisms (Basic Auth for apt, OIDC for browser login, bearer tokens for CI). Basic Auth is not yet enforced on any route; see Known Limitations.
- **No server-side session state**: Sessions are stateless signed cookies; no Redis or external session store required
- **Constant-time token comparison**: CI bearer tokens compared using `crypto/subtle` to prevent timing attacks

## Package Layout

### `internal/model`
- **types.go**: Core data structures — `Index`, `Distribution`, `Component`, `Package`, `PackageVersion`, `RenderedDist`
- **snapshot.go**: Index serialization to/from JSON `SnapshotV1` for persistence to MinIO
- **aptuser.go**: HTTP Basic Auth credential model — `AptUserStoreV1{FormatVersion, GeneratedAt, Users[]}`
  - Each `AptUser{Username, PasswordHash (bcrypt), CreatedAt, UpdatedAt, Disabled}`
  - Own persisted entity, independent of package index
- **checksumsidecar.go**: Checksum cache model — `ChecksumSidecarV1{FormatVersion, Package, Version, Architecture, ControlFields, MD5, SHA1, SHA256, Size, ETag, WrittenAt}`
  - Pre-computed checksums written at upload time, read during reconciliation
  - ETag-based staleness detection (falls back to full-hash if object modified)

**Key invariant**: Each `(package, version, architecture)` tuple is a distinct `PackageVersion` entry. Adding a new arch for an existing version creates a new entry, fixing the current metadata-drift bug.

### `internal/index`
- **index.go**: `Manager` — the main index API with per-distribution RWMutex locking

**Key concurrency model**:
- `Distribution.Mu` (per-suite lock) guards mutations; taken for registration
- `atomic.Pointer[RenderedDist]` holds the current Packages/Release/InRelease bytes
- GET handlers read the pointer without taking any lock → never blocked by registrations

**Registration flow**:
1. Validate (stream .deb from staging, extract control, compute checksums)
2. Take `Distribution.Mu`, insert `PackageVersion`, re-render Packages/Release/InRelease into `RenderedDist`, swap the atomic pointer, release lock
3. Persist snapshot to MinIO (async, or sync if `persist_mode: sync`)
4. Write checksum sidecar to MinIO (fire-and-forget, cache for next reconciliation)

### `internal/config`
- **config.go**: YAML config parsing + env var substitution + defaults

Supports runtime flags for:
- `pool_serve_mode` (proxy|passthrough)
- `upload_mode` (direct|presigned)
- `auth.ci_tokens` — CI bearer tokens (constant-time comparison)
- `auth.metrics.token` — Metrics endpoint bearer token
- `auth.apt_users` — HTTP Basic Auth store location + reload interval
- `auth.oidc.*` — OIDC/Keycloak configuration (issuer, client ID/secret, redirect URL, cookie secret, session TTL)

### `internal/storage/minio`
- **client.go**: Wraps `minio.Client` (pattern: mirrors `bankrut-internal-dns/pkg/minio`)
- **snapshotstore.go**: Atomic snapshot persistence (write to temp key, `CopyObject` to final key)
- **aptuserstore.go**: Atomic apt-user credential persistence (HTTP Basic Auth credentials, bcrypt-hashed)
- **checksumsidecar.go**: Checksum sidecar cache (fire-and-forget write on upload, read with ETag validation during reconcile)
- **presigned.go**: Presigned upload URLs for presigned mode, and moving a staged object into the pool
- **basepool.go**: `BaseClientPool` — one base client per distinct storage connection, so each repository is scoped from its own endpoint and credentials

### `internal/gpgsign`
- **signer.go**: In-process OpenPGP signing via `go-crypto/openpgp`, no `gpg` binary
- **keyinfo.go**: Derives fingerprint/keyid/UIDs/armored pubkey from the live key

**Fixes current bug**: Dynamic `gpg-key.html` rendering means the key info is always current, never hardcoded.

### `internal/aptmeta`
- **packages.go**: Renders Packages/Packages.gz stanzas from the index (one per arch/component/suite)
- **release.go**: Renders Release file with dynamic dists/components/architectures

### `internal/validate`
- **deb.go**: Structural `.deb` validation — ar archive check, control member extraction, field parsing via `pault.ag/go/debian`
- **ar.go**: Reading the `ar` archive container

### `internal/templates`
- **loader.go**: Parses and caches the embedded templates, and renders each page. Defaults the navbar links when a handler does not set them.
- **types.go**: The page data structures (`PageData` and one struct per page)
- **pages/*.html**: Install, GPG key, card index, package detail and profile pages
- **components/*.html**: Navbar, theme toggle and user menu, included by the pages
- **static/**: CSS, JavaScript and favicon. Embedded with the templates and served by `handleStatic` from `/static/`.

Templates and static files are embedded with `//go:embed`, so the binary has no runtime file dependencies. See `internal/templates/README.md` for the design system and how to add pages.

### `internal/repo`
- **repo.go**: `Repo` — one repository's storage client, signer, index manager, path prefix, vhost and public URL. `Registry` holds all repositories and routes requests to them.
- **hydrate.go**: Loads each repository's snapshot at startup and renders its metadata in the background. `/readyz` and the metadata routes answer 503 until this completes.
- **staticconfig.go**: Repository records read from the configuration file
- **store.go**: The repository record type and the store interface it is read through

### `internal/acl`
- **authorizer.go, types.go**: Authorization decisions for CI bearer tokens: an identity is granted operations (upload, remove, unprotect, reconcile, list-dists) on repositories, suites and components. Removing from a protected suite that would empty it needs the separate `unprotect` grant.
- **config.go, static.go**: Building the grants from `auth.ci_tokens` configuration

### `internal/apiserver`
- **router.go**: HTTP router setup (net/http mux) with all route registration and authentication middleware
  - Bearer token auth for CI routes (`/api/v1/*`) with constant-time comparison
  - Metrics authentication middleware (`MetricsAuthMiddleware`) for the `/metrics` endpoint
  - OIDC session middleware on `/profile` only; the repository pages are public
  - Apt Basic Auth (`aptauth.Middleware`) exists but is not applied to `/dists`, `/pool` or `/pubkey.gpg`
  - Every repository-scoped route is registered once per repository, under its path prefix (`repo.RoutePattern`)
- **apt_handlers.go**: GET routes for apt clients (metadata, pool files in proxy mode) and the HTML pages
- **pagedata.go**: Common page data (authentication state, repository path prefix, repository host)
- **allowlist.go**: Enforcement of the `validation.allowed_*` lists and `max_deb_size_bytes`
- **acl.go**: `denyIfNotAuthorized`, the per-operation check for CI identities
- **feed_handlers.go**: Atom feed of recent uploads per suite
- **ci_handlers.go**: POST/DELETE routes for CI (upload, presign, register, remove, async reconcile)
  - `handleUpload`: Direct package upload (validates, stages, registers in one step)
  - `handlePresign`: Request presigned URL for large package uploads
  - `handleRegister`: Register pre-uploaded package from staging
  - `handleRemovePackage`: Remove package and delete pool file (async)
    - Collects pool paths matching package/version/arch filters before deletion
    - Removes from index and re-renders Release/Packages metadata
    - Deletes .deb file from pool storage asynchronously (non-blocking)
    - Persists updated snapshot to MinIO
  - `handleMovePackages`: Move packages between suites without re-uploading (`POST /api/v1/admin/move`)
  - `handleReconcile`: Async job submission for rebuilding index from pool (returns 202 Accepted with job ID)
  - `handleReconcileStatus`: Job status polling (returns progress and results)
  - `runReconcileJob`: Background reconciliation worker using context.Background()
- **json_handlers.go**: JSON API endpoints (`/index.json`)
- Liveness and readiness probes (`handleHealthz`, `handleReadyz`) are in `router.go`. `/readyz` is ready only when every repository is hydrated.

### `internal/reconcile`
- **reconcile.go**: Index reconciliation from pool contents
  - `Reconcile(ctx, onProgress)` — Scans all `.deb` files in pool, rebuilds index, compares to current for discrepancies
  - Two-pass scan: pre-count total packages, then process with progress callback
  - **Sidecar cache optimization**: Reads checksum sidecar for each file; if ETag matches (object unchanged), uses cached checksums; if ETag differs or sidecar missing, falls back to full-hash and rewrites sidecar
  - Time-throttled progress logging every 5s with elapsed/ETA
  - Cache hits dramatically speed up subsequent reconciliations (seconds vs. 15-20 minutes)
- **job.go**: Background job management
  - `JobManager` — Enforces single-flight semantics (one reconciliation at a time)
  - `JobState` — Tracks job status (running/complete/failed), progress, results
  - Auto-prunes completed/failed jobs (keeps last 20)
  - Thread-safe via mutex; all accesses return copies to avoid races

### `internal/aptauth`
- **store.go**: In-memory HTTP Basic Auth store with sync.RWMutex
  - `Verify(username, password)` — bcrypt password verification
  - `Middleware` — HTTP Basic Auth middleware returning 401 + WWW-Authenticate header
  - `ReloadLoop` — Periodic reload from MinIO (configurable interval, logs errors but never crashes)

### `internal/webauth`
- **provider.go**: OIDC authentication via Keycloak
  - `NewProvider()` — Discovery-based initialization (fail-fast if configured but unreachable)
  - `HandleLogin`, `HandleCallback`, `HandleLogout` — OAuth2 authorization-code flow with PKCE
  - `Middleware()` — Session cookie validation with redirect to login
  - Stateless session cookies via `gorilla/securecookie` (authenticated + encrypted)
  - Session payload: `{Subject, Email, ExpiresAt}`

### `internal/logging`
- **logger.go**: Structured JSON logging via `log/slog`
  - Custom context key type for request-scoped logging
  - `FromContext`/`WithContext` for correlation ID propagation
- **middleware.go**: Request tracing with logging
  - Correlation IDs (from header or auto-generated)
  - Logs method, path, remote address, auth user, status, latency, bytes written
  - Skips logging for health checks and metrics endpoints

### `internal/feed`
- **atom.go**: Atom feed generation for a suite's recent uploads

### `internal/metrics`
- **metrics.go**: Prometheus metrics. Request and registration counters are not labelled by repository. Index-state gauges are summed across repositories by default, or labelled `repo` when `metrics.per_repo` is set.

### `internal/tracing`
- **tracer.go, http_middleware.go, operations.go**: OpenTelemetry OTLP tracing and HTTP middleware

### `internal/version`
- **version.go**: Build version information reported by `--version`

### `internal/testsupport/gpgtest`
- **key.go**: Generates a throwaway PGP key for tests. No key is committed.

### `cmd/debian-repo`
- **main.go**: Server entrypoint with:
  - Config loading and env var substitution
  - MinIO client initialization
  - GPG signer initialization (fail-fast if key unavailable or passphrase wrong)
  - Index reconstruction from snapshot (or reconciliation if snapshot missing/corrupted)
  - OIDC provider initialization (fail-fast if configured and unreachable, warn if absent)
  - Apt-user store loading and reload ticker (periodic refresh from MinIO)
  - Request tracing middleware (structured JSON logging with correlation IDs)
  - HTTP server startup on configured port with graceful shutdown
  - Separate metrics server on configured metrics port (with bearer token auth)
  - Lifecycle management: cancels reload ticker + both servers on interrupt

### `cmd/repoctl`
- **main.go**: Operator CLI with subcommands:
  - `import [--dry-run|--apply]` — Migrate package pool from current system (scan control metadata, diff against metadata, preview/apply snapshot)
  - `reconcile [--suite SUITE] [--dry-run|--apply]` — Rebuild index from pool contents (detects and logs discrepancies)
  - `users add|list|remove|passwd` — Manage HTTP Basic Auth credentials
    - `add USERNAME [--password PWD]` — Create user (interactive or scripted)
    - `passwd USERNAME [--password PWD]` — Change password
    - `list` — Show users + timestamps, never show hashes
    - `remove USERNAME` — Delete user
    - Passwords hashed via bcrypt, persisted to MinIO

Used for migration and operational tasks.

## Before Committing

1. **Build**: `go build ./...` — all packages must compile
2. **Format**: `gofmt -l .` — no formatting issues
3. **Vet**: `go vet ./...` — no suspicious constructs
4. **Tests**: `go test -race ./...` — all tests pass with race detector
5. **Lint**: `golangci-lint run --timeout=5m` — passes configured linters

## Testing Strategy

### Unit tests
- Index mutations under concurrent access (`go test -race`)
- Snapshot serialization/deserialization
- Packages/Release rendering with known-good fixtures from the current system
- GPG sign/verify round-trips
- `.deb` validation (accept/reject cases)

### Integration tests
- Full direct-upload flow: POST → validate → register → immediately fetchable
- Full presigned flow: request presign URL → PUT to MinIO → register → verify
- Concurrency: N concurrent uploads → exactly N stanzas in the served Packages file
- Restart: snapshot reload vs. forced reconcile fallback
- Corruption: delete snapshot → service falls back to reconcile

### Manual verification (pre-cutover)
- Real apt client: `apt update && apt install <known-package>` through the new service
- GPG verification: signature on InRelease verifies with the real public key
- Metadata correctness: Depends/Description fields match the `.deb` control file

## Migration Path (Phase 1)

`repoctl import [--dry-run|--apply]` scans the current MinIO bucket:
1. Lists `pool/**/*.deb` objects
2. Extracts control data from each (via ranged read, no full download)
3. Builds an in-memory `Index` as the source of truth
4. Diffs against existing `packages/*.json` metadata to surface drift
5. Writes a preview snapshot for manual review, then (with `--apply`) promotes it to the live key

This directly fixes:
- The missing `tree` stanza in apt-ftparchive.conf (we build Packages from control data, not relying on the broken conf)
- Metadata drift (we scan pool contents, not hand-maintained JSON)
- Hardcoded fingerprint (we derive key info from the live key)

## Known Limitations (addressed in this rewrite)

1. **apt-ftparchive.conf missing tree stanza** → Fixed: we render Packages directly from index
2. **update-package-json.py skips new-arch-for-existing-version** → Fixed: each (pkg, ver, arch) is distinct
3. **generate-index.py hardcodes fingerprint** → Fixed: key info is dynamic
4. **mc mirror --overwrite --remove is destructive, not atomic** → Fixed: atomic snapshot writes + staged object uploads
5. **Only stable/main supported** → Fixed: distributions/components are dynamic
6. **SSRF risk in webhook** → Fixed: CI uses authenticated API, not arbitrary URLs
7. **Zero consumer authentication** → Partly fixed: bearer tokens guard CI routes and OIDC guards `/profile`. **Open:** apt Basic Auth is loaded (`aptauth`) but not applied to `/dists`, `/pool` or `/pubkey.gpg`, so apt metadata and pool files are public.
8. **No authorization/audit trail** → Fixed: structured logging with correlation IDs, request tracing with user/method/status
9. **Metrics unprotected** → Fixed: metrics endpoint requires bearer token authentication

## Common Pitfalls

- **Do not hold Distribution.Mu longer than necessary** — GET handlers wait for the pointer swap and then release the lock immediately. Long-held locks will cause apt clients to stall during registrations.
- **Do not skip validation when presigned** — CI might upload a truncated or malformed file; re-fetch and validate it before committing.
- **Do not delete orphan pool objects immediately** — orphans are retained. No `repoctl` command prunes them yet, so removing a pool object is an explicit decision, not a side effect of a removal.
- **Pool objects are shared across suites** — pool keys are `pool/<component>/<pkg>_<ver>_<arch>.deb` with no suite segment, so one object can back the same package in several suites. Removal deletes an object only when `Manager.UnreferencedPoolKeys` reports no suite still references it (reference counting derived from the index, no separate counter). Never delete a pool object on the basis of a single suite's removal.
- **Do not hardcode distribution names** — always accept suite/component from the request, subject to optional allowlist.

## CI Integration: Pushing Packages to the Repository

### For Project Maintainers

Any project that builds `.deb` packages can automatically push them to `debs.myorgname.com` by adding an upload step to its Gitea CI workflow (`.gitea/workflows/release.yaml`).

**Prerequisites:**
1. Project builds a `.deb` package during release (via `dpkg-buildpackage` or similar)
2. Gitea CI secret `DEBIAN_REPO_TOKEN` is configured with a bearer token from the debian-repo service

**Example upload step (using reusable GitHub Action):**

```yaml
- name: Push Debian package to repository
  if: success()
  uses: rossigee/debian-repo-upload-action@v1
  with:
    file: your-package_1.0.0_amd64.deb
    base-url: https://debs.myorgname.com
    token: ${{ secrets.DEBIAN_REPO_TOKEN }}
    suite: stable
    component: main
```

See [`rossigee/debian-repo-upload-action`](https://github.com/rossigee/debian-repo-upload-action) for additional options like wildcard file matching, multi-architecture builds, and error handling.

**Example upload step (raw curl, for non-Action CI systems):**

```yaml
- name: Push Debian package to repository
  if: success()
  run: |
    if [ -f your-package_*.deb ]; then
      DEB_FILE=$(ls your-package_*.deb | head -1)
      echo "Uploading $DEB_FILE to debs.myorgname.com..."
      curl -X POST \
        -H "Authorization: Bearer ${{ secrets.DEBIAN_REPO_TOKEN }}" \
        --data-binary "@$DEB_FILE" \
        "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main" \
        -v
    else
      echo "⚠️ No .deb file found, skipping upload to repository"
    fi
```

The API endpoint:
- **URL**: `POST https://debs.myorgname.com/api/v1/upload?suite={suite}&component={component}`
- **Query params**: `suite` and `component` are optional; if omitted, they default to the repository's configured defaults
- **Auth**: Bearer token in `Authorization` header
- **Body**: Raw `.deb` file bytes (max `validation.max_deb_size_bytes`, default 512MB)
- **Response**: JSON with registered package metadata on success

**Example response (HTTP 200):**
```json
{
  "status": "registered",
  "package": "your-package",
  "version": "1.0.0",
  "architecture": "amd64",
  "filename": "pool/main/y/your-package/your-package_1.0.0_amd64.deb",
  "checksums": {
    "md5": "...",
    "sha1": "...",
    "sha256": "...",
    "size": 12345
  }
}
```

**Customization:**
- Replace `stable` with your desired suite (e.g., `testing`, `unstable`)
- Replace `main` with your desired component (e.g., `contrib`, `non-free`)
- Projects can push to multiple suites/components in separate steps

### For debian-repo Maintainers

The `POST /api/v1/upload` endpoint (with optional `?suite=` and `?component=` query parameters):
1. Validates the `.deb` structure natively (ar archive → control.tar extraction → field parsing)
2. Computes SHA256/SHA1/MD5 checksums in a streaming pass (no temp file)
3. Stages the `.deb` to MinIO at `_staging/{uuid}.deb`
4. On validation success, moves it to its final pool path (e.g., `pool/main/y/your-package/your-package_1.0.0_amd64.deb`)
5. Atomically registers into the index under the Distribution lock
6. Re-renders Packages/Release/InRelease and swaps the atomic pointer
7. Persists snapshot to MinIO (sync or async based on config)

The endpoint is tolerant of concurrent uploads to the same suite/component and produces a consistent, signed Release file without any torn reads from apt clients.

**Reusable GitHub Action for CI integration:** The [`rossigee/debian-repo-upload-action`](https://github.com/rossigee/debian-repo-upload-action) GitHub Action encapsulates this upload flow as a reusable composite action. It provides:
- **Simple syntax** — Single `uses:` and `with:` block for .deb uploads
- **Wildcard support** — Upload multiple packages matching a pattern (e.g., `dist/*.deb`)
- **Matrix-friendly** — Works with GitHub Actions matrix builds for multi-architecture packages (amd64, arm64, etc.)
- **Customizable suites/components** — Upload to any distribution/component combination at runtime
- **Error handling** — Validates checksums and verifies package registration before exiting
- **Token-based auth** — Uses bearer token authentication (configure as `DEBIAN_REPO_TOKEN` secret)

See the action's [GitHub repository](https://github.com/rossigee/debian-repo-upload-action) for detailed setup instructions, examples, and advanced use cases.

### Machine-Readable Package Index (JSON API)

The `/index.json` endpoint provides a summary of all available packages in JSON format, useful for CI tools, dashboards, and automation:

**URL**: `GET https://debs.myorgname.com/index.json`
**Auth**: None required (public)
**Response**: JSON with latest version of each package across all distributions

**Example response:**
```json
{
  "packages": {
    "stable": {
      "your-package": {
        "latest_version": "1.2.3",
        "latest_architecture": "amd64",
        "available_architectures": ["amd64", "arm64"],
        "latest_size_bytes": 12345,
        "uploaded_at": "2026-09-24T08:00:00Z",
        "description": "Package description",
        "maintainer": "name@example.com",
        "section": "main"
      }
    }
  },
  "distributions": ["stable"],
  "components": ["main"],
  "generated_at": "2026-09-24T15:00:00Z"
}
```

**Uses:**
- Check available package versions from CI/CD pipelines
- Feed package inventory to dashboards or monitoring systems
- Integration with package management tools
- Automated dependency tracking

The response is cached for 60 seconds (Cache-Control: public, max-age=60).

## Further Reading

- Snapshot format: `internal/model/snapshot.go` + FormatVersion for migrations
- Index lookup/mutation: `internal/index/index.go` + locking model
- GPG key handling: `internal/gpgsign/signer.go` + `keyinfo.go` for dynamic derivation
- Packages rendering: `internal/aptmeta/packages.go` — compare output with current `dists/stable/.../Packages` for correctness
- Authentication: `internal/aptauth/store.go` (HTTP Basic Auth), `internal/webauth/provider.go` (OIDC), `internal/apiserver/router.go` (bearer tokens, constant-time comparison)
- Logging & Observability: `internal/logging/logger.go` + `middleware.go` for structured JSON logging with correlation IDs
- CSS Theming: `internal/templates/static/common.css` for light/dark mode and `internal/templates/README.md` for the design system
- User Management: `cmd/repoctl/main.go` with `users add|list|remove|passwd` subcommands
- CI integration: See `vault-tool` `.gitea/workflows/release.yaml` for an example of pushing packages to the repository
