#!/bin/bash
# Example: Upload a .deb package to the repository

set -e

DEBIAN_REPO_URL="${DEBIAN_REPO_URL:-https://debs.myorgname.com}"
DEBIAN_REPO_TOKEN="${DEBIAN_REPO_TOKEN:-}"

if [ -z "$DEBIAN_REPO_TOKEN" ]; then
    echo "Error: DEBIAN_REPO_TOKEN environment variable not set"
    exit 1
fi

if [ $# -lt 1 ]; then
    echo "Usage: $0 <deb-file> [suite] [component]"
    echo ""
    echo "Examples:"
    echo "  $0 my-package_1.0.0_amd64.deb"
    echo "  $0 my-package_1.0.0_amd64.deb stable main"
    echo "  $0 my-package_1.0.0_amd64.deb testing contrib"
    exit 1
fi

DEB_FILE="$1"
SUITE="${2:-stable}"
COMPONENT="${3:-main}"

if [ ! -f "$DEB_FILE" ]; then
    echo "Error: File not found: $DEB_FILE"
    exit 1
fi

echo "Uploading $DEB_FILE to $DEBIAN_REPO_URL/$SUITE/$COMPONENT..."

RESPONSE=$(curl -s -w "\n%{http_code}" -X POST \
    -H "Authorization: Bearer $DEBIAN_REPO_TOKEN" \
    --data-binary "@$DEB_FILE" \
    "$DEBIAN_REPO_URL/api/v1/dists/$SUITE/$COMPONENT/upload")

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | head -n-1)

if [ "$HTTP_CODE" != "200" ]; then
    echo "Error: HTTP $HTTP_CODE"
    echo "$BODY"
    exit 1
fi

echo "Success!"
echo "$BODY" | jq .
