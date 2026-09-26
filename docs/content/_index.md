---
title: debian-repo
description: Native Go microservice for hosting Debian package repositories
---


A native Go microservice for hosting Debian package repositories with atomic uploads, multi-repo isolation, and runtime-configurable serving modes.

## Quick Links

### For Users & CI/CD
- **[Getting Started](getting-started/)** — 5-minute overview, basic concepts
- **[User Guide](user-guide/)** — Uploading packages, managing suites, common workflows
- **[API Reference](api/)** — HTTP endpoints, authentication, request/response formats
  - **[Swagger UI](api/swagger-ui.html)** — Interactive API explorer
  - **[ReDoc](api/redoc.html)** — Clean API reference documentation
  - **[Raw OpenAPI](api/openapi.yaml)** — Machine-readable spec

### For Operators & Admins
- **[Installation Guide](admin-guide/installation.md)** — System requirements, build, deploy
- **[Configuration Reference](admin-guide/configuration.md)** — All config options explained
- **[Operations Guide](admin-guide/operations.md)** — Running, monitoring, troubleshooting
- **[Multi-Repo Setup](admin-guide/multi-repo.md)** — Configuring multiple repositories
- **[Maintenance & Backup](admin-guide/maintenance.md)** — Snapshots, reconciliation, recovery

## At a Glance

**What is debian-repo?**

A high-performance Debian package repository service that replaces manual package management with:
- **Atomic uploads** — Packages are fully registered or not at all, no partial states
- **In-memory index** — Fast metadata serving from a cached index
- **Multi-repo** — Host multiple independent repositories with per-repo signing and storage
- **CLI operator tools** — `repoctl` for migration, reconciliation, user management
- **Three-tier auth** — HTTP Basic Auth for apt, OIDC for web UI, bearer tokens for CI

**Key Concepts:**

| Term | Meaning |
|------|---------|
| **Repository** | A named Debian repository (e.g., "main", "testing") with independent signing and storage |
| **Suite** | A release channel within a repo (e.g., "stable", "unstable") |
| **Component** | A section of packages (e.g., "main", "contrib") |
| **Architecture** | CPU architecture (e.g., "amd64", "arm64") |
| **Package Version** | A specific release of a package (e.g., "nginx 1.20.0 for amd64") |
| **Pool** | MinIO S3 bucket containing `.deb` files |
| **Snapshot** | Serialized index state, persisted to MinIO for recovery |

## Common Tasks

### Upload a package from CI/CD
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  --data-binary @package.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
```
→ See **[CI Integration](user-guide/ci-integration.md)**

### Install from the repository
```bash
echo "deb https://username:password@debs.myorgname.com stable main" | \
  sudo tee /etc/apt/sources.list.d/myorgname.list
sudo apt update && sudo apt install package-name
```
→ See **[Client Setup](user-guide/client-setup.md)**

### Manage suites and packages (Admin)
```bash
repoctl reconcile              # Rebuild index from pool contents
repoctl users add myuser       # Create apt user
```
→ See **[Operations Guide](admin-guide/operations.md)**

## Documentation Roadmap

- ✅ User Guide (uploading, managing repos)
- ✅ Admin Guide (installation, configuration, operations)
- ✅ API Reference (endpoints, auth, formats)

## Related Documents

- **[AGENTS.md](../../AGENTS.md)** — Internal package layout, design principles, known limitations

## Support

- **Issues & Bugs** — Report at [repository issues](https://github.com/rossigee/debian-repo/issues)
- **Runbook** — [Operational runbooks](#) (admin-only)
