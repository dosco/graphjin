# Governed private order comments

The pack supports private notes through the `add_internal_comment` operation
of `magento.manage_orders`. Use it only after the user explicitly asks to
record a note and the governed change-set is approved or matches a stored,
capped automatic rule.

Supply the resolved numeric Magento order `entity_id` as `path.id`, a concise
factual comment in `body.statusHistory`, a named store scope, and a stable
idempotency key. These values must not be overridden:

```json
{
  "is_customer_notified": 0,
  "is_visible_on_front": 0
}
```

OpenNeko performs a precondition read, one write attempt, and a reconciliation
read. Do not report success until the action receipt confirms it. A timeout may
have committed upstream; an outcome of `reconcile_required` must be checked and
must never be blindly retried.

If the action reports `graphjin_version_unsupported`,
`integration_token_missing`, `integration_token_invalid`, or `acl_missing`,
explain that the private-note step is unavailable and preserve the completed
read-only investigation. Never substitute raw REST, GraphQL, SQL, or an admin
credential.
