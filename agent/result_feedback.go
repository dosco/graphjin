package agent

import (
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

// resultFeedback is whether a run reads what a function returned before it
// writes the code that depends on it.
//
// Without it the model writes one program: look up the table, run the query,
// call final. The query is written before the look-up has returned, so it is
// written from the model's guess at the columns and the spelling of values,
// and nothing the functions return is seen by a model again unless the
// program prints it. A note attached to a result went unread in every run
// that was traced.
//
// With it, Ax ends the block after a function whose result has to be read and
// gives the model a fresh turn with that result in front of it. A function
// whose result is plain data may still be followed by dependent code in the
// same block.
type resultFeedback string

const (
	feedbackOff resultFeedback = ""
	// feedbackLookups: catalog and help look-ups are read on a fresh turn;
	// a query's rows are data, so the block that runs it may also finish.
	feedbackLookups resultFeedback = "lookups"
	// feedbackAll: every function's result is read on a fresh turn.
	feedbackAll resultFeedback = "all"
)

// WithResultFeedback sets how a run reads its functions' results: "lookups",
// "all", or "" for off.
func WithResultFeedback(mode string) Option {
	return func(a *Agent) {
		switch resultFeedback(strings.TrimSpace(mode)) {
		case feedbackLookups:
			a.feedback = feedbackLookups
		case feedbackAll:
			a.feedback = feedbackAll
		default:
			a.feedback = feedbackOff
		}
	}
}

// policy is Ax's result policy for one of the agent's functions.
func (m resultFeedback) policy(function string) string {
	if m == feedbackLookups && function == "execute_graphql" {
		return "data"
	}
	return "inspect"
}

// functions hands Ax the agent's functions as plain records. Ax runs a typed
// Tool through a path that ends the whole run when its handler returns an
// error; a record's error is returned to the model's code, and the run goes
// on. Every recovery this agent sends the model (the real column names after
// an unknown column, the corrected query, the detail to inspect first) is a
// returned error.
func (m resultFeedback) functions(tools []ax.Tool) ax.Value {
	records := make([]ax.Value, 0, len(tools))
	for _, tool := range tools {
		records = append(records, functionRecord(tool, tool.Handler, m.policy(tool.Name)))
	}
	return ax.Array(records...)
}

// functionRecord is one function as a plain record, with Ax's result policy
// for it: "inspect" ends the block so the result is read on a fresh turn,
// "data" lets the code that follows the call run.
func functionRecord(tool ax.Tool, handler func(map[string]ax.Value) (ax.Value, error), policy string) ax.Value {
	return map[string]ax.Value{
		"name":         tool.Name,
		"description":  tool.Description,
		"parameters":   tool.Schema(),
		"handler":      handler,
		"resultPolicy": policy,
	}
}

// feedbackRewrite is one sentence of the standing instructions that tells the
// model to look up and act in a single program, with what replaces it when a
// look-up ends the block.
type feedbackRewrite struct {
	in      string // where the sentence lives, for the test that keeps these in step
	old     string
	lookups string
	all     string
}

var feedbackRewrites = []feedbackRewrite{
	{
		in:      "agentSignature",
		old:     "Author the query and call query_catalog({id:...}) in the same code block as execution; these lookups reuse verified prefetched facts locally. Avoid a separate turn printing the hidden seed to find IDs or known columns.",
		lookups: "Call query_catalog({id:...}) for the exact detail and end the block there: its result is shown to you on the next turn, and the query is written from what it shows. Do not spend a turn printing the hidden seed to find IDs or known columns.",
	},
	{
		in:      "runtimeSeedUsageInstructions",
		old:     `inspect the exact table or relationship detail with query_catalog({id:"..."}) (batch with {ids:[...]}), author the query with DB-side aggregate fields for any total, count, extreme, or ranking, run execute_graphql, and final from its result.`,
		lookups: `inspect the exact table or relationship detail with query_catalog({id:"..."}) (batch with {ids:[...]}) and end the block; on the next turn its result is in front of you, so author the query from the columns and values it shows, with DB-side aggregate fields for any total, count, extreme, or ranking, run execute_graphql, and final from its result.`,
		all:     `inspect the exact table or relationship detail with query_catalog({id:"..."}) (batch with {ids:[...]}) and end the block; on the next turn its result is in front of you, so author the query from the columns and values it shows, with DB-side aggregate fields for any total, count, extreme, or ranking, run execute_graphql and end the block; on the turn after that, final from the result shown.`,
	},
	{
		in:      "runtimeSeedUsageInstructions",
		old:     "After an execution returns data, immediately final with that data; never make another catalog call.",
		lookups: "After an execution returns data, immediately final with that data; never make another catalog call.",
		all:     "Once an execution's result is shown to you, final with that data on that turn; never make another catalog call.",
	},
	{
		in:      "preparedDiscoveryInstructions",
		old:     "Author from the matching schema and consume its full facts with await query_catalog({id: chosen.id}) in the same code block as execution; this is a local cached lookup.",
		lookups: "Consume the matching schema's full facts with await query_catalog({id: chosen.id}) and end the block; the detail is shown to you on the next turn, and the query is authored from it.",
	},
}

// rewrite replaces, in one of the standing instruction texts, the sentences
// that assume a single program.
func (m resultFeedback) rewrite(text string) string {
	if m == feedbackOff {
		return text
	}
	for _, sentence := range feedbackRewrites {
		replacement := sentence.lookups
		if m == feedbackAll && sentence.all != "" {
			replacement = sentence.all
		}
		text = strings.ReplaceAll(text, sentence.old, replacement)
	}
	return text
}
