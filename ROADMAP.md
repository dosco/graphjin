# OpenNeko roadmap

Concrete follow-ups and integration gates for future work. This is not a complete
product backlog. Older local plans under the git-ignored `docs/` directory may
have stale implementation status; verify them against current source.

## RBAC-1: enforce effective access on every agent request

**Pending — required for fine-grained or multi-user access.**

Every agent turn starts in a fresh sandbox, so no sandbox is reused across
users. Access still has to be enforced per request:

- Enforce RBAC on each data/tool/action request through server-side checks.
- Filter staged files, retrieved memories and library context **before they
  reach the agent**. Existing org-wide knowledge/library staging needs review
  here.
- Cover all effective grants: sources and row/action restrictions, packs,
  plugins, personal/team memories, library documents and concepts, role/group
  membership, ownership and sharing rules.

Acceptance: for each resource class above, revoke access and verify that the
next turn cannot retrieve the revoked content, including revocation during an
active turn.
