# GraphJin MCP (Model Context Protocol) Design & Architecture

GraphJin provides native support for the Model Context Protocol (MCP), enabling AI assistants and LLMs to interact with your database through GraphQL using function calling (tools).

## Overview

**Model Context Protocol (MCP)** is an open standard by Anthropic (November 2024) that standardizes how AI applications connect with external tools and data sources. GraphJin's MCP integration allows AI assistants like Claude to:

- Execute GraphQL queries and mutations against your database
- Discover available saved queries
- Search for relevant queries by name or description
- Explore database schema (tables, columns, relationships)
- Generate OpenAPI specifications

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     MCP Client (Claude, etc.)                    │
└───────────────────────────┬─────────────────────────────────────┘
                            │ JSON-RPC 2.0
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                    MCP Transport Layer                           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐  │
│  │    Stdio    │  │     SSE     │  │    Streamable HTTP      │  │
│  │  (CLI use)  │  │ (web embed) │  │   (API integration)     │  │
│  └─────────────┘  └─────────────┘  └─────────────────────────┘  │
└───────────────────────────┬─────────────────────────────────────┘
                            ▼
┌─────────────────────────────────────────────────────────────────┐
│                     serv/mcp.go                                  │
│  ┌─────────────────────────────────────────────────────────────┐│
│  │                    MCP Server                               ││
│  │  ┌─────────────┐ ┌─────────────┐ ┌─────────────────────┐   ││
│  │  │ Query Tools │ │ Discovery   │ │ Schema Tools        │   ││
│  │  │             │ │ Tools       │ │                     │   ││
│  │  └─────────────┘ └─────────────┘ └─────────────────────┘   ││
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

## Transport Protocols

GraphJin MCP supports multiple transport mechanisms. **Transport is implicit based on context** - no configuration needed:

### 1. Stdio Transport (CLI)

For CLI usage and local AI assistants like Claude Desktop:

```bash
graphjin mcp --config ./config
```

Configure in `claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "graphjin": {
      "command": "graphjin",
      "args": ["mcp", "--config", "/path/to/config"],
      "env": {
        "GRAPHJIN_USER_ID": "admin-user",
        "GRAPHJIN_USER_ROLE": "admin"
      }
    }
  }
}
```

### 2. SSE Transport (Server-Sent Events)

For web-based integrations. Automatically enabled when MCP is enabled and the HTTP service is running.

Endpoint: `GET /api/v1/mcp`

### 3. Streamable HTTP Transport

For stateless API integrations. Automatically enabled alongside SSE.

Endpoint: `POST /api/v1/mcp/message`

**Note**: Both SSE and HTTP endpoints are registered automatically (MCP is enabled by default). Use `mcp.disable: true` to turn off.

## Critical: GraphJin Query DSL

**GraphJin is NOT standard GraphQL.** It has its own Domain-Specific Language (DSL) with specific operators and syntax that differs from standard GraphQL:

| Feature | Standard GraphQL | GraphJin DSL |
|---------|------------------|--------------|
| Filtering | Args defined in schema | `where: { price: { gt: 10 } }` |
| Operators | Schema-dependent | 15+ built-in: `eq`, `neq`, `gt`, `in`, `has_key`, etc. |
| Pagination | `first`/`after` (Relay) | `limit`, `first`/`after` + `_cursor` field |
| Ordering | Schema-dependent | `order_by: { price: desc }` |
| Aggregations | Requires schema | `count_id`, `sum_price`, `avg_price` |
| Recursive | Not supported | `find: "parents"` / `find: "children"` |
| Full-text | Not built-in | `search: "term"` |

**LLMs trained on standard GraphQL won't know this syntax.** The MCP tools include syntax reference tools to teach the DSL.

## Available Tools (16 Total)

### 1. Syntax Reference Tools (Call First!)

These tools teach the LLM GraphJin's query DSL. **Call `get_query_syntax` before writing queries.**

#### `get_query_syntax`

Returns the complete GraphJin query syntax reference.

**Input:** None

**Output:**
```json
{
  "filter_operators": {
    "comparison": ["eq", "neq", "gt", "gte", "lt", "lte"],
    "list": ["in", "nin"],
    "null": ["is_null"],
    "text": ["like", "ilike", "regex", "iregex", "similar"],
    "json": ["has_key", "has_key_any", "has_key_all", "contains", "contained_in"]
  },
  "logical_operators": ["and", "or", "not"],
  "pagination": {
    "limit_offset": "limit: 10, offset: 20",
    "cursor": "first: 10, after: $cursor",
    "cursor_field": "<table>_cursor returns encrypted cursor"
  },
  "ordering": {
    "simple": "order_by: { price: desc }",
    "multiple": "order_by: { price: desc, id: asc }",
    "nested": "order_by: { owner: { name: asc } }"
  },
  "aggregations": ["count_<col>", "sum_<col>", "avg_<col>", "min_<col>", "max_<col>"],
  "recursive": {
    "find_parents": "comments(find: \"parents\")",
    "find_children": "comments(find: \"children\")"
  },
  "full_text_search": "products(search: \"term\")",
  "example_query": "{ products(where: { price: { gt: 10 } }, order_by: { price: desc }, limit: 5) { id name } }"
}
```

#### `get_mutation_syntax`

Returns the GraphJin mutation syntax reference.

**Input:** None

**Output:**
```json
{
  "operations": {
    "insert": "products(insert: { name: \"New\", price: 10 })",
    "bulk_insert": "products(insert: $items)",
    "update": "products(id: $id, update: { name: \"Updated\" })",
    "upsert": "products(upsert: { id: $id, name: \"Name\" })",
    "delete": "products(delete: true, where: { id: { eq: $id } })"
  },
  "nested_mutations": "purchases(insert: { quantity: 5, customer: { email: \"new@test.com\" } })",
  "connect_disconnect": {
    "connect": "products(insert: { name: \"X\", owner: { connect: { id: 5 } } })",
    "disconnect": "users(id: $id, update: { products: { disconnect: { id: 10 } } })"
  },
  "validation": "@constraint(variable: \"email\", format: \"email\")"
}
```

#### `get_query_examples`

Returns annotated example queries for common patterns.

**Input:**
```json
{
  "category": "filtering"  // Optional: basic, filtering, relationships, pagination, mutations
}
```

**Output:**
```json
{
  "examples": [
    {"description": "Filter with comparison", "query": "{ products(where: { price: { gt: 50 } }) { id name } }"},
    {"description": "Filter with AND/OR", "query": "{ products(where: { and: [{ price: { gt: 10 } }, { price: { lt: 100 } }] }) { id } }"},
    {"description": "Filter on relationship", "query": "{ products(where: { owner: { email: { eq: $email } } }) { id } }"}
  ]
}
```

### 2. Query Execution Tools

#### `execute_graphql`

Execute a GraphQL query or mutation against the database.

**Input:**
```json
{
  "query": "{ products(where: { price: { gt: 10 } }, limit: 5) { id name price } }",
  "variables": {},
  "namespace": "optional_namespace"
}
```

**Output:**
```json
{
  "data": {"products": [...]},
  "errors": [],
  "sql": "SELECT ... FROM products WHERE price > 10 LIMIT 5"
}
```

#### `execute_saved_query`

Execute a pre-defined saved query from the allow-list.

**Input:**
```json
{
  "name": "get_products_by_price",
  "variables": {"min_price": 10},
  "namespace": "optional_namespace"
}
```

**Output:** Same as `execute_graphql`

### 3. Schema Discovery Tools

#### `list_tables`

List all database tables available for querying.

**Input:**
```json
{
  "namespace": "optional_namespace"
}
```

**Output:**
```json
{
  "tables": [
    {"name": "users", "type": "table", "columns_count": 8},
    {"name": "products", "type": "table", "columns_count": 12},
    {"name": "hot_products", "type": "view", "columns_count": 3}
  ]
}
```

#### `describe_table`

Get detailed schema information with incoming and outgoing relationships.

**Input:**
```json
{
  "table": "products"
}
```

**Output:**
```json
{
  "name": "products",
  "columns": [
    {"name": "id", "type": "integer", "nullable": false, "primary_key": true},
    {"name": "name", "type": "text", "nullable": false},
    {"name": "price", "type": "numeric", "nullable": false},
    {"name": "owner_id", "type": "integer", "nullable": false, "foreign_key": "users.id"}
  ],
  "relationships": {
    "outgoing": [
      {"field": "owner", "target_table": "users", "type": "many_to_one", "foreign_key": "owner_id"}
    ],
    "incoming": [
      {"field": "purchases", "source_table": "purchases", "type": "one_to_many", "foreign_key": "product_id"},
      {"field": "customers", "source_table": "users", "type": "many_to_many", "through": "purchases"}
    ]
  }
}
```

#### `find_path`

Find the relationship path between two tables.

**Input:**
```json
{
  "from_table": "users",
  "to_table": "categories"
}
```

**Output:**
```json
{
  "path": [
    {"from": "users", "to": "products", "via": "owner_id", "relation": "one_to_many"},
    {"from": "products", "to": "categories", "via": "category_id", "relation": "many_to_one"}
  ],
  "example_query": "{ users { products { category { name } } } }"
}
```

### 4. Saved Query Discovery Tools

#### `list_saved_queries`

List all saved queries from the allow-list.

**Input:** None

**Output:**
```json
{
  "queries": [
    {"name": "get_users", "operation": "query", "description": "Fetch users"},
    {"name": "create_user", "operation": "mutation", "description": "Create user"}
  ]
}
```

#### `search_saved_queries`

Search saved queries by name or description.

**Input:**
```json
{
  "query": "user",
  "limit": 10
}
```

**Output:** Same as `list_saved_queries` (filtered)

#### `get_saved_query`

Get full details of a saved query.

**Input:**
```json
{
  "name": "get_users"
}
```

**Output:**
```json
{
  "name": "get_users",
  "operation": "query",
  "query": "query get_users($role: String) { users(where: {role: {eq: $role}}) { id name } }",
  "variables_schema": {"role": {"type": "string", "required": false}},
  "description": "Fetch users with optional role filter"
}
```

### 5. Fragment Discovery Tools

#### `list_fragments`

List all available GraphQL fragments.

**Input:**
```json
{
  "namespace": "optional_namespace"
}
```

**Output:**
```json
{
  "fragments": [
    {"name": "UserFields", "namespace": ""},
    {"name": "ProductFields", "namespace": ""}
  ],
  "count": 2,
  "usage": "To use a fragment, add: #import \"./fragments/<name>\" at the top of your query"
}
```

#### `get_fragment`

Get full details of a fragment including its definition.

**Input:**
```json
{
  "name": "UserFields"
}
```

**Output:**
```json
{
  "name": "UserFields",
  "on": "users",
  "definition": "fragment UserFields on users {\n  id\n  email\n  name\n}",
  "import_directive": "#import \"./fragments/UserFields\"",
  "usage_example": "query { users { ...UserFields } }"
}
```

#### `search_fragments`

Search fragments by name using fuzzy matching.

**Input:**
```json
{
  "query": "user",
  "limit": 10
}
```

**Output:**
```json
{
  "fragments": [
    {"name": "UserFields", "namespace": ""}
  ],
  "count": 1
}
```

### 6. Utility Tools

#### `validate_graphql`

Validate a query without executing it.

**Input:**
```json
{
  "query": "{ products(where: { price: { gt: 10 } }) { id name } }"
}
```

**Output:**
```json
{
  "valid": true,
  "errors": [],
  "sql": "SELECT ... FROM products WHERE price > 10"
}
```

#### `explain_graphql`

Show the generated SQL for a query.

**Input:**
```json
{
  "query": "{ products { id name owner { email } } }",
  "variables": {}
}
```

**Output:**
```json
{
  "sql": "SELECT jsonb_build_object('products', ...) FROM products LEFT JOIN LATERAL (SELECT ... FROM users WHERE users.id = products.owner_id) AS owner ON true",
  "tables_accessed": ["products", "users"],
  "estimated_rows": 100
}
```

## Configuration

### Basic Configuration

MCP is **enabled by default**. No configuration needed to use it.

```yaml
# MCP works out of the box - these are the defaults:
mcp:
  # disable: false           # MCP is ON by default
  enable_search: true        # Enable search_queries tool
  allow_mutations: true      # Allow mutation operations
  allow_raw_queries: true    # Allow arbitrary GraphQL (vs only named queries)
```

**Note**: Transport is implicit based on context - no configuration needed:
- CLI (`graphjin mcp`) → stdio transport automatically
- HTTP service → SSE/HTTP transport at `/api/v1/mcp` automatically

### Disabling MCP

To turn off MCP:

```yaml
mcp:
  disable: true
```

### Security Configuration

For production environments, consider restricting MCP capabilities:

```yaml
mcp:
  allow_mutations: false     # Disable mutations for safety
  allow_raw_queries: false   # Only allow pre-defined queries
```

### Stdio Authentication

For CLI usage with `graphjin mcp`, configure default credentials:

```yaml
mcp:
  stdio_user_id: "admin-123"     # Default user ID for CLI
  stdio_user_role: "admin"       # Default user role for CLI
```

These can be overridden via environment variables:
- `GRAPHJIN_USER_ID` - overrides `stdio_user_id`
- `GRAPHJIN_USER_ROLE` - overrides `stdio_user_role`

### Full Configuration Reference

| Option | Type | Default | Description |
|--------|------|---------|-------------|
| `disable` | bool | false | Disable MCP server (MCP is ON by default) |
| `enable_search` | bool | true | Enable query search functionality |
| `allow_mutations` | bool | true | Allow mutation operations |
| `allow_raw_queries` | bool | true | Allow arbitrary GraphQL queries |
| `stdio_user_id` | string | "" | Default user ID for stdio transport (CLI) |
| `stdio_user_role` | string | "" | Default user role for stdio transport (CLI) |

## Security Considerations

### 1. Query Restrictions

Control what operations AI can perform:

- **`allow_raw_queries: false`**: Only pre-defined queries from allow-list can execute
- **`allow_mutations: false`**: Disable all write operations

### 2. Authentication

MCP integrates with GraphJin's existing authentication system. Auth context (user_id, user_role) flows through to query execution, enabling role-based access control and user-scoped queries.

#### HTTP Transport (SSE/HTTP)

MCP HTTP endpoints (`/api/v1/mcp`, `/api/v1/mcp/message`) use the same authentication middleware as GraphQL/REST endpoints:

**JWT Authentication:**
```yaml
auth:
  type: jwt
  jwt:
    provider: auth0  # or firebase, jwks, generic
    audience: "my-api"
    issuer: "https://my-tenant.auth0.com/"
```

Token via `Authorization: Bearer <token>` header or cookie.

**Header-based Authentication:**
```yaml
auth:
  type: header
  header:
    name: X-API-Key
    value: $API_KEY
```

**Development Mode:**
```yaml
auth:
  development: true
```

Allows `X-User-ID` and `X-User-Role` headers for testing.

#### Stdio Transport (CLI)

For `graphjin mcp` CLI usage, authentication is provided via:

**1. Environment Variables (recommended for Claude Desktop):**
```bash
export GRAPHJIN_USER_ID="admin-123"
export GRAPHJIN_USER_ROLE="admin"
graphjin mcp --config ./config
```

**2. Claude Desktop Configuration:**
```json
{
  "mcpServers": {
    "graphjin": {
      "command": "graphjin",
      "args": ["mcp", "--config", "/path/to/config"],
      "env": {
        "GRAPHJIN_USER_ID": "admin-user",
        "GRAPHJIN_USER_ROLE": "admin"
      }
    }
  }
}
```

**3. Config File (fallback defaults):**
```yaml
mcp:
  stdio_user_id: "default-user"
  stdio_user_role: "user"
```

Environment variables take precedence over config file values.

### 3. Rate Limiting

MCP endpoints use the same rate limiting as other GraphJin APIs:

```yaml
rate_limiter:
  rate: 100.0
  bucket: 20
```

### 4. Audit Logging

All MCP tool invocations are logged with:
- Tool name and parameters
- User context (if authenticated)
- Timestamp and duration
- Generated SQL (if enabled)

## Usage Examples

### Claude Desktop Integration

1. Install GraphJin globally or ensure it's in PATH
2. Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "my-database": {
      "command": "graphjin",
      "args": ["mcp", "--config", "/path/to/config.yaml"],
      "env": {
        "GRAPHJIN_USER_ID": "admin-user",
        "GRAPHJIN_USER_ROLE": "admin"
      }
    }
  }
}
```

3. Restart Claude Desktop
4. Ask Claude: "List all tables in my database"

### Web Application Integration

MCP is enabled by default. Simply connect from your application:

```javascript
const eventSource = new EventSource('/api/v1/mcp');
// Handle MCP protocol messages
```

### Programmatic Access

Use the MCP Go SDK:

```go
import "github.com/mark3labs/mcp-go/client"

mcpClient := client.NewSSEMCPClient("http://localhost:8080/api/v1/mcp")
if err := mcpClient.Start(ctx); err != nil {
    log.Fatal(err)
}

result, err := mcpClient.CallTool(ctx, "list_tables", map[string]any{})
if err != nil {
    log.Fatal(err)
}
```

## File Structure

```
serv/
├── mcp.go              # MCP server setup, transport handlers, auth integration
├── mcp_syntax.go       # Syntax reference data (query/mutation DSL)
├── mcp_schema.go       # Schema discovery tools (list_tables, describe_table, find_path)
├── mcp_tools.go        # Execution tools (execute_graphql, execute_saved_query)
├── mcp_search.go       # Saved query discovery and search
├── mcp_fragments.go    # Fragment discovery tools (list_fragments, get_fragment, search_fragments)
└── config.go           # MCPConfig struct (includes MCP auth config)
```

## Dependencies

- `github.com/mark3labs/mcp-go` - MCP Go SDK

## Comparison with Other Endpoints

| Feature | GraphQL | REST | WebSocket | MCP |
|---------|---------|------|-----------|-----|
| Query execution | Yes | Yes | Yes | Yes |
| Mutations | Yes | Yes | No | Configurable |
| Subscriptions | No | No | Yes | No (use WebSocket) |
| Schema discovery | Introspection | OpenAPI | No | Yes |
| Query search | No | No | No | Yes |
| Syntax reference | No | No | No | Yes |
| Query validation | No | No | No | Yes |
| AI-optimized | No | No | No | Yes |

## Future Enhancements

1. **MCP Resources**: Expose database tables as MCP resources
2. **MCP Prompts**: Pre-defined prompt templates for common operations
3. **Subscription Support**: Polling-based subscriptions for real-time data
4. **Semantic Search**: Vector-based query search using embeddings
5. **MCP-Native OAuth 2.1**: Full MCP specification compliance (see below)

---

## MCP Protocol-Native OAuth (Deferred)

The MCP specification (November 2025) includes its own OAuth 2.1-based authorization standard. This is documented here for future reference.

### MCP OAuth 2.1 Specification

| Requirement | Details |
|-------------|---------|
| **OAuth 2.1** | MCP servers SHOULD implement OAuth 2.1 for HTTP transports |
| **PKCE** | Mandatory - clients MUST use S256 code challenge |
| **Discovery** | `/.well-known/oauth-authorization-server` and `/.well-known/oauth-protected-resource` |
| **Client Registration** | CIMD (Client ID Metadata Documents) preferred over DCR |
| **Step-up Auth** | Incremental scope requests supported |
| **Resource Indicators** | RFC 8707 for token binding |

### Current Ecosystem Adoption (January 2026)

| Client | OAuth 2.1 | CIMD Support | Notes |
|--------|-----------|--------------|-------|
| Claude Desktop | Yes | Partial | Works with standard OAuth |
| Claude.ai (Web) | Yes | Limited | Still uses Dynamic Registration |
| Cursor | Yes | In Progress | Community requesting CIMD |
| MCP-Inspector | Yes | Yes | Official test tool |

### Why We Defer MCP-Native OAuth

GraphJin currently uses its existing JWT/header-based authentication for MCP, which:
- Works with current Claude Desktop and Cursor implementations
- Leverages existing auth infrastructure
- Is simpler for private/internal deployments

Full MCP OAuth 2.1 implementation would require:
- OAuth 2.1 authorization server endpoints
- PKCE implementation
- `.well-known` discovery documents
- Token generation and validation
- Client registration (CIMD)

**Recommendation**: Revisit when CIMD support is stable across major clients, or when building public MCP APIs.

### References

- [MCP Specification 2025-11-25](https://modelcontextprotocol.io/specification/2025-11-25)
- [MCP Auth Updates (Auth0)](https://auth0.com/blog/mcp-specs-update-all-about-auth/)
- [November 2025 Authorization Changes](https://den.dev/blog/mcp-november-authorization-spec/)
- [MCP Authorization Guide (Stytch)](https://stytch.com/blog/MCP-authentication-and-authorization-guide/)
