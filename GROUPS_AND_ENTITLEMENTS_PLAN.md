# Plan: groups, entitlements and GraphJin data access

## Context

OpenNeko has two sign-in plugins: Scalekit (SSO) and magic link (email). Users, groups and access rules are handled by code written for each plugin, one case at a time:

- **Group rows:** only sign-in claims create groups (`syncSsoGroups`, `apps/web/src/lib/auth.ts:614`). Admins cannot create groups, and magic link has no groups.
- **Role mapping:** `sso_group_mapping` has no UI and no API. `heuristicRoleForGroups` matches group names in code.
- **Access:** access is `app_user.role` (`admin` or `member`). Every member sees every skill, workflow, library concept, metric and briefing. Only records apps have per-item grants (`engine.*_access_grant`).
- **GraphJin:** GraphJin applies one role per request and has no group concept. OpenNeko sends only `admin`, `member` or `service` (`packages/llm/src/graphjin/token.ts:49`, `packages/records/src/graphjin/config.ts:90`).

**Goal:**
1. Plugins supply users and IdP groups through one contract.
2. Admins map IdP groups to OpenNeko groups and create local groups.
3. **Admins grant specific items to groups**, for example the `docx` skill, the "Daily revenue check" workflow or the `revenue/` library collection. Section B lists every item type.
4. Product areas (Briefing, Ask, Workflows, Library, Skills…) are not entitlements. Every signed-in user can open them, and each area shows only the items the user's groups hold.

**Decisions from the user:**
- **Groups only:** groups replace roles, and entitlements are granted to groups only.
- **Items, not areas:** an entitlement is an item such as a skill, a workflow or a library concept. It is not a product area.
- **Provider decides:** the IdP decides memberships that come from a mapping rule.
- **Multiple groups:** a user can be in several OpenNeko groups, and access is their union.
- **GraphJin fixes:** the GraphJin fixes are imminent. The plan depends on them and does not work around them.
- **Two PRs:** all GraphJin changes ship in one GraphJin PR (dosco/graphjin#640), and all OpenNeko changes ship in one separate OpenNeko PR.
- **Group grants in GraphJin:** per-group table rules go into #640 as grants under `sources[].access` (G6), because sources mode rejects `roles[].tables`.

**Assumption for review:** administration powers (users, groups, entitlements, plugins, packs, settings, SSO, rules) belong to the built-in **Administrators** group. They are fixed, not grantable. `requireAdminActor` becomes "the actor is in Administrators".

## Implementation status

Branch `feat/groups-entitlements` in the worktree `openneko-groups` implements sections A to E. Migrations 0074 to 0078 add the tables.

**Changes from this plan:**
- **Package:** grant resolution lives in `@neko/db` (`groups.ts`, `entitlements.ts`, `data-access.ts`), not in a new `@neko/entitlements` package.
- **Role column:** migration 0079 drops `app_user.role`. Administrators membership is the only record of who administers. The release is a major version, so it offers no downgrade path.
- **IdP tables:** `sso_group` and `sso_group_membership` keep their names. Migration 0079 drops `sso_group_mapping`; admin mappings became IdP rules.
- **Sign-in:** the web app calls `reconcileSignInGroups` directly. The worker keeps only directory status and sync.
- **New users:** no group name makes a user an administrator. A new sign-in joins Administrators only when the org has no active administrator, which bootstraps the first account. Every later administrator comes from an IdP rule or from an admin.
- **Defaults:** Everyone holds `*` for every item type except `api_operation`, so exposed mutations keep their `allowed_roles`.
- **Data access:** group grants stay off until an administrator turns them on. Turning them on seeds Everyone with every existing table and column; turning them off restores the previous read modes.
- **Records apps:** access stays in the engine grants. The permissions page grants apps to groups through approved governed actions.
- **Team memory ids:** `global` and `database:<source>`.
- **Channels:** an unlinked channel sender holds nothing. A delivery binding audience is `*` (Everyone) or a group slug.
- **Access panels:** skill, workflow, pack, integration and records pages, plus `/admin/access?type=&id=` for every item type.
- **Approver groups:** `action_policy.approver_group_id` decides approvals. Migration 0079 drops `approver_role`. A pack file or an API caller may still send `approverRole: "admin"`, which the store resolves to the Administrators group when it writes the policy.
- **Route access:** one table (`apps/web/src/lib/access-policy.ts`) says what each area needs, and the web proxy applies it. A path with no entry is refused, so a new area cannot ship open.
- **Account page:** `/profile` lets a person edit their own persona and sign out; the sidebar name opens Profile and Sign out.
- **Row scope:** group grants set a source's read mode to `admin`. Each group grant keeps the source's previous `account` or `owner` filter, and the worker stores the read mode of a source it sees for the first time.
- **Scalekit groups:** the adapter keys a directory group by its display name, because sign-in claims carry group names. A group rename in the IdP needs a rule update. The adapter does not deactivate users; SCIM status decides.
- **Directory writes:** when the directory plugin declares `createUser`, the Users form can also create the user in the identity provider.

**Verified:**
- **GraphJin live test:** `packages/llm/test/integration/graphjin-group-grants-live.test.ts` runs a #640 binary (`OPENNEKO_TEST_GRAPHJIN_BIN`) with a generated config. One group, two groups in union (no cell outside the single-group results), a caller without groups, and `gj_catalog` column paging by source name all pass. PR #640 CI is green.
- **Plan step 3 through the worker:** `apps/worker/test/group-grants-live.test.ts` runs the same #640 binary. It turns group grants on with the column catalog seed, adds Finance (EMEA rows with `amount`) and Sales (all rows without `amount`), applies the config with a GraphJin restart, and queries with run token claims. A user in both groups reads every row of the account, a query for `amount` fails, a user without groups is denied, a new membership works without a restart, and turning grants off restores the previous policy.
- **Plan steps 1, 2, 3 and 5 on a Compose stack (2026-09-16):** magic link signed in a real admin and a member by email, with GraphJin, the databases and the agent sandbox runtime of the time in Docker and web and worker on the host.
  - A Finance-only member saw only the Finance skill and workflow; the Sales URLs returned 404. Adding Sales added its items on the next request, and revoking a grant or a membership removed them with no restart.
  - Group data access on AdventureWorks: Finance read territory 1 rows with `totaldue`, Sales read every row without it. A member of both read every row, and `totaldue` was blocked for the union role. Turning grants off restored `read: authenticated` and `role_mode: first`.
- **End to end on the dev server:** a stub worker reported magic link as the sign-in provider, and the test signed requests as a member and as an administrator. 15 checks passed: a Finance-only member lists and opens only the Finance skill, a second group adds its skill, a revoked grant and a removed membership take effect on the next request, and administrators keep every skill.
- **Screenshots:** Users, Groups, IdP rules with directory sync, group detail, item access, effective access and a member's Skills page, at 1440 px and 400 px.

**Delivery status:**
- **OpenNeko:** open-neko/openneko#330. It pins GraphJin 3.20.78, which contains #640. The live grant tests pass with the released binary.
- **Plugins:** open-neko/plugins#62 (contract copy and Scalekit directory adapter).

**Found while testing (fixed on the branch):**
- The install CLI dropped `provisioning: manual` and the directory capability from a plugin manifest.
- Emailed sign-in links pointed at the container's listen address, and in-app redirects left the app.
- The first sign-in after a worker start timed out while its plugin sandbox started cold.
- Each web or worker restart left its agent sandboxes running.
- The admin GraphJin catalog query used an `and` with one condition, and read only its first page.

**Owner decisions:**
- **Plan step 4 (Scalekit) is not part of this delivery.** Amit chose magic link for the sign-in and membership run (2026-09-16: "lets not test scalekit i want to test the email magic link -> users plugin"). The Scalekit directory adapter ships in open-neko/plugins#62, whose PR body carries the steps to run against a Scalekit Dev organization once that plugin is released.

## Delivery

| PR | Repository | Contents |
|---|---|---|
| 1 | `dosco/graphjin` | PR #640: G1 to G6 below |
| 2 | `open-neko/openneko` | Sections A to E. It pins the GraphJin release that contains #640 in `packages/llm/src/graphjin/version.ts` and the other pins listed there. Today the pin is 3.20.77. |

**Separate repository:** the plugin contract copy (`plugins/packages/types`) and the Scalekit directory adapter (`plugins/packages/scalekit`) live in `open-neko/plugins`. That is a separate git repository, so these changes cannot join PR 2. Section A3 lists them. PR 2 works without them: magic link and local groups need no plugin change.

### GraphJin PR #640 contents

**Done (pushed, CI running):**
- **G1, empty role filters:** a role filter that compiles to nothing, such as `{ or: [{}, {}] }`, stops config load.
- **G2, array `in` and `nin`:** MySQL, MariaDB and MongoDB render `in` and `nin` with array variables correctly on text columns.
- **G3, `identity.role_mode: union`:** GraphJin applies every role in the token and merges their table rules. The merged rule never allows a row, column or operation that no single role allows. Merged rules are built at request time and cached, so a new role combination needs no reload.
- **G4, `$user_groups`:** `identity.group_claims` (default `[groups]`) fills a trusted variable that filters can use.
- **G5, `allowed_roles`:** API operations accept group names and each role of a union.

- **G6, group grants in sources mode:** see C1 for the config shape. GraphJin generates a role table for each grant and merges it with the source's access mode.

**Not included:** a roles-only reload. A grant change still rebuilds the engine in the running process (`serv/mcp_config.go:3929`), so OpenNeko batches reloads (C5). A membership change needs no reload.

## A. Directory: users, IdP groups, OpenNeko groups

### A1. Schema, migration `0074_groups.sql`

- **IdP mirror:** rename `sso_group` to `idp_group` and `sso_group_membership` to `idp_group_membership`. These tables store provider facts only. Rename `sso_group_sync_audit` to `idp_sync_audit`.
- **New table `user_group`:** `id uuid`, `org_id`, `name`, `slug`, `description`, `kind` (`builtin` or `custom`), timestamps.
  - Seed the built-in group **Administrators**.
  - Seed the built-in group **Everyone**. Every active user belongs to it implicitly, and no rows are stored.
- **New table `user_group_membership`:** `org_id`, `group_id`, `user_id`, `source`. `source` is `local`, or `rule:<rule_id>` for rows a rule creates.
- **New table `idp_group_rule`:** `id`, `org_id`, `provider`, `idp_group_id`, `user_group_id`. The mapping is many to many.
- **App user source:** add `app_user.source`, with the value `local` or a plugin name.
- **Data migration:**
  - For every IdP group that a records grant references, create a `user_group` with the same UUID, a rule to it, and rule-sourced memberships. This keeps records grants valid.
  - Users with `app_user.role = 'admin'` get a local membership in Administrators.
  - Records grants with `subject_type = 'role'` move to Administrators (`admin`) or Everyone (`member`).
  - Records grants with `subject_type = 'user'` stay readable, and the UI marks them as legacy.
- **Drop** `sso_group_mapping`, which has no rows and no writer.
- **Drizzle and assets:** update `packages/db/src/schema.ts` and copy the migration to `apps/openneko/assets/migrations/`.

### A2. `DirectoryService` (`apps/worker/src/directory/directory-service.ts`)

- **Reconcile:** `reconcile({ source, users, idpGroups, idpMemberships, scope: "full" | "user" })` performs two steps.
  1. It upserts the IdP mirror for that source. A full sync deactivates rows the snapshot does not contain.
  2. It recomputes rule-sourced memberships from `idp_group_rule`. It never touches `local` rows.
- **Sign-in:** replace `syncSsoGroups` in `apps/web/src/lib/auth.ts` with `POST /admin/directory/reconcile-user`. Delete `heuristicRoleForGroups` and `resolveGroupRole`.
- **Local groups:** the service writes local groups and local memberships. It refuses writes to rule-sourced rows.
- **Lockout guard:** Administrators must keep at least one active member. The guard runs on every membership change, rule change and sync.
- **Worker API:** `/admin/directory/status`, `/sync`, `/reconcile-user`, `/groups`, `/groups/:id/members`, `/rules` in `apps/worker/src/admin-server.ts`. Sync runs on demand and every 6 hours.

### A3. `directory` plugin capability

**In the OpenNeko PR:**
- **Contract:** add `packages/plugin-types/src/directory.ts`.
  - The declaration is `{ providerLabel, read: { users, groups, memberships }, write: { createUser, deactivateUser } }`.
  - RPC `list_directory({ cursor })` returns `{ users[], groups[], memberships[], nextCursor? }`.
  - RPC `apply_directory_change({ op })` covers the declared writes.
- **Wiring:** `manifest.ts`, `rpc.ts`, `runner.ts`, `define-plugin.ts`.
- **Registry:** the capability is a singleton. Reuse the checks at `apps/worker/src/plugins/plugin-registry.ts:1195` and `:1507`.

**In `open-neko/plugins`, outside both PRs:**
- **Contract copy:** align `packages/types` with OpenNeko's copy. They differ in 5 files today. Then add the same `directory.ts`.
- **Scalekit adapter:** read users and directory groups for the connected organization. Confirm `listDirectoryUsers` and `listDirectoryGroups` first. `write.createUser` maps to `create_organization_user`.

## B. Entitlements: items granted to groups

### B1. Model (`packages/entitlements`, `@neko/entitlements`)

- **Grant:** a grant is `(group, item_type, item_id)`. Holding it lets the user see the item and use it, and lets OpenNeko use it in that user's agent runs.
  - `item_id = '*'` grants every current and future item of that type.
  - Two types carry finer rules: records apps keep their object and field grants, and data sources carry table rules (section C).
- **Table:** `item_grant(org_id, group_id, item_type, item_id, created_by_user_id, action_request_id, created_at)`, plus `item_grant_audit` and `item_grant_revision`. Migration `0075_item_grants.sql` reuses the pattern of `db/records/migrations/0010_subject_entitlements.sql`.
- **Resolution:**
  - A user holds an item if any of their groups, including Everyone, holds it or holds `*` for its type.
  - Administrators hold every item.
  - Grants are cached by `item_grant_revision`, so a revocation applies at the next check.
  - The solo local admin (`urn:openneko:solo-admin:*`) is the only bypass.
- **Pack bundles:** a grant on a pack expands to every item the pack installs, including items a later pack upgrade adds. `pack_install` and `pack_artifact` supply the item list.
- **New items:** a new skill, workflow or library concept belongs to its creator and to Administrators. It reaches other users only when an admin grants it to a group. A creator can ask for a grant through an action request.
- **Upgrade parity:** at upgrade, Everyone holds `*` on every type except `data_source`, which gets today's `member` access through C1. Records apps keep their existing grants. No user loses access on upgrade.
- **API:**
  - `holds(actor, itemType, itemId)` returns `{ allowed, via: groupId[] }`.
  - `heldItems(actor, itemType)` returns `"*"` or a set of item IDs, for list filtering.
  - `whoHolds(itemType, itemId)` returns groups, for the item's "Access" panel.
  - `effectiveAccess(userId)` returns every held item with the granting group.
- **Registry:** `packages/entitlements/src/items/<type>.ts` registers each type in B2 as `{ type, label, listItems(orgId), describe(itemId) }`. The admin UI reads the registry, so a new item type needs no UI code.

### B2. Item types

Library documents are not grantable items: GraphJin's file source keeps each user's library private to them.

| Item type | Example items | Items come from | Holding it lets the user | Enforced at |
|---|---|---|---|---|
| **Skill** | `docx`, `magento-check-inventory`, `google-workspace-sheets` | builtin, org and pack skills (`listInstalledSkills`, `packages/llm/src/work/workspace.ts:237`) | See the skill, and have the agent load it in their runs | skill materialization per run (`workspace.ts`); `GET/PUT/DELETE work/skills/**`; pages `/skills`, `/skills/[name]` |
| **Workflow** | Daily revenue check, Aged fulfillment follow-up | `workflow_definition` | See it, run it, see its runs, outputs and artifacts, and receive its observations and briefing cards | `workflows/**`, `workflow-runs/**`, `observations`, `briefing/cards`, `subscriptions/**`; tool `list_workflows`; pages `/workflows/**`, `/runs/**` |
| **Metric** | Net invoiced revenue, Refund rate | `metric` (persona and `pack:*`) | See the metric tile, its value and trend, and have the agent cite it | `briefing`, `briefing/by-metric`, `briefing/value`, `briefing/stats`, `briefing/pins` |
| **Dashboard** | CFO dashboard, COO dashboard | persona role templates (`operator_profile.role_template`, `metric.role`, `dashboard_pin.role`) | Open that persona's briefing and dashboard. Tiles show only held metrics. | `GET /api/briefing?role=`, `briefing/status`, `briefing/findings`, `briefing/recent-actions`; page `/` persona tabs |
| **Watcher** | Stock threshold, Indexer health | `watcher` | See the alert and receive its findings | watcher lists, `briefing/findings`, `observations` |
| **Team memory** | Global company context; memory for the Magento database | `work_memory` scopes `global` and `database` (per data source) | Have the agent recall those memories, and see them on `/memory` | `work/memories/**`; tool `search` (`neko_memory`); memory context in `agent-core.ts` |
| **Data source** | Magento store database, ERP | `data_source` | Query the source through GraphJin, limited by the group's table grants (section C) | tool servers `neko_graphjin`, `neko_graphjin_agent`; token roles (C4) |
| **Saved query** | `average_order_value`, `sales_by_store` | GraphJin saved queries (pack `graphjin/saved-queries`, GraphJin artifacts) | Have the agent run that saved query | `execute_saved_query` brokering in `control-plane.ts` |
| **API operation** | Magento operator `manage_inventory` operations | exposed GraphJin API operations (`expose_api_operation`, `config-change.ts:215`) | Call that operation, which becomes GraphJin `allowed_roles` | group roles (C2) |
| **Action** | `send_slack_message`, `manage_inventory`, `financial_handoff` | plugin action descriptors, `pack_action_definition`, pack `actions/*.yaml` | Have the agent propose that action for the user. Approval still follows the action rules. | tool servers `neko_plugin_actions`, `neko_pack_actions`, `neko_action`; `action-requests` lists |
| **Records app** | Customers app, Vendor onboarding | `engine.record_app`; objects and fields keep `engine.object_access_grant` and `field_access_grant` | Open the app; object and field grants set record operations | `a/[app]/**`, `a/apps`; tool server `neko_records`; records gateway (`packages/records/src/policy/access.ts`) |
| **Integration** | Google Workspace, Shopify, Slack | installed plugins with `connect` or `action` capabilities | Connect their own account and have the agent use that plugin | `integrations/**`, `my/pack-accounts/**`; plugin tool mount in `agent-core.ts` |
| **Channel** | Slack workspace "Acme", Telegram bot | `channel_workspace`, channel plugins | Talk to OpenNeko and receive briefings there | inbound resolution through `channel_identity.app_user_id`; `delivery_binding` recipients |
| **Pack** | Magento pack, Google Workspace pack | `pack_install` | Hold every item the pack installs: skills, workflows, metrics, watchers, saved queries, API operations, actions and sources | expansion in `@neko/entitlements` |

### B3. What stays outside item grants

- **Product areas:** Briefing, Ask OpenNeko, Workflows, Review queue, Memory, Library, Skills, Apps and Integrations stay open to every signed-in user. Each area lists only held items.
- **Personal items:** Ask threads, personal memory overrides and personal library uploads belong to their creator (`apps/web/src/lib/work-thread-auth.ts`). They are never granted.
- **Administration:** the Administrators group holds every administration power. This covers `/admin/**`, `settings/**`, `sso/setup/**`, `admin/users/**`, packs, plugins, rules, approvals, audit export and config restore. The 58 routes that check `admin` today check membership in Administrators instead.
- **Action approval:** approval follows `action_policy.approver_role`, which becomes `approver_group_id`, defaulting to Administrators.
- **Sign-in and version:** `auth/**`, `/signin` and `version` stay public.

### B4. Enforcement

- **Web:**
  - `apps/web/src/lib/entitlements.ts` adds `requireItem(type, id)` for detail routes and `heldItemIds(type)` for list routes.
  - Every route in the "Enforced at" column calls one of them. A detail route for an item the user does not hold returns 404, so it never reveals that the item exists.
  - `requireAdminActor` checks membership in Administrators.
- **Agent:**
  - `agent-core.ts` and `apps/worker/src/agent-sandbox/mcp-bridge.ts` build each run with held items only: skills in the workspace, action descriptors, plugin tools, memory scopes and the GraphJin token roles.
  - `packages/llm/src/work/control-plane.ts` calls `holds` on every brokered call that names an item: workflow, library concept, memory, saved query, action or records app. It uses the actor resolved from `work_run` (`requireSourceConfigAccess`, lines 230-300).
- **Workflows:** a scheduled or subscription run uses its owner's held items at fire time. A pack workflow without an owner runs as `service`, limited to its pack's items.
- **Channels:** an inbound message resolves `channel_identity.app_user_id` to a user and uses that user's held items. An unlinked identity holds nothing.
- **Briefing delivery:** a briefing or finding goes only to recipients who hold its dashboard, metric, workflow or watcher.
- **Role column:**
  - `RunActor.role` and `engine.actor.role` are derived as `admin` when the user is in Administrators, and `member` otherwise.
  - The records GraphJin keeps its fixed roles.
  - Migration 0079 drops `app_user.role`. No reader remains.

## C. GraphJin data access (group grants)

GraphJin merges group rules at request time (G3). OpenNeko writes one grant set per group and puts the user's groups in the token. OpenNeko does not compile a role for each group set.

### C1. Grants in the GraphJin config (G6)

**Shape:**

```yaml
roles:
  - name: og_finance
sources:
  - name: magento
    access:
      read: admin
      grants:
        - role: og_finance
          tables:
            - name: sales_order
              columns: [entity_id, grand_total, store_id]
              filter: '{ store_id: { in: [1, 2] } }'
```

**GraphJin rules for a grant:**
- The role must be listed in `roles[]`, or be `user` or `anon`. A reserved or admin role fails config load.
- `columns` must not be empty. `filter` is optional and can use `$user_id`, `$account_id` and `$user_groups`. G1 rejects a filter that compiles to nothing.
- The grant opens the table for reads to that role, even when `access.read` blocks the role.
- The source's `account` or `owner` filter joins the grant filter with AND. A grant never removes the namespace filter.
- A grant for a table in `blocked_tables`, a table with `blocked` read mode, or an unknown table or column fails config load.
- A role has at most one grant per table.
- A role with no grant for a table keeps the source's access mode.
- **Reads only:** OpenNeko database sources are `read_only` (`assertDatabaseSourcesReadOnly`, `sources-config.ts:27`). Writes stay under `access.write`, and API writes use API operations (B2).

**GraphJin tests:** done in #640. They cover validation for each rule, the generated rule per read mode, first and union modes over JWT, and `Example_queryWithSourceAccessGrants` on each Example database.

### C2. Rules in OpenNeko

- **Table `data_access_rule`:** `org_id`, `group_id`, `source`, `schema`, `table`, `columns text[]` (never empty), `row_filter jsonb`.
- **Row filters** are a structured tree (column, operator, literal, `$user_id`, `$user_groups`, `and`, `or`). Admins never type free text.
- **Scope:** a group needs the data source item (B2) for its rules to apply.
- **Group role:** a group with at least one rule gets the GraphJin role `og_<slug>`. The slug is fixed when the group is created, so the role name never changes.
- **Administrators** maps to `identity.admin_roles` (`admin`) and gets no grants.
- **API operations:** each held API operation item (B2) adds the group role to that operation's `allowed_roles` (`config-change.ts:227`).

### C3. Config generator (`packages/llm/src/graphjin/group-grants.ts`)

- **Input:** the org's groups, rules and API operation grants.
- **Output:** the `roles[]` entries for group roles, `sources[].access.grants`, and `allowed_roles` lists.
- **Identity:** set `identity.role_mode: union`, `role_claims: [roles]` and `group_claims: [groups]`.
- **Deny by default:** `reconcileGraphjinWritePolicy` sets `access.read: admin` on every OpenNeko-managed database source. A non-admin reads only what a grant allows, and new tables and columns need a grant.
- **Legacy roles:** remove `member` from `OPENNEKO_JWT_ROLES` (`sources-config.ts:88`). Keep `service`.
- **Upgrade parity:** Everyone gets grants equal to today's `member` access for each source, using the tables and columns that exist at upgrade.

### C4. Tokens (`packages/llm/src/graphjin/token.ts`)

- **Claims:** `mintGraphjinToken` sends `roles: [og_<slug>, ...]` for the user's groups that have rules, and `groups: [<slug>, ...]` for all the user's groups. Administrators members get `roles: [admin]`.
- **No group role:** GraphJin falls back to `user`, and C3 denies `user` every table.

### C5. Apply and reload

- **Membership change:** the next token carries the new groups. GraphJin does not reload.
- **Rule or API operation grant change:** write through `packages/llm/src/graphjin/persist-source-config.ts`, with a 30-second debounce. GraphJin reloads once per batch.

## D. Admin UI

- **Users page:** `/admin/users` gets tabs: **Users**, **Groups**, **IdP rules**.
  - Users show source, groups (with a lock and rule name when rule-sourced) and an "Effective access" drawer listing each held item with its group.
- **Group page:** `/admin/users/groups/[groupId]` has:
  - **Members:** local members are editable, and rule members are read-only.
  - **Items:** one section per B2 item type, with a search picker for items, an "all current and future" switch per type, and pack bundles.
  - **Data access:** table grants for each held data source (C2). The editor shows the GraphJin role name and the columns each table exposes.
- **Item pages:** skill, workflow, library folder and concept, metric, dashboard, watcher, data source, action, app, integration, channel and pack pages get an **Access** panel. The panel lists holding groups (`whoHolds`), and Administrators can add or remove groups there.
- **Records:** `a/[app]/admin/permissions` embeds the group picker.
- **API routes:** `apps/web/src/app/api/admin/groups/**`, `idp-rules/**`, `item-grants/**`, `data-access/**`.
- **Agent tools:** `buildUserManagerServer` (`packages/llm/src/work/tools.ts:280`) gains `list_groups`, `request_group_change`, `request_item_grant` and `request_data_access_change`, all through `proposeAdminAction`.

## E. Cleanup in the same PR

- Drop `app_user.role`, `sso_group_mapping`, `heuristicRoleForGroups` and `resolveGroupRole`.
- `action_policy.approver_role` becomes `approver_group_id`.

## Verification

- **Unit tests:**
  - `@neko/entitlements`: union across groups, `*` grants, pack expansion (including items added by an upgrade), revision cache, solo bypass, creator-only new items.
  - Upgrade parity: after migration, every existing member holds every item they could see before.
  - `DirectoryService`: rule recomputation, `local` rows kept on sync, lockout guard.
  - Config generator (C3): group roles, `sources[].access.grants`, `allowed_roles`, the identity block, deny by default and upgrade parity. The output is idempotent.
  - Tokens (C4): `roles` and `groups` claims for no groups, one group, several groups and Administrators.
  - Plugin contract tests for `directory`.
- **Item coverage test:**
  - For each B2 item type, create two items and grant one.
  - Call every route and agent tool in its "Enforced at" column as a member of that group.
  - Assert only the granted item appears, and the other returns 404 or is absent.
- **Merge safety:** GraphJin #640 owns the merge tests (`TestUnionReadNeverOverGrants`). OpenNeko adds one test against a Postgres fixture: a token with two group roles returns no cell that neither group alone returns.
- **Existing suites:** `apps/web/test/api/*`, `apps/worker/test/admin-server.test.ts`, the records gateway tests and `packages/records/test/graphjin-config.test.ts` pass with the parity seed.
- **End to end (Docker compose dev):**
  1. **Groups and items:** with magic link, create Finance and Sales. Remove Everyone's `*` grants for skills, workflows and library.
     - Grant Finance the `docx` skill, the "Daily revenue check" workflow, the `revenue/` collection and the CFO dashboard.
     - Grant Sales the `magento-run-promotions` skill.
     - Sign in as a Finance-only user. Confirm `/skills`, `/workflows`, `/library` and the briefing tabs show only those items, a Sales skill URL returns 404, and the agent workspace contains only `docx`.
  2. **Two groups:** add the user to Sales. Confirm both groups' items appear, and the next token carries both group roles.
  3. **Data grants:** grant Finance EMEA `orders` rows with `amount`, and Sales all `orders` rows without `amount`. Confirm a user in both groups reads every `orders` row, and a query for `amount` fails.
  4. **Scalekit (Dev environment):** needs the plugins-repo adapter. Add a rule "IdP group → Finance" and sync.
     - Confirm the membership is locked, items appear at the next sign-in, and removal in the IdP revokes them after the next sync.
  5. **Revocation and reload:** revoke the workflow grant, and confirm the next request returns 404 without a restart. Move a user between groups, and confirm no GraphJin reload runs.
- **Before each PR:**
  - OpenNeko: `pnpm -r typecheck`, `pnpm -r test` and `pnpm -r build`.
  - GraphJin: `go test ./core/... ./serv/...`, the Examples on each Example database, and green CI on #640.
