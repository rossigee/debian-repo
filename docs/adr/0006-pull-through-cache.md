# 0006. Pull-through cache for upstream repositories

- **Status:** Accepted
- **Date:** 2026-09-27
- **Milestone:** `1.1.0`

## Context

Teams want Debian packages to be available through the same service that publishes
their own. The feature is colloquially "pull through caching". Three materially
different designs sit behind that phrase:

**A. Opaque byte cache.** Relay `dists/` from the upstream verbatim; fetch `pool/`
objects from the upstream on first request and store them. Never assert anything
about the content.

**B. Eager index sync plus object cache.** Pull the upstream index periodically,
verify it, render and sign our *own* metadata, and merge it with our own packages in
a suite. A curated mirror.

**C. Fully lazy population.** Fetch a `.deb`, extract its control data from the
archive, and register it in our index on the fly.

**C is a trap.** It makes `dists/` change on every fetch, so clients see a repository
whose metadata is unstable — which contradicts the atomicity invariant the whole
design rests on, that a package is either fully registered or not at all.

**B is a mirror.** It makes this project responsible for being a *correct* Debian
mirror: epoch bumps, renames, security point releases, packages disappearing from
`Packages` as upstream removes them. That is an ongoing operational commitment, not a
one-off implementation, and it is declined. There are not the resources to mirror
whole apt repositories.

**A is chosen.** It captures the operational benefit — bandwidth, latency, and
tolerance of a slow or briefly unavailable upstream — for a fraction of the cost.

## The property that makes A safe

apt verifies every `.deb` against the SHA256 recorded in the `Packages` file it
already holds. A caching proxy therefore inherits apt's integrity model: an attacker
who can tamper with a cached object causes an install to **fail**, not to execute
tampered code.

That removes the need for upstream `Release` signature verification, for re-signing,
and for any trust assertion on our part. We relay someone else's bytes and make no
claim about them.

## Decision

An opaque, opportunistic byte cache of upstream `pool/` objects, behind a
repository configured as a mirror.

```yaml
- id: "debian-cache"
  path_prefix: "/debian"          # client requests /debian/pool/main/f/foo.deb
  key_prefix: "mirror/debian"     # storage key  mirror/debian/pool/main/f/foo.deb
  read_only: true
  mirror:
    upstream: "https://deb.debian.org"
    suite: "bookworm"
    metadata_max_age: 6h
    cache_max_bytes: 50GiB
```

Upstream `Packages` is relayed **verbatim**, so the client requests each object at
exactly the path the upstream `Packages` names, and the repository's path prefix and
storage prefix line up with that layout. No path rewriting, no re-signing, no index
mutation.

The cache is **opportunistic**: objects are fetched on demand and may be lost. There
is no scheduled index sync and no guarantee of availability through a sustained
upstream outage.

The upstream public key is held **locally**, configured per mirror, and served at the
mirror's `/pubkey.gpg`. It is never fetched from the upstream at startup: a
reachable-from-us source for a security-critical artifact is the wrong dependency.

## Alternatives considered

- **B, eager index sync and re-signing.** Better client experience, one key, upstream
  content mergeable with our own packages. Rejected: makes this project a mirror, and
  the ongoing ops cost is not available.
- **C, lazy population into our index.** Elegant, because `internal/validate` already
  extracts control data from an archive with no upstream `Packages` parsing needed.
  Rejected: unstable metadata.
- **A transparent HTTP proxy with no storage.** Simplest of all. Rejected: no saving
  across client restarts or repeated installs, and no protection when the upstream is
  merely slow.
- **Hold the upstream key in object storage alongside the cache.** Rejected: the key
  and the content it authenticates would share a failure domain.

## Consequences

- **The cache must not share a pool namespace with a publisher.** Our published
  `foo_1.0.0_amd64.deb` in `main` and an upstream package with the same name, version
  and architecture produce the *identical* key. A mirror gets its own `key_prefix` or
  bucket, so the question does not arise.
- **`read_only` becomes a repository type.** No uploads, and the removal paths must
  not touch cached objects.
- **The signer becomes optional.** A mirror has no signing key. `renderDistributions`
  iterates zero distributions and so never reaches `DetachSign`, but the nil path needs
  an explicit guard rather than relying on that coincidence.
- **A mirror serves the upstream key, never ours.** This is the security-critical
  detail: telling clients to verify Debian packages with our key would be wrong.
  `handlePublicKey` and the gpgkey page currently serve `rp.Signer.KeyInfo()`.
- **Upstream 404s must never be cached.** A stored negative would make a package that
  appears in a later upstream sync permanently missing.
- **Fetch requires per-key single-flight.** Without it, N clients installing a popular
  package simultaneously all miss and all fetch. `internal/reconcile/job.go` is an
  existing precedent; `golang.org/x/sync` is not currently a dependency.
- **Eviction is always safe.** A published object cannot be evicted without 404ing a
  package that `Release` still promises. A cached object can: the upstream is the
  source of truth, so a re-fetch costs bandwidth and nothing else. The cache needs no
  index-reference tracking and can be bounded and aged out aggressively.
- **Metadata is cached with a TTL below the upstream's `Valid-Until`**, or clients miss
  security updates.
- **Not coupled to #25, and not gated on Phase 2.** The pool has no suite-segment
  problem here because a mirror has one suite, no uploads, and no attribution to
  reconstruct. A single mirror is configurable with the `key_prefix` support that
  already exists.
- **Per-tenant mirrors become a Phase 2 extension**, not a prerequisite. Phase 2 is
  still what makes many isolated caches possible.

## Revisit when

- A mirror must survive a sustained upstream outage. That requires scheduled index
  sync, which is the mirror design this decision declines.
- Per-tenant mirror isolation is needed, which means Phase 2 first.
- Clients need to fetch upstream content filtered or pinned — an allowlist, or a
  requirement that the cache serve only specific versions. Neither is possible while
  metadata is relayed verbatim.
