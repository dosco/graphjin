package agent

import (
	ax "github.com/ax-llm/ax/packages/go"
)

// A lead turn reads what the data agent answered before it writes the code
// that depends on the answer.
//
// Ax ends a block of the lead's code at ask_graphjin and gives the model a
// fresh call with the answers in front of it, so a second question can name
// what the first one found and a turn finishes only after its answers were
// read. remember and follow_up return a confirmation, and code may go on after
// them.
//
// Until 2026-11-18 a block ran to its end: it asked, and whatever followed the
// ask in the same block was written before there was an answer. The model saw
// an answer only if the block printed it, on its next call. That way of
// working is gone. This is how every lead turn works, and there is no setting
// for it: nobody running GraphJin could judge one.
//
// It is the data agent's result feedback (resultFeedback) for the lead, wired
// the same way: the runtime Ax is built with says it supports ending a block,
// Ax's own registration of each function is the one that runs, and the
// functions are handed to Ax as plain records so that an error one returns
// reaches the model instead of ending the turn.
//
// What was measured against the old way before it was removed (2026-11-18,
// each pair run at the same time). What it does depends on the model.
//
// On gpt-6-luna the fresh call is used. After a first answer that had failed,
// the lead asked again in 5 of 5 conversation turns and 11 of 11 routine
// turns, and it asked nothing no more often than before. Conversations: 39 of
// 45 runs right against 36. Routines that finished decided alike (acted when
// due 8 of 10 against 5 of 7); 10 of the 48 firings ran into the lead's
// 120-second turn limit, either way. It costs about a quarter more tokens
// and, in conversations, 5 seconds.
//
// A second routine pair on gpt-6-luna with a 300-second turn limit, 48
// firings a side over 8 threads, had no firing cut off: 44 of 48 right
// against 38 of 48 (acted when due 20 of 24 against 16, quiet when due 24 of
// 24 against 22; over threads, p = 0.12), 9 of 9 failed first answers asked
// again, and about 30% more tokens.
//
// On gpt-5.4-mini, a model two generations older, it was worse. A routine
// that has to look something up proposed its change when it was due in 9 of
// 36 firings against 23 of 36, over two pairs. The lead's code asked the data
// agent nothing in 35 of 72 such turns against 9 of 72: it went straight to
// final(). And the fresh call was not used for what it is for: a second
// question followed a first in 2 of those 72 turns, and in 1 of the 8 whose
// first answer had failed. Conversations: 30 of 45 runs right against 32,
// with a quarter more lead model calls. GraphJin is not built for a model
// that old, so this is how the lead works all the same.
//
// Not measured: any model outside those two, Gemini among them.

// SupportsObservationBoundaries is how Ax asks a runtime whether a block can
// be ended after a function result that has to be read.
func (r leadNamesRuntime) SupportsObservationBoundaries() bool {
	return true
}

// UsageInstructions is the lead's instructions for its code. Ax asks the
// runtime it is built with for them.
func (r leadNamesRuntime) UsageInstructions() string {
	return r.usage
}

// leadResultPolicy is Ax's result policy for one of the lead's functions. A
// data agent's answer is prose that has to be read.
func leadResultPolicy(function string) string {
	if function == toolAskGraphJin {
		return "inspect"
	}
	return "data"
}

// leadFunctionRecords hands Ax the lead's functions as plain records
// (resultFeedback.functions says why). An answer is shown by Ax on the next
// call; what a function that does not end the block returned is still noted
// for a step that printed nothing (unprintedCalls).
func leadFunctionRecords(tools []ax.Tool, returned *unprintedCalls) ax.Value {
	records := make([]ax.Value, 0, len(tools))
	for _, tool := range tools {
		t := tool
		policy := leadResultPolicy(t.Name)
		handler := t.Handler
		if policy == "data" {
			handler = func(args map[string]ax.Value) (ax.Value, error) {
				out, err := t.Handler(args)
				if err == nil {
					returned.record(t.Name, out)
				}
				return out, err
			}
		}
		records = append(records, functionRecord(t, handler, policy))
	}
	return ax.Array(records...)
}
