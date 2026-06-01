# Source Mode Migration

This guide shows how to move a multi-user GraphJin app from legacy role table
rules to source mode. Source mode is the migration target for account-scoped
applications, agentic deployments, and GraphJin system roots such as
`gj_catalog`, `gj_artifacts`, `gj_security`, and `gj_runtime`.

Source mode is enabled by adding `sources:`. Once `sources:` is enabled,
GraphJin rejects user-written `roles[].tables` access rules. That rejection is
intentional: source mode generates the low-level role filters and presets from
`sources[].access` so the same qcode/SQL compiler path still enforces access.

## Before And After

Legacy configs often repeated the same account filter and mutation preset on
every table:

```yaml
roles:
  - name: user
    tables:
      - name: orders
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
        delete:
          block: true
```

In source mode, put the default policy at the database source:

```yaml
identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id
  admin_roles: [admin]

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

  - name: graphjin
    kind: graphjin
    access:
      roots:
        gj_catalog: authenticated
        gj_artifacts: account
        gj_workflow: admin
        gj_workflow_execution: account
        gj_runtime: admin
        gj_security: admin
        gj_config: admin
```

GraphJin then generates the internal filters and presets. For account mode,
reads become a trusted account filter on `namespace_column`, writes overwrite
client-provided namespace values with the trusted identity context, and deletes
stay blocked unless explicitly enabled.

## Identity

Move shared JWT claim mapping to `identity`:

```yaml
identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id
  admin_roles: [admin]
```

The generated policies use `$user_id` and `$account_id` from verified request
identity. Client variables with those names are not trusted for generated
source-mode checks.

Role selection uses candidate roles from JWT claims first, then configured role
`match` behavior, then `user` for authenticated callers or `anon` without auth.

## `identity.query` In V1

V1 treats `identity.query` as a source-mode name for the existing
`roles_query` enrichment path.

```yaml
identity:
  query: |
    SELECT role FROM user_roles WHERE user_id = $user_id
```

The deprecated top-level spelling still works:

```yaml
roles_query: |
  SELECT role FROM user_roles WHERE user_id = $user_id
```

Do not configure different values for both names in source mode. They are
aliases in V1, not two separate policy systems. V1 does not implement arbitrary
identity enrichment for `user_id`, `account_id`, or custom trusted variables.

Future full enrichment should be added deliberately with a defined result shape:
`user_id`, `account_id`, `role` or `roles`, and optional trusted variables.
Precedence should be JWT base identity, then query enrichment or override, then
role resolution. Failures must fail closed for account/owner policies and emit a
redacted `gj_runtime` event.

## Access Modes

| Mode | Meaning |
| --- | --- |
| `blocked` | Hidden from usable graph; direct access is unauthorized. |
| `public` | Shared read-only data with no account filter. |
| `authenticated` | Any non-anonymous caller. |
| `account` | Requires `$account_id` and filters by `namespace_column`. |
| `owner` | Requires `$user_id` and filters by `owner_column`. |
| `admin` | Requires the effective role to match `identity.admin_roles`. |

Source-mode database defaults are:

```yaml
access:
  read: account
  write: blocked
  delete: blocked
  namespace_column: account_id
  missing_namespace_column: block
```

`write` covers insert, update, and upsert. `delete` is separate and should stay
blocked unless a deployment explicitly needs it.

Use classifications for exceptions:

```yaml
access:
  public_tables: [countries, currencies, plans]
  admin_tables: [audit_logs]
  blocked_tables: [internal_events]
```

`public_tables` are read-only and unscoped. `admin_tables` are read-only for
admin roles. `blocked_tables` are unavailable to all callers, including admins.

## Artifacts And Globals

Mutable artifacts use the public config name `artifacts` and the GraphQL/system
root `gj_artifacts`:

```yaml
artifacts:
  enabled: true
  source: app
  schema: _graphjin
  auto_init: true
  globals_path: ./config
```

V1 DB-backed artifacts require a writable SQL database source. GraphJin manages
the physical SQL tables:

```text
_graphjin.artifacts
_graphjin.artifact_revisions
```

Files in the config folder remain global, read-only artifacts. They are exposed
as `source=config`, `visibility=global`, and `read_only=true`; they are not
copied into the database. Account-scoped DB artifacts can override same-name
globals without changing config files.

MongoDB can still be a database source for normal queries, but V1 must not be
configured as the DB-backed artifact source.

## System Roots

GraphJin system roots are controlled by the `graphjin` source:

```yaml
sources:
  - name: graphjin
    kind: graphjin
    access:
      roots:
        gj_catalog: authenticated
        gj_artifacts: account
        gj_workflow: admin
        gj_workflow_execution: account
        gj_runtime: admin
        gj_security: admin
        gj_config: admin
```

Capabilities decide whether a root exists. Access decides who can use it.
Unauthorized users should not see blocked/admin-only roots in introspection,
`gj_catalog`, MCP discovery, or agent-facing discovery rows. Admins can inspect
policy evidence through `gj_security`.

## Migration Checklist

1. Add `sources:` and move each provider into a source.
2. Move JWT claim names and admin roles into `identity`.
3. Replace repeated account read filters with `sources[].access.read: account`.
4. Replace mutation namespace presets with `sources[].access.write` when writes
   are required.
5. Keep `delete: blocked` unless the deployment has a clear delete workflow.
6. Move shared lookup tables to `public_tables`.
7. Move audit tables to `admin_tables`.
8. Move internal-only tables to `blocked_tables`.
9. Enable `artifacts` for account-scoped mutable fragments, saved queries, and
   workflows.
10. Keep config-folder fragments, saved queries, and workflows as read-only
    globals.

After migration, run the dialect integration scripts for the SQL databases you
use. At minimum, shared source-access changes should pass PostgreSQL, MySQL,
and SQLite integration. MongoDB should get a sanity pass to verify config
validation does not imply SQL artifact support.
