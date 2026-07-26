---
title: "Config Reference"
description: "Map the major GraphJin configuration areas to focused guides and canonical source docs."
nav_group: "reference"
doc_kind: "reference"
weight: 20
---

## Start here

New to GraphJin configuration? [How Configuration Works](/configure/how-it-works/)
explains the mental model (one file, engine vs server settings, layered overrides)
and every interface for changing config — editor autocomplete, the `graphjin config`
CLI, the agent, MCP tools, and the `gj_config` control plane.

## Major sections

| Area | Guide |
| --- | --- |
| How configuration works | [How Configuration Works](/configure/how-it-works/) |
| Sources mode | [Sources Mode](/configure/sources-mode/) |
| Database config | [Database Config](/configure/database/) |
| Auth and RBAC | [Auth And RBAC](/configure/auth-rbac/) |
| Caching and Redis | [Caching And Redis](/configure/caching-redis/) |
| Discovery cache and semantic search | [Discovery Cache And Semantic Search](/configure/discovery-semantic-search/) |
| Uploads and filesystems | [Uploads And Filesystems](/configure/uploads-filesystems/) |
| OpenAPI | [OpenAPI Config](/configure/openapi-config/) |
| Environment and production | [Environment And Production](/configure/environment-production/) |
| MCP | [MCP](/agentic/mcp/) and [MCP OAuth](/agentic/oauth/) |
| Built-in agent | [Server-Side Agent](/agentic/server-agent/) |
| Declared tasks | [Durable Verified Tasks](/agentic/tasks/) |
| Federation | [Apollo Federation](/integrations/federation/) |

## Canonical source

The full field-by-field reference remains in `CONFIG.md`. This site turns that file into a progressive reading path but does not replace the checked-in reference.

## Common source-mode skeleton

```yaml
mode: agentic

identity:
  user_id_claim: sub
  namespace_claim: account_id
  role_claims: [role, roles]

sources:
  - name: app
    kind: database
    type: postgres
    default: true
    connection_string: ${DATABASE_URL}
    access:
      read: account
      write: blocked
      delete: blocked

system:
  root_access:
    gj_catalog: authenticated
    gj_security: admin
    gj_runtime: admin
```

{{< verified by="TestConfigDocsTemplatesUseSources" file="serv/mcp_config_docs_test.go" line="8" >}}
{{< verified by="TestAgenticConfigDocsTemplate" file="serv/mcp_config_docs_test.go" line="27" >}}

## Validation surfaces

| Change | Tests to look near |
| --- | --- |
| New source capability | `core/sourcecap`, catalog, security, MCP registration, and source access tests |
| New built-in feature capability | `core/featurecap`, system policy, security, MCP registration, and direct GraphQL tests |
| New config field | config decode/validation tests plus MCP config docs tests |
| New API or file provider | source-mode normalization and runtime init tests |
| New security default | `gj_security`, config scan, and caller capability profile tests |

## Validation

Configuration changes should be validated through the existing config and subsystem tests, especially when adding source capabilities, MCP settings, OpenAPI fields, or security defaults.
