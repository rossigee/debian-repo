# 0004. Bootstrap for the UI, vendored, Sass at release time

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

The UI is roughly 1,480 lines of hand-written CSS across nine files and 53 lines of
hand-written JavaScript. It has no spacing or type scale — `README.md` claims both
exist and neither does. There is no `:focus` styling anywhere, no `aria-current`, no
Escape handling, and dark mode is a partial re-skin that overrides 6 of 13 colour
tokens. The theme script loads at the end of `<body>`, so dark-mode users get a white
flash on every page load. There is no CSP, `X-Frame-Options` or `nosniff` anywhere, and
`internal/templates` has no tests at all.

v1.0 needs an admin UI with forms, tenant branding, and an accessibility baseline.

## Decision

Bootstrap 5.3.8, vendored into `internal/templates/static/`, with a Sass build producing
the committed stylesheet. Delete the hand-written CSS and JS.

## Alternatives considered

- **Hand-rolled, extended.** Avoids a dependency but means writing forms, tables,
  navigation, dropdowns and their accessibility from scratch. Bootstrap's dropdown and
  collapse JavaScript alone supply `aria-expanded`, `aria-controls`, Escape handling and
  focus rings that are currently absent entirely.
- **A CDN.** Always current and no vendor commits, but breaks air-gapped installs and
  forces a permissive CSP with third-party origins.
- **Vendored prebuilt CSS with an override layer, no Sass.** Simplest build, but
  per-tenant branding becomes fighting the cascade instead of overriding variables.

## Consequences

- Bootstrap 5.3 uses the Sass legacy JS API, which Dart Sass removes in 2.0. The
  compiler must be pinned below 2.0.0, its checksum verified, and the four deprecations
  Bootstrap's own documentation says to silence. `sass-embedded` has a documented compile
  bug against Bootstrap 5.3.3, so the plain CLI is used.
- The first build step in a repository that currently has none, so CI must check the
  committed CSS against a fresh build or the artifact will rot.
- `data-theme` migrates to `data-bs-theme`.
- MIT licence, so the header must be preserved in the vendored source.
- Blocking: 5.3's color modes are what make per-tenant branding feasible, which is
  impossible today with hardcoded hex values.

## Revisit when

A tenant needs control beyond colour variables, or the accessibility burden of a
framework outweighs its benefit.
