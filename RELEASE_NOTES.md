# Release Notes: v0.5.1

**Release Date**: 2026-09-27
**Git Tag**: `v0.5.1`
**Docker Image**: `ghcr.io/rossigee/debian-repo:v0.5.1`

## Summary

v0.5.1 fixes a bug that made `apt-get upgrade` reinstall **every package in this repository, at the identical version, on every run, forever**. The packages were reported as being upgraded, never as reinstalled, and apt offered no diagnostic of any kind. All four published packages were affected.

The bug was not in apt. It was in the `Packages` metadata this service rendered.

## Bug Fixes

### apt reinstalled every package on every upgrade

**Symptom**:

```
$ sudo apt-get dist-upgrade
The following packages will be upgraded:
  bucketsyncd fetch-k8s-cert python3-backups vault-tool
4 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.
Need to get 0 B/14.8 MB of archives.
...
Unpacking bucketsyncd (0.4.4-1) over (0.4.4-1)
Setting up bucketsyncd (0.4.4-1)
```

Running it again produced identical output. `Need to get 0 B` was the tell: the archives were already cached, so apt was re-unpacking 14.8 MB of byte-identical content and churning 20.9 MB on every invocation, indefinitely.

**Root cause** — two independent defects in the rendered metadata, each sufficient on its own:

1. **`Installed-Size` was never emitted.** The field whitelist in `RenderPackages` omitted it, so the served stanza carried no `Installed-Size` while dpkg had recorded one in `/var/lib/dpkg/status`.

2. **Multi-line field values were written unindented.** `validate.parseControlFile` strips the fold whitespace from continuation lines, and `RenderPackages` wrote the value back verbatim, so `Description` continuation lines were emitted at column zero. Debian Policy 5.1 requires continuation lines to begin with a space or tab.

**Mechanism** — apt hashes `Installed-Size`, `Depends`, `Pre-Depends`, `Conflicts`, `Breaks` and `Replaces` from both the `Packages` entry and the dpkg status entry (`debListParser::VersionHash`). When the two disagree, `pkgCacheGenerator::MergeListVersion` does not merge them, so the candidate stops being pointer-identical to the current version and `pkgDepCache::MarkInstall` marks it for install. The reinstall is deliberate on apt's part — it lets apt converge when a package has been locally rebuilt or repacked at an unchanged version. What made this pathological is that `VerIterator::CompareVer` infers ordering from position in the version list rather than comparing version strings, so the result was labelled an *upgrade* rather than a reinstall, and the diverging field was never named.

**Evidence** — implementing apt's `VersionHash()` exactly and comparing the served stanzas against the real dpkg status:

| package | served (before) | after | dpkg status |
|---|---|---|---|
| bucketsyncd | 5381 | 112655591 | 112655591 |
| fetch-k8s-cert | 5381 | 266115125 | 266115125 |
| python3-backups | 5381 | 3285827724 | 3285827724 |
| vault-tool | 5381 | 194049126 | 194049126 |

`5381` is apt's seed value, meaning apt found **none** of the six hashed fields in every served stanza. After the fix each hash matches dpkg exactly.

The four packages failed for different reasons, which is worth recording for future regressions:

- `fetch-k8s-cert` — pure `Description` case; its `.deb` carries no `Installed-Size` on either side
- `vault-tool` — pure `Installed-Size` case; single-line `Description` and no `Depends` to swallow
- `bucketsyncd`, `python3-backups` — both defects

**Verification** — `apt-cache policy` shows the merge directly. Before, the identical version was pinned twice; after, it is a single pin with both sources:

```
before:  0.4.4-1 500 → Packages          after:  *** 0.4.4-1 500 → Packages
         *** 0.4.4-1 100 → dpkg status                        100 → dpkg status
```

## Correctness Fixes

### Missing control fields

The same field whitelist dropped `Homepage`, `Multi-Arch`, `Provides` and `Enhances`, all of which belong in a binary `Packages` file. A missing `Provides` is the significant one: a package that provides a virtual package was unresolvable as a dependency from this repository. All are now rendered.

### Continuation-line folding

`RenderPackages` now folds multi-line field values through a single `writeField` helper, which is the exact inverse of the whitespace stripping `parseControlFile` performs. Because the correction is applied at render time rather than parse time, packages **already persisted in MinIO** are fixed on the next re-render without needing re-upload or a reconcile.

## Security

### Test GPG key no longer committed

`test/fixtures/test-key.asc` contained two armored PGP private key blocks and had been in history since the initial commit, on a public repository with GitHub secret scanning and push protection both enabled. The key was throwaway, so nothing needed rotating, but a private key in public git is a standing alert and teaches every secret scanner to be ignored.

Tests now generate a throwaway key per test via `internal/testsupport/gpgtest`. Ed25519 is used rather than the library default of RSA-2048, so generation is effectively free — the package's own tests run in 3 ms. The key is written under `t.TempDir()` with mode `0600`, never touching the working tree, and is removed when the test finishes.

Two loopholes that had allowed the key in were closed at the same time:

- `.gitignore` contained `!test/fixtures/*.asc`, a negation sitting two lines below the `*.asc` rule it defeated. Removed.
- The pre-commit hook's key and credentials checks used `git diff --cached --name-only`, which **includes deletions**, so once a key was committed the hook made it impossible to ever remove. Both checks now use `--diff-filter=ACMR`. Verified in both directions: adding a key is still rejected with exit 1, and removing one is now allowed.

### Latent test hole closed

Five tests previously did:

```go
if _, err := os.Stat(testKeyPath); err != nil {
    t.Skipf("Test GPG key not found at %s", testKeyPath)
}
```

A missing key silently **skipped** every test that signs a Release file. They now fail loudly, and all four end-to-end tests are confirmed running rather than skipping.

## Observability

### CI token identity recorded on request logs

`RequestTracer` logged `remote_addr`, method, path, status and `auth_type`, where `auth_type` is only the first six characters of the `Authorization` header — always the literal string `Bearer` for CI callers. `user` was populated only for BasicAuth. A request made with a CI bearer token was therefore indistinguishable from any other bearer caller.

This was not theoretical: a stuck client polling a reconcile job produced 2,287 identical `WARN` lines that could not be attributed to either configured token. The identity was already resolved and trustworthy — the auth middleware does `r.Header.Set("X-CI-Identity", identity)` from `constantTimeTokenLookup`, overwriting whatever the caller sent — so no trust boundary changed and no new plumbing was needed.

## Internal

### Image publishing was blocked

`build.yaml` ran the Trivy installer with no `continue-on-error` and no timeout, under the runner's `bash -e`. The installer resolves the version and then exits 1, which fails the step, which fails the job before `Login to GitHub Container Registry` and `Push Docker image` run. Every run in this repository's history was red for that reason, including the initial commit, so the workflow had never published an image.

Neither workflow declared a `permissions:` block, so `GITHUB_TOKEN` inherited the repository default, which is read-only — so the push was denied as soon as it was finally reached.

Both fixed:

- `build.yaml`: `continue-on-error`, timeouts, and `contents: read` + `packages: write`
- `release.yaml`: `contents: write` + `packages: write`, as its final step creates a GitHub release through the API

Neither change is sufficient alone. With both, `build` is green and images publish.

Publish steps are now gated on `if: github.event_name != 'pull_request'`, because this workflow also runs for pull requests and an unconditional push let a pull request overwrite `:latest` — the tag the deployed service tracks.

## Testing

```
go build ./...                OK
go vet ./...                  OK
go test -race ./...           all packages pass
golangci-lint run             0 issues
gofmt -l internal/ cmd/ test/ clean
.githooks/pre-commit          all 8 checks pass
```

New tests: `internal/aptmeta` covers the rendered `Packages` against a parser that reproduces apt's cross-line colon search, asserting that every field apt hashes round-trips and that fold lines are indented. `internal/logging` had no tests and now covers CI identity recording. `internal/testsupport/gpgtest` covers key generation, permissions, uniqueness, and a real sign round-trip.
