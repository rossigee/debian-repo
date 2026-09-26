---
title: Uploading Packages
description: Different methods to upload Debian packages
---


Different methods to upload Debian packages to debian-repo.

## Prerequisites

- A valid `.deb` package file
- Bearer token from your admin (for API uploads)
- Network access to debian-repo service

## Method 1: HTTP POST (Recommended)

Upload a package directly via HTTP:

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @package_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=main"
```

Response:
```json
{
  "status": "registered",
  "package": "package",
  "version": "1.0.0",
  "architecture": "amd64",
  "suite": "stable",
  "component": "main",
  "filename": "pool/main/p/package/package_1.0.0_amd64.deb"
}
```

### Options

**Different suite/component (or use repo defaults by omitting query params):**

```bash
# Upload to testing/main
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @package_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=testing&component=main"
```

**Upload to contrib:**

```bash
# Upload to stable/contrib
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @package_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/upload?suite=stable&component=contrib"
```

## Method 2: Presigned URL (Direct to MinIO)

For large packages or concurrent uploads, use presigned URLs:

1. Request a presigned URL:

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"suite":"stable","component":"main","filename":"package_1.0.0_amd64.deb"}' \
  "https://debs.myorgname.com/api/v1/presign"
```

Response:
```json
{
  "presigned_url": "https://minio.minio.internal/debs-myorgname/...",
  "expires_in": 900
}
```

2. Upload directly to MinIO:

```bash
curl -X PUT \
  -H "Content-Type: application/octet-stream" \
  --data-binary @package_1.0.0_amd64.deb \
  "https://minio.minio.internal/debs-myorgname/..."
```

3. Register the package:

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"suite":"stable","component":"main","staging_key":"<uuid from presign response>"}' \
  "https://debs.myorgname.com/api/v1/register"
```

**Advantage:** Bypass debian-repo for upload (faster for large files), only use bandwidth for registration.

## Method 3: Multi-file Batch Upload

Upload multiple packages in a single request:

```bash
# Option 1: Sequential uploads
for deb in package_*.deb; do
  curl -X POST \
    -H "Authorization: Bearer $TOKEN" \
    --data-binary @"$deb" \
    "https://debs.myorgname.com/api/v1/dists/stable/main/upload"
done
```

Note: Each upload is processed separately. For true batch operations, see [Moving Packages](managing.md).

## Workflow: Cross-Distribution Upload

Upload the same package to multiple architectures:

```bash
#!/bin/bash
SUITE=stable
COMPONENT=main
TOKEN=$DEBIAN_REPO_TOKEN

for deb in package_*.deb; do
  echo "Uploading $deb..."
  curl -X POST \
    -H "Authorization: Bearer $TOKEN" \
    --data-binary @"$deb" \
    "https://debs.myorgname.com/api/v1/dists/$SUITE/$COMPONENT/upload" \
    || exit 1
done

echo "✓ All uploads successful"
```

## Upload Modes

Your admin configures how uploads are processed. Check which mode is active:

```bash
curl "https://debs.myorgname.com/" | grep -i "upload mode"
```

### Direct Mode (default)

Package flows: CI → debian-repo → MinIO → served

**Pros:** Simple, one request
**Cons:** debian-repo handles streaming (uses more resources)

### Presigned Mode

Package flows: CI → MinIO (presigned) → debian-repo (registers) → served

**Pros:** debian-repo doesn't handle file streaming, better for large files
**Cons:** More requests (request presign → upload to MinIO → register)

## Validation Before Upload

Check your `.deb` is valid:

```bash
# List package contents
dpkg-deb -c package_1.0.0_amd64.deb

# Extract control file
dpkg-deb -e package_1.0.0_amd64.deb

# Verify it has required fields
dpkg-deb -I package_1.0.0_amd64.deb control

# Full validation
dpkg --info package_1.0.0_amd64.deb
```

Expected output:
```
 new debian package, version 2.0.
 size 12345 bytes: control archive= 1234 bytes.
      1234 bytes,    42 lines      control
      ...
```

## Error Handling

### Package already exists

Uploading the same package twice (same name, version, architecture) will fail:

**Error response:**
```json
{
  "status": "conflict",
  "message": "package already exists: package 1.0.0 amd64"
}
```

**Options:**

1. Remove the old package first:
   ```bash
   curl -X DELETE \
     -H "Authorization: Bearer $TOKEN" \
     "https://debs.myorgname.com/api/v1/dists/stable/main/remove/package/1.0.0/amd64"
   ```

2. Then upload the new one

3. Or increment version and re-upload

### Invalid package format

**Error response:**
```json
{
  "status": "error",
  "message": "invalid .deb file: missing control file"
}
```

**Fix:**
- Ensure package was built correctly: `dpkg-deb -I package.deb control`
- Re-build the package with `dpkg-buildpackage` or similar

### Unauthorized

**Error response:**
```
{
  "status": "forbidden",
  "message": "token unauthorized for suite 'stable'"
}
```

**Fix:**
- Check token is correct: `echo $DEBIAN_REPO_TOKEN`
- Verify token has upload permission for the target suite
- Ask your admin to grant the permission

### Package too large

**Error response:**
```
413 Payload Too Large
```

**Fix:**
- Default limit is 512MB
- Ask your admin to increase `validation.max_deb_size_bytes`
- Or optimize your package (remove unnecessary files)

## Replacing a Package

Update an existing package:

```bash
#!/bin/bash
TOKEN=$DEBIAN_REPO_TOKEN
SUITE=stable
COMPONENT=main
PKG=my-package
VERSION=1.0.0
ARCH=amd64

# 1. Remove old version
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/$SUITE/$COMPONENT/remove/$PKG/$VERSION/$ARCH" || true

# 2. Upload new package
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @"${PKG}_${VERSION}_${ARCH}.deb" \
  "https://debs.myorgname.com/api/v1/dists/$SUITE/$COMPONENT/upload"
```

## Release Workflow

### 1. Upload to testing (experimental)

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @package_1.0.0_amd64.deb \
  "https://debs.myorgname.com/api/v1/dists/testing/main/upload"
```

### 2. Test in CI/integration tests

- Deploy from testing suite
- Run smoke tests
- Verify functionality

### 3. Promote to stable

```bash
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_suite": "testing",
    "to_suite": "stable",
    "packages": ["my-package*"]
  }' \
  "https://debs.myorgname.com/api/v1/dists/move"
```

### 4. Clients install from stable

```bash
sudo apt update
sudo apt install my-package
```

## Performance Tips

**For large packages (>100MB):**
- Use presigned mode to avoid streaming through debian-repo
- Compress files in the package (less obvious, but check for uncompressed data)
- Upload during off-peak hours

**For many packages:**
- Use parallel uploads with rate limiting:
  ```bash
  ls package_*.deb | parallel -j 4 'curl -X POST \
    -H "Authorization: Bearer $TOKEN" \
    --data-binary @{} \
    "https://debs.myorgname.com/api/v1/dists/stable/main/upload"'
  ```

**For production pipelines:**
- Implement retry logic (see [CI Integration](ci-integration.md))
- Use the JSON API to verify successful uploads
- Set up alerts for failed uploads

## See Also

- **[CI Integration](ci-integration.md)** — Automating uploads in CI/CD
- **[Managing Packages](managing.md)** — Removing, moving, organizing
- **[User Guide](_index.md)** — Overview of all user tasks
