---
title: CI Integration
description: Automating package uploads in your CI/CD pipeline
---


Guide for integrating debian-repo uploads into your CI/CD pipeline.

## Prerequisites

- A working CI/CD system (Gitea Actions, GitHub Actions, Tekton, etc.)
- Bearer token from your admin (configured in debian-repo)
- A working Debian package build step in your pipeline

## GitHub Actions Example

Add this to `.github/workflows/release.yaml`:

```yaml
name: Build and Push Debian Package

on:
  push:
    tags:
      - 'v*'

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build Debian package
        run: |
          apt-get update
          apt-get install -y build-essential devscripts
          dpkg-buildpackage -b -uc -us
          ls -la ../*.deb

      - name: Push to debian-repo
        if: success()
        run: |
          DEB_FILE=$(ls ../*.deb | head -1)
          if [ -z "$DEB_FILE" ]; then
            echo "❌ No .deb file found"
            exit 1
          fi
          echo "📦 Uploading $DEB_FILE to debian-repo..."
          curl -X POST \
            -H "Authorization: Bearer ${{ secrets.DEBIAN_REPO_TOKEN }}" \
            --data-binary "@$DEB_FILE" \
            "https://debs.myorgname.com/api/v1/dists/stable/main/upload" \
            -v
```

## GitHub Actions Example

Add this to `.github/workflows/release.yml`:

```yaml
name: Build and Push Debian Package

on:
  release:
    types: [published]

jobs:
  build-and-push:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Build Debian package
        run: |
          sudo apt-get update
          sudo apt-get install -y build-essential devscripts
          dpkg-buildpackage -b -uc -us

      - name: Push to debian-repo
        if: success()
        env:
          DEBIAN_REPO_URL: https://debs.myorgname.com
          DEBIAN_REPO_TOKEN: ${{ secrets.DEBIAN_REPO_TOKEN }}
        run: |
          DEB_FILE=$(ls ../*.deb | head -1)
          echo "📦 Uploading $DEB_FILE to debian-repo..."
          curl -X POST \
            -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
            --data-binary "@$DEB_FILE" \
            "$DEBIAN_REPO_URL/api/v1/dists/stable/main/upload"
```

## Jenkins Pipeline Example

```groovy
pipeline {
    agent any

    options {
        buildDiscarder(logRotator(numToKeepStr: '10'))
    }

    stages {
        stage('Build Package') {
            steps {
                sh '''
                    apt-get update
                    apt-get install -y build-essential devscripts
                    dpkg-buildpackage -b -uc -us
                '''
            }
        }

        stage('Upload to debian-repo') {
            when { branch 'main' }
            environment {
                DEBIAN_REPO_TOKEN = credentials('debian-repo-token')
            }
            steps {
                sh '''
                    DEB_FILE=$(ls ../*.deb | head -1)
                    echo "📦 Uploading $DEB_FILE to debian-repo..."
                    curl -X POST \
                        -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
                        --data-binary "@$DEB_FILE" \
                        "https://debs.myorgname.com/api/v1/dists/stable/main/upload"
                '''
            }
        }
    }
}
```

## GitLab CI Example

Add to `.gitlab-ci.yml`:

```yaml
build_and_push:
  image: ubuntu:22.04
  script:
    - apt-get update
    - apt-get install -y build-essential devscripts curl
    - dpkg-buildpackage -b -uc -us
    - |
      DEB_FILE=$(ls ../*.deb | head -1)
      echo "📦 Uploading $DEB_FILE to debian-repo..."
      curl -X POST \
        -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
        --data-binary "@$DEB_FILE" \
        "https://debs.myorgname.com/api/v1/dists/stable/main/upload"
  only:
    - tags
```

## Multi-Suite Deployment Strategy

### Approach 1: Direct to stable (for hotfixes)

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  --data-binary @package.deb \
  "https://debs.myorgname.com/api/v1/dists/stable/main/upload"
```

### Approach 2: Testing → Stable (for validated releases)

```bash
# 1. Upload to testing
curl -X POST -H "Authorization: Bearer $TOKEN" \
  --data-binary @package.deb \
  "https://debs.myorgname.com/api/v1/dists/testing/main/upload"

# 2. Run smoke tests in CI against testing
# ... test commands here ...

# 3. If tests pass, move to stable
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from_suite":"testing","to_suite":"stable","packages":["package-name*"]}' \
  "https://debs.myorgname.com/api/v1/dists/move"
```

### Approach 3: Testing → Staging → Production

```bash
# 1. Upload to testing
# 2. Integration tests on testing suite
# 3. Move to staging
# 4. Smoke tests on staging
# 5. Move to production/stable
```

## Handling Upload Failures

Implement retry logic:

```bash
# With retry
for attempt in {1..3}; do
  curl -X POST \
    -H "Authorization: Bearer $TOKEN" \
    --data-binary @package.deb \
    "https://debs.myorgname.com/api/v1/dists/stable/main/upload" \
    && echo "✓ Upload successful" && break

  if [ $attempt -lt 3 ]; then
    echo "⚠️ Upload failed, retrying in 10s..."
    sleep 10
  else
    echo "❌ Upload failed after 3 attempts"
    exit 1
  fi
done
```

Or using a shell script:

```bash
#!/bin/bash
set -e

DEB_FILE="$1"
SUITE="${2:-stable}"
COMPONENT="${3:-main}"

if [ ! -f "$DEB_FILE" ]; then
  echo "❌ File not found: $DEB_FILE"
  exit 1
fi

echo "📦 Uploading $DEB_FILE to $SUITE/$COMPONENT..."

curl -X POST \
  -H "Authorization: Bearer ${DEBIAN_REPO_TOKEN?Missing DEBIAN_REPO_TOKEN}" \
  --data-binary "@$DEB_FILE" \
  --fail \
  "https://debs.myorgname.com/api/v1/dists/$SUITE/$COMPONENT/upload"

echo "✓ Upload successful"
```

## Multi-Architecture Builds

For packages built for multiple architectures:

```bash
# Build for both amd64 and arm64
for arch in amd64 arm64; do
  dpkg-buildpackage -a$arch -b -uc -us
  DEB_FILE=$(ls ../package*_${arch}.deb | tail -1)

  curl -X POST \
    -H "Authorization: Bearer $TOKEN" \
    --data-binary "@$DEB_FILE" \
    "https://debs.myorgname.com/api/v1/dists/stable/main/upload"
done
```

## Monitoring & Alerts

### Check upload status

```bash
# View latest packages via JSON API
curl -s "https://debs.myorgname.com/index.json" | jq '.packages.stable'
```

### Set up alerts

Use the JSON API to detect failed uploads:

```bash
#!/bin/bash
# Check if our package version exists in stable

VERSION="1.0.0"
PACKAGE="my-package"

LATEST=$(curl -s "https://debs.myorgname.com/index.json" | \
  jq -r ".packages.stable.${PACKAGE}.latest_version")

if [ "$LATEST" != "$VERSION" ]; then
  echo "❌ ALERT: Expected version $VERSION, found $LATEST"
  # Send to Slack, PagerDuty, etc.
  exit 1
fi

echo "✓ Package $PACKAGE version $VERSION is live"
```

## Security Best Practices

1. **Never commit tokens** to Git
   ```bash
   # ❌ Bad
   TOKEN="ghp_abc123..." curl ...

   # ✅ Good (use CI secrets)
   curl -H "Authorization: Bearer ${{ secrets.DEBIAN_REPO_TOKEN }}" ...
   ```

2. **Rotate tokens regularly** (ask your admin)

3. **Use minimal scopes** — Request token with only necessary permissions:
   - `repos: [main]` (specific repository)
   - `suites: [testing,stable]` (specific suites)
   - `operations: [upload,remove]` (specific operations)

4. **Log upload results** for audit trails

5. **Verify package integrity** before upload:
   ```bash
   dpkg --info my-package_1.0.0_amd64.deb > /dev/null && \
     echo "✓ Package valid" || \
     echo "❌ Package invalid"
   ```

## Debugging Upload Failures

Enable verbose output:

```bash
curl -v -X POST \
  -H "Authorization: Bearer $TOKEN" \
  --data-binary @package.deb \
  "https://debs.myorgname.com/api/v1/dists/stable/main/upload" \
  2>&1 | tee upload.log
```

Check the response:

```
< HTTP/2 200
< content-type: application/json
{
  "status": "registered",
  "package": "my-package",
  "version": "1.0.0",
  "architecture": "amd64",
  "suite": "stable",
  "component": "main"
}
```

Common errors:

| Error | Cause | Fix |
|-------|-------|-----|
| 401 Unauthorized | Invalid/missing token | Check `DEBIAN_REPO_TOKEN` |
| 403 Forbidden | Token lacks permission | Admin should grant `upload` operation |
| 400 Bad Request | Malformed package | Verify `.deb` with `dpkg -I` |
| 409 Conflict | Suite fully drained (protected) | Add `?force=true` to remove endpoint |
| 413 Payload Too Large | Package exceeds size limit | Default: 512MB, ask admin to increase |

## See Also

- **[Uploading Packages](uploading.md)** — Upload methods and options
- **[User Guide](_index.md)** — Overview of all user tasks
- **[Admin Guide — Tokens & ACL](../admin-guide/authentication.md)** — How admins configure tokens
