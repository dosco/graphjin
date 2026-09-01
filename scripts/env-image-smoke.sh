#!/usr/bin/env bash
# Build the environment image locally and drive it, publishing nothing.
#
# What this proves that a Go test cannot: that a container with no shell, no
# curl, no mounted files and a read-only working directory boots ready, serves
# the frozen public suite, answers its own healthcheck, and reports the same
# world on a second start. The numbers it prints are the ones the docs quote.
#
# Skips cleanly (exit 0) when docker or ko is missing, when the daemon is not
# running, or when the base image cannot be pulled — none of which are this
# repository's failures. Set KO_DEFAULTBASEIMAGE to build against a base you
# already hold.
set -euo pipefail

IMAGE="${IMAGE:-ko.local/graphjin-env}"
PORT="${PORT:-8098}"
PORT2="${PORT2:-8099}"
PORT3="${PORT3:-8100}"
NAME="gj-env-smoke"
NAME2="gj-env-smoke-2"
NAME3="gj-env-smoke-size"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

skip() { echo "SKIP: $*"; exit 0; }

cleanup() {
  docker rm -f "$NAME" "$NAME2" "$NAME3" >/dev/null 2>&1 || true
}
trap cleanup EXIT

command -v docker >/dev/null 2>&1 || skip "docker is not installed"
docker info >/dev/null 2>&1 || skip "the docker daemon is not running"
command -v ko >/dev/null 2>&1 || skip "ko is not installed (go install github.com/google/ko@latest)"
command -v curl >/dev/null 2>&1 || skip "curl is not installed"
command -v python3 >/dev/null 2>&1 || skip "python3 is not installed"
cleanup

VERSION="${VERSION:-v0.0.0-smoke}"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)"
# The Makefile's format, so the smoke exercises the date parsing a `make`
# build produces rather than only goreleaser's.
DATE="$(git -C "$ROOT" log -1 --format=%ci 2>/dev/null || echo '')"

echo "==> building ${IMAGE} with ko (role=env, version=${VERSION})"
REFS_FILE="$(mktemp "${TMPDIR:-/tmp}/gj-env-refs.XXXXXX")"
trap 'cleanup; rm -f "$REFS_FILE"' EXIT
set +e
case "$(uname -m)" in
  arm64|aarch64) PLATFORM="linux/arm64" ;;
  *)             PLATFORM="linux/amd64" ;;
esac
BUILD_OUTPUT="$(cd "$ROOT" && KO_DOCKER_REPO="${IMAGE%/*}" ko build --local --bare \
  --platform "$PLATFORM" \
  --image-refs "$REFS_FILE" \
  --ldflags "-s -w -X 'main.version=${VERSION}' -X 'main.commit=${COMMIT}' -X 'main.date=${DATE}' -X 'main.imageRole=env'" \
  ./cmd 2>&1)"
BUILD_STATUS=$?
set -e
if [ "$BUILD_STATUS" -ne 0 ]; then
  # Only a base-image fetch is somebody else's problem. Anything else is this
  # repository's failure and must not be skipped past — a pattern loose enough
  # to match an unrelated error turns a red build into a green skip.
  case "$BUILD_OUTPUT" in
    *"resolving base image"*|*"fetching base image"*|*"no such host"*|*"TLS handshake timeout"*|\
    *"connection refused"*|*"i/o timeout"*|*"UNAUTHORIZED"*)
      skip "could not reach the base image registry (set KO_DEFAULTBASEIMAGE to a local base)" ;;
  esac
  echo "$BUILD_OUTPUT" >&2
  exit 1
fi
REF="$(tail -n 1 "$REFS_FILE" | tr -d '[:space:]')"
[ -n "$REF" ] || { echo "ko printed no image reference:" >&2; echo "$BUILD_OUTPUT" >&2; exit 1; }
echo "==> built ${REF}"

# ko derives the app path from the import path's base, and this module is
# .../cmd/v3 — so the entrypoint is an expectation, not a fact. Print it.
ENTRYPOINT="$(docker inspect --format '{{join .Config.Entrypoint " "}}' "$REF")"
echo "==> entrypoint: ${ENTRYPOINT}"
[ -n "$ENTRYPOINT" ] || { echo "the image has no entrypoint" >&2; exit 1; }

boot() {
  local name="$1" port="$2"; shift 2
  local mount=(--tmpfs /tmp:size=2g)
  if [ "$name" = "$NAME3" ]; then
    mount=()
  fi
  echo "==> starting ${name} on :${port}"
  docker run -d --name "$name" -p "${port}:8090" ${mount[@]+"${mount[@]}"} "$@" "$REF" >/dev/null
  local waited=0
  until curl -sf "http://127.0.0.1:${port}/health" >/dev/null 2>&1; do
    if [ -z "$(docker ps -q -f name="^${name}$")" ]; then
      echo "${name} exited during boot:" >&2
      docker logs "$name" 2>&1 | tail -40 >&2
      return 1
    fi
    sleep 2
    waited=$((waited + 2))
    if [ "$waited" -ge 300 ]; then
      echo "${name} did not become healthy within 300s:" >&2
      docker logs "$name" 2>&1 | tail -40 >&2
      return 1
    fi
  done
  echo "==> ${name} healthy after ~${waited}s"
}

boot "$NAME" "$PORT"
HEALTH="$(curl -sf "http://127.0.0.1:${PORT}/health")"
echo "$HEALTH" | python3 -m json.tool

python3 - "$HEALTH" <<'PY'
import json, sys
health = json.loads(sys.argv[1])
caps, build = health.get("capabilities", {}), health.get("build", {})
def require(condition, message):
    if not condition:
        print("FAIL: " + message, file=sys.stderr)
        sys.exit(1)
require(health.get("status") == "ready", "status is %r" % health.get("status"))
require(caps.get("suite_source") == "public", "suite_source is %r; nothing was mounted" % caps.get("suite_source"))
require(health.get("tasks") == 113, "served %r tasks, want the whole frozen suite" % health.get("tasks"))
require(build.get("version"), "the image reports no version, so two runs cannot be told apart")
require(build.get("commit"), "the image reports no commit")
require(build.get("image_role") == "env", "image_role is %r" % build.get("image_role"))
require(caps.get("catalog_match") is True, "the suite does not describe the world it is served on")
require(caps.get("freeze_time"), "the clock is not pinned, so this tag measures a different thing every day")
require(caps.get("freeze_time_source") == "build", "freeze_time_source is %r" % caps.get("freeze_time_source"))
require(caps.get("data_anchor"), "no data anchor")
print("==> ready in %sms · anchor %s · frozen at %s (%s)" % (
    caps.get("boot_ms"), caps.get("data_anchor"), caps.get("freeze_time"), caps.get("freeze_time_source")))
PY

echo "==> tasks"
curl -sf "http://127.0.0.1:${PORT}/tasks" | python3 -c \
  'import json,sys; t=json.load(sys.stdin); print("   %d tasks, first: %s" % (len(t["tasks"]), t["tasks"][0]["slug"]))'

echo "==> one episode (no provider configured, so it is expected to be graded a failure)"
SLUG="$(curl -sf "http://127.0.0.1:${PORT}/tasks" | python3 -c 'import json,sys; print(json.load(sys.stdin)["tasks"][0]["slug"])')"
curl -sf -X POST "http://127.0.0.1:${PORT}/episodes" -d "{\"slug\":\"${SLUG}\"}" | python3 -c \
  'import json,sys; e=json.load(sys.stdin); print("   %s -> status=%s reward=%s" % (e["slug"], e["status"], e["reward"]))' \
  || echo "   episode request failed (expected without a provider)"

# The image has no shell and no curl, so this is the only healthcheck it can
# have. `docker exec` runs the entrypoint binary directly.
echo "==> healthcheck from inside the container"
docker exec "$NAME" $ENTRYPOINT env health
echo "==> explicit arguments still win"
docker run --rm "$REF" version | head -3

echo "==> a second container must serve the same world"
boot "$NAME2" "$PORT2"
ANCHOR1="$(curl -sf "http://127.0.0.1:${PORT}/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["dataset"]["data_anchor"])')"
ANCHOR2="$(curl -sf "http://127.0.0.1:${PORT2}/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["dataset"]["data_anchor"])')"
CATALOG1="$(curl -sf "http://127.0.0.1:${PORT}/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["dataset"]["catalog_hash"])')"
CATALOG2="$(curl -sf "http://127.0.0.1:${PORT2}/health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["dataset"]["catalog_hash"])')"
if [ "$ANCHOR1" != "$ANCHOR2" ] || [ "$CATALOG1" != "$CATALOG2" ]; then
  echo "two containers from one image serve different worlds: ${ANCHOR1}/${CATALOG1} vs ${ANCHOR2}/${CATALOG2}" >&2
  exit 1
fi
echo "==> both serve anchor ${ANCHOR1}, catalog ${CATALOG1}"

# How much writable space a pool actually needs, which is what the docs must
# state as the tmpfs size. Measured on a container started WITHOUT --tmpfs, so
# everything a boot writes lands in the container layer where docker can size
# it — the two above keep the mount, which is the arrangement being validated.
echo "==> measuring what a pool of worlds writes"
boot "$NAME3" "$PORT3"
docker ps --size --filter "name=^${NAME3}$" --format '   pool of 2 worlds writes {{.Size}}'
docker rm -f "$NAME3" >/dev/null 2>&1 || true

echo "OK: the environment image boots ready, serves the frozen suite, and measures one world."
