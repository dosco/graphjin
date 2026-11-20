---
title: "Measure Your Agent On Your Own Questions"
nav_title: "Your Own Questions"
description: "Turn the questions people already asked the agent into tests whose answer key is the database query that answered them, then run those tests on any model."
nav_group: "environment"
doc_kind: "guide"
weight: 45
---

[Your own graph](/environment/your-own-graph/) builds tests from your schema.
This builds them from your work: the questions people already asked the agent
on a running server. The overview is at
[Learning from real work](/experience/).

## What becomes a test

Every turn on a thread is stored with the questions the lead asked the data
agent, the answers, and the queries the data agent ran. A question becomes a
test when the data agent's last successful read was one of these:

- **One aggregate figure**: `query { accounts { sum_mrr_cents } }`.
- **One top row**: an `order_by` with `limit: 1`. When the row has a name, the
  answer must name it; when it has only an id, the figure itself is the answer
  ("the largest invoice").
- **One looked-up value**: any single row with `limit: 1`.

That query becomes the test's **answer key**. It is not a stored number: the
test re-runs it every time it runs, so the expected answer is always the one
the data has that day.

The engine decides this, not a model. The query is parsed by GraphJin's own
parser, re-run against the database, and the answer is read from the result it
returned.

Two more checks happen before a question is kept:

- **The query re-runs as the person who asked**, never as the admin reading
  the tests, so it sees exactly the rows they saw.
- **The answer they were given must pass it.** If it does not, the query was
  not where the answer came from, or the data has moved on, and the question is
  dropped.

## Mine the questions

Mining reads every person's questions, so it needs an admin. Against a server
set up with `graphjin cli setup`:

```bash
graphjin eval mine --remote
```

Against a server you name, with an admin's token:

```bash
graphjin eval mine --url https://graphjin.internal --token "$ADMIN_TOKEN"
```

The command reads `GET /api/v1/agent/experience` page by page, verifies every
answer key against the evaluation target exactly as `eval create` verifies its
own tasks, and writes `eval/mined.yml`:

```text
Read 14 questions to the data agent from http://127.0.0.1:8191.
  4 became tests on the server (3 incumbent, 1 confirmed).
  not tests yet: 2 list, 2 not_answered, 2 rated_wrong, 2 several_figures, 1 no_query, 1 shape
Wrote 4 tests to eval/mined.yml (3 incumbent, 1 confirmed).
Run them with: graphjin eval run --suite eval/mined.yml
```

That is a real run on the saas-ops demo: ten console turns, fourteen questions
to the data agent. `--since` and `--origin chat|routine|continuation|outcome`
narrow what is read; `--from` reads a response you saved instead of fetching
one.

Mining spends no model tokens. It costs one query per candidate on the
database the questions were asked against.

## Run them

```bash
graphjin eval run --suite eval/mined.yml --yes
```

Mined tests are a suite of their own, apart from `eval/suite.yml`: a generated
suite samples its candidates, and a question somebody really asked should never
lose that draw. A `--suite` run is compared with no baseline and promotes none.

In the run above, all four tests passed on `gpt-6-luna`, using 130,913 tokens.
Then one open ticket was resolved and the open-tickets test ran again: its
answer key now gave 3 instead of 4, the model answered "There are 3 open
support tickets", and the test passed.

To compare models, run the same suite with each and read the reports side by
side. To train on the tests, run them in the [environment](/environment/training/)
and export the attempts with `graphjin eval export`.

## Confirmed and incumbent

Every mined test has a tier, and the tiers are scored apart. A run of mined
tests prints one line per tier, its reports have no combined score, and the
JSON report keeps each tier under `metrics.by_family`. Read those: the JSON's
overall `metrics.recall` still counts every test together.

```text
Confirmed tests: 1 of 1 passed (marked right by a person, so this is correctness).
Incumbent tests: 3 of 3 passed (never rated, so this is agreement with the agent that first answered).
```

| Tier | Where it comes from | What its score means |
|---|---|---|
| `confirmed` | A person marked the answer right, on a turn that asked one question | Correctness |
| `incumbent` | Nobody rated the answer | Agreement with the agent that first answered |

An incumbent test can be wrong: if the first answer was wrong and nobody said
so, a better model scores lower on it. That is why the tiers stay apart. A
thumbs-up on a turn that asked several questions does not say which answer was
right, so those tests stay incumbent.

An answer a person marked wrong never becomes a test. It shows up in
`GET /api/v1/agent/feedback?format=corpus` as a draft with the person's
correction and the queries the agent ran, waiting for someone to write the
right answer key.

## What is not a test yet

The server's summary counts every question it skipped, by reason, and the full
response lists each one under `drops` with the detail:

| Reason | What it means |
|---|---|
| `list` | The answer was a list of rows |
| `several_figures` | The query returned more than one figure, or more than one root |
| `shape` | The result did not have one row of plain values, for example a nested relationship |
| `dated` | The question names a date, such as "November 2026" |
| `uses_variables` | The query took variables, which the action trail does not keep |
| `rated_wrong` | A person marked the answer wrong |
| `not_answered` | The data agent did not answer |
| `no_query` | No read returned data |
| `unsafe` | The run did something a guard did not stop |
| `write` | The question changed data |
| `query_failed`, `empty_result` | The answer key no longer runs, or returns nothing |
| `answer_mismatch` | The answer given does not pass the key |
| `prose` | The question names GraphJin's own mechanism instead of a need |
| `duplicate` | The same question was already kept |
| `asker_unknown` | A routine ran as the service identity, which has since been changed, so the rows it saw cannot be rebuilt |

These counts are worth reading: they say which kinds of real questions your
organization asks that cannot be tested yet.

## Dates

A question like "which account renews soonest?" is often answered with a date
in the query: `renewal_date: { gte: "2026-11-19" }`, the day it was asked. A
test that kept that date would ask about renewals after November 19 forever.
So a literal date becomes an offset from the day the question was asked, and
the test resolves it against the day it runs: `{{today}}`, or `{{today-30}}`
for a date thirty days earlier.

This is done only when the question names no date of its own. A question about
"November 2026" or "11/19" means that day for good, and is skipped as `dated`
rather than shifted.

A date more than about thirteen months from the day of asking is not shifted
either. A query for an all-time total that starts at `2020-01-01` keeps that
date as written, so its window does not creep forward with every run.
