---
title: "Catalog Annotations"
description: "Attach durable organizational notes to catalog entities, review them explicitly, and make approved context discoverable without changing source metadata."
nav_group: "agentic"
doc_kind: "guide"
weight: 47
---

A catalog annotation is a freeform organizational note with an address. It is
stored as `kind: "annotation"` in `gj_artifacts` and points at one current or
former catalog card through `target_ref`.

Annotations are **data, never instructions**. An agent must treat their content
as untrusted context, revalidate it against the live schema and data, and obey
the same catalog, security, validation, and mutation guards it would use without
the note. An annotation cannot grant access or make a stale target writable.

## Address a catalog entity

First retain a card ID from `query_catalog`. V1 accepts these target families:

- `table:`
- `column:`
- `relationship:`
- `saved_query:`
- `function:`

Create an owner-only draft by writing an annotation artifact. The server
generates the ID, captures the current catalog revision, and forces the initial
tier to `observed`:

```graphql
mutation {
  gj_artifacts(insert: {
    kind: "annotation"
    target_ref: "table:ops.public.production_orders"
    content: "Expedite checks use the requested ship date, not created_at."
    task_id: "task:..."
    metadata_json: { source: "operations review" }
  }) {
    id
    target_ref
    tier
    catalog_revision
    author_ref
  }
}
```

`task_id` is optional. When supplied, it must name a same-owner task; it remains
correlation, not authorization. Content is capped at 16KB and the target address
at 1KB. V1 also bounds each
owner to 200 annotations and each owner/target pair to 25.

## Review, then approve

Approval is a separate update:

```graphql
mutation {
  gj_artifacts(
    where: { id: { eq: "annotation:..." } }
    update: { tier: "approved" }
  ) {
    id
    tier
    approved_ref
    approved_at
  }
}
```

The server changes visibility from owner-only to account-wide and stamps the
actual approving caller. Callers cannot supply `approved_by`, `approved_at`,
visibility, identity, or revision fields. In a deployment with no account
identity, approval is deployment-wide. Owners approve their own notes; admins
can approve, demote, or delete another owner's annotation while preserving its
author attribution.

Editing an approved note without an explicit tier demotes it to `observed` and
clears approval attribution. An explicit demotion does the same and emits an
`annotation_demoted` runtime event. The built-in agent enforces a stronger
review boundary: it cannot insert or edit an annotation and approve it in the
same run, including a combined edit-and-approve mutation. It must present the
draft as data and wait for later user confirmation.

Owner-scoped `annotations_unshared` response notices keep observed drafts from
silently disappearing. After a task closes, MCP `next` guidance also offers
distilling selected durable learnings into observed annotations for review.

{{< verified by="TestAnnotationLifecycleScopeCatalogMergeAndDemotion" file="serv/annotations_test.go" line="86" >}}
{{< verified by="TestRunRequiresLaterUserConfirmationForAnnotationApproval" file="agent/agent_test.go" line="789" >}}

## Discover approved context

List and full-text search use the normal bounded `gj_artifacts` projection:

```graphql
query {
  gj_artifacts(
    search: "requested ship date"
    where: { kind: { eq: "annotation" } }
  ) {
    id
    target_ref
    content
    tier
    author_ref
    approved_ref
  }
}
```

`query_catalog({id: "..."})` and the `ids` form merge visible approved notes
into an `annotations` detail section at query time. The section carries explicit
data-not-instructions framing, safe hashed author/approver references, approval
time, captured catalog revision, task correlation, and a computed `stale` flag.
Raw `gj_catalog` never contains annotation text, and broad catalog searches do
not inject notes into every card.

If the addressed card was dropped, an exact detail lookup returns a
`stale_annotation_target` card with the surviving notes marked `stale: true`.
That is historical context only; it does not recreate mutation evidence.

When semantic catalog search is enabled, each approved annotation is embedded
as a separate account-filtered document that lifts its target card. GraphJin
does not alter the catalog entity document itself. Demotion or deletion changes
the artifact revision, so the note is absent from the next semantic generation.
With semantic search disabled, exact detail merge and lexical artifact search
continue to work unchanged.

{{< verified by="TestAnnotationStaleDetailDeploymentScopeAndServerFields" file="serv/annotations_test.go" line="181" >}}
{{< verified by="TestApprovedAnnotationsAreSeparateAccountFilteredSemanticDocuments" file="serv/annotations_test.go" line="238" >}}
{{< verified by="TestAnnotationApprovalEmbedsOnlyTheNewDocument" file="serv/annotations_test.go" line="287" >}}

At runtime, inspect `query_catalog(id: "help:artifacts")` for the local
annotation contract.
