---
title: "Agentic"
description: "MCP, catalog discovery, security posture, source mode, workflows, and OAuth."
nav_group: "agentic"
weight: 4
---

Agentic GraphJin gives AI clients a discoverable, policy-aware surface for data access and controlled operations.

| Page | Agent-facing surface |
| --- | --- |
| [MCP](/agentic/mcp/) | Discovery, syntax guidance, validation, cursor IDs, saved-query execution, and GraphQL tools. |
| [Server-Side Agent](/agentic/server-agent/) | GraphJin runs the catalog-first discovery loop for you and returns a typed answer via `ask_graphjin_agent` and `POST /api/v1/agent`. |
| [Catalog Graph](/agentic/catalog-graph/) | `gj_catalog` rows for sources, tables, relationships, syntax, examples, workflows, and evidence. |
| [Security Graph](/agentic/security-graph/) | `gj_security` rows for effective policy, capabilities, read-only state, and findings. |
| [Source Mode](/agentic/source-mode/) | Source-local ownership, capability defaults, reloads, and `agentic.yml` inheritance. |
| [Workflows](/agentic/workflows/) | Named, reviewed operational procedures that can call GraphJin tools and GraphQL. |
| [MCP OAuth](/agentic/oauth/) | Hosted MCP identity with protected-resource metadata, authorization metadata, and audience checks. |

The intended loop is catalog first, security second, validation or preview third, and then a governed action. That sequence is what keeps agents useful without handing them raw database credentials or arbitrary shell access.
