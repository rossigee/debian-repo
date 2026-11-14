# Release Notes: v0.6.0

**Release Date**: 2026-11-14
**Git Tag**: `v0.6.0`
**Docker Image**: `ghcr.io/rossigee/debian-repo:v0.6.0`

## Summary

v0.6.0 is the Phase 0 correctness release. It fixes the defects that broke apt
or lost data with several repositories, several components, or several suites,
and it makes the configured validation rules take effect. It also reconciles the
documentation with the code.

Several fixes change behaviour that operators rely on. Read **Upgrade notes**
before deploying.

## Upgrade notes

Operator actions required or recommended:

- **Public host.** A repository without a `vhost` needs a public host, set per
  repository with `public_host` or globally with the top-level `public_host`.
  The storage endpoint is never used as the public host any more. Without one,
  repository URLs are the path alone, and the install and GPG key pages show a
  `REPOSITORY_URL` placeholder.
- **Apt-user store location.** Apt-user credentials are read from the global
  `storage.minio` bucket and prefix, not the first repository's. If the first
  repository used a different bucket or prefix, copy the apt-users object
  (`auth.apt_users.store_key`) to the global location before deploying.
- **Validation allowlists are enforced.** `validation.allowed_distributions`,
  `allowed_components` and `allowed_architectures` were declared but ignored. A
  non-empty list is now enforced, and a request outside it gets a 400 naming the
  value. Check any lists you have set. An empty list still means unrestricted.
- **Package size limit.** `validation.max_deb_size_bytes` now replaces the
  hard-coded 512MB limit. The default is unchanged.
- **Index metrics.** The distribution and package gauges now cover every
  repository. They were previously the first repository's only. They are summed
  by default, so metric names and labels are unchanged. Set
  `metrics.per_repo: true` for one series per repository, labelled `repo`. That
  multiplies the series for those two gauges by the number of repositories.
- **Readiness.** `/readyz` reports ready only when every configured repository
  is hydrated.
- **GPG instructions.** The keyring path in the GPG key page is now
  `/usr/share/keyrings/repository.gpg`. Existing installs that already imported
  the key under the old name keep working.
- **Missing repository.** A CI request that reaches a repository-scoped route
  without a repository now gets 404. It previously got 500.

## Fixes

### Multi-component and multi-suite repositories

- **Every component's Packages is served.** `Release` lists a Packages file for
  each component and architecture, but only the first component's file was
  rendered. With two or more components, apt's consistency check failed.
- **Deleting from one suite no longer breaks another.** A `.deb` shared by two
  suites is one pool object. Removal deleted it, leaving the other suite pointing
  at a 404. A pool object is now deleted only when no suite still references it.
- **Pool writes and removals are serialised per repository.** An upload that had
  written an object but not yet registered it could race with a removal that
  deleted it.
- **Each repository uses its own storage connection.** Repositories on a
  different endpoint or credentials were served through the first repository's
  connection.
- **Each distribution carries its repository's identity.** Distributions were
  stamped with a hard-coded origin and label, which were persisted into every
  repository's snapshot. Stale values are corrected when a snapshot is loaded.

### Routing and pages

- **No silent fallback to the first repository.** A repository-scoped request
  without a repository no longer serves repository zero's content.
- **Package detail works behind a path prefix.** `/testing/packages/<name>`
  returned 404.
- **Repository pages stay under their prefix.** Navbar and card links for a
  repository under `/testing` pointed at the default repository.
- **No internal endpoint is published.** A vhost-less repository's URL was built
  from the internal MinIO endpoint, which then appeared on the install page, index
  cards and Atom feed.
- **Install and GPG pages use the repository's own host.** Both had a hard-coded
  hostname.
- **Package descriptions are escaped.** A package's Description, which comes from
  the uploaded `.deb`, was rendered as trusted HTML.

### Validation

- **Allowlists and size limit are enforced.** See Upgrade notes.
- Enforced on upload, presign, register, move and remove, as applicable.

### Operations

- **Index metrics cover every repository.** See Upgrade notes.
- **Apt-user store is explicit.** It reads from the global storage config.

### Documentation

- `AGENTS.md`, `README.md` and `internal/templates/README.md` now match the code.
  Removed: references to a nonexistent package, a `snapshot` repoctl command, a
  `--prune-orphans` flag, and spacing and type scales. The CI upload and remove
  paths are corrected. The repository pages are documented as public.

## Known limitations

- **Apt Basic Auth is not enforced.** Apt-user credentials are loaded, and
  `repoctl users` manages them, but the Basic Auth middleware is not applied to
  `/dists`, `/pool` or `/pubkey.gpg`. Apt metadata and pool files are public.
  This predates v0.6.0 and is unchanged.
- **Pool objects are not pruned.** Orphaned objects from removals are retained.
  No command deletes them yet.
- **Metrics are not labelled by repository.** Request and registration counters
  have no `repo` label. Only the index-state gauges support `metrics.per_repo`.
- **Pull-through cache is not implemented.** See issues #33-#37.

## Build and dependencies

No dependency changes in this release.
