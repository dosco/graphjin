---
title: "Test-Backed Examples"
description: "A feature map tied to the Go examples and focused tests that exercise GraphJin behavior."
nav_group: "reference"
doc_kind: "reference"
weight: 50
---

Source note: this page is curated from `tests/*_test.go`, `core/**/*_test.go`, `serv/**/*_test.go`, `mongodriver/**/*_test.go`, and the MCP syntax tests.

Beyond unit-level examples, the repo ships four runnable demo verticals (coffee-roastery, corrugated-plant, pcb-fab, clinic-scheduler) with end-to-end smoke suites covering data, workflows, watches, artifacts, structured refusals, roles, and MCP sampling — see `examples/README.md` (`make smoke-all`).

## Query examples

| Feature | Test |
| --- | --- |
| Basic query | `Example_query` in `tests/query_test.go` |
| Variables | `Example_queryWithVariables` |
| Filters | `Example_queryWithWhere1`, `Example_queryWithWhereIn` |
| Related filters | `Example_queryWithWhereOnRelatedTable` |
| JSON path filters | `Example_queryJSONPathOperations`, `Example_queryWithWhereHasAnyKey` |
| Geo filters | `Example_queryWithGeoFilter`, `Example_queryWithGeoContains`, `TestGeoNear` |
| Fragments | `Example_queryWithFragments1` |
| Cursors | `Example_queryWithNamedCursorPagination` |
| Nested independent cursors | `Example_queryWithNestedIndependentCursors` |
| Recursive relationships | `Example_queryWithRecursiveRelationship1` |
| Polymorphic relationships | `Example_queryWithUnionForPolymorphicRelationships` |
| Aggregates | `Example_queryWithAggregation`, `Example_queryWithGlobalAggBrokenMdFix` |
| Expression aggregates | `Example_queryWithExprMul`, `Example_queryWithExprBare` |
| Analytics directives | `TestAnalytics_DialectRendering`, `TestAnalytics_DialectUnsupported` |

## Mutation examples

| Feature | Test |
| --- | --- |
| Insert | `Example_insert` |
| Bulk insert | `Example_insertBulk` |
| Nested insert | `Example_insertIntoMultipleRelatedTables` |
| Connect related rows | `Example_insertIntoTableAndConnectToRelatedTables` |
| Update | `Example_update` |
| Delete | `TestMultiAliasDelete` |
| Read-only enforcement | `TestReadOnlyDB_WithRolesAndTables` |

## Integration examples

| Feature | Test |
| --- | --- |
| OpenAPI join | `Example_queryWithOpenAPIJoin` |
| OpenAPI top-level fields | `Example_queryWithOpenAPITopLevel` |
| Multi-database root routing | `Example_multiDBQueryPostgres`, `Example_multiDBQuerySQLite`, `Example_multiDBQueryMongoDB` |
| Multi-database join internals | `TestBuildChildGraphQLQueryNestedDatabaseJoin`, `TestResolveDatabaseJoinsNullID` |
| MongoDB driver DSL | `TestQueryDSLParsing` |
| MongoDB cursor pagination | `Example_queryWithNamedCursorPaginationMultiplePages` |
| MCP install/client behavior | `TestMCPCLIParity`, `TestCallTool_ReturnsStructuredContent` |
| MCP cursor IDs | `TestProcessCursorsForMCP`, `TestMCP_CursorRoundtripIntegration` |
| MCP OAuth | `TestMCPOAuthProtectedResourceMetadata` |
| Workflows | `TestRunNamedWorkflow_CanCallGJTools` |
| CodeSQL source discovery | `TestMetadataGraphLiveLinksCodeSQLRefs` |

Use this page to pick examples when writing docs, demos, or regression tests. The examples are part of the test suite, so they are better anchors than invented snippets.

When adding a public feature:

1. Add or find the test that proves it.
2. Update the focused docs page.
3. Update `serv/mcp_syntax.go` if an AI client needs to learn the syntax.
4. Add the example to this map when it is broadly useful.
