---
title: "OpenAPI Config"
description: "Configure OpenAPI specs, auth schemes, operation overrides, naming, and collision handling."
nav_group: "configure"
doc_kind: "reference"
weight: 60
---

## Basic spec

```yaml
sources:
  - name: upstream
    kind: api
    specs_dir: config/specs
    specs:
      stripe:
        base_url: https://api.stripe.com
        timeout: 5s
        auth:
          scheme: bearer
          token: ${STRIPE_TOKEN}
        concurrency:
          max_concurrent: 8
          rate_limit_per_second: 20
```

`timeout` bounds the complete upstream operation, including authentication,
retries, and response reads. It defaults to `8s`. A timed-out API field is
returned as `null` with a GraphQL error while successful sibling roots remain
available in `data`.

## Supported auth schemes

| Scheme | Use for |
| --- | --- |
| `bearer` | Static or pass-through tokens |
| `basic` | Username/password |
| `api_key` | Header or query API keys |
| `oauth2_client_credentials` | Machine-to-machine OAuth |
| `token_exchange` | Vendor-specific token POST flows |

## Per-user credentials

Use a dedicated incoming header when each caller connects their own account:

```yaml
sources:
  - name: google_workspace
    kind: api
    specs_dir: config/specs
    specs:
      gmail:
        base_url: https://gmail.googleapis.com
        auth:
          scheme: bearer
          token_from_request:
            header: X-Google-Workspace-Token
```

The trusted host resolves the signed-in user's access token and sends it in
`X-Google-Workspace-Token` on each GraphQL or HTTP MCP request. GraphJin's own
`Authorization` header continues to authenticate the caller independently.
Only credential headers explicitly named by configuration are made available to
OpenAPI authentication. Do not expose account selection or tokens as GraphQL
variables or MCP tool arguments. Browser clients using CORS must also include
the dedicated header in `allowed_headers`.

This applies to top-level reads, remote joins, and mutations. Missing, blank,
or repeated credential headers fail before the upstream request. Combining
`token_from_request` with a static `token` or `key_value` is rejected, so one
user can never fall back to a deployment account. API-key auth also supports
`token_from_request` with its configured destination `key_name`.

Personal API fragments are not cached or refreshed in the background. HTTP
responses on deployments with request-token sources use `private, no-store`.
Subscriptions to these sources are rejected: scheduled jobs must issue a new
query with freshly resolved credentials. GraphJin does not persist or refresh
these tokens; OAuth consent, ownership checks, and refresh remain host duties.
Go embedders can use `openapi.WithRequestHeaders(ctx, headers)`; the headers are
copied and remain usable only while the original context is active.

## Overrides

Use operation overrides to rename fields, set result paths, disable operations, provide default parameters, or deliberately expose top-level paths that would otherwise be skipped.

```yaml
sources:
  - name: upstream
    kind: api
    specs_dir: config/specs
    specs:
      payments:
        auth:
          scheme: bearer
          token: ${PAYMENTS_TOKEN}
        joins:
          getPaymentById:
            parent_table: users
            parent_column: stripe_id
            param: paymentId
            expose_as: payment
        operations:
          listPayments:
            expose_as: payments_api
            result_path: data
            defaults:
              limit: "20"
```

{{< verified by="Example_queryWithOpenAPIJoin" file="tests/openapi_test.go" line="31" >}}
{{< verified by="TestExpandSpecConfigDefaultsEnv" file="core/openapi/loader_test.go" line="5" >}}

Collision handling should fail hard when an OpenAPI field conflicts with a real table in the primary schema.

## Query shape

OpenAPI row joins appear like nested fields:

```graphql
query ($id: ID!) {
  users(id: $id) {
    id
    email
    payment {
      desc
      amount
    }
  }
}
```

Top-level operations appear as virtual root fields when classified or explicitly exposed. Unsupported operations are skipped with boot-time diagnostics instead of half-registering a broken field.

OpenAPI list roots and row-join objects accept the common `where`, `order_by`,
`limit`, and `offset` arguments. GraphJin applies these to the returned JSON,
while operation-specific path and query parameters are sent to the upstream:

```graphql
query {
  payments_api(
    where: { status: { eq: "failed" } }
    order_by: { created_at: desc }
    limit: 20
  ) {
    id
    status
    created_at
  }
}
```

Unsupported filter operators fail explicitly. API response fields do not
support cursor pagination or `distinct`; use `limit` and `offset` for paging.
