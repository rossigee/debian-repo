---
title: Working with Suites
description: Creating suites, managing release channels
---


Understanding how debian-repo manages release channels (suites) and how to organize packages across them.

## What is a Suite?

A suite is a release channel within a repository. Think of it as a named snapshot of your packages at different release stages. Examples:

| Suite | Purpose | Who uses it |
|-------|---------|------------|
| `stable` | Production-ready releases | End users, production servers |
| `testing` | Validated pre-release candidates | QA, staging environments |
| `unstable` | Latest development builds | Developers, bleeding-edge users |

## Creating Suites

Suites are **auto-created on first upload** — there's no separate creation step. When you upload a package to a suite that doesn't exist yet, it's automatically created:

```bash
# This creates the "custom" suite if it doesn't exist
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @app_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=custom&component=main"
```

Once created, a suite is available for package queries and serving to apt clients.

## Suite Configuration

Control suite behavior through `config.yaml`:

### Default Suite

The repository has a default suite (default: `stable`), used when no suite is specified in an upload:

```yaml
repos:
  - id: myrepo
    default_suite: stable
    default_component: main
```

If you omit `?suite=...` from an upload, it goes to `default_suite`:

```bash
# Goes to "stable" automatically (the default)
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @app_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload"
```

### Protected Suites

By default, the `default_suite` is protected — you can't remove the last package from it (safeguard against accidental emptying):

```yaml
repos:
  - id: myrepo
    default_suite: stable
    protect_default_suite: true  # default
```

Attempting to empty a protected suite returns `409 Conflict`:

```json
{
  "status": "conflict",
  "message": "suite 'stable' is protected and cannot be fully emptied; retry with ?force=true"
}
```

To override: add `?force=true` to your remove request **AND** your CI token must have the `unprotect` operation grant.

If you want to disable this protection:

```yaml
repos:
  - id: myrepo
    default_suite: stable
    protect_default_suite: false  # allow removing all packages
```

## Common Patterns

### Multi-Stage Promotion

Use multiple suites to stage releases:

1. **unstable** — Latest commits, every build
2. **testing** — Manually promoted after QA passes
3. **stable** — Production-ready, manually promoted after testing validation

```bash
# Promote from testing → stable
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "packages": ["myapp"],
    "dry_run": false
  }' \
  https://debs.myorgname.com/api/v1/admin/move
```

### Per-Team Suites

Separate suites per team for parallel release cycles:

- `backend-stable` — Backend team releases
- `frontend-stable` — Frontend team releases
- `integration-staging` — Combined staging environment

Each team uploads to their own suite, and a release-coordination job moves validated packages into the shared integration suite.

### Release Branches

Use suites like git branches to track versions:

- `v1.0` — Version 1.0.x releases (critical fixes only)
- `v1.1` — Version 1.1.x releases (minor features)
- `v2.0-beta` — Next major version (beta testing)

Clients opt into the track they want via their apt source:

```bash
# Track v1.0 LTS releases
echo "deb https://debs.myorgname.com v1.0 main" | sudo tee /etc/apt/sources.list.d/myapp.list

# Track v2.0 beta features
echo "deb https://debs.myorgname.com v2.0-beta main" | sudo tee /etc/apt/sources.list.d/myapp-beta.list
```

## Moving Packages Between Suites

See **[Managing Packages](managing.md)** for detailed move operations and glob patterns.

Quick reference:

```bash
# Dry run to see what would move
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from_suite":"testing","to_suite":"stable","packages":["myapp"],"dry_run":true}' \
  https://debs.myorgname.com/api/v1/admin/move

# Actually move it
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from_suite":"testing","to_suite":"stable","packages":["myapp"],"dry_run":false}' \
  https://debs.myorgname.com/api/v1/admin/move
```

## See Also

- **[Managing Packages](managing.md)** — Moving and removing packages
- **[Uploading Packages](uploading.md)** — Specifying suite in the upload request
- **[User Guide](_index.md)** — Overview of all user tasks
