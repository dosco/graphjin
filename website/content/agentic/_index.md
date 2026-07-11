---
title: "Agentic"
description: "MCP, catalog discovery, security posture, source mode, workflows, watches, and OAuth."
nav_group: "agentic"
weight: 4
---

Agentic GraphJin gives AI clients a discoverable, policy-aware surface for data access and controlled operations. Send one instruction to the [built-in agent](/agentic/server-agent/) and get a typed, evidence-backed answer back - or drive the discovery loop yourself over [MCP](/agentic/mcp/). The fastest way in is a [runnable demo](/start/demos/).

| Page | Agent-facing surface |
| --- | --- |
| [Server-Side Agent](/agentic/server-agent/) | GraphJin runs the catalog-first discovery loop for you and returns a typed answer via `ask_graphjin_agent` and `POST /api/v1/agent`. |
| [MCP](/agentic/mcp/) | Discovery, syntax guidance, validation, cursor IDs, saved-query execution, GraphQL tools, and watch-event resource notifications. |
| [Catalog Graph](/agentic/catalog-graph/) | `gj_catalog` rows for sources, tables, relationships, syntax, examples, workflows, and evidence. |
| [Security Graph](/agentic/security-graph/) | `gj_security` rows for effective policy, capabilities, read-only state, and findings. |
| [Source Mode](/agentic/source-mode/) | Source-local ownership, capability defaults, reloads, and `agentic.yml` inheritance. |
| [Artifacts Overlay](/agentic/artifacts/) | Global config files plus caller-scoped `gj_artifacts` overrides for queries, fragments, and workflows. |
| [Watches](/agentic/watches/) | Cursor-backed `gj_watch` standing questions evaluated with the owner's permissions; durable `gj_watch_event` inbox, explicit ephemeral leases, REST cleanup, and webhook/workflow/MCP delivery. |
| [Workflows](/agentic/workflows/) | Named, reviewed operational procedures that can call GraphJin tools and GraphQL. |
| [MCP OAuth](/agentic/oauth/) | Hosted MCP identity with protected-resource metadata, authorization metadata, and audience checks. |

The intended loop is catalog first, security second, validation or preview third, and then a governed action. That sequence is what keeps agents useful without handing them raw database credentials or arbitrary shell access.
