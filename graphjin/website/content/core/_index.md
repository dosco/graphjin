---
title: "Core"
description: "Compiler model, query language, filters, relationships, cursors, aggregations, mutations, and subscriptions."
nav_group: "core"
weight: 2
---

Core pages explain the GraphJin query model from first query to production-grade operations.

| Page | What it covers |
| --- | --- |
| [Compiler Model](/core/compiler/) | How GraphQL becomes QCode and then SQL, MongoDB JSON DSL, or source-specific work. |
| [Query Language](/core/query-language/) | Fields, aliases, variables, fragments, directives, expressions, search, and JSON paths. |
| [Filters](/core/filters/) | Scalar, list, text, related-table, JSON, and geo filters with wrong/right examples. |
| [Ordering And Cursors](/core/ordering-cursors/) | Explicit ordering, custom order lists, distinct, root-level cursors, and opaque cursor rules. |
| [Relationships](/core/relationships/) | Same-database joins, MongoDB lookups, remote joins, cross-database joins, recursive paths, and polymorphic selections. |
| [Aggregations And Functions](/core/aggregations-functions/) | Aggregate fields, grouped summaries, expression aggregates, analytics directives, search, and database functions. |
| [Mutations](/core/mutations/) | Inserts, bulk and nested writes, connect, validation, presets, updates, deletes, and dialect mutation strategies. |
| [Subscriptions](/core/subscriptions/) | WebSocket/SSE live queries, resume cursors, MCP cursor IDs, and the dynamic security-prefix pitfall. |

The examples here are intentionally close to the Go examples and compiler tests so docs drift is easier to catch.
