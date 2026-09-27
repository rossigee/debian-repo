# 0002. Keycloak roles plus existing ACL grants

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

Three authentication mechanisms exist: HTTP Basic for apt clients, OIDC via Keycloak
for the web UI, and bearer tokens for CI. Exactly one authorization system exists,
`internal/acl`, and it covers CI bearer tokens only.

`acl.Grant` already scopes an identity by repo, suite and component, and the per-suite
`unprotect` capability is a good precedent for separating privileges. But the OIDC
session carries only `sub`, `email` and `exp`; the scopes requested do not include
`roles`, so Keycloak realm roles never reach the application.

## Decision

Keep `internal/acl` as the single authorization model and extend it. Keycloak realm
roles supply a coarse tier (admin, member). The existing grant model supplies the
fine-grained repo/suite/component tier. Both CI bearer tokens and OIDC sessions resolve
to a shared `Principal` in request context.

## Alternatives considered

- **Keycloak only.** Simpler, but loses per-repo scoping and pushes repository policy
  into the identity provider, which degrades as repository count grows.
- **A local RBAC store in MinIO.** Full control, but adds a user store, a sync story
  and its own session handling for something the identity provider already solves.

## Consequences

- The OAuth scope list must request `roles`, and `HandleCallback` must extract them into
  the session cookie. The cookie is already encrypted and authenticated.
- The `X-CI-Identity` header is replaced by context. It worked only because `wrapAuth`
  overwrote it with `Set`; an unprotected route could receive a client-supplied value.
- The ACL short-circuit that skips suite and component checks when both are empty needs
  a test, so the invariant that handlers default both first cannot regress silently.

## Revisit when

Grant semantics need something expressible as deny-lists or precedence, which this
allowlist-of-ANDed-dimensions model cannot represent.
