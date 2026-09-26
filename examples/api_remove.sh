#!/bin/bash
# Example: Remove a package version from the repository
#
# When a package is removed:
# 1. Package is removed from the repository index
# 2. Release and Packages files are re-rendered
# 3. .deb file is deleted from pool storage (async)
# 4. Updated snapshot is persisted
#
# This is useful for removing broken packages, outdated versions, or
# cleaning up after accidents. The pool file is atomically deleted.

set -e

DEBIAN_REPO_URL="${DEBIAN_REPO_URL:-https://debs.myorgname.com}"
DEBIAN_REPO_TOKEN="${DEBIAN_REPO_TOKEN:-}"
COMPONENT="${COMPONENT:-main}"

if [ -z "$DEBIAN_REPO_TOKEN" ]; then
    echo "Error: DEBIAN_REPO_TOKEN environment variable not set"
    exit 1
fi

if [ $# -lt 1 ]; then
    echo "Usage: $0 <package-name> [version] [architecture] [suite]"
    echo ""
    echo "COMPONENT can be set as env var (default: main)"
    echo ""
    echo "Remove specific version and architecture:"
    echo "  $0 my-package 1.0.0 amd64"
    echo ""
    echo "Remove all architectures of a version:"
    echo "  $0 my-package 1.0.0"
    echo ""
    echo "Remove all versions of a package (requires ?force=true for protected suites):"
    echo "  $0 my-package '' '' stable 'true'"
    echo ""
    echo "Remove from non-default suite:"
    echo "  COMPONENT=contrib $0 my-package 1.0.0 amd64 testing"
    exit 1
fi

PKG_NAME="$1"
VERSION="${2:-}"
ARCH="${3:-}"
SUITE="${4:-stable}"
FORCE="${5:-false}"

# Build the URL path
PATH_PARTS="/api/v1/dists/$SUITE/$COMPONENT/remove/$PKG_NAME"

if [ -n "$VERSION" ]; then
    PATH_PARTS="$PATH_PARTS/$VERSION"
    if [ -n "$ARCH" ]; then
        PATH_PARTS="$PATH_PARTS/$ARCH"
    fi
fi

URL="$DEBIAN_REPO_URL$PATH_PARTS"

# Add force flag if needed (for protected suites)
if [ "$FORCE" = "true" ]; then
    URL="$URL?force=true"
fi

echo "Removing $PKG_NAME from $SUITE/$COMPONENT..."
echo "URL: $URL"
echo ""
echo "This will:"
echo "  1. Remove package from repository index"
echo "  2. Re-render Release and Packages metadata"
echo "  3. Delete .deb file from pool storage"
echo "  4. Persist updated snapshot"
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE \
    -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
    "$URL")

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)

if [ "$HTTP_CODE" = "409" ]; then
    echo "Error: HTTP $HTTP_CODE - Protected suite cannot be fully emptied"
    echo "$BODY"
    echo ""
    echo "To force removal, add ?force=true parameter and ensure your token has 'unprotect' grant"
    exit 1
elif [ "$HTTP_CODE" != "200" ]; then
    echo "Error: HTTP $HTTP_CODE"
    echo "$BODY"
    exit 1
fi

echo "Success! Package removed and pool file deleted."
echo "$BODY" | jq .
