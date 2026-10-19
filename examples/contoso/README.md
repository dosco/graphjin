# Contoso Retail Demo

The **Contoso Retail** example provides a self-contained, zero-dependency retail analytics environment running on SQLite. It models customer profiles, product catalog, brick-and-mortar stores, currency exchange rates, orders, order items, and sales transactions.

This environment powers the **DeepContoso** organizational agent evaluation benchmark.

## Quick Start

Boot the demo directly with GraphJin:

```bash
# Run from repository root
graphjin serve --demo --path examples/contoso
```

Once running:
- **GraphQL:** http://localhost:8084/api/v1/graphql
- **Database Engine:** Embedded SQLite (in-process, zero Docker required)

## Domain Entities

| Collection | Description | Primary Key |
|---|---|---|
| `customer` | Customer demographics, locations, and birthdays | `customer_key` |
| `product` | Catalog items across Audio, Computers, TV, Cameras | `product_key` |
| `store` | Physical retail store locations and operational status | `store_key` |
| `orders` | Customer order transactions with date stamps | `order_key` |
| `order_rows` | Line items for orders with quantities, unit and net prices | `id` |
| `sales` | Normalized sales transactions for fast aggregation | `id` |
| `currency_exchange` | Daily currency conversion rates (USD to EUR, CAD, etc.) | `id` |
| `date` | Calendar dates with working day flags and quarters | `id` |

## Smoke Test

Verify the demo environment:

```bash
examples/contoso/scripts/smoke.sh
```
