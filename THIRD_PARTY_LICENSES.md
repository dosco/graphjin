# Third-party software shipped with OpenNeko

The source code in this repository is distributed under the
[Apache License 2.0](LICENSE) unless a file clearly states different terms.
This file lists third-party software that ships with OpenNeko, and the
additional license obligations it carries.

This file complements [NOTICE](NOTICE). Operators who redistribute OpenNeko in
any form should ship LICENSE, NOTICE, this file, and any applicable third-party
license texts alongside their distribution.

---

## GraphJin

GraphJin lives in this repository under `graphjin/` and is published on its
own as `dosco/graphjin`. Every OpenNeko image ships it inside the `openneko`
binary, as the `graphjin` role.

| Component | License | Notes |
|---|---|---|
| GraphJin | Apache-2.0 | Copyright Vikram Rangnekar and GraphJin contributors. See `graphjin/LICENSE` and `graphjin/NOTICE`. Its Go dependencies are listed in the `go.mod` files under `graphjin/`. |

---

## Bundled skills

Every bundled skill and every skill in a bundled solution pack is Apache-2.0.

| Component | License | Notes |
|---|---|---|
| `internal-comms` skill | Apache-2.0 | Authored by Anthropic. Keep its `LICENSE.txt` and attribution. |

---

## Other notable dependencies

OpenNeko's runtime dependencies are listed in the various `package.json` files across the monorepo. The substantial ones with non-Apache-2.0 licenses:

| Package | License | Notes |
|---|---|---|
| `pg`, `pg-boss` | MIT | Postgres client + job queue |
| `next` | MIT | Web app framework |
| `zod` | MIT | Schema validation |
| `drizzle-orm` | Apache-2.0 | DB schema + queries |

Run `pnpm licenses ls -P` from the repo root to see the full transitive list at a given snapshot.

---

## Reporting

Discrepancies, missing attributions, or compliance questions: https://github.com/open-neko/openneko/issues.
