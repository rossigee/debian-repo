# 0002. Identity-provider roles plus existing ACL grants

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

Three authentication mechanisms exist: HTTP Basic for apt clients, OIDC for the web
UI, and bearer tokens for CI. Exactly one authorization system exists, `internal/acl`,
and it covers CI bearer tokens only.

`acl.Grant` already scopes an identity by repo, suite and component, and the per-suite
`unprotect` capability is a good precedent for separating privileges. But the OIDC
session carries only `sub`, `email` and `exp`, and the requested scopes are
`openid profile email` — so any roles the identity provider holds never reach the
application.

The OIDC integration is provider-agnostic already: configuration is `issuer_url`,
`client_id` and `client_secret`, resolved through standard OIDC discovery with
`coreos/go-oidc`. No provider is named in the code, and none should be.

## Decision

Keep `internal/acl` as the single authorization model and extend it. Roles issued by
the identity provider supply a coarse tier (admin, member). The existing grant model
supplies the fine-grained repo/suite/component tier. Both CI bearer tokens and OIDC
sessions resolve to a shared `Principal` in request context.

Two consequences of staying provider-agnostic:

1. Request the `roles` scope and read roles from a **claim whose shape is
   provider-specific**. OIDC standardises the token, not a roles claim layout. Common
   conventions differ — some providers nest roles under a namespaced object such as
   `realm_access.roles`, others use a flat `roles` array, some use group membership.
   The claim path therefore needs to be configurable rather than hardcoded to one
   provider's layout.
2. Do not name a provider in code, config field names, documentation or error
   messages. `config.example.yaml` may show one as a concrete example; that is the
   right place for it.

## Alternatives considered

- **Provider-issued claims only.** Simpler, but loses per-repo scoping and pushes
  repository policy into the identity provider, which degrades as repository count
  grows.
- **A local RBAC store in object storage.** Full control, but adds a user store, a
  sync story and its own session handling for something the identity provider already
  solves.

## Consequences

- The scope list must request `roles`, and `HandleCallback` must extract them into the
  session cookie. The cookie is already encrypted and authenticated, so this is a
  field addition.
- A configurable claim path is a new config surface, and it needs a test that asserts a
  token with no matching claim yields no roles rather than a panic or a silent allow.
- The `X-CI-Identity` header is replaced by context. It worked only because `wrapAuth`
  overwrote it with `Set`; an unprotected route could receive a client-supplied value.
- The ACL short-circuit that skips suite and component checks when both are empty needs
  a test, so the invariant that handlers default both first cannot regress silently.

## Revisit when

Grant semantics need something expressible as deny-lists or precedence, which this
allowlist-of-ANDed-dimensions model cannot represent.
