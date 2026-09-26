#!/bin/bash
# Example: Remove a package version from the repository

set -e

DEBIAN_REPO_URL="${DEBIAN_REPO_URL:-https://debs.myorgname.com}"
DEBIAN_REPO_TOKEN="${DEBIAN_REPO_TOKEN:-}"

if [ -z "$DEBIAN_REPO_TOKEN" ]; then
    echo "Error: DEBIAN_REPO_TOKEN environment variable not set"
    exit 1
fi

if [ $# -lt 2 ]; then
    echo "Usage: $0 <package-name> [version] [architecture] [suite]"
    echo ""
    echo "Remove entire package:"
    echo "  $0 my-package"
    echo ""
    echo "Remove specific version (all architectures):"
    echo "  $0 my-package 1.0.0"
    echo ""
    echo "Remove specific version and architecture:"
    echo "  $0 my-package 1.0.0 amd64"
    echo ""
    echo "Remove from non-default suite:"
    echo "  $0 my-package 1.0.0 amd64 testing"
    exit 1
fi

PKG_NAME="$1"
VERSION="${2:-}"
ARCH="${3:-}"
SUITE="${4:-stable}"

# Build the URL path
if [ -n "$VERSION" ] && [ -n "$ARCH" ]; then
    # Specific version and architecture
    URL="$DEBIAN_REPO_URL/api/v1/dists/$SUITE/remove/$PKG_NAME/$VERSION/$ARCH"
elif [ -n "$VERSION" ]; then
    # Specific version (all architectures)
    URL="$DEBIAN_REPO_URL/api/v1/dists/$SUITE/remove/$PKG_NAME/$VERSION"
else
    # All versions of the package
    URL="$DEBIAN_REPO_URL/api/v1/dists/$SUITE/remove/$PKG_NAME"
fi

echo "Removing $PKG_NAME from $SUITE..."
echo "URL: $URL"
echo ""

RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
    "$URL")

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)

if [ "$HTTP_CODE" != "200" ]; then
    echo "Error: HTTP $HTTP_CODE"
    echo "$BODY"
    exit 1
fi

echo "Success!"
echo "$BODY" | jq .
