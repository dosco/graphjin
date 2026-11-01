#!/usr/bin/env bash
# Install the CLI used for local agent development:
#   - graphjin  (always required; the metric agent runs `graphjin cli`). It
#               builds from graphjin/ as a role of the openneko binary.
#
# The agent itself (Ax Harness) is built from apps/ax-harness and is a role of
# the same binary, so it needs no separate install here.
#
# Idempotent. Needs Go 1.26.10 or later.
#
# Usage:
#   ./scripts/install-clis.sh
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"

SKIP_GRAPHJIN=false
for arg in "$@"; do
  case "$arg" in
    --skip-graphjin) SKIP_GRAPHJIN=true ;;
    -h|--help)
      sed -n '2,18p' "$0"; exit 0 ;;
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
# GraphJin builds from graphjin/ in this repository as a role of the openneko
# binary. Install that binary and a graphjin link to it.
if ! $SKIP_GRAPHJIN; then
  have go || { echo "graphjin needs Go to build from graphjin/; install Go 1.26.10 or later" >&2; exit 1; }
  log "building openneko with GraphJin from $REPO_ROOT/graphjin"
  bin_dir="${OPENNEKO_BIN_DIR:-/usr/local/bin}"
  tmp=$(mktemp -d)
  (cd "$REPO_ROOT/apps/openneko" && go build -o "$tmp/openneko" ./cmd/openneko)
  sudo install -m 0755 "$tmp/openneko" "$bin_dir/openneko"
  sudo ln -sf openneko "$bin_dir/graphjin"
  rm -rf "$tmp"
  graphjin version
fi

log "done. installed:"
$SKIP_GRAPHJIN || { printf '  graphjin: '; have graphjin && graphjin version | head -1 || echo 'NOT FOUND'; }
