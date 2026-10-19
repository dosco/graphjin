#!/usr/bin/env bash
# Fast static guard for the release recovery state machine. GitHub owns the
# workflow runtime, so this check ensures the source keeps the transitions that
# a failed-then-recovered stable release needs before Deploy-to-VM runs.
set -euo pipefail

release_workflow=".github/workflows/release-binaries.yml"
smoke_workflow=".github/workflows/post-release-smoke.yml"
budgets="scripts/image-size-budgets.json"

jq -e '
  .agent == 800
  and ."neko-librarian" == 850
  and .uniqueStack == 2500
  and (.agent | type) == "number"
  and (."neko-librarian" | type) == "number"
  and (.uniqueStack | type) == "number"
' "$budgets" >/dev/null

grep -Fq "Manual recovery runs may contain CI hardening newer than the tag" "$release_workflow"
grep -Fq "github.event_name == 'workflow_dispatch' && github.ref" "$release_workflow"
grep -Fq "Promote recovered stable release to \"Latest\" after smoke" "$smoke_workflow"
grep -Fq "success() && steps.ctx.outputs.stable == 'true' && steps.ctx.outputs.tag != ''" "$smoke_workflow"
grep -Fq -- "--prerelease=false --latest" "$smoke_workflow"
grep -Fq "failure() && steps.ctx.outputs.stable == 'true' && steps.ctx.outputs.tag != ''" "$smoke_workflow"
grep -Fq -- "--prerelease --latest=false" "$smoke_workflow"
grep -Fq 'for image in neko-db records-db neko-backup; do' "$smoke_workflow"
grep -Fq 'org.openneko.storage-owner' "$smoke_workflow"
grep -Fq 'test "$image_owner" = "999:999"' "$smoke_workflow"

# Release builds compile GraphJin from graphjin/ at the release commit. They
# must not download a GraphJin release, which a network outage or rate limit
# can break and which can drift from the source.
if grep -Fq "github.com/dosco/graphjin/releases" Dockerfile; then
  echo "GraphJin build still downloads a GraphJin release instead of building graphjin/" >&2
  exit 1
fi
grep -Fq 'sh scripts/build-openneko.sh /out/openneko' Dockerfile
grep -Fq 'GraphJinVersion=${graphjin_version}' scripts/build-openneko.sh

echo "release recovery contract is intact"
