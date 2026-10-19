#!/usr/bin/env bash
# Install the external CLI used for local agent development:
#   - graphjin  (always required; the metric agent runs `graphjin cli`)
#
# The agent itself (Ax Harness) is built from apps/ax-harness and ships in the
# agent image, so it needs no separate install here.
#
# Idempotent. macOS uses Homebrew where possible; Debian/Ubuntu uses direct
# installers. Other distros: read the body and adapt.
#
# Usage:
#   ./scripts/install-clis.sh
set -euo pipefail

GRAPHJIN_VERSION="${GRAPHJIN_VERSION:-3.21.13}"

SKIP_GRAPHJIN=false
for arg in "$@"; do
  case "$arg" in
    --skip-graphjin) SKIP_GRAPHJIN=true ;;
    -h|--help)
      sed -n '2,13p' "$0"; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

log() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
have() { command -v "$1" >/dev/null 2>&1; }

uname_s=$(uname -s)
case "$uname_s" in
  Darwin) os=macos ;;
  Linux)  os=linux ;;
  *) echo "unsupported OS: $uname_s" >&2; exit 1 ;;
esac

# ─── graphjin ──────────────────────────────────────────────────────────
if ! $SKIP_GRAPHJIN && ! have graphjin; then
  log "installing graphjin v${GRAPHJIN_VERSION}"
  if [ "$os" = macos ] && have brew; then
    brew install dosco/tap/graphjin
  else
    arch=$(uname -m)
    case "$arch" in
      x86_64|amd64) gj_arch=amd64 ;;
      aarch64|arm64) gj_arch=arm64 ;;
      *) echo "unsupported arch for graphjin: $arch" >&2; exit 1 ;;
    esac
    suffix="${os}_${gj_arch}"
    [ "$os" = macos ] && suffix="darwin_${gj_arch}"
    tmp=$(mktemp -d)
    curl -fsSL -o "$tmp/gj.tgz" \
      "https://github.com/dosco/graphjin/releases/download/v${GRAPHJIN_VERSION}/graphjin_${GRAPHJIN_VERSION}_${suffix}.tar.gz"
    sudo tar -xzf "$tmp/gj.tgz" -C /usr/local/bin graphjin
    rm -rf "$tmp"
  fi
  graphjin version
fi

log "done. installed:"
$SKIP_GRAPHJIN || { printf '  graphjin: '; have graphjin && graphjin version | head -1 || echo 'NOT FOUND'; }
