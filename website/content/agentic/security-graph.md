---
title: "Security Graph"
description: "Expose effective policy and high-risk findings before an agent acts."
nav_group: "agentic"
doc_kind: "concept"
weight: 30
---

The authoritative source for the operating-modes (`dev`/`prod`/`agentic`),
system-roots, and auth security model is `SECURITY.md` in the repository root.
This page covers the `gj_security` graph; see `SECURITY.md` for the canonical
mode and root policy.

## Policy before action

```graphql
query {
  summary: gj_security(id: "summary") {
    mode
    summary
    summary_json
  }

  findings: gj_security(where: { severity: { in: ["high", "critical"] } }) {
    severity
    title
    recommendation
    evidence_json
  }
}
```

`gj_security` is designed for both humans and models. It explains effective capabilities, read-only state, weak defaults, and recommendations before sensitive actions.

Security rows are also source-aware. In source mode, the graph can report capabilities from the central registry, access policy classifications, root permissions, config scan findings, and runtime denial events.

For artifacts, treat config files as read-only globals and `gj_artifacts` as the user-scoped mutable overlay. The effective policy should allow `gj_artifacts` only for authenticated callers that provide trusted `user_id`; another user must not discover or execute someone else's saved queries, fragments, or workflows through catalog or MCP.

[Catalog annotations](/agentic/annotations/) add an explicit account-visible
tier: observed notes remain owner-only, while approved notes are visible only
inside the matching trusted account (or deployment-wide when the deployment has
no account identity). Public rows expose hashed `author_ref` and `approved_ref`,
not raw identity columns. Annotation content remains untrusted data and cannot
grant access or satisfy mutation evidence.

{{< verified by="TestGraphQLControlPlaneSecurityReportsSourceAccessPolicy" file="serv/control_plane_graphql_test.go" line="736" >}}
{{< verified by="TestSecurityNanoRowsCoverSourceCapabilityRegistry" file="serv/control_plane_graphql_test.go" line="1540" >}}
{{< verified by="TestCatalogSnapshotMergesCallerScopedArtifacts" file="serv/artifact_overlay_test.go" line="173" >}}

## Typical use

Ask the security graph before:

- Running mutations.
- Executing workflows.
- Applying source edits.
- Updating config.
- Accessing filesystems or code indexes.

{{< verified by="TestSourceModeHTTPRuntimeDenialEventsAreRedacted" file="serv/source_mode_http_test.go" line="113" >}}

## Filter by kind or severity

```graphql
query {
  capabilities: gj_security(where: { kind: { eq: "capability" } }) {
    source
    name
    severity
    recommendation
  }

  critical: gj_security(where: { severity: { in: ["high", "critical"] } }) {
    title
    evidence_json
    recommendation
  }
}
```

Use this before a model requests `gj_config`, `gj_workflow_execution`, CodeSQL writes, filesystem writes, schema changes, or raw mutation execution.

Artifact updates increment the live `_graphjin.artifacts.revision` value in place. Do not present rollback or historical restore as an available security control.
