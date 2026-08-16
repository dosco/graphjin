package agent

import ax "github.com/ax-llm/ax/packages/go"

// Two few-shot trajectory examples for the executor stage — the one teaching
// channel that shows a whole program rather than a query shape (catalog
// examples) or a rule (skills). They target the two families where weak
// models fail at trajectory assembly rather than at any single query:
// follow-up turns that must be read from inputs.history, and answers that
// span two sources in one execution.
//
// Discipline, in order of importance:
//   - Generic domain only (orders/status/policies) — never a benchmark
//     table. Teaching the language is legitimate; teaching the test is not.
//   - These render into EVERY actor step's provider request (the executor
//     re-renders examples per step), so the byte budget in
//     TestProgramExamplesBudget multiplies by max_actor_steps in practice.
//   - Their bytes are part of the provider-visible prompt, so they are
//     hashed into the prompt registry; an eval run's provenance changes when
//     they change.
//
// Element shape is the ax contract: {input: {<executor fields>}, output:
// {javascriptCode}}. The executor's inputs are NOT the agent signature —
// history and context arrive only as contextMetadata prose plus runtime
// globals, which is exactly what the first example demonstrates reading.
func executorTrajectoryExamples() []ax.Value {
	return []ax.Value{
		map[string]ax.Value{
			"input": map[string]ax.Value{
				"input": map[string]ax.Value{
					"instruction":  "Yes, go ahead and set that up.",
					"current_date": "2026-01-14",
				},
				"executorRequest": "Execute the watch the prior turn proposed and confirm it.",
				"contextMetadata": "- history: list loaded in the runtime as inputs.history (2 items) — read and narrow it with code; never retype its contents; item keys: role, content",
				"actionLog":       "query_catalog: help:security, help:runtime, help:watches, table detail for orders read",
			},
			"output": map[string]ax.Value{
				"javascriptCode": `const history = inputs.history || [];
const proposal = [...history].reverse().find(t => t.role === "assistant" && t.content.includes("watch"));
if (!proposal) { await final({status: "needs_clarification", answer: "No watch proposal found in the conversation to confirm."}); }
// The proposal named the watch, its filter, and hourly inbox digests; quotes
// inside the subscription string must be escaped as \".
const res = await execute_graphql({query: 'mutation { gj_watch(insert: { name: "failed_orders", query: "subscription { orders(where: { status: { eq: \\"failed\\" } }, first: 25, after: $cursor) { id status } orders_cursor }", delivery_json: { kind: "inbox", digest: { window: "1h" } } }) { id status enabled } }'});
const watch = res.data && res.data.gj_watch;
if (!watch) { await final({status: "error", answer: "Watch creation failed: " + JSON.stringify(res.errors)}); }
await final({status: "answered", answer: "Created the standing watch **failed_orders** (" + watch.id + "): it follows orders with status failed and delivers an hourly inbox digest."});`,
			},
		},
		map[string]ax.Value{
			"input": map[string]ax.Value{
				"input": map[string]ax.Value{
					"instruction":  "How many pending orders are we carrying, and what does the returns policy say about them?",
					"current_date": "2026-01-14",
				},
				"executorRequest": "Compute the count in the database and read the policy file in the same execution.",
				"contextMetadata": "- context: object loaded in the runtime as inputs.context — catalog seed only, not live rows",
				"actionLog":       "query_catalog: table details for orders and the policies file source read",
			},
			"output": map[string]ax.Value{
				"javascriptCode": `// One execution can span sources: a database aggregate root and a file root
// compose in a single query, so the answer never stitches client-side.
const res = await execute_graphql({query: 'query { orders(where: { status: { eq: "pending" } }) { count_id } policies(key: "docs/returns-policy.md") { key text } }'});
const count = res.data && res.data.orders && res.data.orders[0] && res.data.orders[0].count_id;
const policy = res.data && res.data.policies && res.data.policies[0];
if (count === undefined || !policy) { await final({status: "error", answer: "Cross-source read failed: " + JSON.stringify(res.errors)}); }
await final({status: "answered", answer: "We are carrying **" + count + " pending orders**. The returns policy (" + policy.key + ") says: " + policy.text.slice(0, 200)});`,
			},
		},
	}
}
