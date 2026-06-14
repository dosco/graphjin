---
title: "Security Graph"
description: "Expose effective policy and high-risk findings before an agent acts."
nav_group: "agentic"
doc_kind: "concept"
weight: 30
---

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

{{< verified by="TestGraphQLControlPlaneSecurityReportsSourceAccessPolicy" file="serv/control_plane_graphql_test.go" line="736" >}}
{{< verified by="TestSecurityNanoRowsCoverSourceCapabilityRegistry" file="serv/control_plane_graphql_test.go" line="1540" >}}

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
