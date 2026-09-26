---
title: Operations Guide
description: Running, monitoring, and troubleshooting
---


Guidance for operating debian-repo in production: monitoring, health checks, metrics, and troubleshooting common issues.

## Health Checks

### Liveness Probe

Indicates the service is alive and responding:

```bash
curl http://localhost:8080/healthz
```

Returns `200 OK` with body `"OK\n"` if the service is running.

**Kubernetes:**
```yaml
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 10
```

### Readiness Probe

Indicates the service is ready to handle requests (index loaded):

```bash
curl http://localhost:8080/readyz
```

Returns `200 OK` if at least one distribution is loaded; `503 Service Unavailable` if the index is empty.

**Kubernetes:**
```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

## Metrics

Prometheus metrics are exposed at `/metrics` on the HTTP port (default 8080), protected by bearer token:

```bash
curl -H "Authorization: Bearer $METRICS_TOKEN" \
  http://localhost:8080/metrics
```

Configure the metrics token in `config.yaml`:

```yaml
auth:
  metrics:
    token: "your-metrics-token"
```

### Key Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `debian_repo_http_requests_total{method,path,status}` | Counter | HTTP requests per endpoint and status |
| `debian_repo_http_request_duration_seconds{method,path}` | Histogram | Request latency |
| `debian_repo_registrations_total{suite,component}` | Counter | Packages uploaded |
| `debian_repo_registration_errors_total{reason}` | Counter | Upload failures |
| `debian_repo_registration_duration_seconds{suite,component}` | Histogram | Upload time |
| `debian_repo_snapshot_generation` | Gauge | Current snapshot generation number |
| `debian_repo_pool_downloads_total{suite,component,architecture}` | Counter | .deb file downloads |
| `debian_repo_distributions_count` | Gauge | Total suites |
| `debian_repo_packages_total` | Gauge | Total unique packages |

### Alerting Examples

**Upload failures increasing:**
```prometheus
rate(debian_repo_registration_errors_total[5m]) > 0
```

**Slow uploads:**
```prometheus
debian_repo_registration_duration_seconds > 30
```

**Index generation stalled:**
```prometheus
increase(debian_repo_snapshot_generation[10m]) == 0
```

## Logging

debian-repo uses structured JSON logging. Configure log level in `config.yaml`:

```yaml
log_level: "info"     # debug, info, warn, error
```

### Log Fields

Every request includes:
- `correlation_id` — Unique ID for tracing a request across logs
- `method` — HTTP method
- `path` — Request path
- `status` — HTTP status code
- `duration_ms` — Request latency
- `user` — Authenticated user (for Basic Auth)
- `auth_type` — Auth method (Basic, Bearer, etc.)

### Using Correlation IDs

Pass a custom correlation ID via header:

```bash
curl -H "X-Correlation-ID: deploy-12345" \
  http://localhost:8080/api/v1/upload
```

If not provided, debian-repo generates one automatically. Use correlation IDs to trace a workflow across multiple requests.

### Excluded from Request Logging

These paths are NOT logged (to reduce noise):
- `/healthz` — Liveness probe
- `/readyz` — Readiness probe
- `/metrics` — Metrics scrape

This means health checks and metrics collection won't fill your logs.

## Troubleshooting

### "Authorization denied" when uploading

**Symptoms:**
```
HTTP 401 Unauthorized
{"status":"error","message":"invalid credentials"}
```

**Check:**
1. Bearer token is valid and not expired
2. Token has `upload` operation grant
3. Token has access to the target suite/component

```bash
# Verify token format
curl -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/upload?suite=testing
```

### "Suite not found" (404)

**Symptoms:**
```
HTTP 404 Not Found
{"status":"error","message":"distribution testing not found"}
```

**Cause:** Suite doesn't exist yet (suites are auto-created on first upload, or don't have any packages).

**Fix:** Upload to an existing suite first, or use `default_suite` config.

### "Protected suite cannot be emptied" (409)

**Symptoms:**
```
HTTP 409 Conflict
{"status":"conflict","message":"suite 'stable' is protected..."}
```

**Cause:** Trying to remove the last package from a protected suite (usually `default_suite` with `protect_default_suite: true`).

**Fix:** Either:
1. Add another package first, then remove
2. Override with `?force=true` (requires `unprotect` grant)
3. Disable protection: `protect_default_suite: false` in config

### Reconciliation already running (409)

**Symptoms:**
```
HTTP 409 Conflict
{"job_id":"job-xyz","status":"conflict"}
```

**Cause:** Another reconciliation is in progress; debian-repo allows only one at a time.

**Fix:** Wait for it to finish:
```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/admin/reconcile/job-xyz
```

### Slow uploads or index operations

**Check metrics:**
```bash
curl -s -H "Authorization: Bearer $METRICS_TOKEN" \
  http://localhost:8080/metrics | grep registration_duration
```

**Common causes:**
- MinIO latency (network, bucket performance)
- Large .deb file (validation takes time)
- Concurrent uploads (lock contention on distribution)

**Mitigation:**
- Use presigned URLs for large files (`upload_mode: "presigned"`)
- Enable debounced snapshots: `persist_mode: "debounced"`
- Ensure MinIO is performant (SSD backend, network latency <10ms)

## Performance Tuning

### Debounced Snapshots

For high-upload-rate scenarios, batch snapshot writes:

```yaml
index:
  persist_mode: "debounced"
  persist_debounce: 30s  # Wait 30s before writing to MinIO
```

Trade-off: snapshots are delayed, but fewer MinIO writes.

### Cache Headers

Metadata responses include cache headers:
- Release files: `Cache-Control: max-age=300` (5 minutes)
- JSON API: `Cache-Control: max-age=60` (1 minute)
- Atom feed: `Cache-Control: max-age=60`

Clients respect these; configure your reverse proxy cache to match.

## Monitoring Checklist

- [ ] Liveness probe returns 200
- [ ] Readiness probe returns 200
- [ ] Metrics endpoint is scrape-able
- [ ] Log level is appropriate (info for production)
- [ ] Correlation IDs are propagated in CI scripts
- [ ] Upload error rate is near zero
- [ ] Package distribution count matches expectations
- [ ] Snapshots are generating regularly

## See Also

- **[Configuration Reference](configuration.md)** — Tuning options
- **[Maintenance & Backup](maintenance.md)** — Snapshots and recovery
- **[Admin Guide](_index.md)** — Overview of all admin topics
