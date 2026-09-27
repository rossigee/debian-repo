# 0003. Per-repo public or private visibility

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

`/dists`, `/pool` and `/pubkey.gpg` are currently served with no authentication at all.
`aptauth.Middleware` is implemented and never called, while the OpenAPI spec and
`docs/content/admin-guide/authentication.md` both declare these routes Basic-Auth
protected. The credential store is loaded from MinIO and reloaded on a ticker, then
never consulted on the request path.

## Decision

Add `visibility: public|private` per repository. Public repositories serve anonymously;
private ones require HTTP Basic Auth, with the credential scoped to the repositories it
may access. `AptUser` gains a `Repos` dimension and `Store.Verify` returns the user
rather than a bare `bool` so handlers can enforce scope.

## Alternatives considered

- **All repositories private.** Uniform, but breaks the common case of a public Debian
  mirror and forces every apt client to hold a credential.
- **All public, no apt auth.** Matches today's behaviour and fixes the documentation
  lie, but makes private repositories impossible, which multi-tenancy requires.

## Consequences

- The spec and the admin guide become accurate rather than aspirational.
- Public repositories remain genuinely public, so an open source deployment does not
  have to issue credentials to its own contributors.
- The timing side channel must be fixed at the same time: the disabled check currently
  runs before bcrypt, which is measurably faster than a wrong password and reintroduces
  the enumeration the dummy hash was added to prevent.
- The read path needs the verified principal in context so `handleDists` and
  `handlePoolProxy` can consult it. Neither currently receives a principal.

## Revisit when

Token-authenticated or anonymous-download use cases appear, which Basic Auth per repo
cannot express.
