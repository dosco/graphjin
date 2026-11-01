#!/bin/bash
# One-time setup for the neko-vm host (demo.openneko.app).
# Run as root (or via sudo) on the VM. Idempotent — re-running is safe.
#
# Usage:  sudo bash scripts/neko-vm-setup.sh [DEPLOY_USER]
# DEPLOY_USER defaults to `neko` — override if your VM uses a different account.

set -euo pipefail

DEPLOY_USER="${1:-neko}"
# Drop-in written by older versions of this script for the removed plugin
# system. Removed on re-run.
LEGACY_DROPIN="/etc/systemd/system/neko-worker.service.d/openneko-plugins.conf"
SUDOERS="/etc/sudoers.d/neko-deploy"

if [ "$(id -u)" -ne 0 ]; then
  echo "[setup] must run as root (try: sudo bash $0 $*)" >&2
  exit 1
fi

id -u "$DEPLOY_USER" >/dev/null 2>&1 || { echo "[setup] user '$DEPLOY_USER' missing" >&2; exit 1; }
echo "[setup] deploy user: $DEPLOY_USER"

if [ -f "$LEGACY_DROPIN" ]; then
  rm -f "$LEGACY_DROPIN"
  systemctl daemon-reload
  echo "[setup] removed legacy plugin drop-in $LEGACY_DROPIN"
fi

ARCH=$(uname -m)
case "$ARCH" in
  x86_64) GOARCH=amd64 ;;
  aarch64|arm64) GOARCH=arm64 ;;
  *) echo "[setup] unsupported arch: $ARCH" >&2; exit 1 ;;
esac
LATEST=$(curl -fsSL https://api.github.com/repos/open-neko/openneko/releases/latest \
  | grep -oE '"tag_name":[[:space:]]*"[^"]+"' | head -1 | cut -d'"' -f4)
WANTED="${LATEST#v}"
INSTALLED=$(/usr/local/bin/openneko version --short 2>/dev/null || echo none)
if [ "$INSTALLED" != "$WANTED" ]; then
  TMP=$(mktemp -d)
  curl -fsSL -o "$TMP/openneko.tar.gz" \
    "https://github.com/open-neko/openneko/releases/download/$LATEST/openneko_${WANTED}_linux_${GOARCH}.tar.gz"
  tar -xzf "$TMP/openneko.tar.gz" -C "$TMP" openneko
  install -m 0755 "$TMP/openneko" /usr/local/bin/openneko
  rm -rf "$TMP"
  echo "[setup] installed openneko $LATEST (was: $INSTALLED)"
else
  echo "[setup] openneko $LATEST already current"
fi

cat > "$SUDOERS" <<EOF
# Managed by scripts/neko-vm-setup.sh — re-run to refresh.
Defaults:$DEPLOY_USER !requiretty
$DEPLOY_USER ALL=(root) NOPASSWD: /usr/bin/systemctl restart neko-web
$DEPLOY_USER ALL=(root) NOPASSWD: /usr/bin/systemctl restart neko-worker
EOF
chmod 0440 "$SUDOERS"
visudo -c -f "$SUDOERS"
echo "[setup] sudoers ensured at $SUDOERS"

systemctl restart neko-worker
echo "[setup] done. neko-worker restarted with new env."
