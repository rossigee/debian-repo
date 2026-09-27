# Architecture decision records

Design decisions with consequences that outlive the change that made them.

Format is [Lightweight ADRs](https://adr.github.io/): a numbered file per decision,
immutable once accepted. Supersede rather than edit.

| # | Decision | Status | Date |
|---|---|---|---|
| 0001 | Multi-tenant storage isolation: per-tenant MinIO endpoint and credentials | Accepted | 2026-09-27 |
| 0002 | Authorization: Keycloak realm roles plus existing ACL grants | Accepted | 2026-09-27 |
| 0003 | Per-repo public/private visibility | Accepted | 2026-09-27 |
| 0004 | UI framework: Bootstrap 5.3.8, vendored, Sass at release time | Accepted | 2026-09-27 |
| 0005 | Release workflows are the sole owner of version tags | Accepted | 2026-09-27 |

## Adding a decision

Copy `0000-template.md`, fill it in, open a pull request referencing the discussion
where the argument happened. Accepted ADRs are not edited; a changed decision gets a new
number and supersedes the old one.
