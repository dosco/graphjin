# OpenNeko roadmap

Concrete follow-ups and integration gates for future work. This is not a complete
product backlog. Older local plans under the git-ignored `docs/` directory may
have stale implementation status; verify them against current source.

## RBAC-1: enforce effective access on every agent request

**Pending — required for fine-grained or multi-user access.**
Updated 19 October 2026: the warm sandbox pool and its per-user sandbox reuse
were removed together with the retired pre-Ax backend. Every Ax run starts a fresh
runner with current staged context and a fresh broker token, so no access
revision is needed to decide sandbox reuse.

### Work required

- Enforce RBAC on each data/tool/action request and filter staged files, retrieved
  memories and library context **before they reach the agent**. Cover sources and
  row/action restrictions, packs, personal/team memories, library documents,
  role/group membership, ownership and sharing rules.
  Existing org-wide knowledge/library staging needs review here.
- Shared role/group or organisation policy changes must take effect for every
  affected user on their next request, across web and worker hosts.

### Acceptance checks

For each resource class above, revoke access and verify that the next turn cannot
retrieve the revoked content. Also test role/group changes, different web/worker
hosts, and revocation during an active turn through server-side authorization
checks.

See the [startup guide](apps/worker/AGENT_STARTUP.md).
