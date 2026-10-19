#!/bin/sh
# Starts the development backing services in Docker. Web and worker run on
# the host with hot reload: `pnpm dev`.
set -eu

. scripts/dev-env.sh

compose() {
  docker compose -f compose.yml -f compose.openshell.yml -f compose.adventureworks.yml -f compose.graphjin-agent.yml -f compose.dev.yml "$@"
}

mkdir -p "$OPENNEKO_DEV_STATE/graphjin" "$OPENNEKO_DEV_STATE/records-graphjin" "$OPENNEKO_CONFIG_VOLUME" "$OPENSHELL_STATE_DIR"
printf '%s\n' "$COMPOSE_PROJECT_NAME" > "$OPENNEKO_DEV_STATE/compose-project"
if [ ! -s .openneko/development-backup-key ]; then
  (umask 077 && openssl rand -hex 32 > .openneko/development-backup-key)
fi

# Host processes validate GraphJin configs with the graphjin role of the
# openneko binary, built from graphjin/ in this checkout.
mkdir -p "$OPENNEKO_DEV_STATE/bin"
(cd apps/openneko && go build -o "$OPENNEKO_DEV_STATE/bin/openneko" ./cmd/openneko)
ln -sf openneko "$OPENNEKO_DEV_STATE/bin/graphjin"

# Host processes start agent sandboxes with the pinned OpenShell CLI.
openshell_version="$(sed -n 's/^ARG OPENSHELL_VERSION=\(.*\)$/\1/p' Dockerfile)"
if ! "$OPENNEKO_DEV_STATE/bin/openshell" --version 2>/dev/null | grep -qx "openshell $openshell_version"; then
  case "$(uname -s)-$(uname -m)" in
    Darwin-arm64) target=aarch64-apple-darwin ;;
    Linux-x86_64) target=x86_64-unknown-linux-musl ;;
    Linux-aarch64 | Linux-arm64) target=aarch64-unknown-linux-musl ;;
    *) echo "OpenShell $openshell_version has no CLI build for $(uname -s) $(uname -m)" >&2; exit 1 ;;
  esac
  asset="openshell-$target.tar.gz"
  download="$(mktemp -d)"
  base="https://github.com/NVIDIA/OpenShell/releases/download/v$openshell_version"
  curl -fsSL -o "$download/$asset" "$base/$asset"
  curl -fsSL -o "$download/checksums.txt" "$base/openshell-checksums-sha256.txt"
  (cd "$download" && grep " $asset\$" checksums.txt | shasum -a 256 -c -)
  mkdir -p "$OPENNEKO_DEV_STATE/bin"
  tar -xzf "$download/$asset" -C "$OPENNEKO_DEV_STATE/bin" openshell
  rm -rf "$download"
fi

compose up -d --wait neko-db records-db
(cd apps/openneko && go run ./cmd/openneko migrate)

# The OpenShell gateway signs in as its own role. Give it the password the
# gateway container carries, or every sandbox start fails.
compose exec -T neko-db psql -U neko -d neko -v ON_ERROR_STOP=1 \
  -c "DO \$\$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'openshell') THEN CREATE ROLE openshell LOGIN; END IF; END \$\$" \
  -c "ALTER ROLE openshell WITH LOGIN PASSWORD '$OPENNEKO_OPENSHELL_DB_PASSWORD'" \
  -c "GRANT neko TO openshell" > /dev/null

# GraphJin reads its auth key only at start, so the per-org secret goes into
# agentic.yml before the server starts.
compose run --rm --no-deps graphjin-config-init
GRAPHJIN_SOURCES_CONFIG="$OPENNEKO_GRAPHJIN_CONFIG" \
  pnpm --filter @neko/worker exec tsx ../../packages/llm/src/graphjin/init-secret.mjs

# Compose reuses a built image after the GraphJin pin moves. Rebuild each
# GraphJin image that reports another version, or the worker stops at start.
for service in neko-graphjin graphjin records-graphjin; do
  if ! compose run --rm --no-deps -T --entrypoint graphjin "$service" version 2>/dev/null \
    | grep -q "GraphJin $graphjin_version"; then
    echo "Rebuilding $service for GraphJin $graphjin_version"
    compose build "$service"
  fi
done

compose up -d \
  neko-graphjin graphjin records-graphjin records-watch-graphjin \
  embedding librarian agent-image openshell-gateway \
  adventureworks-simulator adventureworks-scenario-injector

# Host processes reach the OpenShell gateway on its loopback port.
registration="${XDG_CONFIG_HOME:-$HOME/.config}/openshell/gateways/$OPENSHELL_GATEWAY"
mkdir -p "$registration/mtls"
cp "$OPENSHELL_STATE_DIR/pki/ca.crt" "$registration/mtls/ca.crt"
cp "$OPENSHELL_STATE_DIR/pki/client/tls.crt" "$registration/mtls/tls.crt"
(umask 077 && cp "$OPENSHELL_STATE_DIR/pki/client/tls.key" "$registration/mtls/tls.key")
printf '{"name":"%s","gateway_endpoint":"https://127.0.0.1:%s","is_remote":false,"gateway_port":0,"auth_mode":"mtls"}\n' \
  "$OPENSHELL_GATEWAY" "${OPENSHELL_PORT:-18080}" > "$registration/metadata.json"

# Sandboxes call the host broker; OpenShell needs the host address as an IP.
docker run --rm alpine:3.22 getent hosts host.docker.internal 2>/dev/null \
  | awk '{ print $1; exit }' > "$OPENNEKO_DEV_STATE/broker-host" || true

echo "Backing services are up. Start web and worker with: pnpm dev"
