#!/usr/bin/env bash
# Enforce the compressed pull-size budget of one pushed Linux image manifest.
set -euo pipefail

ref="${1:?usage: check-image-size.sh IMAGE@DIGEST MAX_MIB ARCH}"
max_mib="${2:?usage: check-image-size.sh IMAGE@DIGEST MAX_MIB ARCH}"
arch="${3:?usage: check-image-size.sh IMAGE@DIGEST MAX_MIB ARCH}"

raw="$(docker buildx imagetools inspect --raw "$ref")"
if jq -e '.manifests' >/dev/null <<<"$raw"; then
  child_digest="$(
    jq -er --arg arch "$arch" '
      .manifests[]
      | select(.platform.os == "linux" and .platform.architecture == $arch)
      | select(.platform."os.version" == null)
      | .digest
    ' <<<"$raw" | head -n 1
  )"
  image_name="${ref%%@*}"
  raw="$(docker buildx imagetools inspect --raw "${image_name}@${child_digest}")"
fi

size_bytes="$(jq -er '(.config.size // 0) + ([.layers[].size] | add // 0)' <<<"$raw")"
max_bytes="$((max_mib * 1024 * 1024))"
size_mib="$(awk -v bytes="$size_bytes" 'BEGIN { printf "%.1f", bytes / 1048576 }')"

echo "compressed image size: ${size_mib} MiB (budget ${max_mib} MiB) — ${ref}"
if ((size_bytes > max_bytes)); then
  echo "image exceeds its compressed pull-size budget" >&2
  exit 1
fi
