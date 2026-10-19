---
title: "Workflows"
description: "Run named, policy-controlled workflow steps that can call GraphJin tools and GraphQL."
nav_group: "agentic"
doc_kind: "guide"
weight: 50
---

## Named workflows

Workflows let GraphJin expose reviewed operational procedures instead of letting agents improvise multi-step actions.

When a workflow is attached to a standing watch, it becomes an autonomous action and requires a separate hash-pinned review through `gj_watch`. See [Choosing Watches, Flows, and Workflows](/agentic/watch-automation/) for the notification-versus-action decision and approval lifecycle.

```yaml
workflows:
  path: ./workflows
  capabilities:
    execute: true
    read: false
    write: false
```

```bash
graphjin workflow run nightly-report --vars report-vars.json
```

Workflows can call GraphJin tools and GraphQL when allowed by configuration. They respect declared variables, timeouts, context cancellation, and workflow execution policy.

Files under `workflows/` are global workflow definitions. When artifacts are enabled (the `dev`/`agentic` default) and a request has `user_id`, `gj_artifacts` provides a caller-scoped workflow overlay. Execution resolves a user workflow artifact first, then falls back to the global file.

{{< verified by="TestRunNamedWorkflow_CanCallGJTools" file="serv/workflows_test.go" line="109" >}}
{{< verified by="TestRunNamedWorkflow_CanExecuteGraphQLWhenAllowed" file="serv/workflows_test.go" line="142" >}}
{{< verified by="TestUserArtifactWorkflowOverridesGlobalOnlyForOwner" file="serv/artifact_overlay_test.go" line="148" >}}

## GraphQL control-plane shape

```graphql
mutation RunWorkflow($vars: JSON!) {
  gj_workflow_execution(insert: {
    workflow_name: "nightly-report"
    variables: $vars
  }) {
    id
    workflow_name
    status
    result_json
    error
  }
}
```

Workflow rows also appear in `gj_catalog`, so a model can inspect names, variable contracts, lifecycle metadata, and safety notes before execution.

`gj_workflow` and `save_workflow` use the same write policy as saved queries: user artifact when `user_id` and the artifact store are present, global `workflows/*.js` only in dev fallback mode.

{{< verified by="TestQueryCatalogReturnsWorkflowCards" file="serv/mcp_catalog_workflow_test.go" line="15" >}}
{{< verified by="TestGraphQLControlPlaneWorkflowLifecycle" file="serv/control_plane_graphql_test.go" line="307" >}}

## Safety

Use workflows for bounded operations that need a name, review trail, inputs, and clear failure behavior.

Workflows should declare variables and timeouts. The runtime respects context cancellation and blocks workflow-management tools unless the caller/config explicitly allows them.

{{< verified by="TestHandleExecuteWorkflow_RequiresDeclaredVariables" file="serv/workflows_test.go" line="216" >}}
{{< verified by="TestRunNamedWorkflow_BlocksWorkflowMutationTools" file="serv/workflows_test.go" line="383" >}}
