---
title: "Start"
description: "Install GraphJin, run the first query, and understand the production saved-query model."
nav_group: "start"
weight: 1
---

Start with a local project, then move toward production mode where clients run reviewed operations instead of arbitrary GraphQL text.

| Page | Use it for |
| --- | --- |
| [Install](/start/install/) | Install the CLI, scaffold a project, and connect MCP clients. |
| [Quick Start](/start/quick-start/) | Create the smallest useful config and run a joined query. |
| [First Query](/start/first-query/) | Understand root fields, primary-key lookups, aliases, variables, and explicit ordering. |
| [Saved Queries](/start/saved-queries/) | Move from dynamic development queries to reviewed production operations. |

Most examples use the test schema from `tests/postgres.sql`: `users`, `products`, `purchases`, `comments`, categories, and a few JSON/geo fixtures. The same GraphQL shape is what the Go example tests execute across dialects.
