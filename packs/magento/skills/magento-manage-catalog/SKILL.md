---
name: magento-manage-catalog
description: Prepare and execute governed Magento product, category, assignment, and price changes with clear approval requirements, limits, before images, reconciliation, and safe inverse drafts. Use when a user asks to create, edit, bulk update, move, assign, price, or delete catalog entities.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, catalog, products, categories, pricing, change-set]
    category: commerce
    requires_toolsets: [graphjin, pack-actions]
    related_skills: [magento-check-inventory, magento-run-promotions, magento-review-performance]
---

# Manage the Magento catalog

Resolve the store scope and exact product SKU or category ID, then read the
current entity before proposing a change. Use only an operation installed in
`magento.manage_catalog`. Include a stable idempotency key, one `entity_ref`
per row, path parameters, and the Magento request body.

For a single product update, use `operation: product_update`. Each row must put
the SKU in `path.sku` and the update in `body.product`; for example:
`{"entity_ref":"24-MB01","path":{"sku":"24-MB01"},"body":{"product":{"sku":"24-MB01","price":34.01}}}`.
Use the Magento store code in `scope.store` (for example,
`{"store":"default"}` when the store's code is `default`). For an existing
text attribute, update its `custom_attributes` entry; do not invent a new
attribute code. A complete single-product payload looks like:
`{"operation":"product_update","scope":{"store":"default"},"rows":[{"entity_ref":"24-MB01","path":{"sku":"24-MB01"},"body":{"product":{"sku":"24-MB01","custom_attributes":[{"attribute_code":"care_instructions","value":"Updated care text"}]}}}],"idempotency_key":"update-24-MB01-care-20260924"}`.
Read the current text first and preserve every part the operator did not ask
to change. Once the store code, SKU, and before value are known, call
`magento.manage_catalog` to create the governed request; the approval card is
where the operator reviews it. Do not inspect MCP bridge or worker files to
infer the payload shape.

Show the before image, requested diff, approval requirement, limits, row count,
and whether the operation is reversible. Unknown attributes and price, tax,
visibility, status, website assignment, or destructive changes escalate to
human administrator approval. A named automatic rule may proceed only for an
eligible change inside its stored daily limit and entity cooldown.

For more than one product, prefer `product_bulk_update`. Treat Magento's bulk
UUID as submission evidence, not completion. Wait for terminal operation
statuses and then read every product back. If any row drifts after preview,
submit nothing. If reconciliation is ambiguous, report `reconcile_required`
and never retry automatically.

Undo is a new `magento.undo_changeset` request. Generate it only for an applied,
reversible change-set and only after the current value still matches the
recorded reconciled image.

Boundary: Never bypass `magento.manage_catalog` or `magento.undo_changeset` with SQL, raw GraphQL, or raw REST, and never describe a preview or bulk submission as an applied change.
