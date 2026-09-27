# Roadmap to v1.0

The target is a multi-tenant, multi-repo, multi-suite Debian repository service with a
real admin surface: a proper admin API, per-tenant user management, and a UI built on
Bootstrap.

**Status:** planning. Phases 0 and 1 are the current work.

Live tracking lives in the [v1.0 Project](https://github.com/users/rossigee/projects).
Phase narrative and decisions live in the epic issues linked below. Durable design
decisions are recorded as ADRs in [`docs/adr/`](docs/adr/).

---

## What already exists

More of this is in place than the version number suggests, and the plan deliberately
does not rebuild it:

- **Multi-repo is structurally real.** `repo.Repo` is an aggregate root with its own
  index, storage client, snapshot store, signer and job manager. Vhost and
  path-prefix routing works.
- **Multi-suite per repo works.** A repository holds several suites simultaneously.
- **The ACL already scopes by repo, suite and component.** v1.0 extends it rather than
  replacing it.
- **`RepoConfigStore` and `SuiteRecord` are already the intended seams** for runtime
  repo and suite configuration. They are declared but unimplemented.
- GPG signing, atomic snapshots, reconciliation, checksum sidecars and the CI upload
  flows are production-ready.

## Decisions taken

| Area | Decision |
|---|---|
| Tenant storage | Per-tenant MinIO endpoint and credentials (bring your own) |
| Authorization | Keycloak realm roles for the coarse tier, existing `acl.Grant` for repo/suite/component |
| Repo visibility | Per-repo `public` / `private` |
| UI framework | Bootstrap 5.3.8, vendored, compiled from Sass at release time |

## Phase 0 — correctness blockers · milestone `0.6.0`

Latent bugs that multi-tenancy *exposes* rather than causes. Each is independently
valuable and small. Ships without changing served metadata.

The two that must not be deferred:

- **Only the first component's `Packages` is rendered**, while `RenderRelease`
  advertises checksums for every component. With two or more components in a suite,
  apt's Release/Packages consistency check fails and the repository breaks.
- **Pool object keys contain no suite segment.** A `.deb` in two suites is one shared
  object, so removing it from one suite deletes the object the other suite still
  lists. That is data loss.

Also in this phase: `/readyz` reporting only the first repository, the
`currentRepo` wrong-repository fallback, prefix routing broken on package detail
pages, hardcoded origin metadata in the persisted snapshot, the MinIO endpoint leaking
into install pages, three `repos[0]` hardcodings, unenforced validation allowlists,
and template link paths that ignore the repository prefix.

## Phase 1 — identity and authorization · milestone `0.7.0`

One authorization model, two identity sources.

- A `Principal` in request context, replacing the `X-CI-Identity` header
- Keycloak realm roles carried into the session, which requires requesting the `roles`
  scope — today only `openid profile email` is requested, so roles never arrive
- A role dimension added to `acl.Request`
- Per-repo `public` / `private`, wiring `aptauth.Middleware`, which is implemented but
  **never called** — the apt routes are currently unauthenticated while the OpenAPI
  spec and admin guide both declare them protected
- An `AptUser` gains a repo dimension
- Auth timing fixes: the disabled-user check short-circuits before bcrypt, defeating
  the anti-enumeration work already done

## Phase 2 — tenancy and storage · milestone `0.8.0`

- Per-repository MinIO clients. Per-repo endpoint and credentials are already resolved
  by config and then **silently discarded**.
- A tenant entity. No tenancy concept exists anywhere today.
- `key_prefix` is not derived from the repository id, so two prefix-routed
  repositories can silently share a `pool/` namespace.
- A writable `RepoConfigStore` with a MinIO-backed implementation
- `SuiteRecord` wired at runtime, enabling per-suite component allowlists and per-suite
  protection instead of one protected suite per repo
- Enforce the `validation.*` allowlists that are declared but never read
- `repoctl` gains `--repo`; it currently reads the global storage block while the
  server reads the first repository's client, so with any override they read and write
  different objects

## Phase 3 — admin API and audit · milestone `0.9.0`

Repository, suite, user and grant CRUD under `/api/v1`, with `GET /api/v1/me` returning
the caller's effective permissions.

There is currently **no audit trail**. `UploadedBy` exists for uploads only;
removals, moves, reconciles and configuration changes record nothing, and allow
decisions are not logged. Proposing an `AuditStore` port with a MinIO implementation
emitting structured events per mutation.

Metrics gain a `repo` label — 18 metric families currently have none, and two gauges
reflect only the first repository.

The OpenAPI spec gains an `openIdConnect` scheme, a global `security:` default so new
paths cannot silently ship public, and roughly 14 missing endpoints.

## Phase 4 — UI/UX on Bootstrap · milestone `1.0.0-rc.1`

Delete roughly 1,480 lines of hand-written CSS and 53 lines of hand-written JS in
favour of Bootstrap 5.3.8.

Bootstrap's `data-bs-theme` replaces the custom `data-theme` and its official toggler
fixes a white flash on every page load in dark mode. Its dropdown and collapse
JavaScript supply the `aria-expanded`, `aria-controls`, Escape handling and focus rings
that are entirely absent today. Its CSS variables are what make per-tenant branding
possible — impossible today with hardcoded hex values.

Compiled from Sass at release time with a pinned dart-sass CLI, since Bootstrap 5.3
uses the legacy Sass JS API that Dart Sass 2.0 will remove.

Prerequisites from Phase 0: a shared layout, `BasePath` threaded into page data, and
neutralising a `template.HTML` conversion of package-controlled data that is currently
only inert because its one referencing template is dead.

No CSP, `X-Frame-Options` or `nosniff` is set anywhere today, and no HTML form has ever
existed. Both need addressing before an admin UI with forms.

## Phase 5 — release engineering · milestone `1.0.0`

Snapshot v2 with a `RepoID`, with dual-read migration from v1. Documentation
reconciliation — OIDC is undocumented, no guide covers the web UI, and two substantial
pages are still marked "(TBD)". OpenAPI 1.0.0.

---

## Contribution model

- `master` is protected by a ruleset requiring a pull request, one approving review,
  thread resolution, and passing `build` and `lint` checks.
- Every pull request must reference an issue with `Refs: #NNN`; CI enforces it.
- Milestones are the six releases above. Phases are tracked in the Project, not by
  duplicate labels.
- Dependencies between issues are recorded as Projects relations, not prose.

## Definition of ready

An issue is ready to start when it has testable acceptance criteria, an explicit
**Out of scope**, a size, and either a design note or a link to a discussion. The issue
template enforces this.

## Risk register

| Risk | Impact | Mitigation |
|---|---|---|
| Per-tenant credentials in one `config.yaml` does not scale | Operational | Move tenant storage config into MinIO behind `RepoConfigStore`; support secret indirection |
| Multi-component rendering break (Phase 0 #6) | apt clients fail outright | Fix before 0.6.0 ships, not after |
| Cross-suite pool deletion (Phase 0 #7) | Data loss | Fix before 0.6.0 ships, as its own change with a clean revert path |
| Phase 1 auth rewrite | Lockout or bypass | Extend the existing `acl.Grant` model rather than replacing it; test the suite/component short-circuit |
| Bootstrap Sass build | Dart Sass 2.0 breaks the build | Pin the CLI below 2.0.0, verify its checksum, and CI-check the committed CSS against a fresh build |
| Repo is public | Anything committed is public | Secret scanning push protection, no keys on runners, private vulnerability reporting |

## Decision records

Design decisions with lasting consequences are recorded as ADRs in `docs/adr/`, using
the Lightweight format. The tenancy model, the authorization model, the audit design
and the Bootstrap/Sass build step all qualify.
