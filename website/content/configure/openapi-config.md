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
        auth:
          scheme: bearer
          token: ${STRIPE_TOKEN}
        concurrency:
          max_concurrent: 8
          rate_limit_per_second: 20
```

## Supported auth schemes

| Scheme | Use for |
| --- | --- |
| `bearer` | Static or pass-through tokens |
| `basic` | Username/password |
| `api_key` | Header or query API keys |
| `oauth2_client_credentials` | Machine-to-machine OAuth |
| `token_exchange` | Vendor-specific token POST flows |

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
