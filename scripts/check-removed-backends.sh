#!/usr/bin/env bash
# Guard the active product against accidentally restoring a removed agent
# backend: the Claude Code / Agent SDK backend and the Hermes ACP backend.
# Ax Harness is the only agent runtime. Immutable migrations, release history
# and recorded eval/benchmark results remain so existing installations can
# still upgrade through every schema version and old results stay verifiable.
# graphjin/ is GraphJin, which supports Claude Code as an MCP client, so the
# guard skips it.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

search() {
  rg -n -i --hidden \
    --glob '!.git/**' \
    --glob '!node_modules/**' \
    --glob '!.next/**' \
    --glob '!dist/**' \
    --glob '!graphjin/**' \
    --glob '!.claude/worktrees/**' \
    "$1" . || true
}

violations=()

# ─── Claude Code / Agent SDK backend ───────────────────────────────────
claude_pattern='@anthropic-ai/(claude-agent-sdk|claude-code)|claude[-_ ]?(agent|code)|claudeSdk|claudeAgent|claude_agent_sdk|\.claude'

while IFS= read -r match; do
  [[ -z "$match" ]] && continue
  path="${match%%:*}"
  path="${path#./}"
  case "$path" in
    CHANGELOG.md|FEATURES.md|.gitignore|.dockerignore|scripts/check-removed-backends.sh)
      continue
      ;;
    db/migrations/0002_agent_backend_and_setup.sql|\
    db/migrations/0003_rename_claude_sdk_to_claude_agent.sql|\
    db/migrations/0020_action_policy_created_by.sql|\
    db/migrations/0062_hermes_only_agent.sql|\
    apps/openneko/assets/migrations/0002_agent_backend_and_setup.sql|\
    apps/openneko/assets/migrations/0003_rename_claude_sdk_to_claude_agent.sql|\
    apps/openneko/assets/migrations/0020_action_policy_created_by.sql|\
    apps/openneko/assets/migrations/0062_hermes_only_agent.sql|\
    packages/db/test/integration/migrations.test.ts)
      continue
      ;;
  esac
  violations+=("$match")
done < <(search "$claude_pattern")

while IFS= read -r path; do
  [[ -z "$path" ]] && continue
  [[ -e "$path" ]] || continue
  case "$path" in
    db/migrations/0003_rename_claude_sdk_to_claude_agent.sql|\
    apps/openneko/assets/migrations/0003_rename_claude_sdk_to_claude_agent.sql)
      continue
      ;;
  esac
  violations+=("forbidden path: $path")
done < <(git ls-files -- . ':!graphjin' | rg -i 'claude[-_ ]?(agent|code)|claude_agent_sdk' || true)

# ─── Hermes ACP backend ────────────────────────────────────────────────
# Any mention of Hermes (hermes-agent, hermes_acp, HERMES_HOME, the Hermes
# runtime, its config.yaml, warm pool or patches) is forbidden outside the
# allowlist below.
hermes_pattern='hermes'

hermes_history_path() {
  case "$1" in
    # FEATURES.md keeps a dated history table of shipped PRs.
    CHANGELOG.md|FEATURES.md|scripts/check-removed-backends.sh|THIRD_PARTY_LICENSES.md)
      return 0
      ;;
    db/migrations/0002_agent_backend_and_setup.sql|\
    db/migrations/0020_action_policy_created_by.sql|\
    db/migrations/0062_hermes_only_agent.sql|\
    db/migrations/0065_skill_usage.sql|\
    apps/openneko/assets/migrations/0002_agent_backend_and_setup.sql|\
    apps/openneko/assets/migrations/0020_action_policy_created_by.sql|\
    apps/openneko/assets/migrations/0062_hermes_only_agent.sql|\
    apps/openneko/assets/migrations/0065_skill_usage.sql|\
    packages/db/test/integration/migrations.test.ts)
      return 0
      ;;
    # Recorded eval runs and dated benchmark reports describe what ran then.
    evals/results/*|apps/worker/benchmarks/*)
      return 0
      ;;
    # Vendored embedding-model vocabulary, not product code.
    apps/worker/.cache/*)
      return 0
      ;;
  esac
  return 1
}

while IFS= read -r match; do
  [[ -z "$match" ]] && continue
  path="${match%%:*}"
  path="${path#./}"
  hermes_history_path "$path" && continue
  # Meta's unrelated JavaScript parser packages (via eslint-plugin-react-hooks).
  case "$path" in
    pnpm-lock.yaml|*/pnpm-lock.yaml)
      line="${match#*:*:}"
      line="${line//hermes-estree/}"
      line="${line//hermes-parser/}"
      line="$(printf '%s' "$line" | tr '[:upper:]' '[:lower:]')"
      [[ "$line" == *hermes* ]] || continue
      ;;
  esac
  violations+=("$match")
done < <(search "$hermes_pattern")

while IFS= read -r path; do
  [[ -z "$path" ]] && continue
  [[ -e "$path" ]] || continue
  hermes_history_path "$path" && continue
  violations+=("forbidden path: $path")
done < <(git ls-files -- . ':!graphjin' | rg -i "$hermes_pattern" || true)

if ((${#violations[@]} > 0)); then
  printf 'Removed agent backend artifacts are forbidden outside immutable migration/history allowlists:\n' >&2
  printf '  %s\n' "${violations[@]}" >&2
  exit 1
fi

echo "Ax-only agent backend contract: ok"
