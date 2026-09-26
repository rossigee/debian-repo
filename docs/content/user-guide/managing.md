---
title: Managing Packages
description: Removing, moving, and organizing packages
---


Guide for removing packages, promoting packages between suites, and organizing your repository.

## Removing Packages

### Remove a single package version

```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx/1.20.0/amd64
```

**Response (200):**
```json
{
  "status": "removed",
  "package": "nginx",
  "suite": "stable"
}
```

### Remove all architectures of a version

```bash
# Omit /{arch} to remove all architectures
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx/1.20.0
```

### Remove all versions of a package

```bash
# Omit /{version}/{arch} to remove entire package
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx
```

### Protected Suites

If you're removing the last remaining package from a protected suite (by default, the `default_suite` with `protect_default_suite: true` in config), the operation fails with a `409 Conflict` error:

```json
{
  "status": "conflict",
  "message": "suite 'stable' is protected and cannot be fully emptied; retry with ?force=true"
}
```

To override protection, add `?force=true` **AND** your token must have the `unprotect` operation grant:

```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx?force=true"
```

## Moving Packages Between Suites

Promote packages from testing/staging to stable without re-uploading the `.deb` files.

### Basic move

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "component": "main",
    "packages": ["nginx"],
    "dry_run": false
  }' \
  https://debs.myorgname.com/api/v1/admin/move
```

**Response (200):**
```json
{
  "status": "moved",
  "from_suite": "testing",
  "to_suite": "stable",
  "count": 1,
  "moved": [
    {
      "package": "nginx",
      "version": "1.20.0",
      "architecture": "amd64",
      "component": "main",
      "filename": "pool/main/n/nginx/nginx_1.20.0_amd64.deb"
    }
  ]
}
```

### Move multiple packages with glob patterns

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "component": "main",
    "packages": ["app-*", "lib-foo"],
    "dry_run": false
  }' \
  https://debs.myorgname.com/api/v1/admin/move
```

Glob patterns use `*` (match any characters) and are applied to package names.

### Move from all components

Leave `component` empty or omit it to move from all components:

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "packages": ["nginx"],
    "dry_run": false
  }' \
  https://debs.myorgname.com/api/v1/admin/move
```

### Dry run

Add `"dry_run": true` to preview what would be moved without making changes:

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "packages": ["nginx*"],
    "dry_run": true
  }' \
  https://debs.myorgname.com/api/v1/admin/move
```

Response uses `"status": "dry_run"` instead of `"moved"`, but same shape.

### Protected Suites

Moving packages can trigger the same protection checks as removal:
- Draining a protected suite requires `?force=true` + `unprotect` grant
- Moving INTO a protected suite requires `upload` grant on that suite

## Workflow: Testing → Stable Promotion

Typical release workflow:

```bash
# 1. CI uploads to testing
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @app_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=testing&component=main"

# 2. Run smoke tests against testing suite
# (your test script here)

# 3. Move validated packages to stable
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "packages": ["app"],
    "dry_run": false
  }' \
  https://debs.myorgname.com/api/v1/admin/move

# 4. Clients install from stable
apt update && apt install app
```

## See Also

- **[Uploading Packages](uploading.md)** — How to upload packages initially
- **[Working with Suites](suites.md)** — Understanding suites and release channels
- **[User Guide](_index.md)** — Overview of all user tasks
