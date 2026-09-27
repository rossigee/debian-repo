---
title: User Guide
description: Practical guide for uploading packages and managing repositories
---


Practical guide for uploading packages, managing repositories, and working with debian-repo.

## Table of Contents

1. **[Client Setup](client-setup.md)** — Installing packages as an apt client
2. **[CI Integration](ci-integration.md)** — Automating package uploads from CI/CD
3. **[Uploading Packages](uploading.md)** — Different upload methods and modes
4. **[Upload Flows](upload-flows.md)** — Deep dive into direct vs. presigned upload modes
5. **[Managing Packages](managing.md)** — Removing, moving, and organizing packages
6. **[Working with Suites](suites.md)** — Creating suites, managing release channels
7. **[JSON API](json-api.md)** — Machine-readable package index

## Quick Reference

### Upload a package
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  --data-binary @package.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
```
→ See **[Uploading Packages](uploading.md)**

### Install a package
```bash
echo "deb https://user:pass@debs.myorgname.com stable main" | \
  sudo tee /etc/apt/sources.list.d/myorgname.list
sudo apt update && sudo apt install package-name
```
→ See **[Client Setup](client-setup.md)**

### Remove a package
```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/remove/package-name/1.0.0/amd64"
```
→ See **[Managing Packages](managing.md)**

### Move packages between suites
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from_suite":"testing","to_suite":"stable","packages":["pkg-*"]}' \
  "https://debs.myorgname.com/api/v1/dists/move"
```
→ See **[Managing Packages](managing.md)**

## Workflows

### Publishing a Release

1. Build packages in CI (Gitea Actions, GitHub Actions, etc.)
2. Upload to `testing` suite for validation
3. Run tests against the `testing` suite
4. Move approved packages from `testing` to `stable`
5. Clients install from `stable` via `apt`

### Rolling Back a Release

1. Move package from `stable` back to `unstable` (or remove it)
2. Restart your services or uninstall/reinstall from older version
3. Clients will downgrade on next `apt update && apt upgrade`

### Multi-Architecture Support

Upload the same package for multiple architectures:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  --data-binary @package_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"

curl -X POST -H "Authorization: Bearer $TOKEN" \
  --data-binary @package_1.0.0_arm64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
```

Clients will get the architecture-specific package automatically.

## Authentication

### For apt clients (HTTP Basic Auth)

Credentials are managed by your admin using `repoctl users`. Add to `/etc/apt/sources.list.d/`:

```
deb https://username:password@debs.myorgname.com stable main
```

The password is bcrypt-hashed server-side. Never commit credentials to Git.

### For CI/CD (Bearer Token)

Your admin provides a bearer token. Use in `Authorization` header:

```bash
curl -H "Authorization: Bearer $TOKEN" \
  --data-binary @package.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
```

Never commit tokens to Git. Use CI secrets (GitHub Secrets, Gitea Variables, etc.).

## Rate Limiting & Quotas

- No per-user rate limits
- No storage quotas (limited by MinIO bucket size)
- Concurrent uploads are supported
- Package deduplication: uploading the same version twice is idempotent (replaces the first)

## Troubleshooting

### "Authorization denied"
- Bearer token missing or invalid
- Check `Authorization: Bearer $TOKEN` header
- Verify token has grants for the target suite/component

### "Invalid .deb file"
- Package is corrupted or malformed
- Verify with `dpkg-deb -I package.deb`
- Check package `control` file is valid

### "Suite not found"
- Suite doesn't exist in the repository
- Create it via config (admin-only) or use an existing suite
- See **[Admin Guide — Multi-Repo Setup](../admin-guide/multi-repo.md)**

### "Package already exists"
- A package with that name/version/architecture already exists
- To replace: remove the old one first, then upload the new one
- Or manually increment the version number

## See Also

- **[Getting Started](../getting-started/)** — 5-minute overview
- **[API Reference](../api/)** — All endpoints and their parameters
- **[Admin Guide](../admin-guide/)** — For operators and admins
