---
title: "Auth And RBAC"
description: "Configure authentication, role queries, table permissions, row filters, and column blocking."
nav_group: "configure"
doc_kind: "reference"
weight: 30
---

## Auth providers

GraphJin accepts JWTs minted by your application or identity provider and verifies them before a
request reaches the compiler. It supports rotating JWKS endpoints, shared HMAC secrets, static public
keys, and Bearer-token authentication. This section covers request verification after a token has been
issued; login redirects and session creation are separate from the verifier.

{{< svg "auth-modes" "GraphJin authentication and authorization modes" >}}

### Choose a JWT verification mode

| Use case | `auth.jwt.provider` | Verification material |
| --- | --- | --- |
| OIDC provider with rotating keys | `jwks` | `jwks_url` |
| Application-issued HMAC token | `other` | `secret` |
| Application-issued RSA/ECDSA token | `other` | `public_key` and `public_key_type` |
| Firebase ID token | `firebase` | `audience` (your Firebase project ID) |

For an external OIDC provider, configure the JWKS endpoint and pin the expected audience and issuer:

```yaml
auth_fail_block: true

auth:
  type: jwt
  jwt:
    provider: jwks
    jwks_url: https://issuer.example.com/.well-known/jwks.json
    audience: graphjin-api
    issuer: https://issuer.example.com/
```

`provider: jwks` is required for a JWKS endpoint. Without it, GraphJin selects the generic key
provider, which expects `secret` or `public_key` instead. Set `audience` and `issuer` to the exact
values emitted by your identity provider; GraphJin checks each claim whose expected value is
configured. GraphJin fetches the JWKS when token verification first needs a key; startup only checks
that `jwks_url` is set.

For an application-issued HMAC token, keep the shared secret outside the config file:

```yaml
auth_fail_block: true

auth:
  type: jwt
  jwt:
    provider: other
    secret: "" # supplied by GJ_AUTH_JWT_SECRET
    audience: graphjin-api
    issuer: https://app.example.com/
```

Set the shared key in the process environment as `GJ_AUTH_JWT_SECRET`; it maps directly to
the empty `auth.jwt.secret` field without putting the key in the config file.

The full field list and static-public-key example are in the
[configuration reference](https://github.com/dosco/graphjin/blob/master/CONFIG.md#authentication-configuration).

### Send an authenticated request

Every accepted JWT must contain a string `sub` claim. If `audience` or `issuer` is configured, the
token must also contain the matching `aud` or `iss` claim.

The examples enable `auth_fail_block`. With an explicit `anon` role, a missing or invalid token returns
HTTP 401 before the request reaches the compiler. When no `anon` role exists and `default_block`
remains enabled, GraphJin delegates rejection to the compiler's default-block policy instead.

Send the token as a Bearer credential:

```bash
curl http://localhost:8080/api/v1/graphql \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  --data '{"query":"query { users { id name } }"}'
```

{{< verified by="TestJWTTokenInAuthorizationHeader" file="auth/auth_test.go" line="13" >}}

Browser applications can instead set `auth.cookie` to the name of the cookie that contains the JWT.
GraphJin checks that cookie first, then falls back to the `Authorization` header when the cookie is
absent or empty. Cookie attributes such as `HttpOnly`, `Secure`, and `SameSite` are set by the
application or identity flow that issues the cookie, not by the JWT verifier.

## Identity mapping in source mode

Source mode centralizes common identity claims and then generates the lower-level filters and presets GraphJin already enforces in the compiler:

```yaml
identity:
  user_id_claim: sub
  role_claims: [role, roles]
  namespace_claim: account_id
  admin_roles: [admin]

sources:
  - name: app
    kind: database
    type: postgres
    access:
      read: account
      write: blocked
      delete: blocked
      namespace_column: account_id
      public_tables: [countries, plans]
      admin_tables: [audit_logs]
      blocked_tables: [internal_events]
```

{{< verified by="TestApplySourceAccessRulesGeneratesAccountFiltersAndClassifications" file="core/source_access_test.go" line="14" >}}

{{< verified by="TestSourceModeHTTPJWTIdentityAndSystemRoots" file="serv/source_mode_http_test.go" line="27" >}}

## Role query

Roles can come from SQL or GraphQL role queries. GraphQL role queries return fields that role predicates match against.

{{< verified by="TestGraphQLRoleQueryMatchesConfiguredRole" file="core/role_query_graphql_test.go" line="14" >}}

## Table permissions

Per-table role rules control query, insert, update, upsert, and delete operations. Rules can set limits, filters, column allow/block lists, presets, and operation blocks.

```yaml
roles:
  - name: user
    tables:
      - name: products
        query:
          filters:
            - "{ owner_id: { eq: $user_id } }"
          columns: ["id", "name", "price"]
        insert:
          columns: ["name", "price"]
          presets:
            owner_id: "$user_id"
        delete:
          block: true
```

In source mode, do not mix user-written `roles[].tables` rules with `sources:`. Migrate repeated account filters to `sources[].access` and keep legacy role table rules for database-only legacy configs.

## Column and aggregate enforcement

Column allow-lists also apply inside expressions and aggregate metrics. If a role cannot read `price`, `sum(of: price)` and expression aggregates that reference `price` should fail the same way a direct `price` field would.

{{< verified by="Example_queryWithExprRoleAllowlist" file="tests/query_test.go" line="2468" >}}
