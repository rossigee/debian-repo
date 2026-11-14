# Release Notes: v0.5.3

**Release Date**: 2026-11-14
**Git Tag**: `v0.5.3`
**Docker Image**: `ghcr.io/rossigee/debian-repo:v0.5.3`

## Summary

v0.5.3 is a maintenance release. It moves the build to Go 1.27.4, refreshes every
Go dependency, and updates the CI and docs tooling. It makes no intended change to
the service's behaviour or to the `Packages`, `Release` and `InRelease` content
served to apt clients.

## Build and dependencies

- Go toolchain 1.27.1 → 1.27.4 (`go.mod`, Dockerfile, build, lint and release
  workflows)
- All Go dependencies updated with `go get -u ./...` and `go mod tidy`. Notable
  bumps: `minio-go/v7` 7.3.0 → 7.4.1, `prometheus/client_golang` 1.24.1 → 1.25.0,
  OpenTelemetry 1.46.0 → 1.47.0, `grpc` 1.84.0 → 1.85.0, `golang.org/x/crypto`
  0.57.0 → 0.59.0, `golang.org/x/net` 0.59.0 → 0.62.0, `go-jose/v4` 4.1.6 → 4.1.9

## CI

- golangci-lint pinned to v2.15.0 in the lint workflow
- `actions/checkout`, `actions/setup-go` and `actions/upload-artifact` moved to
  current majors (v7)
- The area-labels governance job passes `--repo` to `gh pr edit`. It has no
  checkout, so the previous call failed with `not a git repository`.

## Docs

- Docs site built with Hugo 0.170.0 (was 0.128.0)
- The table render hook was rewritten to build tables from `THead`/`TBody`, because
  Hugo removed the `.Table` field the old hook used

## Deployment

Pin by release tag, not by `latest`:

```yaml
image: ghcr.io/rossigee/debian-repo:v0.5.3
```

No schema, API or configuration change. Existing clients need no action.

## Testing

- `go build ./...`, `go vet ./...`, `gofmt -l .` clean on Go 1.27.4
- `go test -race ./...` green across all packages
- golangci-lint v2.15.0: 0 issues
- actionlint clean on all workflows

Not verified before this release: the Hugo 0.170.0 docs build, the Docker image
build, and the workflows on GitHub Actions runners.
