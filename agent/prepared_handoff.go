package agent

import (
	"context"
	"strings"

	ax "github.com/ax-llm/ax/packages/go"
)

const preparedHandoffCode = "await final(inputs.instruction, {});"

// LocalHandoffOutputKey marks a distiller stage output that GraphJin supplied
// itself instead of asking a model. Ax records the stage request and response
// for a cache hit exactly as it does for a provider call, so without the mark
// a trace reader counts a model call that never happened and has no prompt.
const LocalHandoffOutputKey = "graphjinLocalHandoff"

// localHandoff is the distiller output both handoffs supply: the instruction
// passed on unchanged, marked as GraphJin's own.
func localHandoff(_ string, _ map[string]ax.Value) (map[string]ax.Value, error) {
	return map[string]ax.Value{"javascriptCode": preparedHandoffCode, LocalHandoffOutputKey: true}, nil
}

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
	if len(req.Context) != 0 {
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
	axProgram.Distiller.Options["cachingFunction"] = ax.AxCachingFunction(localHandoff)
	prepared.stats.PreparedHandoff = true
}

// installSeedHandoff skips request distillation for a plain question when no
// preparation could. The distiller's only work on such a request was to print
// the catalog seed it cannot see in its prompt and pick ids from it — a whole
// model call in every observed run, plus another to hand the choice on. The
// seed digest in catalog_facets now puts the top tables and their real columns
// in front of the executor, which inspects and executes in its first step,
// governed by the same protocol guards.
func installSeedHandoff(program Program, req Request) bool {
	if len(req.Context) != 0 {
		return false
	}
	axProgram, ok := program.(*ax.AxAgent)
	if !ok {
		return false
	}
	if _, set := axProgram.Distiller.Options["cachingFunction"]; set {
		return false
	}
	axProgram.Distiller.Options["cachingFunction"] = ax.AxCachingFunction(localHandoff)
	return true
}

const (
	seedDigestTables  = 6
	seedDigestColumns = 24
	seedDigestOthers  = 6
)

// seedDigest condenses the catalog seed into what an executor needs to act
// without printing it: each seed table's id and real column names (with
// types), and the ids of seed saved queries and workflows. Cards keep seed
// order, which is relevance order.
func seedDigest(seed any) []any {
	type table struct {
		id, name string
		columns  []string
	}
	var tables []*table
	byID := map[string]*table{}
	tableFor := func(id, name string) *table {
		if t := byID[id]; t != nil {
			return t
		}
		if len(tables) == seedDigestTables {
			return nil
		}
		t := &table{id: id, name: name}
		byID[id] = t
		tables = append(tables, t)
		return t
	}
	var others []any
	for _, card := range catalogCards(seed) {
		id, kind := stringFromMap(card, "id"), stringFromMap(card, "kind")
		switch kind {
		case "table":
			tableFor(id, stringFromMap(card, "table_name"))
		case "column":
			column := stringFromMap(card, "column_name")
			rest := strings.TrimPrefix(id, "column:")
			cut := strings.LastIndex(rest, "."+column)
			if column == "" || cut < 0 {
				continue
			}
			t := tableFor("table:"+rest[:cut], stringFromMap(card, "table_name"))
			if t == nil || len(t.columns) == seedDigestColumns {
				continue
			}
			entry := column
			if typ := strings.TrimSpace(strings.SplitN(stringFromMap(card, "summary"), ",", 2)[0]); typ != "" {
				entry += ":" + typ
			}
			t.columns = append(t.columns, entry)
		default:
			if discoveryRootKind(kind) && len(others) < seedDigestOthers {
				others = append(others, map[string]any{"id": id, "kind": kind, "name": stringFromMap(card, "title")})
			}
		}
	}
	out := make([]any, 0, len(tables)+len(others))
	for _, t := range tables {
		entry := map[string]any{"id": t.id, "kind": "table", "name": t.name}
		if len(t.columns) != 0 {
			entry["columns"] = t.columns
		}
		out = append(out, entry)
	}
	return append(out, others...)
}
