#!/usr/bin/env bash
# Install the two CLIs used for local agent development:
#   - graphjin  (always required; the metric agent runs `graphjin cli`). It
#               builds from graphjin/ as a role of the openneko binary.
#   - hermes    (the OpenNeko agent runtime)
#
# Idempotent. Pass --skip-hermes for GraphJin-only development. macOS uses
# Homebrew where possible; Debian/Ubuntu uses apt +
# direct installers. Other distros: read the body and adapt.
#
# Usage:
#   ./scripts/install-clis.sh                # install both
#   ./scripts/install-clis.sh --skip-hermes  # graphjin only
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"
# Pin Hermes to the same ref baked into the Dockerfile so local-dev installs
# match what ships in the container image. v2026.8.31 / v0.21.0.
HERMES_AGENT_REF="${HERMES_AGENT_REF:-29112bef099274229cadff79cdff7bf7b99c4b77}"
HERMES_AGENT_VERSION="0.21.0"

SKIP_GRAPHJIN=false
SKIP_HERMES=false
for arg in "$@"; do
  case "$arg" in
    --skip-graphjin) SKIP_GRAPHJIN=true ;;
    --skip-hermes)   SKIP_HERMES=true ;;
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
  have go || { echo "graphjin needs Go to build from graphjin/; install Go 1.25 or later" >&2; exit 1; }
  log "building openneko with GraphJin from $REPO_ROOT/graphjin"
  bin_dir="${OPENNEKO_BIN_DIR:-/usr/local/bin}"
  tmp=$(mktemp -d)
  (cd "$REPO_ROOT/apps/openneko" && go build -o "$tmp/openneko" ./cmd/openneko)
  sudo install -m 0755 "$tmp/openneko" "$bin_dir/openneko"
  sudo ln -sf openneko "$bin_dir/graphjin"
  rm -rf "$tmp"
  graphjin version
fi

# ─── hermes (Nous Research) ────────────────────────────────────────────
# Hermes 0.21 deliberately blocks wheel builds. Install the pinned checkout in
# editable mode, matching its supported source layout and the Docker image.
# Check the active executable so this also replaces an older installation.
hermes_matches_version() {
  have hermes && hermes --version 2>/dev/null | grep -Eq '(^|[^0-9])0\.21\.0([^0-9]|$)'
}

if ! $SKIP_HERMES && ! hermes_matches_version; then
  log "installing hermes (Nous Research) @ ${HERMES_AGENT_REF:0:8}"
  if [ "$os" = linux ] && have apt-get; then
    sudo apt-get update -qq
    sudo apt-get install -y -qq python3 python3-venv python3-dev libffi-dev git patch build-essential ripgrep ffmpeg
  fi
  if ! have uv; then
    log "installing uv"
    if [ "$os" = macos ] && have brew; then
      brew install uv
    else
      curl -LsSf https://astral.sh/uv/install.sh | sudo env UV_INSTALL_DIR=/usr/local/bin UV_NO_MODIFY_PATH=1 sh
    fi
  fi

  hermes_tools_dir="$(uv tool dir)"
  hermes_tool_root="$hermes_tools_dir/hermes-agent"
  hermes_source_root="$hermes_tools_dir/hermes-agent-openneko-${HERMES_AGENT_VERSION}-${HERMES_AGENT_REF:0:12}-patchset5"
  hermes_bin_dir="$(uv tool dir --bin)"
  case "$hermes_tools_dir" in
    ""|"/") echo "refusing unsafe uv tool directory: $hermes_tools_dir" >&2; exit 1 ;;
  esac
  mkdir -p "$hermes_tools_dir" "$hermes_bin_dir"
  if [ ! -d "$hermes_source_root/.git" ]; then
    if [ -e "$hermes_source_root" ]; then
      echo "hermes source path exists but is not a managed checkout: $hermes_source_root" >&2
      exit 1
    fi
    git init "$hermes_source_root"
    git -C "$hermes_source_root" remote add origin https://github.com/NousResearch/hermes-agent.git
    git -C "$hermes_source_root" fetch --depth 1 origin "$HERMES_AGENT_REF"
    git -C "$hermes_source_root" checkout --detach FETCH_HEAD
  fi
  if [ "$(git -C "$hermes_source_root" rev-parse HEAD)" != "$HERMES_AGENT_REF" ]; then
    echo "managed hermes checkout is not pinned to $HERMES_AGENT_REF" >&2
    exit 1
  fi
  if ! grep -q '"reasoning_config": resolve_reasoning_config' "$hermes_source_root/acp_adapter/session.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-reasoning-config.patch"
  fi
  if ! grep -q 'make_interim_message_cb' "$hermes_source_root/acp_adapter/events.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-interim-messages.patch"
  fi
  if ! grep -q '_emit_unstreamed_anthropic_reasoning' "$hermes_source_root/agent/chat_completion_helpers.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-anthropic-reasoning.patch"
  fi
  if ! grep -q 'OPENNEKO_HERMES_NATIVE_DELEGATION' "$hermes_source_root/acp_adapter/session.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-native-delegation-policy.patch"
  fi
  if ! grep -q 'usage_getter' "$hermes_source_root/acp_adapter/events.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-tool-usage.patch"
  fi
  if ! grep -q 'MCP completions are absent' "$hermes_source_root/acp_adapter/events.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-mcp-output.patch"
  fi
  if ! grep -q '_DATED_OFFICIAL_DOCS_PRICING' "$hermes_source_root/agent/usage_pricing.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-cost.patch"
  fi
  if ! grep -q 'OPENNEKO_HERMES_MAX_ITERATIONS' "$hermes_source_root/acp_adapter/session.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_source_root" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-run-budget.patch"
  fi
  uv venv --clear "$hermes_tool_root" --python 3.11
  UV_PROJECT_ENVIRONMENT="$hermes_tool_root" \
    uv sync --project "$hermes_source_root" --locked --no-dev --extra acp --extra mcp --extra anthropic
  ln -sf "$hermes_tool_root/bin/hermes" "$hermes_bin_dir/hermes"

  hash -r
  if ! hermes_matches_version; then
    echo "hermes install completed but v${HERMES_AGENT_VERSION} is not active on PATH" >&2
    exit 1
  fi
elif ! $SKIP_HERMES; then
  log "hermes already matches v${HERMES_AGENT_VERSION}"
fi

if ! $SKIP_HERMES; then
  hermes_tool_root="$(uv tool dir)/hermes-agent"
  hermes_python="$hermes_tool_root/bin/python"
  hermes_site="$("$hermes_python" -c 'import pathlib, acp_adapter; print(pathlib.Path(acp_adapter.__file__).resolve().parent.parent)')"
  hermes_session="$hermes_site/acp_adapter/session.py"
  if [ ! -f "$hermes_session" ]; then
    echo "hermes v${HERMES_AGENT_VERSION} ACP session adapter was not installed at $hermes_session" >&2
    exit 1
  fi
  if ! grep -q '"reasoning_config": resolve_reasoning_config' "$hermes_session"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-reasoning-config.patch"
  fi
  hermes_events="$hermes_site/acp_adapter/events.py"
  if ! grep -q 'make_interim_message_cb' "$hermes_events"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-interim-messages.patch"
  fi
  hermes_chat_helpers="$hermes_site/agent/chat_completion_helpers.py"
  if ! grep -q '_emit_unstreamed_anthropic_reasoning' "$hermes_chat_helpers"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-anthropic-reasoning.patch"
  fi
  if ! grep -q 'OPENNEKO_HERMES_NATIVE_DELEGATION' "$hermes_session"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-native-delegation-policy.patch"
  fi
  if ! grep -q 'usage_getter' "$hermes_events"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-tool-usage.patch"
  fi
  if ! grep -q 'MCP completions are absent' "$hermes_events"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-mcp-output.patch"
  fi
  if ! grep -q '_DATED_OFFICIAL_DOCS_PRICING' "$hermes_site/agent/usage_pricing.py"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-cost.patch"
  fi
  if ! grep -q 'OPENNEKO_HERMES_MAX_ITERATIONS' "$hermes_session"; then
    patch --batch --forward --fuzz=0 -d "$hermes_site" -p1 \
      < "$REPO_ROOT/scripts/patches/hermes-acp-run-budget.patch"
  fi
  "$hermes_python" -c \
    "from acp_adapter.session import _openneko_run_budget; import os; os.environ['OPENNEKO_HERMES_MAX_ITERATIONS']='60'; assert _openneko_run_budget()['max_iterations'] == 60, 'Hermes ACP run budget missing'"
  "$hermes_python" -c \
    "import hermes_cli; assert hermes_cli.__version__ == '${HERMES_AGENT_VERSION}'"
  "$hermes_python" -c \
    "import openai; assert openai.__version__, 'Hermes OpenAI provider SDK missing'"
  "$hermes_python" -c \
    "import anthropic; assert anthropic.__version__, 'Hermes Anthropic provider SDK missing'"
  "$hermes_python" -c \
    "from mcp.types import CallToolResult; result = CallToolResult(content=[]); assert hasattr(result, 'is_error')"
  "$hermes_python" -c \
    "from acp_adapter.session import SessionManager; import inspect; source = inspect.getsource(SessionManager._make_agent); assert 'reasoning_config' in source and 'resolve_reasoning_config' in source, 'Hermes ACP must pass configured reasoning into AIAgent'"
  "$hermes_python" -c \
    "from acp_adapter.server import HermesACPAgent; import inspect; source = inspect.getsource(HermesACPAgent.prompt); assert 'usage=usage' in source, 'Hermes ACP prompt response must expose exact turn usage'"
  "$hermes_python" -c \
    "from acp_adapter.events import make_interim_message_cb; from acp_adapter.server import HermesACPAgent; import inspect; source = inspect.getsource(HermesACPAgent.prompt); assert 'interim_assistant_callback' in source and 'pending_streamed_message.append(text)' in source and 'raw_interim_cb(text, already_streamed=False)' in source and 'not streamed_message' not in source; assert callable(make_interim_message_cb), 'Hermes ACP buffered interim callback missing'"
  "$hermes_python" -c \
    "from agent import chat_completion_helpers; from pathlib import Path; source = Path(chat_completion_helpers.__file__).read_text(); assert '_emit_unstreamed_anthropic_reasoning' in source and 'reasoning_was_streamed' in source, 'Hermes ACP Anthropic reasoning fallback missing'"
  "$hermes_python" -c \
    "from acp_adapter.session import _openneko_disabled_toolsets; import os; os.environ['OPENNEKO_HERMES_NATIVE_DELEGATION']='disabled'; assert _openneko_disabled_toolsets() == ['delegation'], 'Hermes ACP native delegation policy missing'"
  "$hermes_python" "$REPO_ROOT/scripts/test-hermes-acp-tool-usage.py"
  "$hermes_python" "$REPO_ROOT/scripts/test-hermes-acp-cost.py"
fi

log "done. installed:"
$SKIP_GRAPHJIN || { printf '  graphjin: '; have graphjin && graphjin version | head -1 || echo 'NOT FOUND'; }
$SKIP_HERMES   || { printf '  hermes:   '; have hermes   && hermes --version 2>/dev/null || echo '(installed; --version may differ)'; }
