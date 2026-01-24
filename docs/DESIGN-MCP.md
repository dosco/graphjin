# MCP Implementation Design

## Overview

GraphJin's MCP (Model Context Protocol) implementation enables AI assistants and LLMs to interact with databases through GraphQL using function calling (tools). The implementation follows the MCP specification from Anthropic (November 2024).

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     MCP Client (Claude, etc.)                    │
└───────────────────────────┬─────────────────────────────────────┘
                            │ JSON-RPC 2.0
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Transport Layer (implicit)                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │    Stdio    │  │     SSE     │  │    Streamable HTTP      │  │
│  │  (CLI use)  │  │ (web embed) │  │   (API integration)     │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                       mcpServer struct                           │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  srv     *server.MCPServer   // MCP SDK server              ││
│  │  service *graphjinService    // GraphJin service            ││
│  │  ctx     context.Context     // Auth context (user_id, role)││
│  └─────────────────────────────────────────────────────────────┘│
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                    graphjinService                               │
│  ┌──────────────────┐  ┌──────────────────┐                     │
│  │  core.GraphJin   │  │   Allow List     │                     │
│  │  - GraphQL()     │  │  - ListAll()     │                     │
│  │  - Subscribe()   │  │  - GetByName()   │                     │
│  └──────────────────┘  └──────────────────┘                     │
└─────────────────────────────────────────────────────────────────┘
```

## File Structure

| File | Lines | Purpose |
|------|-------|---------|
| `serv/mcp.go` | ~145 | Server init, transport handlers, auth context |
| `serv/mcp_syntax.go` | ~320 | DSL reference data structures & syntax tools |
| `serv/mcp_schema.go` | ~480 | Schema discovery tools (incl. validate_where_clause) |
| `serv/mcp_prompts.go` | ~210 | MCP prompts (write_where_clause) |
| `serv/mcp_tools.go` | ~184 | Query execution tools |
| `serv/mcp_search.go` | ~230 | Saved query discovery & fuzzy search |
| `serv/mcp_fragments.go` | ~198 | Fragment discovery tools |
| `serv/config.go` | (partial) | MCPConfig struct definition |

## Core Data Structures

### mcpServer (mcp.go:14-18)

```go
type mcpServer struct {
    srv     *server.MCPServer   // MCP Go SDK server
    service *graphjinService    // GraphJin service instance
    ctx     context.Context     // Auth context (user_id, user_role)
}
```

### MCPConfig (config.go:193-214)

```go
type MCPConfig struct {
    Disable         bool   // Disable MCP server (default: false)
    EnableSearch    bool   // Enable query/fragment search (default: true)
    AllowMutations  bool   // Allow mutation operations (default: true)
    AllowRawQueries bool   // Allow arbitrary GraphQL (default: true)
    StdioUserID     string // Default user ID for CLI
    StdioUserRole   string // Default user role for CLI
    Only            bool   // MCP-only mode (disables other endpoints)
}
```

## Tool Registration Flow

```
newMCPServerWithContext()
    ↓
registerTools()
    ├── registerSyntaxTools()      → 2 tools
    ├── registerSchemaTools()      → 6 tools
    ├── registerExecutionTools()   → 2 tools
    ├── registerQueryDiscoveryTools() → 3 tools
    └── registerFragmentTools()    → 3 tools
    ↓
registerPrompts()
    └── write_where_clause         → 1 prompt
```

## Tools Inventory (16 Total)

### 1. Syntax Reference Tools (mcp_syntax.go)

| Tool | Description | Config Check |
|------|-------------|--------------|
| `get_query_syntax` | Returns complete query DSL reference with examples | None |
| `get_mutation_syntax` | Returns mutation DSL reference with examples | None |

**Design Note**: These tools are critical because GraphJin uses a custom DSL that differs from standard GraphQL. LLMs trained on standard GraphQL won't know this syntax, so these tools teach the DSL. Examples are embedded directly in the syntax references for a better LLM experience.

### 2. Query Execution Tools (mcp_tools.go)

| Tool | Description | Config Check |
|------|-------------|--------------|
| `execute_graphql` | Execute arbitrary GraphQL queries/mutations | `AllowRawQueries`, `AllowMutations` |
| `execute_saved_query` | Execute pre-defined saved queries by name | None |

**Key Functions**:
- `handleExecuteGraphQL()` - Executes via `service.gj.GraphQL()`
- `handleExecuteSavedQuery()` - Executes via `service.gj.GraphQLByName()`
- `isMutation()` - Simple heuristic to detect mutations (checks for "mutation" keyword)

### 3. Schema Discovery Tools (mcp_schema.go)

| Tool | Description | Core API Used |
|------|-------------|---------------|
| `list_tables` | List all database tables | `gj.GetTables()` |
| `describe_table` | Get detailed schema for a table | `gj.GetTableSchema()` |
| `find_path` | Find relationship path between tables | `gj.FindRelationshipPath()` |
| `validate_graphql` | Validate query without executing | `gj.GraphQL()` (reads result) |
| `explain_graphql` | Show generated SQL for a query | `gj.GraphQL()` (reads SQL) |
| `validate_where_clause` | Validate where clause syntax and types | `gj.GetTableSchema()` |

### 4. Saved Query Discovery Tools (mcp_search.go)

| Tool | Description | Config Check |
|------|-------------|--------------|
| `list_saved_queries` | List all saved queries from allow-list | `EnableSearch` |
| `search_saved_queries` | Search queries by name (fuzzy) | `EnableSearch` |
| `get_saved_query` | Get full details of a saved query | None |

### 5. Fragment Discovery Tools (mcp_fragments.go)

| Tool | Description | Config Check |
|------|-------------|--------------|
| `list_fragments` | List all available GraphQL fragments | `EnableSearch` |
| `get_fragment` | Get full fragment definition and usage | None |
| `search_fragments` | Search fragments by name (fuzzy) | `EnableSearch` |

## MCP Prompts (mcp_prompts.go)

MCP prompts provide structured guidance to help LLMs construct valid queries.

| Prompt | Description | Arguments |
|--------|-------------|-----------|
| `write_where_clause` | Generate where clause guidance | `table` (required), `intent` (required) |

The `write_where_clause` prompt:
1. Fetches table schema via `gj.GetTableSchema(table)`
2. Builds operator-type mapping based on column types
3. Returns structured guidance with examples for each operator type

### Operator-Type Mapping

| Column Type | Valid Operators |
|-------------|-----------------|
| numeric (int, float, decimal) | eq, neq, gt, gte, lt, lte, in, nin, is_null |
| text (varchar, text, char) | eq, neq, like, ilike, regex, in, nin, is_null |
| boolean | eq, neq, is_null |
| json/jsonb | has_key, has_key_any, has_key_all, contains, contained_in, is_null |
| array | contains, contained_in, has_in_common, is_null |
| geometry | st_dwithin, st_within, st_contains, st_intersects, near |
| timestamp/date | eq, neq, gt, gte, lt, lte, in, is_null |
| uuid | eq, neq, in, nin, is_null |

## Fuzzy Search Algorithm (mcp_search.go:186-230)

The search tools use a scoring algorithm:

| Match Type | Score |
|------------|-------|
| Exact match | 100 |
| Starts with | 90 |
| Contains | 70 |
| Word boundary (prefix of word segment) | 60 |
| Character-by-character fuzzy | 0-50 (weighted) |

```go
func fuzzyScore(search, target string) int
```

## Transport Mechanisms

### 1. Stdio Transport (CLI)

```go
func (s *HttpService) RunMCPStdio(ctx context.Context) error
```

- Entry point: `graphjin mcp --config ./config`
- Auth: Environment variables (`GRAPHJIN_USER_ID`, `GRAPHJIN_USER_ROLE`) or config defaults
- Uses: `server.ServeStdio(mcpSrv.srv)`

### 2. SSE Transport (Server-Sent Events)

```go
func (s *HttpService) MCPHandler() http.Handler
func (s *HttpService) MCPHandlerWithAuth(ah auth.HandlerFunc) http.Handler
```

- Endpoint: `GET /api/v1/mcp`
- Uses: `server.NewSSEServer(mcpSrv.srv).ServeHTTP()`

### 3. Streamable HTTP Transport

```go
func (s *HttpService) MCPMessageHandler() http.Handler
func (s *HttpService) MCPMessageHandlerWithAuth(ah auth.HandlerFunc) http.Handler
```

- Endpoint: `POST /api/v1/mcp/message`
- Note: Currently reuses SSE server which handles both

## Auth Context Flow

```
Request/CLI
    ↓
Auth extraction (env vars, JWT, headers)
    ↓
context.WithValue(ctx, core.UserIDKey, userID)
context.WithValue(ctx, core.UserRoleKey, userRole)
    ↓
mcpServer.ctx
    ↓
Passed to gj.GraphQL() calls
    ↓
Used for role-based access control in queries
```

## Security Controls

1. **Query Restrictions**:
   - `AllowRawQueries: false` → Only `execute_saved_query` works
   - `AllowMutations: false` → All mutations blocked in `execute_graphql`

2. **Search Restrictions**:
   - `EnableSearch: false` → Disables `list_saved_queries`, `search_saved_queries`, `list_fragments`, `search_fragments`

3. **Auth Integration**:
   - HTTP: Uses same auth middleware as GraphQL/REST endpoints
   - CLI: Environment variables or config defaults

## DSL Reference Data (mcp_syntax.go)

Static reference data is defined as Go variables:

- `querySyntaxReference` - Filter operators, pagination, ordering, aggregations, recursive, directives
- `mutationSyntaxReference` - Insert, update, upsert, delete, nested mutations, connect/disconnect
- `queryExamples` - Categorized examples (basic, filtering, relationships, pagination, aggregations, recursive, mutations, spatial)

### Filter Operators

| Category | Operators |
|----------|-----------|
| Comparison | eq, neq, gt, gte, lt, lte |
| List | in, nin |
| Null | is_null |
| Text | like, ilike, regex, iregex, similar |
| JSON | has_key, has_key_any, has_key_all, contains, contained_in |
| Spatial | st_dwithin, st_within, st_contains, st_intersects, st_coveredby, st_covers, st_touches, st_overlaps, near |

## Dependencies

- `github.com/mark3labs/mcp-go` - MCP Go SDK
  - `server.MCPServer` - Main server type
  - `server.ServeStdio()` - Stdio transport
  - `server.NewSSEServer()` - SSE transport
  - `mcp.NewTool()` - Tool definition
  - `mcp.CallToolRequest` - Tool call request
  - `mcp.NewToolResultText()` / `mcp.NewToolResultError()` - Results

## Response Format

All tools return JSON-formatted responses via `mcp.NewToolResultText(string(data))`:

```go
data, err := json.MarshalIndent(result, "", "  ")
if err != nil {
    return mcp.NewToolResultError(err.Error()), nil
}
return mcp.NewToolResultText(string(data)), nil
```

Execution tools include:
- `data` - Query result
- `errors` - Any errors
- `sql` - Generated SQL (for debugging/transparency)

## Key Design Decisions

1. **DSL Education First**: Syntax tools are registered first and should be called before writing queries, because GraphJin DSL differs from standard GraphQL.

2. **Transport Abstraction**: Transport is implicit based on context (CLI vs HTTP) - no configuration needed.

3. **Namespace Support**: Most tools accept optional `namespace` parameter for multi-tenant deployments.

4. **Safety by Default**: MCP is enabled by default but can be restricted via configuration.

5. **Fuzzy Search**: Search tools use intelligent scoring to help find relevant queries/fragments even with partial matches.

6. **Auth Context Preservation**: User context flows through to query execution, enabling role-based access control.
