#!/usr/bin/env bash
set -euo pipefail

DEMO_NAME="contoso retail"
BASE_URL="${GRAPHJIN_URL:-http://localhost:8084}"
SMOKE_LOCKED_KIND="${SMOKE_LOCKED_KIND:-runbook}"
SMOKE_JWT_SECRET="${SMOKE_JWT_SECRET:-contoso-retail-demo-secret}"
SMOKE_USAGE_TEXT='Contoso Retail GraphJin smoke suite.

Run after starting the demo server:
  graphjin serve --demo --path examples/contoso

Usage:
  examples/contoso/scripts/smoke.sh [--url URL] [--agent|--no-agent]

Options:
  --url URL     GraphJin base URL. Defaults to GRAPHJIN_URL or http://localhost:8084.
  --agent       Require REST and MCP agent checks.
  --no-agent    Skip REST and MCP agent checks.'

# shellcheck source=../../lib/smoke-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")/../../lib" && pwd)/smoke-common.sh"
smoke_parse_args "$@"

# --- base checks ---------------------------------------------------------------

log "checking customer and product collections"
out="$(graphql contoso-customers 'query {
  customer(limit: 5) { id customer_key name city state }
  product(limit: 5) { id product_key product_code brand unit_price }
}')"
assert_jq "$out" '.data.customer | length > 0' "customer records queryable"
assert_jq "$out" '.data.product | length > 0' "product records queryable"

log "checking store and sales aggregations"
out="$(graphql contoso-stores 'query {
  store(where: { status: { eq: "Open" } }) {
    store_key
    name
  }
  sales(limit: 5) {
    order_number
    quantity
    unit_price
    net_price
  }
}')"
assert_jq "$out" '.data.store | length > 0' "open stores queryable"
assert_jq "$out" '.data.sales | length > 0' "sales records queryable"

log "checking orders and order_rows"
out="$(graphql contoso-orders 'query {
  orders(limit: 3) {
    order_key
    customer_key
    order_date
  }
  order_rows(limit: 3) {
    order_key
    line_number
    quantity
    net_price
  }
}')"
assert_jq "$out" '.data.orders | length > 0' "orders queryable"
assert_jq "$out" '.data.order_rows | length > 0' "order_rows queryable"

log "checking saved queries"
saved_out="$(rest_saved_query top_selling_products)"
assert_jq "$saved_out" '.data.sales | length > 0' "top_selling_products saved query works"

log "Contoso Retail smoke suite completed successfully!"
