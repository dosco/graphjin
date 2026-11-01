#!/usr/bin/env bash
# Report and cap the aggregate compressed payload of a complete install.
# Bare image names resolve under ghcr.io/open-neko at TAG; fully-qualified
# refs are inspected as written. Layers are deduplicated by digest, matching
# what one architecture actually downloads rather than summing shared roots.
set -euo pipefail

tag="${1:?usage: check-image-stack-size.sh TAG ARCH MAX_MIB IMAGE...}"
arch="${2:?usage: check-image-stack-size.sh TAG ARCH MAX_MIB IMAGE...}"
max_mib="${3:?usage: check-image-stack-size.sh TAG ARCH MAX_MIB IMAGE...}"
shift 3

if (($# == 0)); then
  echo "at least one image name is required" >&2
  exit 2
fi

layer_file="$(mktemp)"
trap 'rm -f "$layer_file"' EXIT

for image in "$@"; do
  case "$image" in
    */*|*:*) ref="$image" ;;
    *) ref="ghcr.io/open-neko/${image}:${tag}" ;;
  esac
  manifest="$(docker buildx imagetools inspect --raw "$ref")"
  if jq -e '.manifests' >/dev/null <<<"$manifest"; then
    digest="$(
      jq -er --arg arch "$arch" '
        .manifests[]
        | select(.platform.os == "linux" and .platform.architecture == $arch)
        | select(.platform."os.version" == null)
        | .digest
      ' <<<"$manifest" | head -n 1
    )"
    image_name="${ref%%@*}"
    last_segment="${image_name##*/}"
    if [[ "$last_segment" == *:* ]]; then
      image_name="${image_name%:*}"
    fi
    manifest="$(docker buildx imagetools inspect --raw "${image_name}@${digest}")"
  fi
  image_bytes="$(jq -er '(.config.size // 0) + ([.layers[].size] | add // 0)' <<<"$manifest")"
  image_mib="$(awk -v bytes="$image_bytes" 'BEGIN { printf "%.1f", bytes / 1048576 }')"
  printf '%-22s %8s MiB\n' "$image" "$image_mib"
  jq -r '
    (.config | "\(.digest) \(.size)"),
    (.layers[] | "\(.digest) \(.size)")
  ' <<<"$manifest" >>"$layer_file"
done

unique_bytes="$(awk '!seen[$1]++ { total += $2 } END { printf "%.0f", total }' "$layer_file")"
unique_mib="$(awk -v bytes="$unique_bytes" 'BEGIN { printf "%.1f", bytes / 1048576 }')"
max_bytes="$((max_mib * 1024 * 1024))"

echo "unique compressed install stack: ${unique_mib} MiB (budget ${max_mib} MiB, linux/${arch})"
if ((unique_bytes > max_bytes)); then
  echo "install stack exceeds its compressed pull-size budget" >&2
  exit 1
fi
