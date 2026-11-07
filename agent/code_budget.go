package agent

import (
	ax "github.com/ax-llm/ax/packages/go"
	axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"
)

// minCodeBlockSeconds is the least time a code block may run. goja interrupts
// a block still running after its timeoutMs, 5s by default, and the clock
// includes time spent inside host calls: a delegation (a whole data-agent run,
// 8 to 40s on the demo) or a query against a slow warehouse outlasts it. The
// call still completes, but the block is interrupted as soon as it returns,
// so an answer already in hand is thrown away as "goja execution timed out"
// and asked for again.
const minCodeBlockSeconds = 60

// codeBlockPolicy lets a block run as long as its turn, and never less than
// minCodeBlockSeconds. The turn's own deadline still ends it; this budget only
// stops code that never yields, such as a loop with no host call.
func codeBlockPolicy(turnSeconds int) axgoja.Option {
	seconds := max(turnSeconds, minCodeBlockSeconds)
	return axgoja.WithRuntimePolicy(map[string]ax.Value{"timeoutMs": seconds * 1000})
}
