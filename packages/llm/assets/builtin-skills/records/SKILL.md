---
type: Skill
name: records
description: Browse and query OpenNeko generated record apps, then propose governed record_create, record_update, record_delete, or record_restore actions safely. Use when a user asks to find, list, inspect, create, change, delete, restore, summarize, or answer questions about records in a generated app such as a CRM, support desk, inventory tracker, or custom workflow.
---

# Records

Use the native records catalog and actor-scoped read tools to ground every
answer and mutation. All writes go through installed `record_*` actions with
`scope: internal`; never write SQL, submit arbitrary GraphQL, or claim a
change succeeded before its action reports successful execution.

## Discover the app

1. Call `mcp_neko_records_browse_catalog` before the first query or action.
   Use its exact app, object, and field API names. Catalog results already
   reflect the current actor's readable objects and CRUD grants.
2. If the requested app or object is absent, say it is unavailable to this
   actor. Do not infer a hidden schema or substitute a similarly named app.
3. Use `mcp_neko_records_find_records` for lists, searches, filters, and ID
   resolution. Use `mcp_neko_records_get_record` only with an exact ID that
   was just resolved or supplied by a trusted record-page context.

The native records tools query the dedicated records GraphJin under the human
actor's current admin/member identity. Do not use the customer-source
`neko_graphjin` tool for generated apps.

## Read and answer

- Search the object's configured name field with `search`. Use typed `filters`
  for exact values, picklists, null checks, and reference fields.
- Keep pages small. Follow the returned cursor only when the question needs
  more rows; do not imply a partial page is the whole result. Use `total` when
  reporting counts.
- Use `myRecords: true` only for owner-visible objects and only when the user
  asks for their records.
- Treat a `null` detail row as absent or invisible. Do not distinguish those
  cases or suggest that a hidden record exists.
- Derive date and status meaning from the catalog field names and labels. In
  particular, `occurred_at` means when an activity occurred or is scheduled;
  it is not a due date and does not prove an item is overdue.
- If a question asks for urgency but the object has no explicit priority,
  due-date, or status field, say objective urgency cannot be determined. You
  may offer a clearly labeled heuristic (for example, the latest scheduled
  task), but never rename that heuristic as urgency, priority, or overdue.
- Stay on the trusted app/object surface for contextual record chats. Do not
  inspect other objects to embellish an answer unless the operator explicitly
  asks for that cross-object analysis.
- If the records data plane is unavailable, report that plainly. Never fill
  missing results from memory or another source.

## Resolve before every targeted write

Never guess a record ID. Before `record_update`, `record_delete`, or
`record_restore`, resolve or verify the exact row through the native records
read surface in the current turn.

After `find_records`:

- Zero matches: explain what was searched and ask for another identifying fact.
- One match: use that returned `id`. Read detail when the requested change
  depends on fields outside the list result.
- Multiple matches: show concise distinguishing facts and ask the user to
  choose. Do not pick the first or nearest-looking row.

Resolve reference values the same way: query the target object and write its
exact ID, never a display label. A restore requires an exact deleted-record ID
from an explicit recycle-bin context; if the current read surface cannot verify
it, ask the user to open or provide that context rather than guessing.

## Propose governed writes

Use only operations permitted by the catalog, and use the catalog's existing
fields whenever one fits.

When the user wants to keep information the object has no field for, put it
under a new snake_case key, such as `follow_up_date`. The approval then lists
it as a new field, typed from the value: a `YYYY-MM-DD` string becomes a date,
a number becomes a number, `true` or `false` a yes-or-no field. An
administrator approves the new fields together with the write. Never add a
near-duplicate of an existing field, and say in the approval which new fields
the write adds.

Create:

```json
{
  "app": "equipment",
  "object": "loan",
  "fields": { "subject": "Camera kit", "status": "checked_out" }
}
```

Update with optimistic concurrency:

```json
{
  "app": "equipment",
  "object": "loan",
  "id": "loan-0182",
  "fields": { "status": "returned" },
  "expected": { "status": "checked_out" }
}
```

Delete or restore:

```json
{ "app": "equipment", "object": "loan", "id": "loan-0182" }
```

For `record_update`, put every field whose observed value the change relies on
in `expected`. At minimum include the field being replaced. If the action
reports a concurrency conflict, query again, explain what changed, and ask or
re-plan; never silently retry with weaker expectations.

Submit the matching installed `record_create`, `record_update`,
`record_delete`, or `record_restore` action with `scope: internal`. Summarize
the exact app, object, record, changed fields, and soft-delete semantics in the
approval. A delete moves the row to the recycle bin; it is not a hard delete.

## Guardrails

- Never invent app, object, picklist, reference, or record IDs. Add a new
  field only when the user asks to keep information the object lacks.
- Never update all search matches as a shortcut; one action targets one row.
- Never bypass a denied CRUD grant or ownership boundary.
- Never turn an informational request into a write proposal.
- Never claim approval means execution. Report the action's actual final state.
