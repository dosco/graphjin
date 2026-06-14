---
title: "OpenAPI"
description: "Expose selected OpenAPI GET operations as root fields or row joins in GraphQL."
nav_group: "integrations"
doc_kind: "guide"
weight: 30
---

## Join remote APIs to database rows

```yaml
sources:
  - name: billing_api
    kind: api
    specs_dir: config/specs
    specs:
      stripe:
        base_url: "https://api.stripe.com"
        auth:
          scheme: bearer
          token: ${STRIPE_TOKEN}
        joins:
          listInvoices:
            parent_table: customers
            parent_column: stripe_customer_id
            param: customer
            expose_as: invoices
```

```graphql
query ($id: ID!) {
  customers(id: $id) {
    email
    invoices {
      id
      status
      total
    }
  }
}
```

{{< svg "openapi-flow" "OpenAPI operations exposed as graph fields" >}}

{{< verified by="Example_queryWithOpenAPIJoin" file="tests/openapi_test.go" line="31" >}}
{{< verified by="Example_queryWithOpenAPITopLevel" file="tests/openapi_toplevel_test.go" line="29" >}}

## Classification

GraphJin classifies supported GET operations as row joins, top-level single-record fields, or top-level list fields. Mutating verbs, binary responses, async responses, and unsupported auth are skipped with boot-time diagnostics.

Top-level operations can sit beside database roots in one query:

```graphql
query {
  users(limit: 2, order_by: { id: asc }) {
    id
    email
  }
  payments_api(limit: 2) {
    id
    amount
  }
}
```

{{< verified by="Example_queryMixedRootDBPlusOpenAPI" file="tests/openapi_toplevel_test.go" line="142" >}}

Remote API joins are executed after the parent database rows are known. The response cache keys include a resolver fingerprint so changing an upstream resolver does not reuse stale remote fragments.

{{< verified by="TestRemoteFragmentKeyIncludesResolverFingerprint" file="core/cache_response_test.go" line="163" >}}
