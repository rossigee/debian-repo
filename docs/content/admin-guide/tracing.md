---
title: OpenTelemetry Tracing
description: Distributed tracing and observability setup
---


debian-repo includes OpenTelemetry (OTLP) tracing support for distributed tracing and observability. This guide covers server-side setup and what data is available.

## Configuration

Enable tracing in `config.yaml`:

```yaml
tracing:
  enabled: true
  otlp_host: "tempo.monitoring.svc.cluster.local"  # OTLP receiver endpoint
  otlp_port: "4318"
  service_name: "debian-repo"
  sampler: "probabilistic"  # always, never, or probabilistic
  sample_rate: 0.1          # 0.0-1.0 (10% of traces when probabilistic)
```

Or use environment variables:
```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="http://tempo:4318"
export OTEL_SDK_DISABLED="false"
```

## Sampling Strategies

| Strategy | Use Case |
|----------|----------|
| `always` | Development/debugging (high volume) |
| `never` | Disable tracing |
| `probabilistic` | Production (recommended) with configurable sample_rate |

## What Gets Traced

All traces include automatic instrumentation for:

- **HTTP Requests**: method, path, status, response size, duration
- **Package Operations**: registration, removal, validation, index mutations
- **Storage**: MinIO uploads/downloads, snapshot persistence
- **Authentication**: HTTP Basic Auth, OIDC, token validation
- **Cryptography**: GPG signing operations

Each trace includes context attributes:
```
trace_id, span_id, http.method, http.status_code, duration_ms,
correlation_id, suite, component, package, version, architecture, error
```

## Trace IDs and Correlation

Each request receives a correlation ID (header: `X-Trace-ID`). Use this ID to:
- Correlate traces with logs
- Track a request across service boundaries
- Debug specific operations

## Common OTLP Endpoints

Point your config to any OTLP-compatible receiver:

```yaml
# Grafana Tempo
otlp_host: "tempo.monitoring.svc.cluster.local"
otlp_port: "4318"

# Jaeger
otlp_host: "jaeger-collector.monitoring.svc.cluster.local"
otlp_port: "4318"

# Generic OTLP receiver
otlp_host: "tracing-backend.example.com"
otlp_port: "4318"
```

## Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: debian-repo
spec:
  template:
    spec:
      containers:
      - name: debian-repo
        image: debian-repo:latest
        env:
        - name: OTEL_EXPORTER_OTLP_ENDPOINT
          value: "http://tempo.monitoring:4318"
```

## Performance

- **Overhead**: <1% with 0.1 sample rate (recommended)
- **Export**: Batched, non-blocking OTLP export
- **Disable**: Set `enabled: false` or `sampler: "never"`

## Available Trace Data

Once traces are exported to your OTLP backend, you can analyze:

- **Operation duration**: How long each request/operation took
- **Error spans**: Which operations failed (error=true in span attributes)
- **Request details**: HTTP method, status code, request path
- **Package metadata**: Which package/suite/component was affected
- **Correlation IDs**: Link to logs for debugging

Example trace attributes available for querying:
```
service.name = "debian-repo"
http.method = "POST"
http.target = "/api/v1/upload?suite=stable&component=main"
http.status_code = 200
duration_ms >= 100
error = false
correlation_id = "..."
```

## See Also

- **[OpenTelemetry](https://opentelemetry.io/)** — Standard for tracing/metrics
- **[OTLP Protocol](https://opentelemetry.io/docs/reference/protocol/)** — OTLP specification
- Your tracing backend's documentation (Tempo, Jaeger, etc.) for querying and visualization
