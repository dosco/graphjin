#!/usr/bin/env bash
# Guard the active product against accidentally restoring the removed Claude
# Code / Agent SDK backend. Immutable migrations and release history remain so
# existing installations can still upgrade through every schema version.
# graphjin/ is GraphJin, which supports Claude Code as an MCP client, so the
# guard skips it.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

pattern='@anthropic-ai/(claude-agent-sdk|claude-code)|claude[-_ ]?(agent|code)|claudeSdk|claudeAgent|claude_agent_sdk|\.claude'
violations=()

while IFS= read -r match; do
  [[ -z "$match" ]] && continue
  path="${match%%:*}"
  path="${path#./}"
  case "$path" in
    CHANGELOG.md|FEATURES.md|.gitignore|.dockerignore|scripts/check-hermes-only.sh)
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
done < <(
  rg -n -i --hidden \
    --glob '!.git/**' \
    --glob '!node_modules/**' \
    --glob '!.next/**' \
    --glob '!dist/**' \
    --glob '!graphjin/**' \
    "$pattern" . || true
)

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

if ((${#violations[@]} > 0)); then
  printf 'Claude backend artifacts are forbidden outside immutable migration/history allowlists:\n' >&2
  printf '  %s\n' "${violations[@]}" >&2
  exit 1
fi

echo "Hermes-only backend contract: ok"
