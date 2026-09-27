---
title: Getting Started
description: 5-minute overview of debian-repo concepts
---


A 5-minute overview of debian-repo concepts and how to get your first package into a repository.

## What You Need to Know

**debian-repo** is a Debian package repository service. Think of it like an `apt` server — you upload `.deb` packages, and machines download them via `apt update && apt install`.

Key differences from traditional `apt-ftparchive`:
- **Atomic uploads** — No partial package registrations
- **REST API** — Upload via HTTP instead of `scp`
- **Multi-repository** — Host multiple independent repositories
- **No external tools** — Signing and metadata generation are built-in

## Architecture at a Glance

```
┌─────────────────────────────────────────────────────────────┐
│  Your CI/CD (Gitea Actions, GitHub Actions, etc.)          │
│  Builds package → curl POST to debian-repo /api/v1/upload  │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  debian-repo Service (Kubernetes Pod or VM)                │
│  - Receives upload, validates .deb                         │
│  - Registers in index, renders Release/Packages           │
│  - Signs Release with GPG                                  │
│  - Stores to MinIO bucket                                 │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│  MinIO S3 (package storage)                                │
│  - pool/ → actual .deb files                              │
│  - dists/ → Release/Packages metadata                     │
└─────────────────────────────────────────────────────────────┘
```

And on the client side:

```
┌──────────────────────────────────┐
│  Your machine (apt client)       │
│  1. Add source: deb https://...  │
│  2. apt update → fetches Release │
│  3. apt install → downloads .deb │
└──────────────────────────────────┘
```

## Key Concepts

| Concept | Example | Explanation |
|---------|---------|-------------|
| **Repository** | `main`, `testing` | An independent Debian repository with its own signing key |
| **Suite** | `stable`, `unstable` | A release channel within a repository (apt calls these "distributions") |
| **Component** | `main`, `contrib` | Package sections (free software, contrib, etc.) |
| **Architecture** | `amd64`, `arm64` | CPU architecture |
| **Package Version** | `nginx 1.20.0 amd64` | A single installable package |

A typical repository structure:
```
main/
├── stable/
│   ├── main/         # free software
│   │   └── amd64
│   └── contrib/      # contributed packages
│       └── amd64
└── unstable/
    ├── main/
    │   └── amd64
    └── contrib/
        └── amd64
```

## Your First Upload (5 minutes)

### 1. Build a Debian package

You already have one, or use this example:

```bash
# Create a minimal package
mkdir -p my-package-1.0.0/DEBIAN
cat > my-package-1.0.0/DEBIAN/control <<EOF
Package: my-package
Version: 1.0.0
Architecture: amd64
Maintainer: You <you@example.com>
Description: My first package
EOF

dpkg-deb --build my-package-1.0.0 my-package_1.0.0_amd64.deb
```

### 2. Upload to debian-repo

```bash
export TOKEN="your-ci-bearer-token"
export REPO_URL="https://debs.myorgname.com"

curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @my-package_1.0.0_amd64.deb \
  "$REPO_URL/api/v1/upload?suite=stable&component=main"
```

Response:
```json
{
  "status": "registered",
  "package": "my-package",
  "version": "1.0.0",
  "architecture": "amd64",
  "suite": "stable",
  "component": "main"
}
```

### 3. Install from a client

On any machine:

```bash
echo "deb https://username:password@debs.myorgname.com stable main" | \
  sudo tee /etc/apt/sources.list.d/myorgname.list
sudo apt update
sudo apt install my-package
```

Done! You've uploaded a package and installed it.

## Next Steps

- **[User Guide](../user-guide/)** — More upload scenarios, removing packages, moving between suites
- **[CI Integration](../user-guide/ci-integration.md)** — Integrate package uploads into your CI/CD
- **[API Reference](../api/)** — Full endpoint documentation
- **[Admin Guide](../admin-guide/)** — Installation, configuration, operations

## Common Questions

**Q: Do I need a gpg key?**
A: No. debian-repo has a built-in GPG key for signing Release metadata. Your CI just uploads `.deb` files.

**Q: Can I have multiple repositories?**
A: Yes! Each repository has independent storage, signing, and URL routing. See [Multi-Repo Setup](../admin-guide/multi-repo.md).

**Q: What if I want to remove a package?**
A: Use the `/api/v1/dists/{suite}/{component}/remove/{package}/{version}/{arch}` endpoint. See [User Guide — Managing Packages](../user-guide/managing.md).

**Q: How do I authenticate apt clients?**
A: HTTP Basic Auth. Credentials are stored as bcrypt hashes in MinIO. Manage with `repoctl users`. See [Client Setup](../user-guide/client-setup.md).

**Q: Is there a web UI?**
A: Yes, at `https://debs.myorgname.com/`. You can browse suites, view package details, and download keys. Requires OIDC login if configured.
