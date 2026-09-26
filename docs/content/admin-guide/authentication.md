---
title: Client & Token Authentication
description: HTTP Basic Auth for apt, bearer tokens for CI/CD
---


Reference for configuring HTTP Basic Auth (for apt clients) and bearer tokens (for CI/CD pipelines).

## HTTP Basic Auth (apt Clients)

Clients install packages using HTTP Basic Auth credentials stored in your debian-repo configuration.

### Configuration

In `config.yaml`:

```yaml
auth:
  apt_users:
    store_key: _meta/apt-users.json.gz          # MinIO location (default)
    reload_interval: 60s                        # Reload credentials periodically (default)
```

### Managing Users

Use the `repoctl` CLI to manage credentials (bcrypt-hashed, never stored in plaintext):

```bash
# Add a user (prompts for password interactively)
repoctl users add myuser

# Add with password via flag
repoctl users add myuser --password "mypassword"

# List all users (shows timestamps, never passwords)
repoctl users list

# Change password
repoctl users passwd myuser
repoctl users passwd myuser --password "newpassword"

# Remove user
repoctl users remove myuser
```

### Client Usage

Clients add credentials to `/etc/apt/sources.list.d/`:

```bash
echo "deb https://myuser:mypassword@debs.myorgname.com stable main" | \
  sudo tee /etc/apt/sources.list.d/myrepo.list

sudo apt update && sudo apt install package-name
```

### Security Notes

- Passwords are hashed with **bcrypt** (cost factor 10) — impossible to reverse
- Credential storage uses **constant-time comparison** to prevent timing attacks
- Credentials are checked against the MinIO-backed store
- Reload interval (default 60s) means changes propagate within one minute
- In CI pipelines, use bearer tokens instead of Basic Auth when possible

## CI/CD Bearer Tokens

CI pipelines authenticate with bearer tokens instead of Basic Auth. Tokens are configured in `config.yaml` and validated with constant-time comparison.

### Configuration

In `config.yaml`:

```yaml
auth:
  ci_tokens:
    - token: "secret-token-123"
      identity: "github-ci"                    # Name for logging and audit
      grants:
        - repos: ["*"]                         # Wildcard or specific repo IDs
          suites: ["testing", "stable"]        # Which suites this token can access
          components: ["main", "contrib"]      # Which components (empty = all)
          operations:                          # Allowed operations
            - upload                           # Upload packages
            - remove                           # Remove packages
            - unprotect                        # Override protected-suite checks
            - reconcile                        # Run reconciliation jobs
            - list-dists                       # List distributions
```

### Using Bearer Tokens in CI

Upload packages with token auth:

```bash
curl -X POST \
  -H "Authorization: Bearer secret-token-123" \
  --data-binary @app_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=testing&component=main"
```

Move packages between suites:

```bash
curl -X POST \
  -H "Authorization: Bearer secret-token-123" \
  -H "Content-Type: application/json" \
  -d '{"from_suite":"testing","to_suite":"stable","packages":["app"]}' \
  https://debs.myorgname.com/api/v1/admin/move
```

### Grant Operations

| Operation | Purpose |
|-----------|---------|
| `upload` | Upload packages to specified suites/components |
| `remove` | Remove packages from specified suites/components |
| `unprotect` | Override protected-suite safeguards (requires explicit grant) |
| `reconcile` | Trigger async index reconciliation jobs |
| `list-dists` | Query available distributions (implicitly granted to all authenticated requests) |

### Wildcards in Grants

- `repos: ["*"]` — Token can access all repositories
- `suites: ["*"]` — Token can access all suites
- `components: ["*"]` or empty — Token can access all components
- `packages: ["*"]` (in move operations) — Matches all package names

### Token Security

- Tokens are validated with **constant-time comparison** (`crypto/subtle.ConstantTimeCompare`) to prevent timing attacks
- Tokens are transmitted in the `Authorization: Bearer <token>` header (always use HTTPS)
- No token is ever logged in plaintext — only the first 8 characters and the identity are visible in logs
- Token grants are coarse-grained by suite/component, not per-package (use CI-level ACLs for fine-grained control)

### CI/CD Best Practices

1. **Use CI secrets** — Store tokens in GitHub Secrets, GitLab CI variables, etc., not in code
2. **Limit grants** — Give each CI token only the operations and suites it needs
3. **Rotate periodically** — Change tokens quarterly or when a team member leaves
4. **Use separate tokens per project** — Easier to revoke if a project is compromised
5. **Monitor uploads** — Log uploads and alert on unexpected activity

### Example: GitHub Actions

```yaml
name: Release
on:
  release:
    types: [published]

jobs:
  upload:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: 1.21
      - run: make build-deb
      - run: |
          curl -X POST \
            -H "Authorization: Bearer ${{ secrets.DEBIAN_REPO_TOKEN }}" \
            --data-binary @dist/app_*.deb \
            "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
```

## OIDC / Web UI Authentication

For web UI (dashboard) access, see the existing OIDC documentation. OIDC is separate from package repository authentication.

## See Also

- **[Configuration Reference](configuration.md)** — Full auth config options
- **[Operations Guide](operations.md)** — Monitoring and troubleshooting
- **[Admin Guide](_index.md)** — Overview of all admin topics
