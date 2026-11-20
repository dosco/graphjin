---
title: "Your rules, your approval"
headline: "It sees only what you allow. A change waits for your yes."
description: "DeepORG works inside your access rules: roles, row filters and column lists enforced by the engine on every query the agent writes. By default a change is rehearsed, held, and run only after a person approves that exact version."
lede: "DeepORG works inside the access rules you write, and by default it does not change your data on its own. The rules are enforced by the engine that runs the query, not by the model that wrote it."
scene: "gate"
scene_label: "Animated diagram: a proposed change passes the access rules, is rehearsed in a rolled-back transaction, is held until a person approves it, and only then runs."
scene_caption: "rules / rehearsal / your yes"
actions:
  - label: "Try it in 2 minutes"
    href: "/#quickstart"
  - label: "Access rules docs"
    href: "/configure/auth-rbac/"
landing: true
toc: false
---

{{< band label="Access rules" title="Declared once. Applied to every query the agent writes." >}}

Say who may see what, once. For a whole source:

```yaml
sources:
  - name: app
    kind: database
    type: postgres
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
      public_tables: [countries, plans]
      admin_tables: [audit_logs]
      blocked_tables: [internal_events]
```

Or table by table, with row filters and column lists:

```yaml
roles:
  - name: user
    tables:
      - name: products
        query:
          filters:
            - "{ owner_id: { eq: $user_id } }"
          columns: ["id", "name", "price"]
        delete:
          block: true
```

The agent has no sign-in mode of its own. Every query it writes passes through the same role and row filters at the compiler as a query from any other client.

{{< grid >}}

- **Identity comes from the token.** Not from anything the caller or the model types. A caller cannot write another account's `account_id`.
- **Columns stay hidden in totals too.** If a role cannot read `price`, it cannot sum it either.
- **Delete is separate, and off.** It has to be allowed on purpose.
- **Blocked is an error.** A refused request does not come back looking like “no rows”.

{{< endgrid >}}

{{< endband >}}

{{< band label="Hard edges" title="Some things are not up for discussion." >}}

{{< grid >}}

- **Read-only stays read-only.** `read_only: true` on a source is a veto. A config change made through an assistant cannot turn it writable.
- **One switch makes the agent read-only.** `agent.read_only` limits it to reading and discovery, whatever a role allows.
- **Secrets stay out of reach.** Config values are redacted wherever they can be read, and a runtime change accepts a reference to a secret, not the secret.
- **API writes are opt-in.** A call that changes something on a remote API is off until you expose it, and it always waits for a person.
- **The strict mode is the default.** Production runs reviewed saved queries with every agent surface switched off. The agent runs in its own mode, which refuses to start with development sign-in. A missing or ambiguous mode is treated as production.

{{< endgrid >}}

{{< endband >}}

{{< band label="Changes" title="Proposed, rehearsed, held. Then run once, as you." >}}

With write approval required, which is the default where the agent runs, it never changes application data or calls a write API itself.

1. **Rehearsed.** At the point a write would run, DeepORG runs it inside a transaction and rolls it back.
2. **Held.** The reply is `needs_approval`, with one line to read (`update 3 rows in support_tickets`), the exact query and variables, a preview of the rows it would change, and an expiry (24 hours by default).
3. **Decided by you.** Approve or reject, sending back the hash that pins exactly what you were shown.
4. **Run once.** The stored query runs once, as the person who asked, after checking that nothing about the proposal has changed.

```graphql
mutation {
  gj_approval(where: { id: { eq: "apr_..." } },
              update: { decision: "approve", expected_hash: "..." }) {
    id status result error
  }
}
```

The same decision can be sent to `POST /api/v1/agent/approvals/{id}`, and a pending change shows up in the console's inbox. Approvals are personal: no other user, not even an admin, can read or decide yours, and the agent can never approve its own.

{{< note >}}

**Two honest limits.** The rehearsal is a real transaction, so sequences advance and triggers fire before the rollback. And sources a rehearsal would really affect, such as APIs, warehouses and managed databases, are held without a preview.

{{< endnote >}}

{{< endband >}}

{{< band label="Acting alone" title="Autonomy is a rule you write, row limit included." >}}

Some changes are not worth a person's attention every time. Say which, on which table, and how many rows at most:

```yaml
agent:
  autonomy:
    - { action: data.update, table: support_tickets, level: notify, max_rows: 5 }
    - { action: data.insert, table: notes, level: auto }
```

`notify` runs the change and tells the owner. `auto` runs it and records it. Anything without a rule still asks, and so does every API call. Each change made this way is stored as an approval decided by its rule, so nothing the agent did on its own is off the record.

{{< endband >}}

{{< band label="Afterwards" title="A change says what it should achieve. DeepORG checks whether it did." >}}

A proposed change can carry an expectation: a query that measures the effect, the number to watch, and a window. Once the change has run and the window has passed, DeepORG measures again and records a hit or a miss.

The owner is told either way. A miss also wakes the agent in the same thread, so it can find out why and propose a correction or an undo. The tally of hits and misses, by routine and by kind of change, is the record that says how far an agent can be trusted.

{{< endband >}}

{{< band id="security-model" label="Before it acts" title="Safer agents, not smaller agents." >}}

One policy controls what agents can see, query, and change, and it can be read before anything is attempted. `gj_security` reports effective capabilities, read-only state, weak defaults and recommendations, such as delete enabled outside development or anonymous access to something that should not be public.

{{< code-card filename="policy-before-action.graphql" language="graphql" >}}
query {
  policy: gj_security(id: "summary") {
    mode
    summary
  }

  blockers: gj_security(
    where: { severity: { in: ["high", "critical"] } }
  ) {
    title
    recommendation
  }
}
{{< /code-card >}}

{{< endband >}}

{{< band label="In the docs" title="Go deeper." >}}

- [Access rules](/configure/auth-rbac/): roles, filters, columns and sign-in.
- [Security](/story/security/): the full model, mode by mode.
- [The security graph](/agentic/security-graph/): what `gj_security` reports.
- [Production](/configure/environment-production/): the settings for a live server.
- [How the agent works](/agentic/server-agent/): approvals, autonomy and outcomes.

{{< endband >}}
