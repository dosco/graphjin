---
title: "CodeSQL"
description: "Index source trees into queryable tables for symbols, references, imports, docs, and parse diagnostics."
nav_group: "integrations"
doc_kind: "guide"
weight: 50
---

## What CodeSQL exposes

CodeSQL is a managed `kind: code` source. GraphJin indexes the source tree into a read-only SQLite runtime database and exposes the public projection as `gj_code`.

```yaml
sources:
  - name: code
    kind: code
    path: ../app
    capabilities:
      code.search: true
      code.read: true
      code.write: false
      code.watch: false
      code.infer_db_refs: true
```

| `gj_code.kind` | What it represents |
| --- | --- |
| `file` | Source files, paths, hashes, languages, and text metadata |
| `symbol` | Functions, types, classes, methods, and their byte ranges |
| `ref` / `db_ref` | Code references, including database/table/column references when inferred |
| `import` | Import edges between files/modules |
| `doc` | Indexed documentation text |
| `parse_error` | Tree-sitter diagnostics for files that could not be parsed cleanly |

{{< verified by="TestCodeSQLMultiDBInitializesManagedSQLiteRuntime" file="serv/codesql_test.go" line="16" >}}

## Query code like data

```graphql
query {
  gj_code(
    where: { kind: { eq: "symbol" }, name: { iregex: "Reload" } }
    limit: 20
    order_by: { path: asc }
  ) {
    name
    symbol_kind
    path
    language
    start_byte
    end_byte
  }
}
```

{{< verified by="TestCodeSQLServiceLiveIndexAndWatch" file="tests/codesql_live_test.go" line="18" >}}
{{< verified by="TestMetadataGraphLiveLinksCodeSQLRefs" file="tests/codesql_live_test.go" line="67" >}}

## Agentic workflow

Agents can discover source facts through `gj_catalog`, inspect security posture, preview edits, and only apply changes through policy-controlled source operations.

Use CodeSQL when a model needs evidence from source code without raw shell access.

## Preview and apply edits

Write-capable CodeSQL does not expose raw derived-table mutation roots. Source edits go through change-set rows with hashes, byte ranges, expected old text, preview diffs, optional locks, and an explicit apply action.

```graphql
mutation PreviewCodeChange($input: gj_code_insert_input!) {
  gj_code(insert: $input) {
    id
    kind
    status
    diff
    errors_json
  }
}
```

{{< verified by="TestCodeSQLGraphQLSourceMutationsPreviewApplyAndLocks" file="serv/codesql_test.go" line="267" >}}
{{< verified by="TestCodeSQLRawTableMutationRootsUnavailable" file="serv/codesql_test.go" line="657" >}}

In development, live watch is enabled by default. In production, enable it only with `code.watch: true`; read-only CodeSQL disables live watching and source mutations.

{{< verified by="TestCodeSQLProductionDisablesLiveWatcherByDefault" file="serv/codesql_test.go" line="103" >}}
{{< verified by="TestCodeSQLProductionEnablesLiveWatcherWithCapability" file="serv/codesql_test.go" line="136" >}}
