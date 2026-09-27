# 0005. Releases are the sole owner of version tags

- **Status:** Accepted
- **Date:** 2026-09-27

## Context

`build.yaml` pushed `:latest` and `:${{ env.VERSION }}` on every push to `master`, while
`release.yaml` pushed `:${{ env.VERSION }}` again on tag. Two workflows wrote the same
tags. A master push could republish a version number that had already been released, and
the two tags differed only by a `v` prefix, so a master build could overwrite a release
image with an unrelated build of the same number.

`release.yaml` additionally pushed a `sha-<commit>` tag it never created, so the push
failed and took every later step with it — which is why the v0.5.2 GitHub release was
never created.

## Decision

`release.yaml` is the only writer of version-suffixed tags, always `v`-prefixed and
matching the git tag exactly. `build.yaml` pushes `:latest` and `:sha-<commit>` only.

## Alternatives considered

- **Keep both, coordinate them.** Rejected: the coordination failure mode is silent
  image substitution, and the whole point of the split is that ownership is unambiguous.
- **Let `build.yaml` own everything and drop tag releases.** Rejected: a release needs a
  git tag, a GitHub release, binaries and a provenance-bearing image, which is not a
  master-push concern.

## Consequences

- Image tags are always `v`-prefixed. The bare `:0.5.1` tag published before this change
  is stale and will never be republished.
- `release.yaml` verifies the pushed tag matches the `VERSION` file and fails fast
  otherwise, so a mismatched tag cannot produce a mislabelled image.
- Tag protection should be added to the ruleset so version tags cannot be moved or
  deleted after publication.
- `sha-<commit>` remains a `build.yaml` responsibility, giving every master push a
  traceable image without touching version tags.

## Revisit when

There is a need for release candidates published outside the tag flow.
