# AdventureWorks data eval

GraphJin's eval engine runs this suite. It asks GraphJin's agent 53 data
questions about the frozen AdventureWorks snapshot and grades each answer
against a GraphQL oracle that GraphJin resolves itself.

```sh
scripts/eval-adventureworks/environment.sh up        # the frozen snapshot on 127.0.0.1:15433
GJ_AGENT_PROVIDER=openai GJ_AGENT_MODEL=gpt-4.1-mini GRAPHJIN_AGENT_API_KEY=... \
  openneko graphjin --path evals/adventureworks eval run --yes
```

Reports land in `evals/adventureworks/.graphjin-evals/reports/`, and the admin
screen at `/admin/graphjin/evals` reads them. `eval rescore <run-id>` regrades a
run without model traffic.

## Where the tasks come from

The tasks are the 40 AdventureWorks questions of OpenNeko's former eval suite,
q01 to q40. Each question with a window comparison is two tasks, one for the
latest window and one for the period before it. A "which X ranked first"
question is one task, graded on the amount and the winner's name. Every oracle
was checked against the former SQL oracle on the pinned snapshot; all 53 agree.

Nine questions have no GraphJin oracle yet, because GraphJin cannot express
their value as one query result:

| Question | Why |
| --- | --- |
| q06, q12, q29 | distinct counts: GraphJin has no count-distinct aggregate |
| q08, q13, q25, q38 | a ratio of two aggregates |
| q21 | grouped by product category, three joins from the order line |
| q24 | grouped by territory group, a column of a joined table |
| q30, q31 | purchase-order totals are subtotal + tax + freight, and the table has no total column |

q16 pins its winning product with `dimension_literal`: selecting the product
relation through `specialofferproduct` changes GraphJin's grouping, so the
oracle query leaves it out.

The suite file is written by GraphJin's suite writer; change it with
`eval add` and `eval rm`, not by hand.
