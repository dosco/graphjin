# Magento pack and platform runbook

For deployment-level health, direct an administrator to **Admin → Settings →
Solution packs → Magento** or the equivalent local commands:

```text
openneko pack status magento
openneko pack doctor magento
```

The doctor verifies database connectivity, SELECT-only grants, Magento/store
discovery, the GraphJin catalog, and optional operator readiness. Readiness
reasons have specific remediations:

- `graphjin_version_unsupported`: keep analytics available and upgrade only to
  the tagged GraphJin release supported by the pack.
- `integration_token_missing`: configure a least-privilege Magento Integration
  token through OpenNeko's encrypted secret flow if governed writes are wanted.
- `integration_token_invalid`: rotate or replace the Integration token through
  the pack administration flow.
- `acl_missing`: grant only the Magento API resources required by the curated
  operation.

Write readiness is optional. Its blocked state must not be reported as an
analytics outage.

For cron, identify the exact job code/status/timestamps before recommending an
operator-run command. For indexers, identify the exact indexer state and age.
Reindexing, cron execution, cache flushes, restarts, and process termination are
outside this read-only skill and require a separate operator decision.
