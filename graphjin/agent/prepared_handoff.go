package agent

import (
	"context"

	ax "github.com/ax-llm/ax/packages/go"
)

const preparedHandoffCode = "await final(inputs.instruction, {});"

// installPreparedHandoff replaces redundant request distillation with a local
// identity handoff for fresh requests whose starting root is already prepared.
// Ax currently always runs a distiller, even without contextFields. Its public
// per-program output-cache hook can supply this precomputed stage output without
// a provider call, while retaining Ax's session patch, executor and responder.
// This is a run-local constant, not a cross-request cache of schema or answers.
// It performs no discovery or execution: the executor must still consume exact,
// revision-checked catalog facts before acting through the normal protocol.
func installPreparedHandoff(ctx context.Context, program Program, prepared *preparationResult, rt GraphRuntime, req Request) {
	// Follow-ups and caller-provided context still need context interpretation.
	// Custom program factories retain their own pipeline contract.
	if len(req.History) != 0 || len(req.Context) != 0 {
		return
	}
	axProgram, ok := program.(*ax.AxAgent)
	if !ok {
		return
	}
	digest := prepared.digest()
	if len(digest) == 0 || !discoveryRootKind(stringFromMap(mapValue(digest[0]), "kind")) {
		return
	}
	if mapValue(digest[0])["scope_match"] != true {
		return
	}
	if !prepared.revalidate(ctx, rt, req.Namespace) {
		return
	}
	axProgram.Distiller.Options["cachingFunction"] = ax.AxCachingFunction(func(_ string, _ map[string]ax.Value) (map[string]ax.Value, error) {
		return map[string]ax.Value{"javascriptCode": preparedHandoffCode}, nil
	})
	prepared.stats.PreparedHandoff = true
}
