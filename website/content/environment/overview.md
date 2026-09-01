---
title: "What A Graded Environment Is"
nav_title: "Overview"
description: "Hidden oracles, isolated resettable worlds, and a reward a policy cannot talk its way past."
nav_group: "environment"
doc_kind: "concept"
weight: 10
---

## The reward is the environment's job

An agent that answers "roughly 40 accounts are past due" in a confident tone is
indistinguishable, to a language model grading it, from one that answers
correctly. It is entirely distinguishable to a database.

Every task in a GraphJin environment carries an **oracle**: a read-only query,
written when the task was generated, resolved against the same world the agent
is working in. The agent never sees it. The answer it gives is checked against
what the database actually returns, at the moment the episode runs.

That single decision is what the rest of this section is built on. It means a
reward is a measurement rather than an opinion, that two runs of the same suite
are comparable, and that a policy being trained against this reward cannot
improve its score by becoming more persuasive.

{{< verified by="TestCheaterBatteryScoresZero" file="agent/eval/cheater_battery_test.go" >}}

## Answering is not the same as having done the work

A correct answer with nothing behind it is a failure, and the environment can
tell the difference. Every tool call an agent makes is recorded, so a task can
require that the *database* did the aggregation rather than the model, that a
mutation actually landed, and that no row outside the task's scope changed.

An agent that submits the right number without ever querying scores zero.

{{< verified by="TestExternalAnswerWithoutDoingTheWorkScoresZero" file="cmd/eval_external_test.go" >}}

## Worlds are isolated, resettable, and identical

Episodes that write need somewhere to write. Each worker in the pool provisions
its own copy of the project — its own database, its own state — and an episode
leases one for its duration. A task that inserts a payment does so in a world
nobody else is reading.

The pool refuses to start unless every worker reports the same dataset. Oracles
are resolved once for a run, so a worker whose rows differed would mark correct
answers wrong, and it would look like a model regression rather than a
provisioning bug.

{{< verified by="TestPoolBootsIsolatedDemoInstancesThatAgree" file="cmd/eval_pool_test.go" >}}

Time is pinned the same way. A question about "the last 30 days" is a different
question tomorrow, so both the clock the agent is told and the day the data is
seeded for can be frozen, and the answer to "how many overdue invoices" stops
depending on when you asked.

## Where to go next

**If you want an environment to train against**

1. [Run the environment](/environment/quickstart/) — pull the image, read
   `/health`, drive one graded episode.
2. [Driving episodes](/environment/drive-modes/) — hosted, one completion at a
   time, or your own agent over MCP.
3. [Building and baking worlds](/environment/worlds/) — generate new
   organizations, or bake your own into an image.
4. [Training a policy against it](/environment/training/) — the three workflows,
   and why their order is forced.

**If you want to measure an agent on your own data**

1. [Use your own graph](/environment/your-own-graph/) — clone the shape of a
   running server, generate a verified suite, serve it.
2. [Evaluate the agent](/agentic/evaluation/) — gate a release against a trusted
   baseline.
3. [Reward, comparability, and provenance](/environment/reward/) — what to
   record so two numbers can be compared at all.

Reference for both: the [CLI](/environment/cli-reference/), the
[HTTP API](/environment/http-api/), and the
[file formats](/environment/file-formats/).
