# Examples

This directory contains example scripts and configuration files for debian-repo.

## Quick Start

- **Shell scripts** (`api_*.sh`) — Example curl commands for common API operations
- **Config examples** (`config.*.yaml`) — Configuration templates for specific features

For comprehensive guides and detailed walkthroughs, see the main documentation:

- **[Getting Started](../docs/getting-started/)** — 5-minute quickstart
- **[User Guide](../docs/user-guide/)** — Upload workflows, CI integration, client setup
- **[API Reference](../docs/api/)** — Full endpoint documentation with examples
- **[Admin Guide](../docs/admin-guide/)** — Installation, configuration, operations

## Example Files

| File | Purpose |
|------|---------|
| `api_upload.sh` | Example: upload a package via API |
| `api_remove.sh` | Example: remove a package via API |
| `config.tracing.yaml` | Example: OpenTelemetry tracing config |

All scripts use environment variables for easy customization. Edit the variables at the top of each script to match your environment.
