# Third-party software shipped with OpenNeko

The source code in this repository is distributed under the
[Apache License 2.0](LICENSE) unless a file clearly states different terms.
This file lists third-party software and modular components that are shipped
alongside, referenced by, or designed to interoperate with OpenNeko, and the
additional license obligations those carry.

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

## Modular agents, skills, and plugins

OpenNeko can invoke or interoperate with external agents, skills, and plugins.
Those components remain under their own licenses.

| Component | License | Notes |
|---|---|---|
| Hermes Agent | MIT | External/modular agent runtime from Nous Research. Preserve upstream copyright and license notices when distributing it or substantial portions of it. |
| Anthropic-authored bundled skills | As declared | Preserve the upstream terms and attribution shipped with each skill. |
| Built-in skills with `license:` frontmatter | As declared in each skill | Some bundled skills are Apache-2.0, MIT, or proprietary to their upstream source. Check each `SKILL.md` and local `LICENSE.txt`. |
| Third-party plugins | Plugin author's license | Plugins communicate across the OpenNeko plugin RPC boundary and are independent works unless their own license says otherwise. |

---

## Other notable dependencies

OpenNeko's runtime dependencies are listed in the various `package.json` files across the monorepo. The substantial ones with non-Apache-2.0 licenses:

| Package | License | Notes |
|---|---|---|
| `pg`, `pg-boss` | MIT | Postgres client + job queue |
| `next` | MIT | Web app framework |
| `@huggingface/transformers` | Apache-2.0 | Embedding model runtime |
| `zod` | MIT | Schema validation |
| `drizzle-orm` | Apache-2.0 | DB schema + queries |

Run `pnpm licenses ls -P` from the repo root to see the full transitive list at a given snapshot.

---

## Reporting

Discrepancies, missing attributions, or compliance questions: https://github.com/open-neko/openneko/issues.
