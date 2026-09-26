---
title: Installation Guide
description: How to install and deploy debian-repo in production
---


How to install and deploy debian-repo in production.

## Prerequisites

- **Go 1.21+** — Required for building
- **MinIO S3 bucket** — For package storage
- **Kubernetes cluster** (or Docker/systemd) — For running the service
- **GPG key** (RSA 4096 or EdDSA) — For signing Release metadata

Optional but recommended:
- **Keycloak** — For web UI authentication
- **Prometheus** — For monitoring

## System Requirements

| Component | Minimum | Recommended |
|-----------|---------|-------------|
| CPU | 2 cores | 4+ cores |
| RAM | 2 GB | 4+ GB |
| Disk | 10 GB (local temp) | 20+ GB (snapshots cache) |
| Network | 1 Gbps | 10 Gbps (for large uploads) |

## Building from Source

1. Clone the repository:

```bash
git clone https://github.com/rossigee/debian-repo.git
cd debian-repo
```

2. Build binaries:

```bash
make build        # Builds debian-repo and repoctl
make docker       # Builds Docker image (optional)
```

3. Verify build:

```bash
./debian-repo --version
./repoctl --help
```

## Docker Deployment

### Option 1: Docker Compose (Development)

```yaml
services:
  debian-repo:
    image: debian-repo:latest
    ports:
      - "8080:8080"   # HTTP API
      - "9090:9090"   # Metrics
    volumes:
      - ./config.yaml:/etc/debian-repo/config.yaml:ro
      - ./signing-key.asc:/etc/debian-repo/signing-key.asc:ro
    environment:
      - SIGNING_KEY_PASSPHRASE=${GPG_PASSPHRASE}
      - MINIO_ACCESS_KEY=${MINIO_ACCESS_KEY}
      - MINIO_SECRET_KEY=${MINIO_SECRET_KEY}
    restart: always

  minio:
    image: minio/minio:latest
    ports:
      - "9000:9000"
      - "9001:9001"
    environment:
      MINIO_ROOT_USER: minioadmin
      MINIO_ROOT_PASSWORD: minioadmin
    volumes:
      - minio-data:/minio_root
    command: server /minio_root --console-address ":9001"
    restart: always

volumes:
  minio-data:
```

Start:
```bash
docker-compose up -d
```

### Option 2: Production Docker Image

```dockerfile
FROM ubuntu:22.04

RUN apt-get update && apt-get install -y \
    ca-certificates \
    gnupg \
    && rm -rf /var/lib/apt/lists/*

COPY debian-repo /usr/local/bin/
COPY repoctl /usr/local/bin/

EXPOSE 8080 9090
ENTRYPOINT ["debian-repo", "-config", "/etc/debian-repo/config.yaml"]
```

Build and push:
```bash
docker build -t your-registry/debian-repo:latest .
docker push your-registry/debian-repo:latest
```

## Kubernetes Deployment

1. Create namespace:

```bash
kubectl create namespace debian-repo
```

2. Store secrets:

```bash
# GPG key
kubectl -n debian-repo create secret generic debian-repo-signing-key \
  --from-file=key=signing-key.asc

# MinIO credentials
kubectl -n debian-repo create secret generic debian-repo-minio \
  --from-literal=access-key=$MINIO_ACCESS_KEY \
  --from-literal=secret-key=$MINIO_SECRET_KEY

# OIDC secret
kubectl -n debian-repo create secret generic debian-repo-oidc \
  --from-literal=cookie-secret=$(openssl rand -hex 16)
```

3. Create ConfigMap for config:

```bash
kubectl -n debian-repo create configmap debian-repo-config \
  --from-file=config.yaml=config.yaml
```

4. Deploy using Helm or kubectl manifest:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: debian-repo
  namespace: debian-repo
spec:
  replicas: 2
  selector:
    matchLabels:
      app: debian-repo
  template:
    metadata:
      labels:
        app: debian-repo
    spec:
      containers:
      - name: debian-repo
        image: your-registry/debian-repo:latest
        ports:
        - containerPort: 8080
          name: http
        - containerPort: 9090
          name: metrics
        env:
        - name: SIGNING_KEY_PASSPHRASE
          valueFrom:
            secretKeyRef:
              name: debian-repo-signing-key
              key: passphrase
        - name: MINIO_ACCESS_KEY
          valueFrom:
            secretKeyRef:
              name: debian-repo-minio
              key: access-key
        - name: MINIO_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: debian-repo-minio
              key: secret-key
        volumeMounts:
        - name: config
          mountPath: /etc/debian-repo
          readOnly: true
        - name: signing-key
          mountPath: /etc/debian-repo/signing-key.asc
          subPath: key
          readOnly: true
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            cpu: 100m
            memory: 256Mi
          limits:
            cpu: 2000m
            memory: 2Gi
      volumes:
      - name: config
        configMap:
          name: debian-repo-config
      - name: signing-key
        secret:
          secretName: debian-repo-signing-key

---
apiVersion: v1
kind: Service
metadata:
  name: debian-repo
  namespace: debian-repo
spec:
  selector:
    app: debian-repo
  ports:
  - name: http
    port: 80
    targetPort: 8080
  - name: metrics
    port: 9090
    targetPort: 9090
  type: LoadBalancer
```

Apply:
```bash
kubectl apply -f deployment.yaml
```

## SystemD Service (VM Deployment)

Create `/etc/systemd/system/debian-repo.service`:

```ini
[Unit]
Description=Debian Package Repository Service
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=debian-repo
Group=debian-repo
ExecStart=/usr/local/bin/debian-repo -config /etc/debian-repo/config.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
Environment="SIGNING_KEY_PASSPHRASE=%s/signing-key-passphrase"

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo useradd -r -s /bin/false debian-repo
sudo systemctl daemon-reload
sudo systemctl enable debian-repo
sudo systemctl start debian-repo
sudo systemctl status debian-repo
```

## Configuration

Copy `config.example.yaml` to `/etc/debian-repo/config.yaml` and edit:

```bash
cp config.example.yaml /etc/debian-repo/config.yaml
sudo vim /etc/debian-repo/config.yaml
```

Key settings:
- `listen.http` — Service port (default: `:8080`)
- `storage.minio.*` — MinIO credentials and endpoint
- `signing.key_path` — Path to GPG key
- `signing.passphrase` — GPG key passphrase (from env var)
- `auth.ci_tokens.*` — Bearer tokens for CI/CD
- `auth.oidc.*` — Keycloak configuration (optional)

See [Configuration Reference](configuration.md) for all options.

## GPG Key Setup

### Generate a new key (if needed)

```bash
gpg --full-generate-key
```

Select:
- Kind: EdDSA
- Size: 4096
- User ID: `Debian Repository <repo@myorgname.com>`
- Passphrase: Strong, store securely (e.g., `SIGNING_KEY_PASSPHRASE` env var)

Export for debian-repo:

```bash
gpg --export-secret-keys repo@myorgname.com > signing-key.asc
# Keep passphrase ready for config
```

### Use existing key

If you have an existing key, export it:

```bash
gpg --export-secret-keys KEY_ID > signing-key.asc
```

Store securely (not in Git):

```bash
sudo install -m 0600 -o debian-repo signing-key.asc /etc/debian-repo/
```

## MinIO Bucket Setup

1. Create bucket (if not exists):

```bash
mc mb minio/debs-myorg
```

2. Set lifecycle policy for snapshots (optional):

```bash
# Keep 20 snapshots, delete older ones
mc ilm rule add --expiry-days 30 minio/debs-myorg
```

3. Test access:

```bash
# List buckets
mc ls minio/

# Check bucket contents
mc ls minio/debs-myorg/
```

## Health Checks

Verify service is running:

```bash
# Liveness check
curl http://localhost:8080/healthz

# Readiness check
curl http://localhost:8080/readyz

# Metrics check
curl -H "Authorization: Bearer $METRICS_TOKEN" \
  http://localhost:9090/metrics

# List available packages
curl http://localhost:8080/index.json | jq '.packages'
```

## Migration from Old System

If migrating from an existing repository:

1. Export packages from old system
2. Use `repoctl import` to migrate:

```bash
repoctl import --apply
```

See [Maintenance & Backup](maintenance.md) for details.

## Troubleshooting

### Service won't start

**Check logs:**
```bash
journalctl -u debian-repo -n 50 -f
```

**Common issues:**
- Missing GPG key: Set `signing.key_path` and `signing.passphrase`
- MinIO unreachable: Verify `storage.minio.endpoint` and credentials
- Port in use: Change `listen.http` to different port

### Metrics unavailable

```bash
# Check metrics token
echo "Check that METRICS_TOKEN is set and used in Authorization header"

# Verify metrics are being exposed
curl http://localhost:9090/metrics 2>&1 | head -5
```

## Next Steps

1. **[Configuration Reference](configuration.md)** — All config options
2. **[Operations Guide](operations.md)** — Running and monitoring
3. **[Security Setup](authentication.md)** — Configure authentication
4. **[Multi-Repo Setup](multi-repo.md)** — Multiple repositories
