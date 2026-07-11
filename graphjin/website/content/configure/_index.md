---
title: "Configure"
description: "Configure sources, databases, auth, caching, uploads, OpenAPI, and production settings."
nav_group: "configure"
weight: 5
---

Configuration pages turn the reference files into decision-oriented guides. New
to GraphJin config? Start with **[How Configuration Works](/configure/how-it-works/)** —
it explains the one-file/two-halves model, how values are resolved, and every way
to change config (editor, CLI, agent, MCP, GraphQL). The pages below go deep on
each area.

| Page | What it covers |
| --- | --- |
| [How Configuration Works](/configure/how-it-works/) | The mental model plus every interface: editor autocomplete, the `graphjin config` CLI, the agent, MCP tools, and `gj_config`. |
| [Sources Mode](/configure/sources-mode/) | Named providers for databases, files, APIs, CodeSQL, workflows, and GraphJin system roots. |
| [Database Config](/configure/database/) | Single and multi-database connection settings, pools, TLS, table mapping, and relationships. |
| [Auth And RBAC](/configure/auth-rbac/) | JWT/OIDC auth, identity claims, role queries, source access, table rules, and column controls. |
| [Caching And Redis](/configure/caching-redis/) | Response cache keys, stale-while-revalidate, Redis sharing, and invalidation. |
| [Uploads And Filesystems](/configure/uploads-filesystems/) | File sources, uploads, local/S3/GCS backends, presigned URLs, and read-only policy. |
| [OpenAPI Config](/configure/openapi-config/) | API sources, spec discovery, auth, joins, result paths, and operation overrides. |
| [Environment And Production](/configure/environment-production/) | `dev`, `prod`, and `agentic` modes, inheritance, environment variables, and production defaults. |

New deployments should prefer `sources:` for all graph providers. Legacy top-level database-only config still exists for simple apps, but file, API, CodeSQL, workflow, catalog, and security surfaces are source-mode features.
