# MCP Implementation Design

## Overview

GraphJin exposes a Model Context Protocol server so AI clients can discover,
validate, and execute governed GraphQL work through the same runtime that serves
HTTP, REST/OpenAPI, subscriptions, workflows, and source-mode control-plane
tables.

Current public connection paths:

```bash
# Hosted GraphJin with standards MCP OAuth
codex mcp add graphjin --url https://graphjin.example.com/api/v1/mcp

# Local/dev or legacy auth_login helper
graphjin mcp add codex http://localhost:8080
graphjin mcp add claude http://localhost:8080
```

`graphjin mcp setup` remains for CLI token refresh and older automation. New
onboarding should use `graphjin mcp add`.

## Architecture

```
MCP client
  |
  | JSON-RPC over stdio or Streamable HTTP
  v
GraphJin MCP transport
  |
  | request context with user_id, claims, roles, source access
  v
mcpServer
  |
  | tools, prompts, catalog resources
  v
graphjinService -> core.GraphJin -> configured databases/sources/workflows
```

The MCP server is not a resolver layer. Tool calls route back into the compiler
and service surfaces so RBAC, row filters, allow-lists, source capabilities,
workflow policy, and audit/runtime events stay consistent with the HTTP API.

## Transport

### Stdio

`graphjin mcp` runs a local stdio MCP server when no saved client config exists.
This is used for local projects and demos. Identity can come from:

- `--user-id` / `--user-role`
- `GRAPHJIN_USER_ID` / `GRAPHJIN_USER_ROLE`
- `mcp.stdio_user_id` / `mcp.stdio_user_role`

When `~/.config/graphjin/client.json` exists, `graphjin mcp` switches to proxy
mode and forwards stdio JSON-RPC to the saved remote GraphJin server with the
saved bearer token.

### Streamable HTTP

The primary remote MCP endpoint is:

```text
/api/v1/mcp
```

It uses the streamable HTTP MCP server in stateless mode. The older
`/api/v1/mcp/message` route remains for compatibility with the local proxy and
older clients, but public docs should point clients at `/api/v1/mcp`.

## CLI Add Flow

`graphjin mcp add [client] [server-url]` defaults to:

- `client=codex`
- `server=http://localhost:8080`
- project scope

It normalizes the server URL to `/api/v1/mcp`, probes the endpoint, then chooses:

| Probe result | Installed config |
| --- | --- |
| No auth | Native MCP URL |
| Standards OAuth challenge with `resource_metadata` | Native MCP URL |
| GraphJin `auth_login` device endpoint | Device-code login, save `client.json`, install local proxy |
| Other auth | Short actionable error |

`--global` is the simple global-scope shortcut. `--scope` remains for scripts.
`install` is a compatibility alias for `add`; `plugin install` is a deprecated
Claude alias.

## OAuth

`mcp.oauth` enables standards-compatible hosted MCP OAuth.

```yaml
auth:
  type: jwt

mcp:
  oauth:
    enabled: true
    mode: builtin   # builtin or external
    scopes: ["mcp"]
```

### Protected Resource Metadata

GraphJin serves OAuth protected-resource metadata at:

```text
/.well-known/oauth-protected-resource
/.well-known/oauth-protected-resource/api/v1/mcp
```

The document advertises the MCP resource, authorization servers, supported
scopes, and bearer method. MCP 401 responses include:

```text
WWW-Authenticate: Bearer resource_metadata="https://graphjin.example.com/.well-known/oauth-protected-resource/api/v1/mcp"
```

### Authorization Server Metadata

In builtin mode GraphJin also serves:

```text
/.well-known/oauth-authorization-server
/.well-known/openid-configuration
```

The metadata advertises authorization-code + PKCE, refresh tokens, DCR, and
Client ID Metadata Documents (CIMD) when enabled.

### Builtin Mode

Builtin mode reuses `auth_login`:

1. MCP client discovers protected-resource metadata.
2. Client discovers GraphJin's authorization-server metadata.
3. Client registers through DCR or supplies a CIMD `client_id` URL.
4. GraphJin starts an authorization-code + PKCE flow.
5. User signs in through the configured OIDC provider.
6. GraphJin validates identity with the existing `auth_login` allow-lists.
7. GraphJin mints a local JWT with `aud` set to the MCP resource URL.
8. MCP requests validate signature/issuer and reject audience/resource mismatch.

The same verified identity is then used by existing roles, row filters, source
access rules, and MCP tool gates.

### External Mode

External mode advertises configured authorization servers and expects bearer
tokens to be validated by `auth.jwt`. MCP-specific auth ignores the global
`auth.jwt.audience` during signature/issuer verification, then explicitly checks
that the token `aud` contains the MCP resource URL.

### Safety

- Hosted MCP URLs should use HTTPS.
- `http://localhost:8080/api/v1/mcp` is valid for loopback development.
- DCR redirect URIs must be HTTPS, except loopback HTTP for local clients.
- CIMD metadata URLs must be HTTPS and cannot point at localhost/private
  network addresses.
- Authorization codes are one-time use and PKCE S256 is required.

## Tool Surface

The current tool surface is catalog-first:

- `query_catalog`
- `get_catalog_card`
- `graphql_help`
- `validate_where_clause`
- execution tools for saved queries/raw GraphQL when enabled
- workflow, config, schema, security, runtime, and source-mode tools gated by
  `mcp.*` config and source capabilities

Legacy discovery/syntax tools can still be enabled with `mcp.legacy_discovery`,
but new clients should start with catalog/search guidance.

## Key Files

| File | Responsibility |
| --- | --- |
| `cmd/mcp_install.go` | `mcp add`, compatibility aliases, probing, client config writes |
| `cmd/mcp_proxy.go` | local stdio-to-HTTP proxy for saved `auth_login` tokens |
| `serv/mcp.go` | MCP server creation and stdio/HTTP handlers |
| `serv/mcp_oauth.go` | OAuth metadata, DCR, CIMD, PKCE token flow, MCP audience checks |
| `serv/auth_login.go` | OIDC device-code flow reused by builtin MCP OAuth |
| `serv/mcp_catalog.go` | catalog-first discovery tools |
| `serv/mcp_config.go` | config and source-mode control-plane tools |
| `serv/config.go` | `MCPConfig` and `MCPOAuthConfig` |

## Testing Notes

Focused checks:

```bash
GOCACHE=/tmp/go-build go test ./cmd
GOCACHE=/tmp/go-build go test ./serv -run 'TestMCPOAuth|TestNewMCPAuthHandler'
```

Full service tests include packages that open loopback `httptest` servers and
database clients, so they may need to run outside restricted sandboxes.
