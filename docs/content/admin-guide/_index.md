---
title: Admin Guide
description: Operational and administrative guidance for running debian-repo in production
---


Operational and administrative guidance for running and managing debian-repo in production.

## Table of Contents

1. **[Installation Guide](installation.md)** — System requirements, build, deployment (Docker, Kubernetes, SystemD)
2. **[Configuration Reference](configuration.md)** — All configuration options explained
3. **[Operations Guide](operations.md)** — Running, monitoring, troubleshooting
4. **[Multi-Repo Setup](multi-repo.md)** — Configuring multiple independent repositories
5. **[Tracing Setup](tracing.md)** — OpenTelemetry OTLP tracing and observability
6. **[Maintenance & Backup](maintenance.md)** — Snapshots, reconciliation, recovery

## Quick Reference

### Deployment Checklist

- ✅ Install Go 1.21+
- ✅ Set up MinIO bucket
- ✅ Generate or import GPG key
- ✅ Configure `config.yaml`
- ✅ Deploy via Docker, Kubernetes, or SystemD
- ✅ Set up monitoring and logging
- ✅ Configure CI/CD tokens

### Common Tasks

- **Start the service**: `debian-repo -config /etc/debian-repo/config.yaml`
- **Manage apt users**: `repoctl users add|list|remove|passwd`
- **Rebuild index**: `repoctl reconcile`
- **Import packages**: `repoctl import --apply`

## See Also

- **[User Guide](../user-guide/)** — For uploading and managing packages
- **[Getting Started](../getting-started/)** — 5-minute overview
- **[API Reference](../api/)** — Endpoint documentation
