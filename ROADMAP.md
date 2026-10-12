# OpenNeko roadmap

Concrete follow-ups and integration gates for future work. This is not a complete
product backlog. Older local plans under the git-ignored `docs/` directory may
have stale implementation status; verify them against current source.

## RBAC-1: connect effective access revisions to warm sandbox reuse

**Pending — required for reuse with fine-grained or multi-user access. Solo admin reuse is enabled.**
Updated 12 September 2026 alongside the warm sandbox implementation.

Generic prewarming defaults to one slot. Assigned reuse is scoped to
**organisation + user**, across chats, with a **three-minute idle timeout**.
Every turn gets a fresh Hermes child/session, current staged context and a fresh
broker token. Solo deployments retain their sole active admin's sandbox, using
their persisted local admin account ID. Solo upgrades adopt or create that account
automatically; email is required only before enabling SSO, which links the same ID. Their idle timeout starts after the turn finishes. Other actors require an access revision; generic
prewarming remains available without one. Expired generic spares replenish
automatically while the host is alive. The pool serves Hermes only; Ax runs start
in a fresh sandbox, so RBAC-1 does not apply to them.

### Revision contract

The trusted host hook is
[`RunChatTurnDeps.sandboxAuthorizationRevision`](packages/llm/src/work/run-chat-turn.ts).
It receives the organisation, thread and stored run actor's user ID/role and
returns `Promise<string | null>`. The browser and agent must not supply this value.

The revision is an **opaque string**, not necessarily a hash. A database-managed
version number serialized as `"42"` is sufficient. A deterministic digest of all
effective grants is also valid. It must change whenever effective access changes,
even if the user ID and role name stay the same. Do not use a hash of those two
identity fields alone.

The [sandbox launcher](packages/llm/src/work/sandbox-launcher.ts) separately hashes
the supplied revision together with its model/provider configuration, tool/action
surface, access settings, backend state and broker endpoint using **SHA-256**.
That combined fingerprint decides whether an assigned sandbox is compatible.
The incoming revision and the launcher's SHA-256 fingerprint are different values.

Example: the user has revision `"42"`; an admin revokes a library grant; the
revision becomes `"43"`. The next turn destroys the old assigned sandbox and uses
a fresh one. Never keep the old sandbox merely because its idle timeout has not
expired. The [pool](packages/llm/src/work/sandbox-pool.ts) never puts an assigned
sandbox back into the generic pool.

### Work required

- Implement a trusted revision lookup covering **all effective grants**: sources
  and row/action restrictions, packs, plugins, personal/team memories, library
  documents and concepts, role/group membership, ownership and sharing rules.
  Shared role/group or organisation policy changes must invalidate every affected
  user, not only users whose direct-grant rows changed.
- Make grant mutations and revision changes consistent: once a revocation is
  visible, no host may retrieve the old revision from a stale cache. Scope revision
  lookup by organisation and user and handle invalidation across web/worker hosts.
- Wire the hook into both web and worker `runChatTurn` callers. Return null when a
  reliable revision is unavailable; lookup failure must not silently reuse old
  state. Do not enable reuse with a constant placeholder revision.
- Enforce RBAC on each data/tool/action request and filter staged files, retrieved
  memories and library context **before they reach Hermes**. Revision checking
  only protects reuse; it does not authorize a tool call or revoke an already
  running turn. Existing org-wide knowledge/library staging needs review here.

### Acceptance checks

Prove same-user reuse across chats, isolation between users and organisations,
separate sandboxes for concurrent turns, and deletion after three idle minutes.
For each resource class above, revoke access while a sandbox is warm and verify
that the next turn cannot reuse it or retrieve the revoked content. Also test
role/group changes, missing/failed revision lookup, different web/worker hosts,
and revocation during an active turn through server-side authorization checks.

See the [startup guide](apps/worker/AGENT_STARTUP.md) and
[implementation benchmark and limitations](apps/worker/benchmarks/2026-09-12-user-warm-pool.md).
