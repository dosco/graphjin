---
title: "MCP OAuth"
description: "Protect hosted MCP endpoints with resource metadata, DCR/CIMD, PKCE, and audience checks."
nav_group: "agentic"
doc_kind: "reference"
weight: 60
---

## Hosted MCP identity

GraphJin serves MCP at:

```text
/api/v1/mcp
```

When OAuth is enabled, GraphJin can advertise protected-resource metadata and authorization-server metadata for clients that understand the MCP OAuth flow.

```yaml
mcp:
  oauth:
    enabled: true
    mode: builtin
    scopes: ["mcp"]
```

{{< verified by="TestMCPOAuthProtectedResourceMetadata" file="serv/mcp_oauth_test.go" line="17" >}}
{{< verified by="TestMCPOAuthAuthorizationServerMetadataIncludesDCRCIMD" file="serv/mcp_oauth_test.go" line="45" >}}

## Audience checks

Tokens must match the expected MCP resource/audience. Wrong-audience requests are rejected with a challenge rather than silently accepted.

{{< verified by="TestNewMCPAuthHandlerRejectsWrongAudienceWithChallenge" file="serv/mcp_oauth_test.go" line="106" >}}

## Client expectations

Hosted MCP clients discover the protected resource metadata before authorization. A working setup should expose:

| Surface | Purpose |
| --- | --- |
| Protected resource metadata | Tells the client which MCP resource it is requesting access to. |
| Authorization server metadata | Publishes issuer, token endpoint, PKCE support, and DCR/CIMD support. |
| Audience/resource validation | Prevents a token minted for one MCP server from being replayed against another. |

```yaml
mcp:
  oauth:
    enabled: true
    mode: builtin
    issuer: https://graphjin.example.com
    audience: https://graphjin.example.com/api/v1/mcp
```

Validate hosted MCP with the same care as the GraphQL endpoint: TLS, resource audience, allowed origins, token lifetime, and whether raw GraphQL or mutation tools are advertised for the current caller.

{{< verified by="TestValidateMCPOAuthConfig" file="serv/mcp_oauth_test.go" line="239" >}}
