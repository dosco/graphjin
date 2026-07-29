---
title: "Artifacts Overlay"
description: "Use gj_artifacts for caller-scoped saved queries, fragments, workflows, and addressed catalog annotations."
nav_group: "agentic"
doc_kind: "guide"
weight: 45
---

## Globals and user artifacts

GraphJin has one resolution model for saved queries, fragments, and workflows:

1. Look for a user artifact in `gj_artifacts` by `(kind, name)` for the request `user_id`.
2. Fall back to the global config file under the configured config path.

Kinds do not mask each other. A saved query named `daily_report` does not hide a workflow named `daily_report`.

The same store also holds [catalog annotations](/agentic/annotations/): bounded
freeform notes addressed to catalog card IDs. Unlike executable artifacts,
annotations never override config or source metadata. Observed notes are
owner-only drafts; explicitly approved notes become account-visible context.

Global files are the baseline:

```text
config/
  queries/
    products.graphql
    fragments/
      product_fields.gql
  workflows/
    nightly-report.js
```

In `dev` and `agentic` modes, GraphJin configures a private managed artifact store automatically. When the request has `user_id`, GraphJin stores user-owned edits in `gj_artifacts` with `visibility = "user"`, `owner_id = <user_id>`, optional `account_id`, `source = "database"`, and `read_only = false`. Config files appear as `visibility = "global"`, `source = "config"`, and `read_only = true`.

{{< verified by="TestUserArtifactSavedQueryOverridesGlobalOnlyForOwner" file="serv/artifact_overlay_test.go" line="94" >}}
{{< verified by="TestCatalogSnapshotMergesCallerScopedArtifacts" file="serv/artifact_overlay_test.go" line="173" >}}

## Configure the store

No artifact configuration is required in `dev` or `agentic` mode. GraphJin creates `.graphjin/artifacts.sqlite3` with private permissions, WAL journaling, a busy timeout, and a single-process connection pool. This internal database is intentionally absent from public sources, catalog metadata, application database selection, and security reports.

Configure an explicit SQL source for a shared or clustered deployment:

```yaml
sources:
  - name: app
    kind: database
    type: postgres

artifacts:
  enabled: true
  source: app
  schema: _graphjin
  globals_path: ./config
  auto_init: true
  max_per_owner: 200
```

With `auto_init: true`, GraphJin creates `_graphjin.artifacts` when the service starts. Current updates increment the live artifact row's `revision` value in place; GraphJin does not create or expose artifact history rows.

For compatibility, an explicit legacy `artifacts.enabled: true` without `source` continues to select the first writable SQL source. Set `artifacts.enabled: false` to opt out. A managed-store open or initialization failure is fatal rather than silently falling back to nondurable storage.

## One store, one bounded projection

`gj_artifacts` is backed by a real SQL table, but GraphJin never hand-writes SQL against it: control-plane reads and writes run back through GraphJin's own engine under the reserved, non-forgeable `__graphjin_internal_store` role — the same compiled, validated query machinery that serves your app, across every supported database. [Catalog annotations](/agentic/annotations/), [declared tasks](/agentic/tasks/), and [watches](/agentic/watches/) persist through the same store.

List and search reads are served from an in-memory nanoDB projection, so discovery costs no database round-trips. The projection is a **bounded search index**, not a copy of the store: per artifact, `content` is capped (default 32KB, tunable via `artifacts.projection_content_max_bytes`) and marked with `content_truncated: true`; oversized `content_json`/`metadata_json` are dropped from the projection rather than truncated into invalid JSON. The shared owner budget for catalog-visible saved queries, fragments, and workflows defaults to 200 and is tunable via `artifacts.max_per_owner`; existing rows remain updatable at the cap. Execution reads — running a saved query, loading a workflow — always read through to the store, so nothing functional is ever truncated. The projection refreshes immediately on GraphJin-made mutations and through a private revision subscription for external writes. `artifacts.poll_seconds` controls reconnect and fallback checks when that subscription is unavailable; an idle system does no projection work.

{{< verified by="TestArtifactProjectionCapsJSONFieldsButStoreKeepsFull" file="serv/artifact_overlay_test.go" line="345" >}}
{{< verified by="TestArtifactProjectionHonorsConfiguredCap" file="serv/artifact_overlay_test.go" line="395" >}}

## Writes and learning

Named query auto-save is a development-only learning behavior. It uses the artifact overlay when all conditions are true:

- The service runs in `mode: dev`; agentic mode keeps dynamic authoring enabled but disables learning.
- The artifact store is enabled.
- The request context has `user_id`.

With all conditions, named queries and captured fragments save to `gj_artifacts`. In dev mode without a configured store or user identity, auto-save falls back to the global `queries/` and `queries/fragments/` files. Agentic and production modes do not learn queries.

Workflow writes use the same rule. `gj_workflow` and `save_workflow` write user workflow artifacts when `user_id` and the store are present. Otherwise they write global `workflows/*.js` files only in dev fallback mode.

{{< verified by="TestSavedQueryAutoSaveUsesUserArtifactWhenConfigured" file="serv/artifact_overlay_test.go" line="71" >}}
{{< verified by="TestUserArtifactWorkflowOverridesGlobalOnlyForOwner" file="serv/artifact_overlay_test.go" line="148" >}}

## Reads and execution

REST saved-query routes, MCP `execute_saved_query`, the server-side agent, workflow scripts, `gj_workflow_execution`, and catalog discovery all use the merged resolver. This means a user artifact can override a global saved query or workflow for that caller without changing what another caller sees.

Artifact-backed saved queries can import artifact-backed fragments with the same `#import "./fragments/name"` syntax as files. Import traversal and absolute paths are rejected by the same resolver used for filesystem saved queries.

{{< verified by="TestArtifactSavedQueryImportsArtifactFragmentsAndRejectsTraversal" file="serv/artifact_overlay_test.go" line="120" >}}
