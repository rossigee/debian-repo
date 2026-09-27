# Release Notes: v0.5.2

**Release Date**: 2026-09-27
**Git Tag**: `v0.5.2`
**Docker Image**: `ghcr.io/rossigee/debian-repo:v0.5.2`

## Summary

v0.5.2 changes how the service starts and how it names its images. It is a
behavioural release for operators, not a metadata change: the `Packages` and
`Release` content served to apt clients is byte-for-byte equivalent to v0.5.1.

Two independent changes are included.

## Reliability

### Snapshot hydration no longer blocks startup

**Symptom** — if MinIO was slow or briefly unavailable at startup, the process
refused connections for the whole duration. A TCP health check saw a closed port
and reported the service down, and apt clients received a connection reset rather
than a retryable response.

**Cause** — snapshot load and metadata rendering ran synchronously in `main()`
before `ListenAndServe`. The listener was not bound until the whole hydration
step completed.

**Change** — hydration now runs in the background after the listener is up. Until
it finishes, the repository reports itself unhydrated:

- `/healthz` and `/health` return `200` (liveness — the process is running)
- `/readyz` returns `503` with `Retry-After: 5` (readiness — metadata not served yet)
- metadata routes return `503` with `Retry-After: 5` until hydration completes

This gives orchestrators an honest liveness/readiness split and gives apt a
retryable response instead of a connection reset.

**Operational note** — readiness is now meaningful. A deployment that only checks
`/health` will report healthy during hydration; readiness-gated rollout should
check `/readyz`.

**Known limitation** — `repo.BuildRepo` still calls `EnsureBucket` synchronously
before the listener starts. If MinIO is unreachable *and* the bucket is missing,
startup still blocks. The async path only helps once the process is serving.

## Reliability

### Hydration is now covered by tests

Hydration was the whole runtime change and had no tests: it lived in `package
main`, which has no test files, and read its snapshot through a concrete
`*minio.SnapshotStore` that no test could substitute.

It now lives in `internal/repo/hydrate.go` and takes its snapshot source as a
`SnapshotLoader` interface, which `*minio.SnapshotStore` already satisfies.
Production behaviour is unchanged.

The tests caught a nil-interface bug in the refactor: passing a nil
`*minio.SnapshotStore` into `SnapshotLoader` yields a non-nil interface holding a
nil pointer, so the nil check never fired and `GetLatestSnapshot` segfaulted.
This could not occur in production, where `BuildRepo` always sets the field, but
it is a live footgun for any repository built without a store.

## CI

### Releases are the sole owner of version tags

Previously `build.yaml` pushed `:latest` and `:<version>` on every push to
`master`, and `release.yaml` pushed `:<version>` again on tag. Two workflows
wrote the same tags, and a push to `master` could republish a version number that
had already been released.

`build.yaml` no longer writes version tags. It still pushes `:latest` so that
`master` remains observable. `release.yaml` is now the only writer of
version-suffixed tags.

**Migration** — image tags are now always `v`-prefixed and match the git tag
exactly. `ghcr.io/rossigee/debian-repo:v0.5.1`, not `:0.5.1`.

The bare `:0.5.1` tag published by the old workflow still exists in the registry
and will not be republished. New deployments must pin a `v`-prefixed tag.

**Release validation** — the release job now fails fast if the pushed tag does not
match the `VERSION` file, so a mismatched tag cannot produce a mislabelled image.

## Deployment

Pin by release tag, not by `latest`:

```yaml
image: ghcr.io/rossigee/debian-repo:v0.5.2
```

This is a rolling change with no schema or API break. Existing clients need no
action.

## Testing

- `go build ./...`, `go vet ./...`, `gofmt -l .` clean
- `go test -race ./...` green across all 18 packages
- `Hydrate` and `loadSnapshot` 0% → 100%; `HydrateAll` 0% → 92%;
  `MarkHydrated`/`Hydrated` 0% → 100%
- `TestHydrateProducesVerifiableSignatures` verifies `Release.gpg` and the
  `InRelease` clearsign against the repository key and asserts the clearsigned
  payload matches the detached `Release` content
