---
name: magento-diagnose-platform-health
description: Diagnose Magento cron failures, stale running jobs, unhealthy indexers, data freshness, and Magento pack connectivity or readiness. Use for cron/indexer watcher findings, stale dashboards, API or analytics health questions, and pack-doctor remediation; do not use for ordinary sales or inventory analysis.
license: Apache-2.0
metadata:
  openneko:
    tags: [magento, cron, indexers, diagnostics, freshness, pack-doctor]
    category: commerce
    requires_toolsets: [graphjin]
    related_skills: [magento-review-performance, magento-triage-fulfillment, magento-check-inventory]
---

# Diagnose Magento platform health

Separate store-runtime health, analytics freshness, and optional governed-write
readiness. Query `magento_analytics` for evidence and keep remediation
read-only unless an administrator performs a separately governed operation.

## Select the diagnostic path

- For cron failures or stalls, use `cron_health` and `watch_cron_health`.
- For invalid, working, or stale indexers, use `indexer_health` and
  `watch_indexer_health`.
- For a stale dashboard or suspected connection gap, use `data_freshness` and
  `watch_api_data_freshness`.
- For installation, credential, GraphJin, or action readiness, use the Magento
  pack status/doctor path described in
  [the runbook](references/platform-runbook.md).

Resolve the configured Magento timezone and the exact stale/failure threshold
before labelling a row stuck or stale. A quiet store with no recent order may be
healthy; corroborate order freshness with successful cron evidence and pack
connectivity.

## Diagnose cron and indexers

For cron, distinguish `error`, `missed`, `pending`, and a genuinely stale
`running` row. Report job code, status, scheduled/executed/finished timestamps,
and error text only when those fields are approved and necessary. Do not expose
arguments or payload fields that may contain sensitive application data.

For indexers, report indexer ID, state, update time, and threshold. A non-valid
state or old timestamp is evidence of indexer health, not proof of the upstream
cause. Correlate time windows before connecting a cron failure to an indexer.

## Deliver a bounded runbook

Return the affected subsystem, observation window, exact evidence, likely
impact, and ordered next checks. Separate verified facts from hypotheses and
state what would confirm each hypothesis.

Commands such as `cron:run`, `indexer:reindex`, queue restarts, cache flushes,
process termination, and container restarts change runtime state. Do not run or
claim to run them as part of this diagnostic. Never request Magento admin
credentials, Integration bearer tokens, database passwords, or Adobe
Marketplace/Composer keys.
