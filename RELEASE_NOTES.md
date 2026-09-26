# Release Notes: v0.5.0

**Release Date**: 2026-09-26
**Git Tag**: `v0.5.0`
**Docker Image**: `ghcr.io/rossigee/debian-repo:v0.5.0`

## Summary

v0.5.0 introduces **atomic pool file deletion** for the remove deb API. When packages are removed from the repository, their `.deb` files are now automatically deleted from MinIO pool storage, eliminating orphaned files and ensuring consistent state between the package index and pool contents.

## Major Features

### Atomic Pool File Deletion

**Problem**: Previously, when removing packages via the remove API, only the index was updated and Release/Packages metadata was re-rendered. The actual `.deb` files in the MinIO pool remained, leading to:
- Orphaned pool files consuming storage
- Inconsistency between index and pool contents
- Manual cleanup required via `repoctl reconcile --prune-orphans`

**Solution**:
1. **Before index mutation**: Collect all `.deb` file paths matching the removal criteria
2. **Index mutation**: Remove package from index and re-render Release/Packages metadata
3. **Async cleanup**: Delete collected pool files from MinIO storage (non-blocking)
4. **Persist snapshot**: Updated repository state is saved

**Behavior**:
- **Request returns immediately** after index update (atomic pointer swap)
- **Pool files deleted asynchronously** in background goroutine
- **Error resilience**: Individual file deletion failures are logged but don't fail the operation
- **Cascading cleanup**: Empty components/suites are automatically removed
- **Flexible removal**: Supports removing specific version/architecture or all versions of a package

**Implementation**:
- `internal/apiserver/ci_handlers.go`: Enhanced `handleRemovePackage` function
  - Lines 612-632: Collect pool paths before deletion
  - Lines 646-666: Async pool file deletion and snapshot persistence
- Pool path format: `pool/{component}/{package}_{version}_{architecture}.deb`
- Uses existing MinIO client (`rp.MinioClient.RemoveObject`)
- Background context prevents cancellation when request completes

**Testing**:
- `TestHandleRemovePackagePoolFileDeletion`: Verifies pool path collection for specific version/arch
- `TestHandleRemovePackagePoolFileMultipleVersions`: Tests removal of multiple architectures
- All existing tests pass with race detector
- No breaking changes to public APIs

### Documentation Updates

- **API Reference** (`docs/content/api/_index.md`): Documented pool file deletion process
- **User Guide** (`docs/content/user-guide/managing.md`): Three removal scenarios with examples
- **Example Script** (`examples/api_remove.sh`): Updated with correct endpoint and improved error handling
- **Architecture Guide** (`AGENTS.md`): Documented handleRemovePackage implementation details

## API Changes

### DELETE /api/v1/dists/{suite}/{component}/remove/{package}[/{version}[/{arch}]]

**Behavior**:
- Removes package from index
- Re-renders Release/Packages metadata
- **Deletes `.deb` file from pool storage** (async)
- Persists updated snapshot

**Examples**:
```bash
# Remove specific version/architecture (pool file deleted: pool/main/nginx_1.20.0_amd64.deb)
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx/1.20.0/amd64

# Remove all architectures of a version
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx/1.20.0?force=true

# Remove all versions (requires force flag + unprotect grant for protected suites)
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  "https://debs.myorgname.com/api/v1/dists/stable/main/remove/nginx?force=true"
```

**Response (200)**:
```json
{
  "status": "removed",
  "package": "nginx",
  "suite": "stable"
}
```

**Protection Handling**:
- Protected suites cannot be fully drained without `?force=true` and `unprotect` operation grant
- Prevents accidental complete depletion of production suites

## Migration Notes

**No migrations required**. The feature:
- Is fully backward compatible
- Automatically handles packages added before v0.5.0
- Uses existing MinIO client
- Integrates seamlessly with existing removal flow

## Performance

- **Request latency**: Unaffected (async deletion happens after response)
- **Index operations**: No overhead (pool paths collected in single pass before mutation)
- **Pool cleanup**: Happens asynchronously, non-blocking to apt clients
- **Snapshot persistence**: Async, doesn't block request

## Deployment Instructions

### Prerequisites
- Docker and Docker Compose on vault.internal
- MinIO access (already configured)
- GPG signing key and passphrase

### Step 1: Build and Push Docker Image

```bash
cd ~/go/src/github.com/rossigee/debian-repo
git checkout v0.5.0

# Build image locally
docker build -t ghcr.io/rossigee/debian-repo:v0.5.0 .

# Push to registry
docker push ghcr.io/rossigee/debian-repo:v0.5.0
```

### Step 2: Deploy to vault.internal

```bash
ssh vault.internal

# Pull the new image
docker pull ghcr.io/rossigee/debian-repo:v0.5.0

# Restart the container (snapshot will be reused if compatible)
cd /opt/docker-compose/debian-repo
docker-compose restart debian-repo

# Wait for startup (~5 seconds)
sleep 5

# Verify it started
docker-compose ps
docker-compose logs debian-repo | tail -20
```

### Step 3: Verify Pool File Deletion

Test the removal endpoint to verify pool files are being deleted:

```bash
# Remove a non-production package
curl -X DELETE \
  -H "Authorization: Bearer $REPO_ADMIN_TOKEN" \
  https://debs.myorgname.com/api/v1/dists/testing/main/remove/test-package/1.0.0/amd64

# Check MinIO to verify pool file was deleted
mc ls minio/debs-myorgname/pool/main/ | grep test-package
# Should show no results (file was deleted)
```

## Breaking Changes

None. This is a purely additive feature.

## Known Issues

None.

## Testing

All tests pass with race detector:
```bash
go test -race ./...
```

## Previous Release

See [v0.4.0 Release Notes](#) for checksum sidecar caching feature.

---

**For questions or issues**: Contact the infrastructure team or file an issue at https://github.com/rossigee/debian-repo
