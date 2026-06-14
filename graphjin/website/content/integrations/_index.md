---
title: "Integrations"
description: "Databases, MongoDB, OpenAPI, filesystems, CodeSQL, and federation."
nav_group: "integrations"
weight: 3
---

Integrations explain how GraphJin exposes external systems as first-class graph surfaces while preserving the same policy model.

| Page | Integration pattern |
| --- | --- |
| [Multi-Database](/integrations/multi-database/) | Root-level result merging, cache isolation, table mapping, and nested database joins. |
| [MongoDB](/integrations/mongodb/) | GraphQL compiled to a JSON DSL and translated into MongoDB aggregation pipelines. |
| [OpenAPI](/integrations/openapi/) | Remote API operations exposed as root fields or joins on database rows. |
| [Filesystem Tables And Uploads](/integrations/filesystem-uploads/) | Local, S3, and GCS object stores exposed as queryable virtual tables. |
| [CodeSQL](/integrations/codesql/) | Source-code indexes for files, symbols, refs, docs, and guarded source edits. |
| [Apollo Federation](/integrations/federation/) | Federation v2 SDL and entity resolution for database-backed subgraphs. |

The shared rule is that each external surface becomes part of the same compiled graph. GraphJin still applies auth context, source capabilities, cache boundaries, and allow-list behavior before it calls the backend.
