---
title: "Answers with the data behind them"
headline: "The model made up an answer. The ledger caught it."
description: "An answer from DeepORG arrives with the data it rests on and a record of what actually ran. Guards block a claim that has nothing behind it, the database does the arithmetic, and a refusal says what would unblock it."
lede: "An answer from DeepORG arrives with the data it rests on and a record of what actually ran. A claim with nothing behind it does not get out, however confident it sounds."
scene: "ledger"
scene_label: "Animated diagram: a claim with no query behind it is blocked, and a claim backed by three queries and their rows is answered with evidence attached."
scene_caption: "checked before an answer leaves"
actions:
  - label: "Try it in 2 minutes"
    href: "/#quickstart"
  - label: "See it graded in public"
    href: "/benchmark/"
landing: true
toc: false
---

{{< band label="Answers backed by evidence" title="If no query ran, the model does not get to pretend one did." >}}

{{< grid >}}

- **01. The model answers anyway.** It names roast batches to hold.
- **02. No query ran.** The execution ledger has no rows behind the claim.
- **03. DeepORG blocks it.** The invented answer never reaches the caller.

{{< endgrid >}}

What the caller gets instead is a refusal it can act on:

{{< code-card filename="response.json" language="json" >}}
{
  "status": "blocked",
  "refusal": {
    "code": "ungrounded_answer_fields",
    "blocked_action": "answer",
    "unblock": [
      {
        "tool": "query_catalog"
      }
    ],
    "retryable": true
  }
}
{{< /code-card >}}

{{< endband >}}

{{< band label="What comes back" title="The answer, its data, and how it was reached." >}}

A reply is more than a sentence. Alongside the `answer` it carries the `data` behind it and the `evidence` it rests on: every claim is backed by what the agent actually looked up, checked or ran on this run.

It also says how the result is best shown: one value, a bar chart, a line or a table, with the rows themselves. A chat window can draw the chart without guessing which column is the label.

{{< endband >}}

{{< band label="Who does the arithmetic" title="The model plans. The database computes." >}}

The common way an agent gets a confident wrong number is to fetch a page of rows and add them up in its head. The page was the first ten of four hundred, and the sum looks fine.

DeepORG does not let the model do sums. The lead agent never computes an aggregate itself. It asks, and the database computes the count, the total or the ranking where the data is.

This is graded, not promised. On [the public benchmark](/benchmark/) a full pass needs the right answer and the right way of getting it: a correct number reached by adding up a page of rows does not count.

{{< endband >}}

{{< band label="Guards" title="A model cannot talk its way past them." >}}

Guards sit between the model and the caller. When a required step was skipped, an answer is downgraded to `blocked`, with the evidence. Only real results count.

{{< grid >}}

- **Named things must have been seen.** In a conversation, an identifier that no question returned gets one chance to be restated, then an `ungrounded_answer_fields` refusal.
- **A held change is never reported as done.** When a change is waiting for approval, the reply is DeepORG's own sentence, not the model's.
- **Memory is not evidence.** A remembered preference shapes how it answers. It is never the source of a figure.
- **A refusal is specific.** A stable code, the action that was blocked, the reasons, the steps that would unblock it, and whether trying again can help.

{{< endgrid >}}

{{< endband >}}

{{< band label="When it is wrong" title="A person can say so, and it counts." >}}

Anyone can mark an answer helpful or wrong and say what the right answer is. A correction is kept with the queries the agent ran, so the miss can be reproduced rather than argued about.

Answers nobody objected to are useful too. Every question answered by one database-computed figure can become a test whose answer key is the query itself, re-run whenever the test runs. An answer a person marked wrong never becomes a test. [Learning from real work](/experience/) shows how.

{{< endband >}}

{{< band label="In the docs" title="Go deeper." >}}

- [How the agent works](/agentic/server-agent/): replies, guards and refusals.
- [Conversations](/agentic/lead-agent/): how the lead keeps its answers grounded.
- [Benchmark methodology](/benchmarks/deeporg/methodology/): how answer, method, behavior and safety are scored.
- [Evaluating the agent](/agentic/evaluation/) on your own questions.

{{< endband >}}
