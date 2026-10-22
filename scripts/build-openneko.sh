#!/bin/sh
# Builds the openneko binary, which carries GraphJin (from graphjin/),
# ax-harness, lazy-service, serve and sandbox as roles. Used by the image
# builds. Usage: scripts/build-openneko.sh OUTPUT
# OPENNEKO_VERSION and OPENNEKO_COMMIT name the build; both default to
# "container".
set -eu

out="$1"
root="$(cd "$(dirname "$0")/.." && pwd)"
graphjin_version="$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$root/graphjin/package.json")"
test -n "$graphjin_version"
version_pkg=github.com/open-neko/neko/apps/openneko/internal/version

cd "$root/apps/openneko"
CGO_ENABLED=0 GOWORK=off go build -trimpath \
  -ldflags "-s -w \
    -X ${version_pkg}.Version=${OPENNEKO_VERSION:-container} \
    -X ${version_pkg}.Commit=${OPENNEKO_COMMIT:-container} \
    -X ${version_pkg}.CommitTimestamp=container \
    -X ${version_pkg}.GraphJinVersion=${graphjin_version} \
    -X github.com/dosco/graphjin/serv/v3.version=${graphjin_version}" \
  -o "$out" ./cmd/openneko
"$out" graphjin version | grep -F "GraphJin ${graphjin_version}" >/dev/null
