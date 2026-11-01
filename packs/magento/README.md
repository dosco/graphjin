# Magento solution pack

The Magento pack connects OpenNeko to Magento Open Source or Adobe Commerce
2.4.x. Analytics use a dedicated SELECT-only MariaDB/MySQL account. Governed
writes use a Magento Integration token through GraphJin's curated REST adapter.

Install the included pack in one click from Admin, Settings, Packs, then fill in its manifest-declared connection settings. Review and apply the configuration to activate the complete pack. GraphJin 3.20.77 provides isolated
executor roles for curated Magento REST operations. A missing or insufficient
Integration token leaves the affected domains view-only without producing a
partial installation.

Adobe Marketplace/Composer keys are not pack inputs. They are needed only when
Composer downloads Magento itself from Adobe's repository.

## Data access policy

The read-only Magento source includes the operational customer, address, order,
status-history, payment-status, invoice, shipment, refund, catalog, inventory,
store, cron, and indexer data needed to run the store. Customer names, email,
telephone numbers, addresses, customer identifiers, order notes, IP addresses,
shipping data, and payment transaction references are available to
authenticated OpenNeko actors. They are not globally classified as forbidden
data.

The source remains SELECT-only. A narrow, unconditional blocklist is reserved
for secrets that should never cross the database boundary: password hashes,
account recovery and confirmation tokens, guest-order protect codes, encrypted
payment numbers, raw payment-gateway payloads, and bank routing numbers. Admin
credentials, OAuth tokens, reset-token tables, quote payments, and vault token
tables are not exposed at all. Deployments that need tighter restrictions
should apply an explicit actor/role policy instead of silently removing data
required by every operator.

## Prerequisites

- A running OpenNeko production stack.
- Magento 2.4.6 through 2.4.x with `/magento_version` reachable from the
  OpenNeko worker.
- MariaDB 10.6+ or MySQL 8.0+ reachable from the customer GraphJin container.
- A dedicated analytics account with SELECT-only grants. Start with
  [`docs/analytics-user.sql`](docs/analytics-user.sql) and narrow its host and
  table grants for production. Existing table-specific grants must include the
  customer, customer-address, order-address, status-history, and order-payment
  tables listed in `graphjin/sources.yaml` before upgrading to pack 0.2.1.
- Optionally, a least-privilege Magento Integration token for governed catalog,
  inventory, order, promotion, content, or customer changes. Customer changes
  are disabled separately by default.
- Magento async/bulk consumers running successfully for bulk catalog and
  inventory operations.

If Magento and OpenNeko share a Docker/OrbStack network, use MariaDB's service
name as `--database-host`. If they are separate stacks, publish MariaDB on a
host port and use `host.docker.internal` with
`--database-connectivity host_gateway`. A remote database hostname also works
with `--database-connectivity remote`.

## Install

For a normal installation, run one command:

```sh
openneko pack install magento
```

The guided installer asks only for the Magento URL, database address/name, and
the dedicated read-only analytics username/password. Password input is hidden,
and the credentials are saved in OpenNeko's local secret store automatically.
The Magento table prefix, database engine, currency, timezone, and active store
IDs are discovered. The first metric refresh is queued as soon as installation
finishes.

For automation or unattended installs, store credentials first and use the
advanced flags. This example targets the US sample catalog's `default` store
view:

```sh
openneko secrets set pack.magento ANALYTICS_USER
openneko secrets set pack.magento ANALYTICS_PASSWORD

openneko pack install magento \
  --non-interactive \
  --base-url http://host.docker.internal:8080 \
  --store-code default \
  --database-connectivity host_gateway \
  --database-host host.docker.internal \
  --database-port 3306 \
  --database-name magento \
  --analytics-username-ref ANALYTICS_USER \
  --analytics-password-ref ANALYTICS_PASSWORD
```

The flags are an automation interface, not the expected first-run user
experience.

## Everyday operator skills

Installation adds focused skills for recurring Magento work:

- reviewing store performance and producing a daily or weekly briefing;
- investigating one order and optionally proposing a private internal note;
- triaging aged or partially shipped fulfillment work;
- investigating refund-value and cancellation spikes;
- checking low stock, MSI sources, and reservation discrepancies;
- diagnosing cron, indexer, data-freshness, and pack-health problems;
- managing products, categories, assignments, and prices through change-sets;
  and
- designing and running capped, approval-required promotions.
- checking the installed release against Adobe Magento security bulletins.

Ask OpenNeko for the task in ordinary language. The matching skill is selected
automatically; operators do not need to remember its installed ID. All nine
skills install even when governed Magento writes are unavailable. The order
investigation still works read-only when its optional private-note action is
blocked.

The security advisory workflow runs daily at 06:00 UTC. The pack declares the
read-only Adobe hosts required by that workflow. If an Adobe request fails, the
workflow reports that live advisory data is unavailable and does not guess the
security status.

## Optional Magento Integration token

Create a Magento Integration with only the ACL required by the domains you
enable, then store and apply its token:

```sh
openneko secrets set pack.magento MAGENTO_INTEGRATION_TOKEN
openneko pack configure magento \
  --integration-token-ref MAGENTO_INTEGRATION_TOKEN
```

The health check probes each enabled domain independently. A domain with a
missing ACL remains view-only while other domains can be ready. OpenNeko never
asks for a Magento admin password.

Use this resource matrix when creating the Integration. Magento installations
can label the same resources slightly differently; grant the smallest matching
subtree and confirm it with **Check health** before enabling a domain.

| Domain | Magento Integration resources | Notes |
| --- | --- | --- |
| Catalog | Catalog > Inventory > Products; Catalog > Categories | Includes product/category reads and the enabled product/category operations. |
| Inventory | Inventory > Sources; Inventory > Source Items | Bulk changes additionally require working async consumers. |
| Orders | Sales > Orders; Sales > Shipments; Sales > Invoices | Refund and return-approval resources are intentionally not used. |
| Promotions | Marketing > Cart Price Rules; Marketing > Coupon Generation | Every promotion requires administrator approval and remains exposure-capped. |
| Content | Content > Pages; Content > Blocks | Versioned before images and inverse drafts are retained. |
| Customers | Customers > All Customers | Disabled by default and data-minimized in receipts. |

Do not grant Stores configuration, tax configuration, payment configuration,
admin users, integrations, ACL administration, returns approval, or online
refund resources. Online refunds, return approvals, store credit above its cap,
and financial configuration have no execution route; OpenNeko creates an
evidence-backed Magento Admin handoff instead.

## Operate and troubleshoot

For day-to-day administration, open **Admin → Settings → Solution packs →
Magento**. The page shows per-domain access, approval requirements, automation
limits, recent change-sets/handoffs, and plain-language health checks. It also
provides buttons to recheck the connection, change credentials, update the
pack, or remove it safely. An analytics-only installation remains healthy;
governed domains are listed separately as view-only until their token and ACL
are ready.

Use one overview command for day-to-day administration:

```sh
openneko pack manage magento
```

It shows installation/readiness state, runs the health summary, and prints the
exact commands for common maintenance tasks. The individual commands are:

```sh
openneko pack status magento
openneko pack doctor magento
openneko pack plan magento
openneko pack upgrade magento
```

`doctor` rechecks database connectivity, SELECT-only grants, Magento/store
discovery, the GraphJin catalog, and operator readiness. JSON output is
available on all of these commands with `--output json`.

To remove the pack configuration while retaining metric history, workflow run
history, and other business data:

```sh
openneko pack uninstall magento
```

Uninstall revokes read, write, and delete access on the pack-owned GraphJin
sources. GraphJin 3.18 retains that disabled source/table metadata so a later
install can safely reclaim it. Pack-owned queries, the OpenAPI spec, skills,
and stored OpenNeko secret section are removed. Native metrics, workflows,
watchers, policy, and action records are disabled instead of deleted. If a managed
artifact was edited after installation, uninstall stops and preserves it; use
`openneko pack plan magento` to identify the drift. A later
`openneko pack install magento` reclaims and re-enables unchanged retired
artifacts.

The development defaults used for the local US sample catalog are recorded in
[`fixtures/us-sample-profile.yaml`](fixtures/us-sample-profile.yaml). They are
not production credentials or production sizing guidance.
