#!/bin/sh

# Ensure a version argument is provided and it is in the correct format
if [ -z "$1" ] || ! echo "$1" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
    echo "Usage: $0 <version> (e.g., 0.1.0)"
    exit 1
fi

new_version=$1

# Find all go.mod files and update the version for specified packages
find . -name 'go.mod' -exec sh -c '
    for file do
        echo "Processing $file"
        # Use sed to update the version of packages starting with github.com/dosco/graphjin
        # Note: -i "" for BSD/macOS sed compatibility, use -i for GNU/Linux
        sed -i"" -e "/github.com\/dosco\/graphjin\//s/v[0-9]*\.[0-9]*\.[0-9]*/v$new_version/" "$file"
    done
' sh {} +

# Note: Git operations are now handled by GitHub Actions
# This script only updates the Go module versions
