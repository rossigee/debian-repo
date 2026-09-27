# 0001. Per-tenant MinIO endpoint and credentials

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

v1.0 introduces tenants. The service today already models per-repository storage
(`ResolvedRepo` carries `Endpoint`, `Bucket`, `AccessKey`, `SecretKey`, `UseTLS`,
`CACert`, `KeyPrefix`), but `BuildRepo` derives its client from a single process-wide
base client and uses only the bucket and key prefix. Per-repository endpoint and
credentials are resolved from config and then silently discarded.

Three isolation shapes were available: one bucket with a per-tenant key prefix, a
bucket per tenant, or a full MinIO endpoint and credential set per tenant.

## Decision

Per-tenant MinIO endpoint and credentials. `BuildRepo` will construct a client from the
repository's own resolved storage settings, through a factory that caches clients keyed
on `(endpoint, accessKey)` so a tenant does not get its own connection pool.

## Alternatives considered

- **One bucket, per-tenant key prefix.** Cheapest, and the key prefix is already
  plumbed. Rejected because it cannot satisfy data-residency requirements, which are
  the likely driver of a multi-tenant product.
- **Bucket per tenant.** Strong isolation and easy per-tenant backup. Rejected because
  it multiplies buckets without addressing which MinIO deployment they live in, and it
  breaks the single base-client design outright.

## Consequences

- `key_prefix` is no longer the isolation boundary and must not be relied on for it.
  Its current lack of an id-derived default is a bug to fix, not a mechanism to keep.
- Credential configuration becomes an operational concern that grows with tenant count.
  Mitigated by moving tenant storage config into MinIO behind `RepoConfigStore` and
  supporting secret indirection.
- The client factory must be injected rather than global, so it is testable.

## Revisit when

Tenant count makes a single configuration file untenable, or a tenant requires
isolation from a shared MinIO deployment.
