---
title: "Sources Mode"
description: "Configure named sources and capabilities for databases, files, APIs, code, and system surfaces."
nav_group: "configure"
doc_kind: "reference"
weight: 10
---

## Source shape

```yaml
sources:
  - name: primary
    kind: database
    type: postgres
    database:
      url: ${DATABASE_URL}
    capabilities:
      data.read: true
      data.write: true
```

Sources mode is the configuration model for multi-source and agentic deployments. It keeps ownership, capabilities, read-only defaults, and reload behavior tied to the source that changed.

## Capability registry

New `sources[].capabilities` keys must come from the central registry in `core/sourcecap`. Catalog, security, MCP, and defaults should consume that registry rather than introducing one-off strings.

## Migration notes

The deprecated top-level database spelling still works, but new source-aware deployments should use named sources.

See the canonical migration reference in `docs/SOURCE-MODE-MIGRATION.md`.
