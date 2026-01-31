#!/bin/sh

# Ensure a version argument is provided and it is in the correct format
if [ -z "$1" ] || ! echo "$1" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "Usage: $0 <version> (e.g., 0.1.0)"
    exit 1
fi

new_version=$1
export new_version

# Find all go.mod files and update the version for specified packages
find . -name 'go.mod' -exec sh -c '
    for file do
        echo "Processing $file"
        # Use sed to update the version of packages starting with github.com/dosco/graphjin
        # Note: -i "" for BSD/macOS sed compatibility, use -i for GNU/Linux
        # Update modules with /v[0-9] suffix (e.g., core/v3) to the new version
        sed -i"" -e "/github.com\/dosco\/graphjin\/.*\/v[0-9]/s/ v[0-9.-]*[^ ]*/ v$new_version/" "$file"
        # Update mongodriver (no version suffix) to v0.0.0 since it uses v0/v1 versioning
        sed -i"" -e "/github.com\/dosco\/graphjin\/mongodriver /s/ v[0-9.-]*[^ ]*/ v0.0.0/" "$file"
    done
' sh {} +

# Note: Git operations are now handled by GitHub Actions
# This script only updates the Go module versions
