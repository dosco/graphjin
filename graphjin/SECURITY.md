# GraphJin Security

This document describes GraphJin's security model for operators and teams
running GraphJin in multi-user, account-scoped, or agentic environments.

GraphJin is a compiler: it turns GraphQL into database queries. It is not a
resolver framework where every field is guarded by custom application code.
For SQL databases, authorization must therefore be resolved before and during
query compilation, then enforced by the generated qcode/SQL path.

## Supported Versions

Security updates are provided for the current release line.

| Version | Supported |
| ------- | --------- |
| latest | yes |

## Reporting a Vulnerability

Please report vulnerabilities privately. Email the maintainer or send a direct
message to https://twitter.com/dosco.

Do not include live credentials, raw JWTs, customer data, connection strings, or
full production query payloads in a report. A minimal reproduction, redacted
configuration, affected version or commit, and observed impact are enough to
start the investigation.

## Security Model Overview

Source mode is GraphJin's secure multi-user configuration model. Source mode is
enabled when `sources:` is present in configuration.

In source mode:

- Request identity is request-wide and comes from verified authentication
  context, usually JWT claims plus optional identity enrichment.
- Source access is declared once at the source level, with small table/root
  classifications for exceptions.
- GraphJin generates internal role table rules from source access. Users should
  not hand-write low-level `roles[].tables` filters and presets in source mode.
- The existing qcode/SQL compiler remains the enforcement path.
- Client variables are not trusted for identity fields such as `$account_id` or
  `$user_id`.
- Blocked or unauthorized direct access returns authorization errors. It should
  not silently look like an empty result set.

Legacy role table security still exists for legacy configurations, but source
mode is intentionally stricter and is the migration target.

For LLM or MCP operators, discovery is intent-first. Start goal-driven config or
security work with:

```graphql
query_catalog(search: "<user instruction>")
```

The best result should usually be a `config_recipe` row for roles, identity,
source access, table classifications, artifacts, GraphJin system roots, or
legacy `roles[].tables` migration. Inspect it with `query_catalog(id: "...")`
before reading `gj_config` or attempting a `gj_config(id: "current", update: ...)`
mutation. Use `graphql_help(for: "discovery")` only when the user intent is
unclear or catalog search returns no useful rows.

MCP discovery is caller-aware. The live `tools/list`, `graphql_help`, and
`query_catalog` responses include the caller's available tools, visible `gj_*`
roots, blocked roots, visible capability rows, recommended entrypoint, and
safety notes. If `gj_catalog`, `gj_security`, `gj_runtime`, or `gj_config` is
not visible to the caller, the operator or model must request the required
authenticated/admin access instead of guessing schema or policy.

## Operating Modes

GraphJin runs in one of three operating modes. The mode sets safe defaults and
decides whether the agentic surface exists at all. The agentic surface is the
catalog, artifacts persistence, workflows, code source, the
`gj_config`/`gj_security`/`gj_runtime` control-plane roots, the MCP server, and
the agent.

| Mode | Purpose | Agentic surface | Auth posture |
| ---- | ------- | --------------- | ------------ |
| `dev` | Local development of the new stack. Not a compatibility mode. | On, wide-open (`public`). | None. No auth. |
| `prod` | The only pre-agentic backward-compatibility mode. | Off — hard-gated, never mounts, startup warning if explicitly enabled. | Classic public REST/GraphQL over compiled/allow-listed queries with roles/RLS on app data. |
| `agentic` | The new mode for trusted agents (MCP + REST). | On, role-gated. | Verified roles plus `user_id`/`account_id` row filters. |

**dev** is for local development of the new stack, not a backward-compatibility
mode. The full agentic surface is on and every `gj_*` root is `public` with no
auth.

**prod** is the only pre-agentic backward-compatibility mode. It serves the
classic public REST/GraphQL API over compiled/allow-listed queries governed by
roles and row-level security. In prod the entire agentic surface is hard-gated
off and never mounts — the catalog, artifacts persistence, workflows, code
source, the `gj_config`/`gj_security`/`gj_runtime` roots, the MCP server, and
the agent are all disabled even if they are explicitly enabled in
configuration. When a prod config explicitly enables one of these subsystems,
GraphJin logs a startup warning rather than silently honoring it. Artifacts
persistence is force-disabled in prod: no saved queries, fragments, or
workflows are written to a database.

**agentic** is the new mode for trusted agents over MCP and REST. The full
agentic surface is available and gated by the caller's role plus per-row
`user_id`/`account_id` filters.

Mode selection and safe defaults:

- The mode is selected by `GO_ENV`, by the config filename (`dev.yml`,
  `prod.yml`, `agentic.yml`), or by an explicit `mode:` key in configuration.
- In source mode, a missing or ambiguous mode fails **closed to `prod`**, never
  to `dev`. The agentic surface mounts only on an explicit `agentic` mode. This
  is the fail-safe for audit finding F1.
- In agentic mode, `auth.development: true` (header-trust auth, e.g.
  `X-User-Role: admin`) is refused at startup with a fatal config error.
  Agentic mode requires verified authentication (JWT), or `none` behind a
  trusted proxy or network boundary. This is audit finding F2.
- The agent has no auth mode of its own. The per-request `safe`,
  `discovery_only`, and `raw_allowed` modes and the `allow_raw_graphql` /
  `allow_mutations` flags are removed. The agent's capability is derived
  entirely from the caller's role: raw GraphQL is always available to the agent,
  and every query still passes through role table access plus
  `user_id`/`account_id` filters at the compiler. Writes are gated solely by the
  role's insert/update/delete access. A single global operator kill-switch,
  `agent.read_only` (default false), forces the agent to read/discovery-only
  regardless of role for incident response.

## Source Mode And Legacy Mode

`sources:` changes GraphJin into source mode. In that mode, old top-level
database, filesystem, OpenAPI, metadata, catalog, and table-access sections are
treated as legacy configuration and should be moved under `sources`.

Most importantly, source mode rejects user-supplied `roles[].tables` access
rules. Those rules are the old escape hatch for table-specific query filters,
mutation presets, column allow lists, and operation blocks. In source mode they
are generated internally from `sources[].access`.

Legacy mode remains supported for existing applications that do not configure
`sources:`. Those applications can continue to use `roles_query`, role `match`
predicates, and `roles[].tables` as before. New multi-user and agentic
deployments should use source mode.

## Identity

Identity is configured at the top level because it is common to all sources.
The common JWT or auth context should describe the caller once; individual
sources should not re-interpret user/account claims independently.

```yaml
identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id
  admin_roles: [admin]
  query: ""
```

| Field | Purpose |
| ----- | ------- |
| `user_id_claim` | JWT claim used as the trusted user id. Default: `sub`. |
| `role_claims` | JWT claim names that may contain role candidates. Default: `[role, roles]`. |
| `namespace_claim` | JWT claim used as the account or tenant namespace. Default: `account_id`. |
| `admin_roles` | Role names that bypass source row filters unless a table/root is explicitly blocked. Default: `[admin]`. |
| `query` | Source-mode name for the existing identity/role enrichment query path. |

Role selection uses this order:

1. Candidate roles from JWT or identity enrichment are matched against
   configured roles and admin roles.
2. Role `match` predicates are evaluated through the existing role enrichment
   path when applicable.
3. Authenticated callers fall back to `user`.
4. Unauthenticated callers fall back to `anon`.

`roles_query` remains available as a deprecated alias in source mode. For V1,
`identity.query` should be treated conservatively as the source-mode name for
the existing `roles_query` enrichment path, not as a general policy language.

Generated source policies use trusted identity variables such as `$account_id`
and `$user_id`. These values must come from verified auth context. Client
provided GraphQL variables with the same names are ignored or rejected for
generated identity checks.

## Source Access Defaults

Database sources get secure defaults:

```yaml
sources:
  - name: app
    kind: database
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
      missing_namespace_column: block
```

Default behavior:

| Setting | Default | Meaning |
| ------- | ------- | ------- |
| `read` | `account` | Reads require `$account_id` and filter by `namespace_column`. |
| `write` | `blocked` | Inserts, updates, and upserts are denied unless explicitly enabled. |
| `delete` | `blocked` | Deletes are denied unless explicitly enabled. |
| `namespace_column` | `account_id` | Column used for account-scoped access. |
| `owner_column` | `user_id` | Column used for owner-scoped access. |
| `missing_namespace_column` | `block` | Account-mode tables missing the namespace column are blocked. |

Access modes:

| Mode | Meaning |
| ---- | ------- |
| `blocked` | Hidden from usable graph; direct access fails authorization. |
| `public` | Readable shared/reference data with no account filter. Public write/delete is not supported. |
| `authenticated` | Any authenticated non-`anon` caller. |
| `account` | Requires trusted `$account_id`; filters by `namespace_column`. |
| `owner` | Requires trusted `$user_id`; filters by `owner_column`. |
| `admin` | Requires effective role in `identity.admin_roles`. |

Table classifications provide source-level exceptions:

```yaml
access:
  read: account
  write: blocked
  delete: blocked
  namespace_column: account_id

  public_tables:
    - countries
    - currencies
    - plans

  admin_tables:
    - audit_logs

  blocked_tables:
    - internal_events
```

Classifications mean:

| Classification | Effect |
| -------------- | ------ |
| `public_tables` | Read-only public/reference tables. No account filter. |
| `admin_tables` | Read-only admin tables. |
| `blocked_tables` | Fully blocked tables. Hidden from normal discovery and direct access. |

`write` covers insert, update, and upsert. `delete` is intentionally separate
and defaults to blocked. In production and agentic environments, enable delete
only when the data model and operational controls require it.

Admin roles bypass generated row filters by default, but not explicitly blocked
tables or roots.

If `missing_namespace_column: allow` is set, an account-mode table without the
namespace column cannot be safely account-filtered. GraphJin treats that table
as authenticated but unscoped and reports the posture through `gj_security`.
Prefer the default `block` behavior.

## Generated Enforcement

Source access compiles into internal role rules before qcode compilation.
Operators should configure the high-level source policy, not the generated
rules.

Conceptually:

```yaml
access:
  read: account
  write: account
  delete: blocked
  namespace_column: account_id
```

generates internal behavior equivalent to:

```yaml
query:
  filters:
    - "{ account_id: { eq: $account_id } }"
insert:
  presets:
    account_id: "$account_id"
update:
  filters:
    - "{ account_id: { eq: $account_id } }"
  presets:
    account_id: "$account_id"
upsert:
  filters:
    - "{ account_id: { eq: $account_id } }"
  presets:
    account_id: "$account_id"
delete:
  block: true
```

The generated mutation presets overwrite client-supplied namespace values with
trusted identity context. A caller cannot write another account's
`account_id` by passing it as a GraphQL variable or input field.

For `delete: account`, GraphJin generates a delete filter. It does not generate
a delete preset because deletes do not create or rewrite row values.

## GraphJin System Roots

GraphJin system roots are built-in features. They exist only when
the agentic surface is on — that is, in `dev` and `agentic` mode. In `prod` the
entire set is gated off and none of these roots mount, regardless of
configuration (see [Operating Modes](#operating-modes)).

```yaml
system:
  root_access:
    gj_catalog: public
    gj_artifacts: owner
    gj_watch: owner
    gj_watch_event: owner
    gj_workflow: owner
    gj_workflow_execution: owner
    gj_config: admin
    gj_security: admin
    gj_runtime: admin
```

Default root access depends on the operating mode:

- In **prod** all system roots are gated off entirely. They do not mount and are
  not reachable over GraphQL.
- In **dev** all system roots are `public` with no auth.
- In **agentic** the defaults are the role-gated matrix below.

Agentic-mode defaults (this table is the canonical access matrix; for how the
roots fit together architecturally, see the canonical diagram in
[`AGENTIC.md`](AGENTIC.md#graph-surfaces-and-boundaries)):

| Root | Agentic access | Notes |
| ---- | -------------- | ----- |
| `gj_catalog` | `public` | Read-only. |
| `gj_code` | read: `authenticated`; write: role-gated (default `admin`) | Code writes are global source-file edits, not per-row scopable, so write is gated by role rather than filtered. Connecting a code source is an explicit operator choice — see the supply-chain note below. |
| `gj_artifacts` | `owner` (`user_id`) | Read/write, owner-private. |
| `gj_watch` / `gj_watch_event` | `owner` (`user_id`) | Read/write, owner-private standing queries and durable inbox events. Watches must reference subscriptions. |
| `gj_workflow` / `gj_workflow_execution` | `owner` (`user_id`) | Read/write, owner-private. Workflows execute under the caller's identity. DB-backed workflows are owner/account-private saved artifacts. Filesystem/global workflows are operator-vetted, read-only, and runnable by all callers (each as themselves). "Global" status is filesystem-only and never user-settable. |
| `gj_config` | `admin` | Read/write. |
| `gj_security` | `admin` | Read-only. |
| `gj_runtime` | `admin` | Read-only. |

Account and owner roots now apply **real per-row** `user_id`/`account_id`
filters, using the same filter/preset machinery as database tables. Previously,
`account`/`owner` on a system root only meant authenticated visibility, not true
per-row scoping.

The state behind these roots (artifacts, watches, watch events, revisions) is
itself read and written through GraphJin's own engine under the reserved
`__graphjin_internal_store` role. That role is not assumable by callers: it
activates only via a non-forgeable in-process marker, and role resolution
rejects it from requests, JWTs, and headers.

System and workflow feature capabilities decide whether sensitive roots exist. Root access decides
who can use roots that exist. For example, disabling a runtime capability can
make `gj_runtime` unavailable; setting `gj_runtime: admin` controls access when
it is available.

`gj_code` write is a supply-chain consideration. Code writes rewrite source
files globally — they are not scoped to a user or account — so write is
role-gated (default `admin`, operator-configurable to a dedicated code-writer
role). Connecting a code source at all is an explicit operator decision, because
it exposes global source-file edits to whichever role is granted code write.

Sensitive roots should stay admin-only unless there is a deliberate operational
reason to expose them more broadly.

## Artifacts

Artifacts are configured with `artifacts` and exposed through the GraphQL system
root `gj_artifacts`.

```yaml
artifacts:
  enabled: true
  source: app
  schema: _graphjin
  auto_init: true
  globals_path: ./config
watches:
  enabled: true
  runner: off
```

V1 artifacts require a writable SQL database source. Object stores and file
backends are not artifact stores.

Logical artifact tables are:

- `_graphjin.artifacts`
- `_graphjin.watches`
- `_graphjin.watch_events`

SQL dialects without schemas may use equivalent prefixed physical table names,
but operators should treat them as GraphJin-managed tables under the configured
artifact schema.

Artifact behavior:

- Auto-init runs before normal schema discovery when artifacts are enabled and
  `auto_init` is true.
- Files from `globals_path` are surfaced as `source=config`,
  `visibility=global`, and `read_only=true`.
- Config-folder globals are not copied into the database and cannot be mutated
  through `gj_artifacts`.
- Database-backed artifacts are user-scoped by `owner_id = user_id`.
- A database artifact can override a same-name global for that user without
  changing config files.
- Non-admin responses redact or hash identity fields such as account/user ids.
- Global writes are denied. Cross-account writes are denied by scoping IDs and
  filters to trusted identity.
- Watches are exposed as `gj_watch` and `gj_watch_event` and use the same
  artifact database, identity scoping, and non-admin identity redaction.
- Watch creation stores `owner_id`, `account_id`, and `owner_role` from trusted
  request context, never from mutation input, so the runner can reconstruct the
  same caller envelope that was validated at creation time.

## Visibility And Discovery

GraphJin may discover blocked or admin-only tables internally so it can produce
security evidence and detect broken posture. Unauthorized callers should not see
those resources in normal discovery.

For non-admin callers, blocked/admin-only tables should be omitted from:

- GraphQL introspection
- `gj_catalog`
- MCP help, search, examples, and agent-facing discovery rows
- legacy discovery surfaces where source mode still exposes compatibility paths

Admins can use `gj_security` to see that a table or root exists and why it is
blocked, admin-only, or unsafe.

## Observability

`gj_security` and `gj_runtime` are complementary.

`gj_security` is the configured/effective policy view. It reports:

- identity claim names, admin roles, and whether identity enrichment is enabled
- source default read/write/delete modes
- namespace columns and missing-column behavior
- public/admin/blocked classifications
- `gj_*` root policy
- artifact source posture
- findings for unsafe or broken configuration

Examples of findings include account-mode tables missing the namespace column,
public write/delete attempts, delete enabled outside development, sensitive
`gj_*` control-plane roots (`gj_config`/`gj_security`/`gj_runtime`) not kept
admin-only in agentic mode, `gj_code` write granted to a non-admin role,
anonymous access to non-public roots, writable system roots missing an ownership
preset, and artifact stores pointing at read-only or non-SQL sources.

`gj_runtime` is bounded, redacted decision support. It can report recent
auth/access outcomes such as:

- missing or invalid JWT
- missing required identity claims
- identity enrichment failure
- role resolution fallback
- missing namespace for account access
- blocked/admin/write/delete denial
- artifact global-write denial
- cross-account artifact/data access attempt

`gj_runtime` is not a forensic audit log. It should not store raw JWTs, secrets,
full variables, full query text, connection strings, or plaintext account/user
identifiers. Prefer hashes, counts, classes, and short reason codes.

## Secure Source-Mode Example

This is an `agentic`-mode configuration, where the full agentic surface is on
and role-gated. In `prod` the agentic surface and its built-in roots do not
mount; in `dev` every enabled root is `public`.

```yaml
mode: agentic

identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id
  admin_roles: [admin]
  query: ""

artifacts:
  enabled: true
  source: app
  schema: _graphjin
  auto_init: true
  globals_path: ./config

sources:
  - name: app
    kind: database
    type: postgres
    default: true
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
      missing_namespace_column: block
      public_tables: [countries, currencies, plans]
      admin_tables: [audit_logs]
      blocked_tables: [internal_events]

system:
  root_access:
    gj_catalog: public
    gj_artifacts: owner
    gj_watch: owner
    gj_watch_event: owner
    gj_workflow: owner
    gj_workflow_execution: owner
    gj_config: admin
    gj_security: admin
    gj_runtime: admin
```

This example starts with account-scoped reads, blocks writes/deletes by default,
keeps reference tables public, hides internal tables, keeps the
`gj_config`/`gj_security`/`gj_runtime` control-plane roots admin-only, scopes
`gj_artifacts` and `gj_workflow` per-owner, gates `gj_code` write to admin by
default, and stores mutable artifacts in GraphJin-managed SQL tables. Because it
runs in `agentic` mode, verified JWT auth is required — `auth.development: true`
would be refused at startup.

## Migration Guidance

For new multi-user deployments, start in source mode rather than adding more
legacy role table rules.

Use [Source Mode Migration](docs/SOURCE-MODE-MIGRATION.md) for step-by-step
before/after examples. This section summarizes the security intent.

For agent-assisted migrations, the model-facing starting point is:

```graphql
query_catalog(search: "migrate legacy roles tables to source access")
```

The returned `config_recipe` row distinguishes currently supported
`gj_config.update` fields from changes that still require direct config-file
edits or future mutation support. Source-mode `gj_config.update` writes are
two-step: first run `mode: "preview"` with `expected_catalog_revision`, then
resend the exact same payload with `mode: "apply"` and the returned
`preview_id`. Preview IDs are bounded in memory, expire after 10 minutes, and
store only a patch hash, base catalog revision, expiry, and redacted summaries.
Source access and GraphJin root policy changes should use `source_patches` by
exact source name so unmentioned source fields are preserved automatically.
Top-level `identity` and `artifacts` changes still require direct config-file
edits or future mutation support.

When migrating:

1. Move provider configuration into `sources`.
2. Move common JWT claim configuration into `identity`.
3. Replace repeated `roles[].tables.query.filters` with source-level
   `access.read`.
4. Replace mutation namespace presets with source-level `access.write`.
5. Keep `delete` blocked unless explicitly required.
6. Classify shared lookup tables as `public_tables`.
7. Classify audit/control tables as `admin_tables`.
8. Classify internal-only tables as `blocked_tables`.
9. Move user-scoped mutable fragments, saved queries, and workflows to
   `gj_artifacts`.
10. Keep config-folder fragments, saved queries, and workflows as read-only
    globals.

The public config name is `artifacts`, and the GraphQL root is
`gj_artifacts`.

## Before Merge: Follow-Up Verification

The source-mode implementation should be verified beyond unit tests before it
is merged or released.

Required follow-up:

- Run dialect integration scripts for PostgreSQL, MySQL, and SQLite.
- Keep the HTTP-level JWT end-to-end coverage passing for `sub`, `roles`, and
  `account_id`.
- Keep coverage proving account rows are filtered and client `$account_id`
  cannot override the trusted claim.
- Keep coverage proving `gj_security`, `gj_runtime`, and `gj_config` are
  admin-only by default.
- Keep runtime denial-event tests proving auth/access failures are redacted.
- Keep `identity.query` as the V1 source-mode alias for the existing
  `roles_query` path. Full arbitrary identity enrichment is a separate follow-up
  feature with its own result schema and fail-closed behavior.

Useful local commands:

```bash
GOCACHE=/tmp/go-build go test ./auth ./core ./serv
./scripts/test-postgres.sh
./scripts/test-mysql.sh
./scripts/test-sqlite.sh
```

Some local sandboxes do not allow `httptest` loopback listeners. If a full Go
test run fails only with `listen tcp6 [::1]:0: bind: operation not permitted`,
rerun in an environment with local networking before treating it as a GraphJin
regression.
