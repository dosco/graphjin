package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const protocolContextKey = "_graphjin_discovery"

const emptySearchKnownIDLimit = 6

// emptyDetailKnownIDLimit bounds the id space searched when a detail lookup
// misses. It is far larger than the display limit because it is matched against,
// not shown: finding the one card a short id names needs the whole space, while
// the model is only ever handed the few best candidates.
const emptyDetailKnownIDLimit = 512

// Keep complete ordinary GraphQL operations in the private action trail so
// evaluation method rules never depend on argument order within a mutation.
// Variables and headers remain redacted separately below.
const actionQueryTraceLimit = 8192

type protocolRuntime struct {
	base          GraphRuntime
	state         *discoveryState
	namespace     string
	seedLimit     int
	catalogSearch CatalogSearchFeatures
}

type discoveryState struct {
	instruction            string
	seedOK                 bool
	seedResult             any
	modelDiscoveryAction   bool
	catalogIDs             map[string]bool
	catalogKinds           map[string]bool
	savedQueriesDiscovered map[string]bool
	savedQueriesDetailed   map[string]bool
	// savedQueryDetailMissed records the names a detail lookup in THIS run
	// asked for and the catalog did not return. Absence has to be tracked
	// separately from presence: an empty lookup deliberately does not mark a
	// query detailed, so without this the run cannot tell "not looked up yet"
	// from "looked up, does not exist" — and the guard demanded the lookup
	// again either way.
	savedQueryDetailMissed  map[string]bool
	savedQueryGraphQL       map[string]string
	securityRuntimeEvidence bool
	// A run's writes are accounted for at finalization: an "answered" final
	// after nothing but failed writes reports work that never happened. Ten of
	// fifteen action tasks in one benchmark run were lost to exactly that —
	// one rejected mutation, then "Successfully recorded..." shipped as the
	// answer through the exhausted-loop rescue.
	mutationAttempted         bool
	mutationSucceeded         bool
	lastMutationFailure       string
	lastMutationRepairedQuery string
	watchDefinitionMutated    bool
	annotationMutated         bool
	// Per-target mutation evidence, populated only by id-detail lookups and
	// validations in THIS run (search hits never count, mirroring the
	// saved-query detail rule).
	detailKinds       map[string]bool
	tablesDetailed    map[string]bool
	tablesValidated   map[string]bool
	workflowsDetailed map[string]bool
	actions           []protocolAction
	helpTopics        []string
	catalogSearches   []map[string]any
	catalogDetails    []string
	// lastCatalogDetail retains the most recent successful exact detail result
	// for the runtime-only distiller -> executor handoff. It is never rendered
	// into a prompt or accepted as authorization by itself.
	lastCatalogDetail any
	// distilledSourceIDs are exact source cards selected by the distiller for
	// the executor handoff. When more than one source is selected, the executor
	// must inspect every source detail before it can author cross-source
	// GraphQL. This prevents the distiller-to-executor boundary from collapsing
	// a multi-source plan into one guessed source.
	distilledSourceIDs []string
	suggestedNext      []any
	validations        []map[string]any
	executions         []map[string]any
	// A failed raw GraphQL execution must be followed by one genuinely
	// different repair execution before the actor may terminate. Identical
	// retries are rejected without spending another database execution. Failed
	// identities remain rejected for the rest of the run; the pending key only
	// tracks whether the current failure still needs one distinct repair.
	pendingFailedQueryKey   string
	pendingFailedCatalogIDs []string
	failedQueryKeys         map[string]bool
	// A caller-visible invented system root has one deterministic, read-only
	// correction. The Go runtime schedules that exact query after the failed
	// actor step so a weak model cannot discard the structured did-you-mean
	// payload or loop on the same alias.
	pendingSystemRootQuery string
	// Successful executions are memoized by their normalized operation and
	// variables. Repeating one never reaches the database again. The first
	// redundant call arms a one-turn completion grace period; another already
	// seen call lets the runtime finalize from the governed cached evidence.
	successfulExecutions       map[string]any
	completionLatchKey         string
	completionReady            bool
	terminalContinuationIssued bool
	// lastExecution retains the most recent successful execution result for
	// one narrow recovery case: an actor that ignores that result and repeats
	// the preceding catalog lookup. Ax's runtime is a persistent REPL, but the
	// next actor prompt emphasizes the latest tool result; replaying already
	// authorized data there prevents the result from being displaced by a
	// catalog-loop error.
	lastExecution any
	rawGraphQL    []map[string]any
	violations    []protocolViolation
	// catalogRequestKeys prevents an actor loop from issuing an identical
	// catalog request repeatedly instead of consuming the result it already
	// received. Only successful model calls are recorded; the seed and failed
	// attempts do not spend this budget.
	catalogRequestKeys map[string]bool
	// One empty lexical/coverage search is allowed and returned with concrete
	// recovery guidance. A second blind search is rejected before dispatch so
	// the actor must consume known ids or enumerate the catalog by kind.
	emptySearchStreak int
	// One unresolved id-detail lookup is likewise allowed with concrete known-id
	// guidance. A second unknown guess is rejected before dispatch. A detail id
	// already surfaced by this run remains eligible for lookup so following the
	// recovery guidance can resolve and reset the streak.
	emptyDetailStreak  int
	capabilities       *CapabilityProfile
	observe            func(ActionEvent)
	coverageSearchUsed bool
	// mutationEvidenceSupplied keeps the one-shot discharge of a missing
	// mutation-shape prerequisite from becoming an unbounded fetch loop.
	mutationEvidenceSupplied bool
	// mutationEvidenceSuppliedFor records which target tables already had their
	// shape evidence supplied, so a write touching a second table is not refused
	// because a first one was helped.
	mutationEvidenceSuppliedFor map[string]bool
	// observedValueLookups records what each write-time value lookup returned, so a
	// check that stays silent can be told apart from one that found nothing.
	observedValueLookups []map[string]any
	// observedValues caches sampled column value sets per table so a write path
	// costs at most one extra catalog read per table.
	observedValues map[string]map[string][]string
	// tableColumnNames caches each table's full column-name list (from the same
	// phase-one catalog read observedValues starts with) so a failed write's
	// recovery can name the real columns without another catalog round trip.
	tableColumnNames map[string][]string
	// crossSourceEvidenceSupplied keeps the one-shot fetch of source cards from
	// repeating if the ids ever stop registering.
	crossSourceEvidenceSupplied bool
	// securityRuntimeEvidenceSupplied keeps the one-shot fetch of the security
	// and runtime guidance cards from repeating if the ids ever stop registering.
	securityRuntimeEvidenceSupplied bool
	// systemRootDiscoverySupplied keeps the one-shot fetch of a system root's
	// help contract from repeating; a run that cannot proceed from the supplied
	// card must fail loudly on the normal refusal instead.
	systemRootDiscoverySupplied bool
	// clarificationNudgeUsed keeps the premature-clarification redirect to one
	// per run: the first ask is redirected to discovery, the second passes
	// through, so a genuinely needed clarification is delayed by at most one
	// turn and never suppressed.
	clarificationNudgeUsed bool
	// ungroundedRewriteUsed records that the answer was restated once after the
	// grounding gate rejected a draft. One attempt per run: the model does not
	// control the retry, so a single latch bounds it completely.
	ungroundedRewriteUsed bool
	// forcedFinalizeUsed records that the answer came from the tool-less
	// finalize after actor-step exhaustion. Surfaced in evidence — never as a
	// violation, since unregistered violation codes score as safety failures —
	// so a run rescued this way stays distinguishable from one that finalized
	// on its own.
	forcedFinalizeUsed bool
	// remoteJoinParents maps a join-only remote table to the parent it is served
	// under, learned from relationship cards whose source is remote_join.
	remoteJoinParents map[string]string
	// remoteJoinRepairOffered records which join-only roots already received the
	// full repair explanation, so a looping model gets the corrected query
	// without the paragraph a second time.
	remoteJoinRepairOffered map[string]bool
	// referentRewriteNoticed keeps the referent-binding violation record to one
	// per query identity; the rewrite itself repeats freely.
	referentRewriteNoticed map[string]bool
	// remoteJoinNoticed keeps the violation record to one per root; the
	// interception itself repeats, because the intercepted call cannot succeed.
	remoteJoinNoticed map[string]bool
	// observedValueStrikes counts refusals per (table, column, value), so an
	// unobserved value is corrected twice and then allowed through — the sole
	// authority for this guard.
	//
	// It briefly shared the job with a query-text identity map, and the pairing
	// was measured to make the let-through unreachable: models never resubmit
	// byte-identical queries, they reword the note or reorder fields, so every
	// repeat hit the unbounded text veto while the strike counter stalled below
	// its limit. One run showed 5–7 fires per episode across ten episodes, all
	// writing the same rejected value, none ever allowed through. Keying every
	// refusal on the mismatch itself makes the third attempt land regardless of
	// phrasing, which is what "two corrections, then the caller decides" meant.
	observedValueStrikes map[string]int
	// historyReferentRejected records the operations already refused for not
	// scoping to the retained subject, by identity.
	//
	// A one-shot flag was wrong here. It let the next attempt through whatever it
	// contained, and the measured behaviour was the model re-sending the identical
	// unscoped query and answering the unscoped question — the escape hatch became
	// the default path. Keying on identity keeps the hatch for a genuinely different
	// operation while refusing a byte-identical retry, which is the same standard
	// the duplicate-query guards already apply.
	historyReferentRejected map[string]int
	// history is bounded, untrusted caller/task context. It never satisfies a
	// discovery guard, but an explicit request to repeat a prior answered turn
	// can safely recover from an actor-loop failure after this run has performed
	// its own model-driven discovery.
	history []Turn
	// groundingCorpus accumulates this run's observed evidence (instruction,
	// history, tool arguments, tool results) for the answer-grounding check.
	groundingCorpus   strings.Builder
	groundingOverflow bool
}

type protocolAction struct {
	Step    int            `json:"step"`
	Source  string         `json:"source"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args,omitempty"`
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
	// Evidence is a bounded copy of a catalog detail card's guidance fields. The
	// summary records that a card was returned but not what it said, which made
	// "did the model actually see this?" unanswerable from a stored trajectory and
	// produced two wrong diagnoses in two days. It is private-episode material
	// only: shareable reports never carry it.
	Evidence map[string]any `json:"evidence,omitempty"`
	Error    string         `json:"error,omitempty"`

	startedAt time.Time
}

type protocolViolation struct {
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Tool     string         `json:"tool,omitempty"`
	Blocking bool           `json:"blocking"`
	Details  map[string]any `json:"details,omitempty"`
}

func newProtocolRuntime(base GraphRuntime, instruction, namespace string, seedLimit int, profile *CapabilityProfile, observe func(ActionEvent), catalogSearch CatalogSearchFeatures) *protocolRuntime {
	if seedLimit <= 0 {
		seedLimit = defaultSeedLimit
	}
	state := newDiscoveryState(instruction)
	state.capabilities = profile
	state.observe = observe
	state.addCapabilityIntentViolation()
	return &protocolRuntime{
		base:          base,
		namespace:     namespace,
		seedLimit:     seedLimit,
		state:         state,
		catalogSearch: catalogSearch,
	}
}

// addCapabilityIntentViolation turns an explicit server-side root denial into
// a deterministic policy result. The model may still inspect visible, redacted
// catalog guidance, but it cannot turn a configuration-write request into an
// answered result merely by describing an out-of-band edit.
func (s *discoveryState) addCapabilityIntentViolation() {
	if s == nil || !profileExplicitlyBlocksRoot(s.capabilities, systemRootConfig) || !configWriteIntent(s.instruction) {
		return
	}
	s.addViolation(
		"access_blocked",
		"the caller capability profile does not permit gj_config changes; an authorized administrator must perform this configuration update",
		toolExecuteGraphQL,
		true,
		map[string]any{"root": systemRootConfig},
	)
}

func profileExplicitlyBlocksRoot(profile *CapabilityProfile, root string) bool {
	if profile == nil {
		return false
	}
	for _, blocked := range profile.BlockedSystemRoots {
		if strings.EqualFold(strings.TrimSpace(blocked), strings.TrimSpace(root)) {
			return true
		}
	}
	return false
}

func configWriteIntent(instruction string) bool {
	words := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(instruction), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}) {
		words[word] = true
	}
	if !words["gj_config"] && !(words["graphjin"] && words["config"]) {
		return false
	}
	for _, verb := range []string{"add", "change", "configure", "delete", "disable", "edit", "enable", "modify", "remove", "set", "update"} {
		if words[verb] {
			return true
		}
	}
	return false
}

func newDiscoveryState(instruction string) *discoveryState {
	state := &discoveryState{
		instruction:            strings.TrimSpace(instruction),
		catalogIDs:             map[string]bool{},
		catalogKinds:           map[string]bool{},
		savedQueriesDiscovered: map[string]bool{},
		savedQueriesDetailed:   map[string]bool{},
		savedQueryGraphQL:      map[string]string{},
		detailKinds:            map[string]bool{},
		tablesDetailed:         map[string]bool{},
		tablesValidated:        map[string]bool{},
		workflowsDetailed:      map[string]bool{},
		catalogRequestKeys:     map[string]bool{},
		failedQueryKeys:        map[string]bool{},
		successfulExecutions:   map[string]any{},
	}
	state.addGrounding(state.instruction)
	return state
}

func (r *protocolRuntime) Seed(ctx context.Context) (any, error) {
	args := map[string]any{
		"search":  r.state.instruction,
		"explain": true,
		"limit":   r.seedLimit,
	}
	r.addNamespace(args)
	action := r.state.startAction("seed", "query_catalog", args)
	out, err := r.base.QueryCatalog(ctx, args)
	r.state.finishAction(action, "query_catalog", args, out, err)
	if err != nil {
		r.state.addViolation("catalog_seed_failed", "initial catalog discovery failed; the agent cannot safely answer without gj_catalog", "query_catalog", true, map[string]any{"error": err.Error()})
		return nil, err
	}
	r.state.seedOK = true
	r.state.recordCatalog(args, out, true)
	r.state.seedResult = normalizeValue(out)
	return r.state.seedResult, nil
}

func (r *protocolRuntime) GraphQLHelp(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	action := r.state.startAction("model", "graphql_help", args)
	out, err := r.base.GraphQLHelp(ctx, args)
	r.state.finishAction(action, "graphql_help", args, out, err)
	if err == nil {
		r.state.clearCompletionLatch()
		r.state.modelDiscoveryAction = true
		topic := stringArg(args, "for")
		if topic == "" {
			topic = "discovery"
		}
		r.state.helpTopics = appendUniqueString(r.state.helpTopics, topic)
		r.state.recordCatalogRows(out, true)
		r.state.recordNext(out)
	}
	return out, err
}

func (r *protocolRuntime) QueryCatalog(ctx context.Context, args map[string]any) (any, error) {
	if _, hasCoverage := args["searches"]; hasCoverage {
		searches, err := r.validateCoverageSearches(args)
		if err != nil {
			action := r.state.startAction("model", "query_catalog", args)
			r.state.finishAction(action, "query_catalog", args, nil, err)
			return nil, err
		}
		args["searches"] = searches
		args["explain"] = true
		r.state.coverageSearchUsed = true
	}
	r.addNamespace(args)
	// A recoverable execution guard already selected the exact missing catalog
	// evidence. Weak models sometimes respond by issuing another broad catalog
	// search instead of following recovery.next, then repeat that search until
	// the actor budget is exhausted. Keep the actor loop unchanged, but route
	// the next catalog call through the narrow protocol-derived continuation.
	protocolRepair := false
	if repairArgs := r.state.pendingRecoverableCatalogArgs(); len(repairArgs) != 0 {
		args = repairArgs
		r.addNamespace(args)
		protocolRepair = true
	}
	// Catalog seed results are authoritative enough to correct an invented
	// watch capability id to the exact help row. This is a deterministic
	// did-you-mean repair, not a new hidden discovery call.
	args = r.state.correctKnownCatalogDetailArgs(args)
	searchRequest := isCatalogSearchRequest(args)
	detailIDs := detailIDsFromArgs(args)
	requestKey := stringify(normalizeValue(args))
	if protocolRepair {
		r.state.emptyDetailStreak = 0
		delete(r.state.catalogRequestKeys, requestKey)
	}
	if searchRequest && r.state.emptySearchStreak > 0 {
		r.state.emptySearchStreak++
		out := r.state.emptySearchExhaustedResult()
		action := r.state.startAction("model", "query_catalog", args)
		r.state.finishAction(action, "query_catalog", args, out, nil)
		return out, nil
	}
	if len(detailIDs) != 0 && r.state.emptyDetailStreak > 0 && !r.state.hasKnownCatalogID(detailIDs) {
		r.state.emptyDetailStreak++
		r.state.recordMissingCatalogDetails(detailIDs)
		out := r.state.emptyDetailExhaustedResult(detailIDs)
		action := r.state.startAction("model", "query_catalog", args)
		r.state.finishAction(action, "query_catalog", args, out, nil)
		return out, nil
	}
	if requestKey != "" && r.state.catalogRequestKeys[requestKey] {
		message := "duplicate query_catalog call rejected: this exact request already returned catalog evidence; reuse the prior result and follow its next guidance instead of searching again"
		if r.state.lastExecution != nil {
			r.state.recordRepeatedCall("query_catalog:" + requestKey)
			out := map[string]any{
				"cards": []any{},
				"recovery": map[string]any{
					"reason":    "catalog evidence and live execution data were already returned in this run",
					"execution": r.state.lastExecution,
					"next":      "call final now using recovery.execution.result.data; do not call query_catalog again",
				},
			}
			action := r.state.startAction("model", "query_catalog", args)
			r.state.finishAction(action, "query_catalog", args, out, nil)
			return out, nil
		}
		err := fmt.Errorf("%s", message)
		action := r.state.startAction("model", "query_catalog", args)
		r.state.finishAction(action, "query_catalog", args, nil, err)
		return nil, err
	}
	action := r.state.startAction("model", "query_catalog", args)
	out, err := r.base.QueryCatalog(ctx, args)
	if err == nil {
		r.state.clearCompletionLatch()
		if requestKey != "" {
			r.state.catalogRequestKeys[requestKey] = true
		}
		r.state.modelDiscoveryAction = true
		r.state.recordCatalog(args, out, false)
		if len(returnedCatalogDetailIDs(detailIDs, out)) != 0 {
			r.state.lastCatalogDetail = normalizeValue(out)
		}
		if len(catalogCards(out)) != 0 {
			r.state.emptySearchStreak = 0
			if len(detailIDs) == 0 {
				r.state.emptyDetailStreak = 0
			}
		} else if searchRequest {
			r.state.emptySearchStreak = 1
			out = attachEmptySearchRecovery(out, r.state.emptySearchNext(
				"No catalog cards matched. Drop search filters and inspect a known id, or enumerate tables with query_catalog({kind:\"table\"}) instead of trying another blind search.",
			))
		}
		if len(detailIDs) != 0 {
			returned := returnedCatalogDetailIDs(detailIDs, out)
			if len(returned) != 0 {
				r.state.emptyDetailStreak = 0
			} else {
				known := r.state.knownCatalogIDs(emptyDetailKnownIDLimit)
				// A short id names a real card whose qualifiers were omitted. Serving
				// it costs one base call; reporting it costs a step, and the step after
				// a missed detail is a refused execution whose envelope has no data
				// field, which is where executor code crashes instead of recovering.
				if repairs, ok := repairCatalogDetailIDs(detailIDs, known); ok {
					if repaired, repairErr := r.base.QueryCatalog(ctx, rewriteCatalogIDArgs(args, repairs)); repairErr == nil {
						canonical := make([]string, 0, len(repairs))
						for _, id := range repairs {
							canonical = appendUniqueString(canonical, id)
						}
						if len(returnedCatalogDetailIDs(canonical, repaired)) != 0 {
							r.state.emptyDetailStreak = 0
							out = attachCatalogIDRepairNotice(repaired, repairs)
							r.state.recordCatalog(rewriteCatalogIDArgs(args, repairs), out, false)
							r.state.lastCatalogDetail = normalizeValue(out)
							r.state.finishAction(action, "query_catalog", args, out, nil)
							return out, nil
						}
					}
				}
				r.state.emptyDetailStreak = 1
				r.state.recordMissingCatalogDetails(detailIDs)
				out = attachEmptyDetailRecovery(out, detailIDs, r.state.emptyDetailNext(
					"The requested catalog id was not returned. Inspect a known id or enumerate tables by kind before trying another detail lookup.",
				), catalogIDSuggestions(detailIDs, known))
			}
		}
	}
	r.state.finishAction(action, "query_catalog", args, out, err)
	return out, err
}

// runtimeHandoffEvidence returns only exact detail evidence selected by the
// model during distillation. The value stays inside the shared Goja session;
// it is a continuity mechanism, while the normal same-run execution and policy
// guards remain authoritative.
func (s *discoveryState) runtimeHandoffEvidence() any {
	if s == nil || s.lastCatalogDetail == nil || len(s.catalogDetails) == 0 {
		return nil
	}
	return map[string]any{
		"catalog_detail_ids": append([]string(nil), s.catalogDetails...),
		"catalog_detail":     normalizeValue(s.lastCatalogDetail),
	}
}

func isCatalogSearchRequest(args map[string]any) bool {
	if len(detailIDsFromArgs(args)) != 0 {
		return false
	}
	_, hasSearch := args["search"]
	_, hasSearches := args["searches"]
	return hasSearch || hasSearches
}

func attachEmptySearchRecovery(out any, next map[string]any) any {
	result := cloneAnyMap(mapValue(out))
	if result == nil {
		result = map[string]any{"cards": []any{}}
	}
	result["recovery"] = map[string]any{
		"kind":        "empty_search",
		"instruction": "The search returned no catalog cards. Broaden once by dropping filters, enumerate tables by kind, or inspect one of the catalog ids already discovered in this run.",
		"known_ids":   next["known_ids"],
		"next":        next,
	}
	return result
}

// attachCatalogIDRepairNotice records which requested id was served under its
// canonical name, so later calls in the run use the published id rather than the
// short form that missed.
func attachCatalogIDRepairNotice(out any, repairs map[string]string) any {
	result := cloneAnyMap(mapValue(out))
	served := make([]map[string]any, 0, len(repairs))
	requested := make([]string, 0, len(repairs))
	for missed := range repairs {
		requested = appendUniqueString(requested, missed)
	}
	sort.Strings(requested)
	for _, missed := range requested {
		served = append(served, map[string]any{"requested": missed, "canonical": repairs[missed]})
	}
	result["recovery"] = map[string]any{
		"kind":        "catalog_id_qualified",
		"instruction": "The requested id omitted its database and schema qualifiers and matched exactly one published card, which is returned here. Use the canonical id for the rest of this run.",
		"resolved":    served,
	}
	return result
}

// attachWatchNormalizationNotice records that a gj_watch subscription string
// arrived with unescaped quotes and was executed as its parse-verified
// re-escaping. The notice rides the recovery block rather than a violation
// code: re-escaping is a reading of what the caller wrote, not a rewrite, and
// the registry contract leaves no room for a non-blocking code — an
// unregistered code fails safety scoring, a registered one must block.
func attachWatchNormalizationNotice(out any, repaired string) any {
	instruction := `The gj_watch subscription string arrived with unescaped quotes; the mutation executed as its verified re-escaping, shown in repaired_query. Escape nested quotes as \" in future watch mutations.`
	recovery := map[string]any{
		"kind":           "watch_subscription_normalized",
		"code":           "watch_subscription_normalized",
		"instruction":    instruction,
		"repaired_query": repaired,
	}
	switch res := out.(type) {
	case executeResult:
		res.Recovery = recovery
		res.Guidance = instruction
		return res
	case *executeResult:
		res.Recovery = recovery
		res.Guidance = instruction
		return res
	case map[string]any:
		res = cloneAnyMap(res)
		res["recovery"] = recovery
		res["guidance"] = instruction
		return res
	default:
		return out
	}
}

// attachRemoteJoinRewriteNotice tells the model what it is looking at. The data
// it receives came back under the parent's field name with the join nested
// inside, which is not the shape it asked for, and an unexplained reshaping is
// how a model ends up reporting that its query returned nothing.
func attachRemoteJoinRewriteNotice(out any, repaired, root, parent string) any {
	recovery := map[string]any{
		"kind":           "remote_join_route_rewritten",
		"code":           "remote_join_route_rewritten",
		"instruction":    root + " is an API join with no rows of its own, so the top-level query could not return data. It executed as the nested route shown in repaired_query, and the results below are " + parent + " rows with " + root + " nested inside each one. Read " + root + " from there, and query it that way from now on.",
		"repaired_query": repaired,
	}
	switch res := out.(type) {
	case executeResult:
		res.Recovery = recovery
		return res
	case *executeResult:
		res.Recovery = recovery
		return res
	case map[string]any:
		res = cloneAnyMap(res)
		res["recovery"] = recovery
		return res
	default:
		return out
	}
}

func attachEmptyDetailRecovery(out any, missedIDs []string, next map[string]any, suggestions map[string][]string) any {
	result := cloneAnyMap(mapValue(out))
	if len(catalogCards(result)) == 0 {
		result["cards"] = []any{}
	}
	recovery := map[string]any{
		"kind":        "empty_detail",
		"instruction": "The requested catalog detail was not found. Use a known catalog id from this run or enumerate tables by kind instead of guessing another id.",
		"missed_ids":  append([]string(nil), missedIDs...),
		"known_ids":   next["known_ids"],
		"next":        next,
	}
	if len(missedIDs) == 1 {
		recovery["missed_id"] = missedIDs[0]
	}
	if len(suggestions) != 0 {
		// Named candidates beat a list to scan: these ids do not exist, and the
		// closest published cards are what the caller has to choose between.
		recovery["did_you_mean"] = suggestions
		recovery["instruction"] = "The requested catalog detail was not found. Inspect one of the did_you_mean candidates, or enumerate tables by kind; do not guess another id."
	}
	result["recovery"] = recovery
	return result
}

func (s *discoveryState) emptySearchExhaustedResult() executeResult {
	next := s.emptySearchNext("A prior catalog search already returned no cards. Enumerate tables by kind or inspect a known id before searching again.")
	return executeResult{
		Errors: []ErrorInfo{{
			Message: "consecutive empty catalog search rejected; stop varying blind search text and use the supplied catalog ids or enumerate tables by kind",
			Extensions: map[string]any{
				"code":      "empty_search_exhausted",
				"retryable": true,
				"graphjin_repair": map[string]any{
					"kind":      "empty_search_exhausted",
					"known_ids": next["known_ids"],
					"next":      next,
				},
			},
		}},
		Recovery: map[string]any{
			"kind":        "empty_search_exhausted",
			"instruction": "Do not issue another lexical search. Enumerate the catalog by kind or inspect a known id.",
			"known_ids":   next["known_ids"],
			"next":        next,
		},
	}
}

func (s *discoveryState) emptyDetailExhaustedResult(missedIDs []string) executeResult {
	next := s.emptyDetailNext("A prior catalog detail lookup returned no matching card. Inspect a known id or enumerate tables by kind before trying another detail lookup.")
	repair := map[string]any{
		"kind":       "empty_detail_exhausted",
		"missed_ids": append([]string(nil), missedIDs...),
		"known_ids":  next["known_ids"],
		"next":       next,
	}
	if len(missedIDs) == 1 {
		repair["missed_id"] = missedIDs[0]
	}
	recovery := map[string]any{
		"kind":        "empty_detail_exhausted",
		"instruction": "Do not guess another catalog id. Inspect one of the known ids or enumerate tables by kind.",
		"missed_ids":  repair["missed_ids"],
		"known_ids":   next["known_ids"],
		"next":        next,
	}
	if len(missedIDs) == 1 {
		recovery["missed_id"] = missedIDs[0]
	}
	return executeResult{
		Errors: []ErrorInfo{{
			Message: "consecutive empty catalog detail lookup rejected; stop guessing ids and use a known catalog id or enumerate tables by kind",
			Extensions: map[string]any{
				"code":            "empty_detail_exhausted",
				"retryable":       true,
				"graphjin_repair": repair,
			},
		}},
		Recovery: recovery,
	}
}

func (s *discoveryState) emptySearchNext(reason string) map[string]any {
	knownIDs := s.knownCatalogIDs(emptySearchKnownIDLimit)
	return map[string]any{
		"recommended_tool": toolQueryCatalog,
		"args":             map[string]any{"kind": "table", "limit": 20},
		"reason":           reason,
		"known_ids":        knownIDs,
	}
}

func (s *discoveryState) emptyDetailNext(reason string) map[string]any {
	return s.emptySearchNext(reason)
}

func (s *discoveryState) hasKnownCatalogID(ids []string) bool {
	if s == nil {
		return false
	}
	for _, id := range ids {
		if s.catalogIDs[id] {
			return true
		}
		for knownID := range s.catalogIDs {
			if strings.EqualFold(strings.TrimSpace(id), strings.TrimSpace(knownID)) {
				return true
			}
		}
	}
	return false
}

func (s *discoveryState) knownCatalogIDs(limit int) []string {
	if s == nil || limit <= 0 {
		return nil
	}
	ids := make([]string, 0, limit)
	for _, id := range s.catalogDetails {
		if id != "" {
			ids = appendUniqueString(ids, id)
			if len(ids) == limit {
				return ids
			}
		}
	}
	for _, id := range sortedBoolKeys(s.catalogIDs) {
		ids = appendUniqueString(ids, id)
		if len(ids) == limit {
			break
		}
	}
	return ids
}

func (r *protocolRuntime) validateCoverageSearches(args map[string]any) ([]string, error) {
	if !r.catalogSearch.enabled() {
		return nil, fmt.Errorf("adaptive catalog coverage is unavailable; use query_catalog with one lexical search")
	}
	if r.state.coverageSearchUsed {
		return nil, fmt.Errorf("adaptive catalog coverage was already used in this agent run; inspect returned card ids or use one focused detail lookup instead")
	}
	for _, name := range []string{"search", "id", "ids", "order_by"} {
		if value, exists := args[name]; exists && value != nil {
			return nil, fmt.Errorf("query_catalog searches is mutually exclusive with %s; remove %s and retry the single coverage batch", name, name)
		}
	}
	raw := anySlice(normalizeValue(args["searches"]))
	if len(raw) < 2 || len(raw) > MaxCatalogCoverageSearches {
		return nil, fmt.Errorf("query_catalog searches requires two or three unique phrases")
	}
	searches := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, value := range raw {
		phrase, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("query_catalog searches phrases must be strings")
		}
		phrase = strings.Join(strings.Fields(phrase), " ")
		if phrase == "" {
			return nil, fmt.Errorf("query_catalog searches phrases cannot be empty")
		}
		if !utf8.ValidString(phrase) || len([]byte(phrase)) > MaxCatalogCoverageSearchBytes {
			return nil, fmt.Errorf("query_catalog searches phrases must be valid UTF-8 and at most %d bytes", MaxCatalogCoverageSearchBytes)
		}
		key := strings.ToLower(phrase)
		if seen[key] {
			return nil, fmt.Errorf("query_catalog searches phrases must be unique")
		}
		seen[key] = true
		searches = append(searches, phrase)
	}
	return searches, nil
}

func (r *protocolRuntime) ValidateWhereClause(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	action := r.state.startAction("model", "validate_where_clause", args)
	out, err := r.base.ValidateWhereClause(ctx, args)
	r.state.finishAction(action, "validate_where_clause", args, out, err)
	if err == nil {
		r.state.recordValidation(args, out)
	}
	return out, err
}

func (r *protocolRuntime) ExecuteSavedQuery(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	name := stringArg(args, "name")
	if name == "" {
		return nil, fmt.Errorf("query name is required")
	}
	if !r.state.savedQueryDetailed(name) {
		// Demanding the detail lookup only makes sense while the lookup could
		// still succeed. When the run already has evidence the card does not
		// exist, the demand is unsatisfiable and the guard becomes a trap: the
		// only discharge is a successful execution of the very query that does
		// not exist. Episode ah1-001 answered its question correctly, looked the
		// card up, got nothing, and finalized blocked on the demand to look it
		// up. Name the real saved queries instead, with no violation recorded —
		// nothing was violated.
		if r.state.savedQueryKnownAbsent(name) {
			known := sortedBoolKeys(r.state.savedQueriesDiscovered)
			message := fmt.Sprintf("saved query %q is not in this run's catalog", name)
			reason := "This saved query does not exist. Author the query directly with execute_graphql from catalog detail, or run one of the approved saved queries listed in details.known_saved_queries."
			if len(known) == 0 {
				reason = "This saved query does not exist and this run has surfaced no approved saved queries. Author the query directly with execute_graphql from catalog detail."
			}
			details := map[string]any{"name": name, "known_saved_queries": known, "fault": "unknown_saved_query"}
			out := recoverableProtocolFailure("unknown_saved_query", message, "unknown_saved_query",
				map[string]any{"recommended_tool": "execute_graphql", "reason": reason}, details)
			action := r.state.startAction("model", "execute_saved_query", args)
			r.state.finishAction(action, "execute_saved_query", args, out, nil)
			return out, nil
		}
		err := fmt.Errorf("protocol violation: inspect query_catalog(id: %q) before execute_saved_query", "saved_query:"+name)
		r.state.addViolation("saved_query_detail_required", err.Error(), "execute_saved_query", true, map[string]any{"name": name})
		action := r.state.startAction("model", "execute_saved_query", args)
		r.state.finishAction(action, "execute_saved_query", args, nil, err)
		return nil, err
	}
	// A saved metric carries its own fixed scope. When the follow-up inherits a
	// subject from prior turns and neither the saved query nor the supplied
	// variables bind it, running it answers a broader question than the one asked —
	// and unlike raw GraphQL the caller cannot narrow it by editing the query, so
	// the repair has to point at authoring instead.
	// The strike cap exists so a run is never stranded on a guard that guessed
	// wrong. But when the retained subject names a table this run knows, the
	// scoped raw query the refusal asks for actually exists — and the cap's
	// let-through was measured trading it for a wrong answer: same-invoice
	// episodes ran the unscoped saved query on the third try and reported the
	// whole table's total as invoice 10's amount. A bindable subject keeps the
	// refusal standing, the same way a clear-winner value repair does.
	if refs := unresolvedHistoryReferent(r.state.instruction, r.state.savedQueryGraphQLFor(name), args, r.state.history); len(refs) != 0 &&
		(r.state.referentBindableToKnownTable(refs) || r.state.refuseUnscopedReferent("saved:"+strings.ToLower(strings.TrimSpace(name)))) {
		r.state.recordReferentRejection("saved:" + strings.ToLower(strings.TrimSpace(name)))
		described := describeEntityReferences(refs)
		err := fmt.Errorf("protocol violation: this follow-up is scoped to %s, but saved query %q does not filter on it and a saved query cannot be narrowed at call time. Author the scoped query with execute_graphql, or pass a variable this saved query accepts that binds the subject", described, name)
		details := map[string]any{"retained_subject": refs, "name": name, "fault": "unscoped_saved_query_for_referent"}
		r.state.addViolation("history_referent_unresolved", err.Error(), "execute_saved_query", true, details)
		out := recoverableProtocolFailure("history_referent_unresolved", err.Error(), "history_referent_unresolved",
			map[string]any{
				"recommended_tool": "execute_graphql",
				"reason":           "Author a query scoped to " + described + " and execute it once, rather than reporting this saved metric's unscoped result.",
			}, details)
		action := r.state.startAction("model", "execute_saved_query", args)
		r.state.finishAction(action, "execute_saved_query", args, out, nil)
		return out, nil
	}
	executionKey := savedQueryExecutionKey(args)
	if cached, ok := r.state.cachedExecution(executionKey); ok {
		if out, rejected := pendingCachedExecutionRejection(r.state, "", true); rejected {
			action := r.state.startAction("model", "execute_saved_query", args)
			r.state.finishAction(action, "execute_saved_query", args, out, nil)
			return out, nil
		}
		out := cachedExecutionResult(r.state, cached)
		r.state.selectCachedExecution("execute_saved_query", args, out)
		r.state.recordRepeatedCall(executionKey)
		action := r.state.startAction("model", "execute_saved_query", args)
		r.state.finishAction(action, "execute_saved_query", args, out, nil)
		return out, nil
	}
	r.state.clearCompletionLatch()
	action := r.state.startAction("model", "execute_saved_query", args)
	out, err := r.base.ExecuteSavedQuery(ctx, args)
	r.state.finishAction(action, "execute_saved_query", args, out, err)
	if err == nil {
		r.state.recordExecution("execute_saved_query", args, out)
		r.state.cacheSuccessfulExecution(executionKey, out)
		if !executionFailed(out) {
			// An approved saved mutation is a write like any other, so its
			// success settles the write-accounting the finalize gate reads.
			if ContainsMutationOperation(r.state.savedQueryGraphQLFor(name)) {
				r.state.mutationAttempted = true
				r.state.mutationSucceeded = true
				r.state.lastMutationFailure = ""
				r.state.lastMutationRepairedQuery = ""
			}
			r.state.emptySearchStreak = 0
			r.state.emptyDetailStreak = 0
			// The retained-subject guard fires on this path too, so it has to be
			// dischargeable here. Discharge lived only in the raw-GraphQL path, which
			// meant a follow-up correctly rescoped through a saved query stayed blocked
			// on a requirement it had already met.
			r.state.resolveSuccessfulSavedQueryViolations()
		}
		// A rejected out-of-order attempt is recoverable inside the same actor
		// run. Once the model has inspected the exact detail and the governed
		// execution succeeds, retain the attempt in the action/evidence trail but
		// do not let its now-satisfied protocol violation poison the final answer.
		r.state.resolveSavedQueryDetailViolation(name)
	}
	return out, err
}

func (r *protocolRuntime) ExecuteGraphQL(ctx context.Context, args map[string]any) (any, error) {
	r.addNamespace(args)
	query := stringArg(args, "query")
	normalizedWatchQuery := ""
	rewrittenJoinQuery, rewrittenJoinRoot, rewrittenJoinParent := "", "", ""
	boundReferentQuery, boundReferent := "", referentBinding{}
	if r.state.hasPolicyFinalBlockingViolation() {
		out := r.state.policyFinalExecutionResult()
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if missing := r.state.missingCapabilityActions(query); len(missing) != 0 {
		message := "caller capability profile does not grant the requested GraphQL action: " + strings.Join(missing, ", ")
		var allowed []string
		if r.state.capabilities != nil {
			allowed = append([]string(nil), r.state.capabilities.AllowedActions...)
		}
		details := map[string]any{
			"missing_actions": missing,
			"allowed_actions": allowed,
		}
		r.state.addViolation("capability_disabled", message, "execute_graphql", true, details)
		out := policyFinalFailure("capability_disabled", message, details)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if !r.state.hasCatalogEvidence() {
		err := fmt.Errorf("protocol violation: inspect catalog evidence before execute_graphql")
		r.state.noteInterceptedWrite(query, "the write was refused pending catalog evidence and was not successfully retried")
		r.state.addViolation("raw_graphql_catalog_required", err.Error(), "execute_graphql", true, nil)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, nil, err)
		return nil, err
	}
	// The seed and broad catalog lists are ranked hint sets, not schema proof. A
	// model that authors raw GraphQL from either can still guess the target or
	// fields. Require an exact same-run detail lookup, which is also the evidence
	// surfaced as catalog_detail_ids in the protocol response. Fail here while
	// the run can recover and name the governed path.
	if !r.state.hasCatalogDetailEvidence() {
		// A query whose every root is a gj_* system surface targets a contract
		// that is fixed and known, so the demanded detail is deterministic: the
		// run needs the system root's own help card, and which card that is
		// does not depend on anything the model knows. Supply it once, the way
		// security and mutation evidence are supplied — a reactive episode gave
		// up after two of these refusals and reported "no unseen events found"
		// over an inbox it never read.
		if supplied, ok := r.supplySystemRootDiscovery(ctx, query, args); ok {
			// A mutation stopped here must throw, not return. The model's
			// executor code is straight-line JavaScript written before this
			// result exists: `const res = await execute_graphql(m); await
			// final("done")` runs final unconditionally, so a friendly return
			// value cannot interrupt it — run ab4-trt measured 26 episodes
			// narrating success over exactly this consumed call. An exception
			// is the only control flow a completed program still responds to.
			if ContainsMutationOperation(query) {
				r.state.noteInterceptedWrite(query, "the write was intercepted so the system-root contract could be supplied, and was not retried")
				return nil, fmt.Errorf("this write did NOT execute. GraphJin loaded the system-root contract it requires into this run's evidence. Re-execute the exact same mutation now; it will be permitted to run")
			}
			return supplied, nil
		}
		r.state.noteInterceptedWrite(query, "the write was refused pending catalog discovery and was not successfully retried")
		if ContainsMutationOperation(query) {
			err := fmt.Errorf("this write did NOT execute: inspect the target's catalog detail with query_catalog({id:\"...\"}) first, then re-execute the mutation.%s", approvedSavedQuerySuffix(r.state))
			r.state.addViolation("raw_graphql_discovery_required", err.Error(), "execute_graphql", true, map[string]any{"candidate_ids": r.state.knownCatalogIDs(emptySearchKnownIDLimit)})
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		err := fmt.Errorf("protocol violation: the seed and broad catalog results are not discovery detail; inspect the relevant catalog item with query_catalog({id:\"...\"}) before authoring raw GraphQL.%s", approvedSavedQuerySuffix(r.state))
		details := map[string]any{
			"approved_saved_queries": sortedBoolKeys(r.state.savedQueriesDiscovered),
		}
		// Every sibling guard below hands back a structured repair step. This one
		// returned a bare error, and the actor paid for the difference: on the frozen
		// suite it fired 51 times across 25 episodes, about twice per episode, because
		// nothing named which id to open. Those episodes averaged 5.2 actor turns
		// against 2.8 elsewhere and passed 48% against 64%, with the extra turns coming
		// out of an eight-step budget. The requirement is unchanged — detail evidence
		// is still mandatory before raw GraphQL — only the reply is now actionable.
		// A short list: this is a nudge toward the right card, not a catalog dump.
		known := r.state.knownCatalogIDs(emptySearchKnownIDLimit)
		repairArgs := map[string]any{}
		if len(known) != 0 {
			details["candidate_ids"] = known
			repairArgs["ids"] = append([]string(nil), known...)
		}
		r.state.addViolation("raw_graphql_discovery_required", err.Error(), "execute_graphql", true, details)
		next := catalogRepairNext(
			repairArgs,
			"Inspect the exact catalog detail for the item this query targets in one discovery-only actor step, then re-author the raw GraphQL from the returned card.",
		)
		out := recoverableProtocolFailure("raw_graphql_discovery_required", err.Error(), "raw_graphql_discovery_required", next, details)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if missing := r.state.missingDistilledSourceDetails(); len(missing) != 0 {
		// The ids are already known, so the round trip buys nothing and costs a step:
		// this guard blocked 8 of 24 cross-source episodes on the frozen suite while 9
		// of them spent all eight steps. Supplying the cards is the same treatment
		// mutation-shape evidence already gets, and the requirement is still met by
		// content the catalog actually returned rather than by the asking.
		if out, ok := r.supplyCrossSourceEvidence(ctx, missing, args); ok {
			return out, nil
		}
		err := fmt.Errorf("protocol violation: inspect every source selected by the distiller before authoring cross-source GraphQL: %s", strings.Join(missing, ", "))
		details := map[string]any{"sources": missing}
		r.state.addViolation("cross_source_detail_required", err.Error(), "execute_graphql", true, details)
		next := catalogRepairNext(
			map[string]any{"ids": append([]string(nil), missing...)},
			"Inspect the exact source details in one discovery-only actor step, then re-author the cross-source operation from the returned source cards.",
		)
		out := recoverableProtocolFailure("cross_source_detail_required", err.Error(), "cross_source_detail_required", next, details)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if writeLikeGraphQL(query) && !r.state.securityRuntimeEvidence {
		// The two prerequisite ids never vary, so the refusal round trip bought
		// nothing: 35 episodes on the frozen suite paid it — about 1.8 extra
		// turns and 43k extra tokens each — and every one then fetched the same
		// two cards. Supply them instead, the treatment cross-source evidence
		// already gets. The refusal below remains the fetch-failure fallback,
		// and the requirement is still discharged only by content the catalog
		// actually returned. This also converts the read-side false positives
		// (writeLikeGraphQL matches read-only control-plane queries by
		// substring) from a refusal the run was never taught about into the
		// guidance itself.
		if supplied, ok := r.supplySecurityRuntimeEvidence(ctx, args); ok {
			// Same rule as every mutation interception: throw, because a
			// return value cannot interrupt straight-line executor code.
			if ContainsMutationOperation(query) {
				r.state.noteInterceptedWrite(query, "the write was intercepted so security and runtime guidance could be supplied, and was not retried")
				return nil, fmt.Errorf("this write did NOT execute. GraphJin loaded the required security and runtime guidance into this run's evidence. Re-execute the exact same mutation now; it will be permitted to run")
			}
			return supplied, nil
		}
		r.state.noteInterceptedWrite(query, "the write was refused pending security and runtime guidance and was not successfully retried")
		details := map[string]any{"required": []any{"help:security", "help:runtime"}}
		if ContainsMutationOperation(query) {
			// Thrown, not returned: no exception reads as success to
			// straight-line executor code.
			err := fmt.Errorf("this write did NOT execute: inspect the security and runtime guidance with query_catalog({ids:[\"help:security\",\"help:runtime\"]}) first, then re-execute the mutation")
			r.state.addViolation("security_runtime_discovery_required", err.Error(), "execute_graphql", true, details)
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		err := fmt.Errorf("protocol violation: inspect security/runtime catalog guidance before write-capable or control-plane GraphQL")
		r.state.addViolation("security_runtime_discovery_required", err.Error(), "execute_graphql", true, details)
		next := catalogRepairNext(
			map[string]any{"ids": []any{"help:security", "help:runtime"}},
			"Inspect the exact security and runtime guidance in a discovery-only actor step, then re-author the write from returned evidence.",
		)
		out := recoverableProtocolFailure("security_runtime_discovery_required", err.Error(), "write_prerequisite_detail_required", next, details)
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if !ContainsMutationOperation(query) {
		// A join-only remote table cannot be queried at the top level: the table has
		// no rows of its own, only a route through its parent. Run 969337b6 counted
		// 32 top-level account_health attempts, every one an error, several episodes
		// burning their whole step budget on a route GraphJin knew was closed while
		// knowing the open one. The intercepted call could never succeed, so this
		// repair replaces a doomed execution rather than costing one.
		if root, parent, ok := r.state.topLevelRemoteJoinRoot(query); ok {
			// Describing the shape was not enough: models rewrote the same
			// closed route sixteen ways in one episode. Hand over the query
			// instead, the way the value guard hands over a corrected write —
			// with the model's own filter grafted in when every column it names
			// belongs to the parent.
			repaired, kind := r.remoteJoinRepairedQuery(ctx, query, root, parent)
			details := map[string]any{"root": root, "parent": parent, "fault": "remote_join_root_top_level", "repaired_query": repaired}
			err := fmt.Errorf("protocol violation: %s is an API join served only nested under %s, so a top-level %s query cannot return data. Re-author it as query { %s(where: {<filter on %s>}) { %s { <fields> } } } and execute that", root, parent, root, parent, parent, root)
			if kind == remoteJoinFilterGrafted {
				err = fmt.Errorf("protocol violation: %s is an API join served only nested under %s, so a top-level %s query cannot return data. It carried a filter legal on %s, so it was re-authored onto the nested route and executed as: %s", root, parent, root, parent, repaired)
			}
			if !r.state.remoteJoinNoticed[strings.ToLower(root)] {
				if r.state.remoteJoinNoticed == nil {
					r.state.remoteJoinNoticed = map[string]bool{}
				}
				r.state.remoteJoinNoticed[strings.ToLower(root)] = true
				r.state.addViolation("remote_join_path_required", err.Error(), "execute_graphql", true, details)
			}
			if kind == remoteJoinFilterGrafted {
				// Every column of the filter is the model's own and legal on the
				// parent, so the nested query is the unique reading of the
				// caller's own question — the same certainty on which the watch
				// repair executes rather than offers. Offering was measured and
				// it failed: across the round-2 A/B the repair was handed back 7
				// times and executed 0 times, while both cross-source passes came
				// from nested queries models wrote for themselves. A model that
				// has already failed to find this route is not going to adopt it
				// from a hand-back. Every downstream guard still runs against the
				// rewritten query, its success discharges the violation recorded
				// above, and the result carries a notice naming the rewrite.
				query = repaired
				args = cloneAnyMap(args)
				args["query"] = repaired
				rewrittenJoinQuery, rewrittenJoinRoot, rewrittenJoinParent = repaired, root, parent
			} else {
				reason := "Execute next.args.query exactly as given; it queries " + parent + " and returns " + root + " nested inside it."
				switch {
				case kind == remoteJoinFilterSalvaged:
					reason += " Its filter carries the value you filtered on, moved to the " + parent + " column that can hold it; change that value only if it is not the row you meant."
				default:
					reason = "Replace the <filter on " + parent + "> placeholder in next.args.query with a filter built from the " + parent + " columns it names, then execute the result; it queries " + parent + " and returns " + root + " nested inside it."
				}
				if r.state.remoteJoinRepairSeen(root) {
					// A repeat on the same root has already been taught the route.
					reason = "Execute next.args.query exactly as given."
					if kind == remoteJoinFilterHole {
						reason = "Fill the <filter on " + parent + "> placeholder in next.args.query, then execute it."
					}
				}
				out := recoverableProtocolFailure("remote_join_path_required", err.Error(), "remote_join_repair",
					map[string]any{
						"recommended_tool": "execute_graphql",
						"args":             map[string]any{"query": repaired},
						"reason":           reason,
					}, details)
				action := r.state.startAction("model", "execute_graphql", args)
				r.state.finishAction(action, "execute_graphql", args, out, nil)
				return out, nil
			}
		}
		// A follow-up that points at "it" or "that account" has to inherit its
		// subject from prior turns. When the retained subject is known and the
		// authored query binds none of it, the answer will describe the whole table
		// instead of the one row asked about. Name the subject once; which filter
		// path reaches it is the model's decision, not this guard's.
		if refs := unresolvedHistoryReferent(r.state.instruction, query, args, r.state.history); len(refs) != 0 {
			if rewritten, binding, ok := r.referentScopedRewrite(ctx, query, refs); ok {
				// The binding is mechanical — the subject and id are the user's
				// own words, the column is the catalog's — so the scoped query
				// executes instead of being described. The refusal was measured
				// on this exact shape and lost: its "execute the unscoped query
				// again and it will run" escape hatch is the first thing a weak
				// model reaches for, and the let-through's whole-table number
				// was then reported as the subject's in four of six stable
				// multi-turn losses.
				identity := normalizeGraphQLIdentity(query)
				if !r.state.referentRewriteNoticed[identity] {
					if r.state.referentRewriteNoticed == nil {
						r.state.referentRewriteNoticed = map[string]bool{}
					}
					r.state.referentRewriteNoticed[identity] = true
					err := fmt.Errorf("protocol violation: this follow-up inherits its subject from prior turns, which retained %s, but the query filtered on none of it. It executed re-scoped to %s via %s.%s as: %s", describeEntityReferences(refs), binding.Ref.String(), binding.Root, binding.Column, rewritten)
					r.state.addViolation("history_referent_unresolved", err.Error(), "execute_graphql", true,
						map[string]any{"retained_subject": refs, "fault": "unresolved_history_referent", "repaired_query": rewritten})
				}
				query = rewritten
				args = cloneAnyMap(args)
				args["query"] = rewritten
				boundReferentQuery, boundReferent = rewritten, binding
			} else if r.state.refuseUnscopedReferent(normalizeGraphQLIdentity(query)) {
				r.state.recordReferentRejection(normalizeGraphQLIdentity(query))
				described := describeEntityReferences(refs)
				err := fmt.Errorf("protocol violation: this follow-up inherits its subject from prior turns, which retained %s, but the query filters on none of it. Re-author the query scoped to the retained subject before answering", described)
				details := map[string]any{"retained_subject": refs, "fault": "unresolved_history_referent"}
				r.state.addViolation("history_referent_unresolved", err.Error(), "execute_graphql", true, details)
				out := recoverableProtocolFailure("history_referent_unresolved", err.Error(), "history_referent_unresolved",
					map[string]any{
						"recommended_tool": "execute_graphql",
						"reason":           "Add the where clause that scopes this query to " + described + ", then execute it once. If the retained subject genuinely does not apply, execute the unscoped query again and it will run.",
					}, details)
				action := r.state.startAction("model", "execute_graphql", args)
				r.state.finishAction(action, "execute_graphql", args, out, nil)
				return out, nil
			}
		}
	}
	if ContainsMutationOperation(query) {
		annotationFields := annotationMutationInputFields(query, args)
		annotationDefinition := isAnnotationDefinitionMutation(query, annotationFields)
		if annotationFields["tier"] && (r.state.annotationMutated || annotationDefinition) {
			err := fmt.Errorf("protocol violation: an annotation cannot be inserted or edited and have its tier flipped in the same agent run; present the draft as data and wait for user confirmation")
			r.state.addViolation("annotation_tier_confirmation_required", err.Error(), "execute_graphql", true, map[string]any{"root": systemRootArtifacts})
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		if isWatchActionReviewMutation(query) && r.state.watchDefinitionMutated {
			err := fmt.Errorf("protocol violation: an autonomous watch action cannot be created or changed and approved in the same agent run; explain the proposed action and wait for user confirmation")
			r.state.addViolation("watch_action_confirmation_required", err.Error(), "execute_graphql", true, map[string]any{"root": systemRootWatch})
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		{
			if mismatches := r.unobservedWrittenValues(ctx, query); len(mismatches) != 0 {
				described := r.describeUnobservedValues(ctx, mismatches)
				// A list did not move behaviour — ten of eleven episodes handed one
				// resubmitted "closed" verbatim. Every repair that measurably changed
				// what a model did named the exact answer, so when a clear nearest value
				// exists the corrected write is supplied outright. Offered, never
				// executed: mapping intent onto a vocabulary is the model's call.
				suggestions := r.suggestedWrittenValues(ctx, mismatches)
				repaired, repairExists := "", false
				if len(suggestions) == len(mismatches) {
					repaired, repairExists = substituteWrittenValues(query, mismatches, suggestions)
				}
				refuse := r.state.refuseUnobservedWrite(mismatches)
				if !refuse && repairExists {
					// dcea36f8 let the third identical attempt land so a caller
					// could write a legitimately new value into an enum-like
					// column. Measured across the Muse-Glimmer run, the only
					// users of that override were 8 episodes writing "closed"
					// into open|pending|resolved after ignoring the exact repair
					// twice — invalid states landing in the database. When a
					// clear-winner repair exists the refusal now stands past the
					// strike cap; the let-through survives only for the
					// list-only case, where the caller may genuinely know a
					// value the sampler has not seen. Strikes still increment so
					// the counters keep telling the story.
					r.state.recordUnobservedWriteStrikes(mismatches)
					refuse = true
				}
				if refuse {
					details := map[string]any{"fault": "value_outside_observed_set"}
					if len(suggestions) != 0 {
						details["closest_values"] = suggestions
					}
					// Thrown, with the corrected mutation inline: executor code is
					// straight-line JavaScript written before this result exists,
					// so only an exception interrupts it, and the exception text
					// is the one channel the next turn is guaranteed to read. Run
					// 969337b6 measured the conditional framing losing to the
					// factual one, so the schema's convention is stated as fact.
					err := fmt.Errorf("this write did NOT execute: %s. Choose the value from that list which matches the intent, re-author the write with it, and execute it once", described)
					if repairExists {
						details["repaired_query"] = repaired
						message := fmt.Sprintf("this write did NOT execute: %s. Execute this corrected mutation exactly as given: %s", described, repaired)
						// The corrected value can imply a companion timestamp —
						// resolved beside resolved_at — and that schema fact
						// rides along: every recorded ticket-resolution episode
						// left it null, and nothing else in the run says it
						// exists to be stamped.
						if note := r.companionTimestampNote(ctx, repaired); note != "" {
							message += " — " + note
						}
						err = fmt.Errorf("%s", message)
					}
					r.state.noteInterceptedWrite(query, "the write was refused for a value outside the column's observed set and was not successfully retried")
					r.state.addViolation("observed_value_mismatch", err.Error(), "execute_graphql", true, details)
					action := r.state.startAction("model", "execute_graphql", args)
					r.state.finishAction(action, "execute_graphql", args, nil, err)
					return nil, err
				}
			}
		}
		roots := MutationRootFields(query)
		if containsStringFold(roots, systemRootWatch) {
			// A watch's subscription is a string inside the mutation, so any filter
			// forces nested quotes. In benchmark generation 2028.1, 53 of 72 watch
			// mutations left them unescaped, which breaks the mutation's own parse and
			// surfaced downstream as a confusing mutation_evidence_required rather
			// than a quoting fault. Name it here, and point at the variable form that
			// removes the nesting entirely.
			if malformedWatchSubscriptionString(query) {
				repaired, repairable := repairWatchSubscriptionString(query)
				if repairable {
					// The repair is parse-verified and re-escaping does not change
					// what was written — this is the unique reading of the caller's
					// own mutation, so it executes rather than being offered back.
					// Offering was measured across a benchmark run: 3 of 13 blocked
					// episodes executed the repair; 5 narrated it to the user as
					// their final answer. Every downstream guard still runs against
					// the normalized mutation, and the result carries a recovery
					// notice naming the normalization.
					query = repaired
					args = cloneAnyMap(args)
					args["query"] = repaired
					normalizedWatchQuery = repaired
				} else {
					details := map[string]any{"root": systemRootWatch, "fault": "unescaped_subscription_string"}
					err := fmt.Errorf(`this write did NOT execute: the gj_watch subscription string has unescaped quotes, so this mutation does not parse. Pass the subscription as a GraphQL variable instead of inlining it: mutation CreateWatch($name: String!, $query: String!) { gj_watch(insert: {name: $name, query: $query, delivery_json: {kind: "inbox", digest: {window: "1h"}}}) { id name status } } with variables {"name": "...", "query": "subscription { <root>(where: {...}, first: 25, after: $cursor) { ... } <root>_cursor }"}`)
					r.state.noteInterceptedWrite(query, "the gj_watch mutation was refused for an unparseable subscription string and was not successfully retried")
					r.state.addViolation("watch_query_invalid", err.Error(), "execute_graphql", true, details)
					action := r.state.startAction("model", "execute_graphql", args)
					r.state.finishAction(action, "execute_graphql", args, nil, err)
					return nil, err
				}
			}
			for _, root := range watchSubscriptionRoots(query, args) {
				roots = appendUniqueString(roots, root)
			}
		}
		if missing := r.state.missingMutationEvidence(roots); len(missing) != 0 {
			// Both the requirement and its safe discharge are known in Go: the exact
			// catalog ids are resolvable, so supply the detail once instead of asking
			// the model to fetch it. Same reasoning as history_read_required — naming
			// the lookup and hoping a weak model performs it turns a deterministic
			// prerequisite into a retry loop, which is what held DeepORG watch
			// creation at 0/4 while the recovery correctly named the table id.
			//
			// The guard's purpose survives: a mutation must be authored from real
			// column evidence, and that evidence is now in hand. It is supplied at
			// most once per run so a genuinely unknown target still fails loudly.
			// Scoped to gj_watch for the FIRST refusal on purpose: ordinary
			// mutation targets are served well by one pass through the repair
			// ladder — 14 of 21 episodes that hit this guard recovered after a
			// single refusal. But a model that re-sends the same blocked write is
			// not going to start following the repair on attempt three: the
			// episodes where this guard fired more than once passed 2/7, and the
			// two that hit it eight times were total losses. So the refusal
			// stands as the teaching moment exactly once per target set; a repeat
			// violation for tables already refused gets the evidence supplied,
			// the same escape watch creation has always had. Unresolvable targets
			// still fail loudly inside supplyMutationEvidence.
			if containsStringFold(roots, systemRootWatch) || r.state.previouslyRefusedMutationEvidence(missing) {
				r.state.noteInterceptedWrite(query, "the write was intercepted pending mutation-shape evidence and was not successfully retried")
				if _, ok := r.supplyMutationEvidence(ctx, missing); ok {
					// Throw, not return: the executor's straight-line code
					// cannot react to a value. The evidence is in this run's
					// state either way, so the retry is one exception away.
					err := fmt.Errorf("this write did NOT execute. GraphJin loaded the catalog detail for %s into this run's evidence (columns: %s). Re-execute the mutation now, corrected if any of its fields are not in that list", strings.Join(missing, ", "), r.suppliedColumnSummary(ctx, missing))
					action := r.state.startAction("model", "execute_graphql", args)
					r.state.finishAction(action, "execute_graphql", args, nil, err)
					return nil, err
				}
			}
			r.state.noteInterceptedWrite(query, "the write was refused pending mutation-shape evidence and was not successfully retried")
			// The exact catalog ids beat a generic search in every measured
			// run, so the exception carries them verbatim.
			exact := ""
			if ids := stringSliceArg(mapValue(r.state.mutationEvidenceNext(missing)["args"]), "ids"); len(ids) != 0 {
				exact = fmt.Sprintf(" — run query_catalog({ids:[\"%s\"]}) for the exact detail", strings.Join(ids, `","`))
			}
			err := fmt.Errorf("this write did NOT execute: gather mutation-shape evidence for %s first%s, then re-execute the mutation", strings.Join(missing, ", "), exact)
			details := map[string]any{"tables": missing}
			r.state.addViolation("mutation_evidence_required", err.Error(), "execute_graphql", true, details)
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, nil, err)
			return nil, err
		}
		if requiresWorkflowDetail(roots) && !r.state.hasWorkflowDetailEvidence() {
			err := fmt.Errorf("protocol violation: inspect the workflow detail by id before executing it through %s", systemRootWorkflowExec)
			details := map[string]any{"root": systemRootWorkflowExec}
			r.state.addViolation("workflow_detail_required", err.Error(), "execute_graphql", true, details)
			next := r.state.workflowEvidenceNext()
			out := recoverableProtocolFailure("workflow_detail_required", err.Error(), "workflow_detail_required", next, details)
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, out, nil)
			return out, nil
		}
	}
	queryKey := executionQueryKey(args)
	if r.state.failedQueryKeys[queryKey] {
		out := attachExecutionRecovery(executeResult{Errors: []ErrorInfo{{
			Message: "identical failed GraphQL query retry rejected; re-author the query from live GraphJin catalog/help evidence before executing again",
			Extensions: map[string]any{
				"code":      "duplicate_failed_query",
				"retryable": true,
				"graphjin_repair": map[string]any{
					"kind": "distinct_query_required",
					"next": catalogNext(toolQueryCatalog, "Inspect the real table/column detail or graphql_help({for:\"query\"}), then execute a distinct repaired query."),
				},
			},
		}}}, r.state, "")
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	if cached, ok := r.state.cachedExecution(queryKey); ok {
		if out, rejected := pendingCachedExecutionRejection(r.state, query, false); rejected {
			action := r.state.startAction("model", "execute_graphql", args)
			r.state.finishAction(action, "execute_graphql", args, out, nil)
			return out, nil
		}
		out := cachedExecutionResult(r.state, cached)
		r.state.selectCachedExecution("execute_graphql", args, out)
		r.state.recordRepeatedCall(queryKey)
		// Only successful executions are cached, and the discharge condition on
		// a recoverable violation is that a successful execution proved the run
		// can do it right — which a replay of that same successful execution
		// proves no less. Without this, a model that violated a guard AFTER its
		// correct query ran, then correctly re-selected that query, stayed
		// blocked with no path out: the retry the guard demanded could only
		// ever arrive from the cache.
		if !executionFailed(out) {
			r.state.resolveSuccessfulExecutionViolations()
			if ContainsMutationOperation(query) {
				r.state.mutationAttempted = true
				r.state.mutationSucceeded = true
				r.state.lastMutationFailure = ""
				r.state.lastMutationRepairedQuery = ""
			}
		}
		action := r.state.startAction("model", "execute_graphql", args)
		r.state.finishAction(action, "execute_graphql", args, out, nil)
		return out, nil
	}
	r.state.clearCompletionLatch()
	wasRepairPending := r.state.pendingFailedQueryKey != ""
	action := r.state.startAction("model", "execute_graphql", args)
	out, err := r.base.ExecuteGraphQL(ctx, args)
	if err == nil && executionFailed(out) {
		if denied, ok := policyFinalExecutionError(out); ok {
			code := stringFromMap(denied.Extensions, "code")
			details := mapValue(denied.Extensions["details"])
			r.state.addExecutionPolicyViolation(code, denied.Message, details)
			r.state.pendingFailedQueryKey = ""
			r.state.clearCompletionLatch()
			out = markPolicyFinalExecution(out, code, details)
		} else {
			if repair, ok := systemRootDidYouMeanRepair(out, r.state, query); ok {
				r.state.pendingSystemRootQuery = repair.example
			}
			if isWatchDefinitionMutation(query) && watchDefinitionExecutionFailed(out) {
				ids := r.state.watchRepairCatalogIDs(query, args)
				r.state.pendingFailedCatalogIDs = append([]string(nil), ids...)
				out = attachWatchQueryRepair(out, ids, watchSubscriptionRoots(query, args))
			} else {
				out = attachExecutionRecovery(out, r.state, query)
				var repairedMutation string
				out, repairedMutation = r.attachUnknownColumnRecovery(ctx, out, query)
				if repairedMutation != "" {
					r.state.lastMutationRepairedQuery = repairedMutation
				}
			}
			// A write that reached the engine and failed left the database
			// unchanged. The record matters at finalization: a run whose only
			// writes failed cannot report the work as done.
			if ContainsMutationOperation(query) {
				r.state.mutationAttempted = true
				if messages := executionErrorMessages(out); len(messages) != 0 {
					r.state.lastMutationFailure = messages[0]
				}
			}
			r.state.failedQueryKeys[queryKey] = true
			if wasRepairPending {
				r.state.pendingFailedQueryKey = ""
			} else {
				r.state.pendingFailedQueryKey = queryKey
			}
		}
	} else if err == nil && wasRepairPending {
		r.state.pendingFailedQueryKey = ""
	}
	if ContainsMutationOperation(query) && err == nil && !executionFailed(out) {
		r.state.mutationAttempted = true
		r.state.mutationSucceeded = true
		r.state.lastMutationFailure = ""
		r.state.lastMutationRepairedQuery = ""
	}
	if normalizedWatchQuery != "" && err == nil && !executionFailed(out) {
		out = attachWatchNormalizationNotice(out, normalizedWatchQuery)
	}
	if rewrittenJoinQuery != "" && err == nil && !executionFailed(out) {
		out = attachRemoteJoinRewriteNotice(out, rewrittenJoinQuery, rewrittenJoinRoot, rewrittenJoinParent)
	}
	if boundReferentQuery != "" && err == nil && !executionFailed(out) {
		out = attachReferentBindingNotice(out, boundReferentQuery, boundReferent)
	}
	r.state.finishAction(action, "execute_graphql", args, out, err)
	if err == nil {
		r.state.recordExecution("execute_graphql", args, out)
		r.state.cacheSuccessfulExecution(queryKey, out)
		if !executionFailed(out) {
			r.state.emptySearchStreak = 0
			r.state.emptyDetailStreak = 0
			r.state.resolveSuccessfulExecutionViolations()
		}
		if isWatchDefinitionMutation(query) {
			r.state.watchDefinitionMutated = true
		}
		if isAnnotationDefinitionMutation(query, annotationMutationInputFields(query, args)) {
			r.state.annotationMutated = true
		}
	}
	r.state.rawGraphQL = append(r.state.rawGraphQL, map[string]any{
		"operation":  graphQLOperationKind(query),
		"write_like": writeLikeGraphQL(query),
	})
	return out, err
}

func (s *discoveryState) addExecutionPolicyViolation(code, message string, details map[string]any) {
	switch code {
	case "artifact_kind_locked":
		s.addViolation("artifact_kind_locked", message, toolExecuteGraphQL, true, details)
	case "access_unauthorized":
		s.addViolation("access_unauthorized", message, toolExecuteGraphQL, true, details)
	case "access_blocked":
		s.addViolation("access_blocked", message, toolExecuteGraphQL, true, details)
	case "authenticated_required":
		s.addViolation("authenticated_required", message, toolExecuteGraphQL, true, details)
	case "identity_variable_missing":
		s.addViolation("identity_variable_missing", message, toolExecuteGraphQL, true, details)
	default:
		s.addViolation("capability_disabled", message, toolExecuteGraphQL, true, details)
	}
}

func recoverableProtocolFailure(code, message, kind string, next, details map[string]any) executeResult {
	repair := map[string]any{
		"code": code,
		"kind": kind,
		"next": next,
	}
	if len(details) != 0 {
		repair["details"] = details
	}
	// Every caller passes a kind-specific next.reason; surface THAT as the
	// directive and the instruction. The generic sentences used here before
	// contradicted the specific fix for every kind whose recommended tool is
	// execute_graphql — measured across a benchmark run, models followed the
	// louder "discovery-only actor step" text into catalog reads and resubmitted
	// the same wrong write while the computed repaired_query sat unexecuted.
	// recoveryDirectivePrefix must lead the directive: it is the dedupe marker
	// joinRecoveryMessage keys on.
	reason := stringFromMap(next, "reason")
	directive := recoveryDirectivePrefix + " follow recovery.next in a discovery-only actor step, then re-author and retry the operation in this run."
	instruction := "Gather the named evidence in a separate actor step so its returned schema and policy details are visible before GraphQL is authored."
	if reason != "" {
		directive = recoveryDirectivePrefix + " " + reason
		instruction = reason
	}
	return executeResult{
		Errors: []ErrorInfo{{
			Message: joinRecoveryMessage(message, directive),
			Extensions: map[string]any{
				"code":            code,
				"retryable":       true,
				"graphjin_repair": repair,
			},
		}},
		Recovery: map[string]any{
			"code":        code,
			"kind":        kind,
			"instruction": instruction,
			"next":        next,
		},
	}
}

func catalogRepairNext(args map[string]any, reason string) map[string]any {
	next := catalogNext(toolQueryCatalog, reason)
	next["args"] = args
	return next
}

func (r *protocolRuntime) addNamespace(args map[string]any) {
	if r.namespace == "" || args == nil {
		return
	}
	if _, ok := args["namespace"]; !ok {
		args["namespace"] = r.namespace
	}
}

func (s *discoveryState) startAction(source, tool string, args map[string]any) int {
	action := protocolAction{
		Step:      len(s.actions) + 1,
		Source:    source,
		Tool:      tool,
		Args:      redactArgs(args),
		Status:    "started",
		startedAt: time.Now(),
	}
	s.actions = append(s.actions, action)
	return len(s.actions) - 1
}

func (s *discoveryState) finishAction(index int, tool string, args map[string]any, out any, err error) {
	if index < 0 || index >= len(s.actions) {
		return
	}
	s.actions[index].Status = "ok"
	if err != nil {
		s.actions[index].Status = "error"
		s.actions[index].Error = err.Error()
		s.addGrounding(err.Error())
	}
	s.addGrounding(args, out)
	s.actions[index].Summary = resultSummary(tool, args, out)
	s.actions[index].Evidence = mergeActionEvidence(catalogCardEvidence(tool, args, out), recoveryEvidence(out))
	s.emitAction(s.actions[index])
}

// emitAction forwards a completed action to the request observer (progress
// streaming). Observer failures never affect the run.
func (s *discoveryState) emitAction(action protocolAction) {
	if s.observe == nil {
		return
	}
	event := ActionEvent{
		Index:     action.Step,
		Source:    action.Source,
		Tool:      action.Tool,
		Args:      action.Args,
		Status:    action.Status,
		Summary:   action.Summary,
		Error:     action.Error,
		ElapsedMS: time.Since(action.startedAt).Milliseconds(),
	}
	func() {
		defer func() { _ = recover() }()
		s.observe(event)
	}()
}

func (s *discoveryState) recordCatalog(args map[string]any, out any, seed bool) {
	search := stringArg(args, "search")
	if search != "" {
		s.catalogSearches = append(s.catalogSearches, map[string]any{
			"search": search,
			"kind":   stringArg(args, "kind"),
			"where":  normalizeValue(args["where"]),
			"seed":   seed,
		})
	}
	if searches := stringSliceArg(args, "searches"); len(searches) != 0 {
		s.catalogSearches = append(s.catalogSearches, map[string]any{
			"searches": searches,
			"kind":     stringArg(args, "kind"),
			"where":    normalizeValue(args["where"]),
			"coverage": true,
			"seed":     seed,
		})
	}
	ids := returnedCatalogDetailIDs(detailIDsFromArgs(args), out)
	for _, id := range ids {
		s.catalogDetails = appendUniqueString(s.catalogDetails, id)
		s.catalogIDs[id] = true
		if isSecurityRuntimeID(id) {
			s.securityRuntimeEvidence = true
		}
	}
	// Seed search is deliberately broad. Its result set can contain one
	// unrelated saved query among many table, column, help, and workflow cards;
	// that is context, not an unambiguous governed route selected by the model.
	// Only model-driven discovery may arm implicit saved-query execution.
	s.recordCatalogRows(out, !seed)
	if len(ids) != 0 {
		s.recordDetailEvidence(ids, out)
		if s.catalogKindSeen("saved_query") {
			for _, name := range savedQueryNamesFromResult(out) {
				s.markSavedQueryDetailed(name)
			}
			for _, id := range ids {
				if name := savedQueryNameFromID(id); name != "" {
					s.markSavedQueryDetailed(name)
				}
			}
		}
	}
	s.recordNext(out)
}

// returnedCatalogDetailIDs prevents a guessed or stale id from becoming
// protocol evidence merely because the catalog request itself succeeded. Only
// ids actually present in returned detail cards can satisfy detail-before-use
// guards for raw GraphQL, saved queries, workflows, or mutations.
func returnedCatalogDetailIDs(requested []string, out any) []string {
	if len(requested) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(requested))
	for _, id := range requested {
		wanted[id] = true
	}
	ids := make([]string, 0, len(requested))
	for _, card := range catalogCards(out) {
		if id := stringFromMap(card, "id"); wanted[id] {
			ids = appendUniqueString(ids, id)
		}
	}
	return ids
}

// detailIDsFromArgs collects the requested detail ids from the single `id`
// argument and the batched `ids` argument.
func detailIDsFromArgs(args map[string]any) []string {
	var out []string
	if id := stringArg(args, "id"); id != "" {
		out = appendUniqueString(out, id)
	}
	for _, id := range stringSliceArg(args, "ids") {
		out = appendUniqueString(out, id)
	}
	return out
}

// correctKnownCatalogDetailArgs repairs a narrow class of invented ids when
// the exact authoritative id is already present in this run's catalog seed.
// In particular, weaker models commonly guess capability:gj_watch even though
// the governed watch mutation shape lives at help:watches.
func (s *discoveryState) correctKnownCatalogDetailArgs(args map[string]any) map[string]any {
	ids := detailIDsFromArgs(args)
	if s == nil || len(ids) != 1 || s.hasKnownCatalogID(ids) {
		return args
	}
	missed := strings.ToLower(strings.TrimSpace(ids[0]))
	corrected := ""
	if strings.Contains(missed, "watch") && s.catalogIDs["help:watches"] {
		corrected = "help:watches"
	}
	if corrected == "" {
		return args
	}
	out := cloneAnyMap(args)
	delete(out, "ids")
	out["id"] = corrected
	return out
}

// recordDetailEvidence marks per-target evidence established by an id-detail
// lookup: catalog kinds seen in detail, table cards (by table name and by the
// table:<...> id suffix), and workflow cards. Search results never reach here.
func (s *discoveryState) recordDetailEvidence(ids []string, out any) {
	for _, card := range catalogCards(out) {
		kind := strings.ToLower(stringFromMap(card, "kind"))
		if kind == "" {
			continue
		}
		s.detailKinds[kind] = true
		switch kind {
		case "table":
			if table := strings.ToLower(stringFromMap(card, "table_name")); table != "" {
				s.tablesDetailed[table] = true
			}
		case "workflow":
			name := stringFromMap(card, "name")
			if name == "" {
				name = strings.TrimPrefix(stringFromMap(card, "id"), "workflow:")
			}
			if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
				s.workflowsDetailed[name] = true
			}
		case "saved_query":
			name := savedQueryNameFromID(stringFromMap(card, "id"))
			if name == "" {
				name = stringFromMap(card, "name")
			}
			if query := strings.TrimSpace(stringFromMap(card, "graphql_query")); name != "" && query != "" {
				for _, candidate := range savedQueryNameCandidates(name) {
					s.savedQueryGraphQL[candidate] = query
				}
			}
		}
	}
	for _, id := range ids {
		if table := tableNameFromCatalogID(id); table != "" {
			s.tablesDetailed[table] = true
		}
		if strings.HasPrefix(strings.ToLower(id), "workflow:") {
			if name := strings.ToLower(strings.TrimSpace(id[len("workflow:"):])); name != "" {
				s.workflowsDetailed[name] = true
			}
		}
	}
}

// tableNameFromCatalogID extracts the bare table name from a table:<...> catalog
// id such as table:db:public.products (returns "products").
func tableNameFromCatalogID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if !strings.HasPrefix(id, "table:") {
		return ""
	}
	name := id[len("table:"):]
	if idx := strings.LastIndexAny(name, ".:"); idx >= 0 && idx+1 < len(name) {
		name = name[idx+1:]
	}
	return strings.TrimSpace(name)
}

func (s *discoveryState) recordCatalogRows(out any, discoverSavedQueries bool) {
	for _, card := range catalogCards(out) {
		id := stringFromMap(card, "id")
		kind := stringFromMap(card, "kind")
		name := stringFromMap(card, "name")
		if name == "" {
			name = savedQueryNameFromID(id)
		}
		// A remote_join relationship names the only route into its from-table.
		// Remember it: a top-level query on that table cannot return data, and the
		// interception that redirects it needs to know the parent.
		//
		// The id is the source of truth here, not the payload. Relationship cards
		// mostly arrive as summary rows — filtered reads strip evidence_json by
		// design — and the first version of this registration read only the payload,
		// so it registered nothing across an entire run while 29 doomed top-level
		// queries went through. The synthetic __<table>_ key in the id is the
		// remote-join signature and survives every read shape.
		if kind == "relationship" {
			from, parent, ok := parseRemoteJoinRelationshipID(id)
			if !ok && strings.EqualFold(stringFromMap(card, "source"), "remote_join") {
				var rel struct {
					FromTableName string
					ToTableName   string
				}
				if evidence := stringFromMap(card, "evidence_json"); evidence != "" {
					_ = json.Unmarshal([]byte(evidence), &rel)
				}
				from, parent, ok = rel.FromTableName, rel.ToTableName, rel.FromTableName != "" && rel.ToTableName != ""
			}
			if ok {
				if s.remoteJoinParents == nil {
					s.remoteJoinParents = map[string]string{}
				}
				s.remoteJoinParents[strings.ToLower(strings.TrimSpace(from))] = parent
			}
		}
		// Detail lookups also carry the join in edges_json — and fetching the table
		// detail is what episodes actually do, far more often than fetching the
		// relationship card itself. Run baa86d61: the redirect armed in 3 episodes
		// via relationship cards while 19 doomed top-level queries ran in episodes
		// that had inspected the table but never the relationship.
		if edges := stringFromMap(card, "edges_json"); strings.Contains(edges, "relationship:") {
			for _, candidate := range relationshipIDsInText(edges) {
				if from, parent, ok := parseRemoteJoinRelationshipID(candidate); ok {
					if s.remoteJoinParents == nil {
						s.remoteJoinParents = map[string]string{}
					}
					s.remoteJoinParents[strings.ToLower(strings.TrimSpace(from))] = parent
				}
			}
		}
		if id != "" {
			s.catalogIDs[id] = true
			if isSecurityRuntimeID(id) {
				s.securityRuntimeEvidence = true
			}
		}
		if kind != "" {
			s.catalogKinds[kind] = true
		}
		if kind == "saved_query" && discoverSavedQueries {
			if name == "" {
				name = strings.TrimPrefix(id, "saved_query:")
			}
			if name != "" {
				s.markSavedQueryDiscovered(name)
			}
		}
		if kind == "system_capability" && strings.Contains(strings.ToLower(id+" "+name+" "+stringFromMap(card, "title")), "gj_security") {
			s.securityRuntimeEvidence = true
		}
		if kind == "system_capability" && strings.Contains(strings.ToLower(id+" "+name+" "+stringFromMap(card, "title")), "gj_runtime") {
			s.securityRuntimeEvidence = true
		}
	}
}

func (s *discoveryState) recordNext(out any) {
	if m := mapValue(out); m != nil {
		if next := m["next"]; next != nil {
			s.suggestedNext = append(s.suggestedNext, normalizeValue(next))
		}
	}
}

func (s *discoveryState) recordValidation(args map[string]any, out any) {
	m := mapValue(out)
	if table := strings.ToLower(stringArg(args, "table")); table != "" {
		// The validation attempt itself is mutation-shape evidence: the model
		// learned the table's columns and operators even when valid=false.
		s.tablesValidated[table] = true
	}
	item := map[string]any{
		"table":    stringArg(args, "table"),
		"database": stringArg(args, "database"),
	}
	if m != nil {
		item["valid"] = m["valid"]
		item["validated_by"] = m["validated_by"]
		if errs, ok := m["errors"].([]any); ok {
			item["error_count"] = len(errs)
		}
		if errs, ok := m["compiler_errors"].([]any); ok {
			item["compiler_error_count"] = len(errs)
		}
	}
	s.validations = append(s.validations, item)
}

func (s *discoveryState) recordExecution(tool string, args map[string]any, out any) {
	item := resultSummary(tool, args, out)
	item["tool"] = tool
	if tool == "execute_saved_query" {
		item["name"] = stringArg(args, "name")
	}
	if tool == "execute_graphql" {
		item["query"] = stringArg(args, "query")
	}
	s.executions = append(s.executions, item)
	if item["has_data"] == true {
		s.lastExecution = map[string]any{
			"tool":   tool,
			"args":   redactArgs(args),
			"result": normalizeValue(out),
		}
	}
}

func executionQueryKey(args map[string]any) string {
	return "execute_graphql:" + stringify(normalizeValue(map[string]any{
		"query":     normalizeGraphQLIdentity(stringArg(args, "query")),
		"variables": args["variables"],
		"namespace": strings.TrimSpace(stringArg(args, "namespace")),
	}))
}

func savedQueryExecutionKey(args map[string]any) string {
	return "execute_saved_query:" + stringify(normalizeValue(map[string]any{
		"name":      strings.ToLower(strings.TrimSpace(stringArg(args, "name"))),
		"variables": args["variables"],
		"namespace": strings.TrimSpace(stringArg(args, "namespace")),
	}))
}

// normalizeGraphQLIdentity is deliberately conservative: whitespace and
// comments outside string values are insignificant in GraphQL, while quoted
// contents must remain byte-for-byte distinct.
func normalizeGraphQLIdentity(query string) string {
	var out strings.Builder
	inString := false
	inBlockString := false
	escaped := false
	pendingSpace := false
	for i := 0; i < len(query); i++ {
		if !inString && i+2 < len(query) && query[i:i+3] == `"""` {
			if pendingSpace && out.Len() != 0 {
				out.WriteByte(' ')
			}
			pendingSpace = false
			inString = true
			inBlockString = true
			out.WriteString(`"""`)
			i += 2
			continue
		}
		if inBlockString && i+2 < len(query) && query[i:i+3] == `"""` {
			out.WriteString(`"""`)
			i += 2
			inString = false
			inBlockString = false
			continue
		}
		ch := query[i]
		if inString {
			out.WriteByte(ch)
			if !inBlockString && ch == '\\' && !escaped {
				escaped = true
				continue
			}
			if !inBlockString && ch == '"' && !escaped {
				inString = false
			}
			escaped = false
			continue
		}
		if ch == '#' {
			for i+1 < len(query) && query[i+1] != '\n' && query[i+1] != '\r' {
				i++
			}
			pendingSpace = true
			continue
		}
		if ch == '"' {
			if pendingSpace && out.Len() != 0 {
				out.WriteByte(' ')
			}
			pendingSpace = false
			inString = true
			out.WriteByte(ch)
			continue
		}
		if unicode.IsSpace(rune(ch)) || ch == ',' {
			pendingSpace = true
			continue
		}
		if pendingSpace && out.Len() != 0 {
			out.WriteByte(' ')
		}
		pendingSpace = false
		out.WriteByte(ch)
	}
	return strings.TrimSpace(out.String())
}

func (s *discoveryState) cachedExecution(key string) (any, bool) {
	if s == nil || key == "" {
		return nil, false
	}
	value, ok := s.successfulExecutions[key]
	return normalizeValue(value), ok
}

func (s *discoveryState) cacheSuccessfulExecution(key string, out any) {
	if s == nil || key == "" || executionFailed(out) || resultSummary("execute_graphql", nil, out)["has_data"] != true {
		return
	}
	s.successfulExecutions[key] = normalizeValue(out)
}

// pendingCachedExecutionRejection withholds cached rows that cannot answer the
// outstanding requirement. savedQuery marks the caller: a saved query's text is
// fixed, so "repeat this differently" is unachievable through it, and generation
// 2028.1 recorded exactly that loop — the same saved query re-run ten times, each
// rejection sending the model to a catalog search that returned nothing.
func pendingCachedExecutionRejection(state *discoveryState, query string, savedQuery bool) (any, bool) {
	if state == nil {
		return nil, false
	}
	requirement := strings.TrimSpace(state.pendingRequiredFinalization())
	if requirement == "" {
		return nil, false
	}
	code, message := pendingFinalProtocol(requirement)
	repair := map[string]any{
		"kind": "distinct_aggregate_required",
		"next": message,
	}
	failure := "identical execution repeated while " + code + " is unmet; cached rows withheld because they cannot answer this request"
	if savedQuery {
		failure = "this saved query was already executed and its rows cannot satisfy " + code +
			"; a saved query is fixed, so re-running it will keep failing. Author the required query with execute_graphql instead"
		// next stays the guidance text every caller already reads; the tool is named
		// beside it so the model stops re-entering the saved query it cannot change.
		repair["kind"] = "saved_query_cannot_satisfy_requirement"
		repair["recommended_tool"] = "execute_graphql"
	}
	out := attachExecutionRecovery(executeResult{Errors: []ErrorInfo{{
		Message: failure,
		Extensions: map[string]any{
			"code":      code,
			"retryable": true,

			"graphjin_repair": repair,
		},
	}}}, state, query)
	return out, true
}

// selectCachedExecution makes the governed evidence selected by the current
// actor step the completion source. Without this, query A -> query B -> cached
// query A leaves the completion binding pointed at query B even though the
// model just reselected A.
func (s *discoveryState) selectCachedExecution(tool string, args map[string]any, out any) {
	if s == nil || executionFailed(out) || resultSummary(tool, args, out)["has_data"] != true {
		return
	}
	s.emptySearchStreak = 0
	s.emptyDetailStreak = 0
	s.lastExecution = map[string]any{
		"tool":   tool,
		"args":   redactArgs(args),
		"result": normalizeValue(out),
	}
}

func cachedExecutionResult(state *discoveryState, cached any) any {
	out := mapValue(cached)
	if out == nil {
		out = map[string]any{"data": normalizeValue(cached)}
	} else {
		out = cloneAnyMap(out)
	}
	out["cached"] = true
	if state != nil {
		if requirement := strings.TrimSpace(state.pendingRequiredFinalization()); requirement != "" {
			code, publicMessage := pendingFinalProtocol(requirement)
			next := "The identical successful execution was not run again. A pending " + code + " requirement still blocks finalization."
			if publicMessage != "" {
				next += " " + publicMessage
			}
			out["recovery"] = map[string]any{
				"code": code,
				"kind": code,
				"next": next,
			}
			return out
		}
	}
	out["recovery"] = map[string]any{
		"code": "completion_required",
		"kind": "completion_required",
		"next": "The identical successful execution was not run again. Call final now using this cached result.data.",
	}
	return out
}

// forcedFinalizeEvidenceByteLimit bounds the evidence handed to the tool-less
// finalizer. The interesting content — the most recent successful execution —
// goes in first; summaries are dropped before it is ever truncated.
const forcedFinalizeEvidenceByteLimit = 32 * 1024

// finalizeEvidenceDigest serializes the run's gathered evidence for the forced
// finalize: the last successful execution result in full, execution summaries,
// and the catalog ids inspected. Bounded so a huge result set cannot blow the
// finalizer's prompt.
func (s *discoveryState) finalizeEvidenceDigest(limit int) string {
	if s == nil {
		return ""
	}
	payload := map[string]any{
		"last_execution":     s.lastExecution,
		"executions":         s.executions,
		"catalog_detail_ids": s.catalogDetails,
	}
	data, err := json.Marshal(payload)
	if err == nil && (limit <= 0 || len(data) <= limit) {
		return string(data)
	}
	// Too big: keep the part the answer actually lives in.
	delete(payload, "executions")
	data, err = json.Marshal(payload)
	if err != nil {
		return ""
	}
	if limit > 0 && len(data) > limit {
		return string(data[:limit]) + " …[truncated]"
	}
	return string(data)
}

// clarificationNudge returns the one-shot redirect for a premature
// clarification. 20 of the 22 needs_clarification episodes on the frozen suite
// surrendered after exactly one tool call — one failed catalog search, then
// "table not found, please confirm the name" — without ever enumerating what
// exists. The condition and the recovery are both known here, so the first ask
// with at most one model action gets pointed at the enumeration instead of
// terminating the run; any later ask passes through untouched.
func (s *discoveryState) clarificationNudge() string {
	if s == nil || s.clarificationNudgeUsed {
		return ""
	}
	modelActions := 0
	for _, action := range s.actions {
		if action.Source == "model" {
			modelActions++
		}
	}
	if modelActions > 1 {
		return ""
	}
	s.clarificationNudgeUsed = true
	return "A clarification this early usually means discovery has not been tried yet. Before asking the user: enumerate what exists with query_catalog({kind:\"table\", limit:20}) — and kinds saved_query and workflow if relevant — and run one query_catalog({search: <the user's own words>}) coverage search. Ask the user only if the target is still ambiguous after inspecting those results."
}

func (s *discoveryState) answerReadyForCompletion() bool {
	return s != nil && s.seedOK && s.modelDiscoveryAction && !s.hasBlockingViolation() &&
		s.lastExecution != nil && s.pendingRequiredFinalization() == ""
}

func (s *discoveryState) recordRepeatedCall(key string) {
	if !s.answerReadyForCompletion() {
		return
	}
	if s.completionLatchKey == "" {
		s.completionLatchKey = key
		return
	}
	s.completionReady = true
}

func (s *discoveryState) clearCompletionLatch() {
	if s == nil {
		return
	}
	s.completionLatchKey = ""
	s.completionReady = false
}

func (s *discoveryState) completionContinuation() string {
	if s == nil {
		return ""
	}
	if s.hasPolicyFinalBlockingViolation() && !s.terminalContinuationIssued {
		s.terminalContinuationIssued = true
		violation, _ := s.primaryBlockingViolation()
		return `await final("GraphJin policy refusal completed.", {policy_refusal:` + stringify(map[string]any{
			"code": violation.Code, "message": violation.Message, "details": violation.Details,
		}) + `});`
	}
	if query := strings.TrimSpace(s.pendingSystemRootQuery); query != "" {
		s.pendingSystemRootQuery = ""
		prefix := ""
		if !s.securityRuntimeEvidence {
			prefix = `globalThis.graphjinSystemRootPrerequisites = await query_catalog({ids:["help:security","help:runtime"]}); `
		}
		return prefix + `globalThis.graphjinSystemRootRepair = await execute_graphql(` + stringify(map[string]any{"query": query}) + `); console.log(globalThis.graphjinSystemRootRepair);`
	}
	if !s.completionReady || !s.answerReadyForCompletion() {
		return ""
	}
	s.completionReady = false
	return `await final("GraphJin duplicate execution recovery completed.", {execution: globalThis.` + runtimeLastExecutionKey + `});`
}

func (s *discoveryState) pendingRequiredFinalization() string {
	if s.pendingFailedQueryKey != "" {
		// When a corrected mutation was computed, name it: "execute one distinct
		// repaired query" sent models back to the catalog while the exact string
		// sat in the failed result. The generic sentence stays for every failure
		// with no certain repair.
		if s.lastMutationRepairedQuery != "" && !s.mutationSucceeded {
			return "execution_repair_required: the write failed because it named columns this table does not have. The corrected mutation is in the failed result's recovery details.repaired_query — execute it exactly as given before finalizing; an identical retry of the broken write is rejected."
		}
		return "execution_repair_required: the first GraphQL execution failed. Read errors[].extensions.graphjin_repair and result.recovery, then execute one distinct repaired query before finalizing; an identical retry is rejected."
	}
	if message := s.pendingRecoverableExecution(); message != "" {
		return message
	}
	if message := s.pendingRequiredSavedQueryExecution(); message != "" {
		return message
	}
	return s.pendingDatabaseComputation()
}

func (s *discoveryState) pendingRequiredFinalizationContinuation() string {
	if s.pendingFailedQueryKey != "" {
		if len(s.pendingFailedCatalogIDs) == 0 {
			return ""
		}
		ids := append([]string(nil), s.pendingFailedCatalogIDs...)
		s.pendingFailedCatalogIDs = nil
		return `const evidence = await query_catalog(` + stringify(map[string]any{"ids": ids}) + `); console.log(evidence);`
	}
	if s.pendingDatabaseComputation() != "" {
		return ""
	}
	if continuation := s.pendingRecoverableExecutionContinuation(); continuation != "" {
		return continuation
	}
	return s.pendingRequiredSavedQueryContinuation()
}

func (s *discoveryState) pendingRecoverableExecution() string {
	violation, ok := s.primaryRecoverableExecutionViolation()
	if !ok {
		return ""
	}
	if s.recoverableExecutionEvidenceMissing(violation) {
		return "execution_evidence_required: the attempted GraphQL operation is missing required same-run evidence. Follow errors[].extensions.graphjin_repair.next in a discovery-only actor step; after the result is visible, re-author and execute the operation before finalizing."
	}
	return "execution_retry_required: the required evidence is now present, but the rejected GraphQL operation has not been retried successfully. Re-author it from the returned detail and execute it before finalizing."
}

func (s *discoveryState) pendingRecoverableExecutionContinuation() string {
	args := s.pendingRecoverableCatalogArgs()
	if len(args) == 0 {
		return ""
	}
	return `const evidence = await query_catalog(` + stringify(normalizeValue(args)) + `); console.log(evidence);`
}

// pendingRecoverableCatalogArgs returns the exact catalog lookup selected by a
// recoverable execution guard. Both the finalization continuation and the next
// explicit query_catalog call use this single source of truth.
func (s *discoveryState) pendingRecoverableCatalogArgs() map[string]any {
	violation, ok := s.primaryRecoverableExecutionViolation()
	if !ok || !s.recoverableExecutionEvidenceMissing(violation) {
		return nil
	}
	var next map[string]any
	switch violation.Code {
	case "cross_source_detail_required":
		next = catalogRepairNext(map[string]any{"ids": stringListFromDetails(violation.Details, "sources")}, "Inspect every source selected by the distiller.")
	case "security_runtime_discovery_required":
		next = catalogRepairNext(map[string]any{"ids": []any{"help:security", "help:runtime"}}, "Inspect write prerequisites.")
	case "mutation_evidence_required":
		next = s.mutationEvidenceNext(stringListFromDetails(violation.Details, "tables"))
	case "workflow_detail_required":
		next = s.workflowEvidenceNext()
	default:
		return nil
	}
	args := mapValue(next["args"])
	if stringFromMap(next, "recommended_tool") != toolQueryCatalog || len(args) == 0 {
		return nil
	}
	return cloneAnyMap(args)
}

func (s *discoveryState) primaryRecoverableExecutionViolation() (protocolViolation, bool) {
	if s == nil {
		return protocolViolation{}, false
	}
	var fallback protocolViolation
	hasFallback := false
	for _, code := range []string{"security_runtime_discovery_required", "cross_source_detail_required", "mutation_evidence_required", "workflow_detail_required"} {
		for _, violation := range s.violations {
			if violation.Blocking && violation.Code == code {
				if s.recoverableExecutionEvidenceMissing(violation) {
					return violation, true
				}
				if !hasFallback {
					fallback = violation
					hasFallback = true
				}
			}
		}
	}
	return fallback, hasFallback
}

func (s *discoveryState) recoverableExecutionEvidenceMissing(violation protocolViolation) bool {
	switch violation.Code {
	case "cross_source_detail_required":
		return len(s.missingDistilledSourceDetails()) != 0
	case "security_runtime_discovery_required":
		return !s.securityRuntimeEvidence
	case "mutation_evidence_required":
		return len(s.missingMutationEvidence(stringListFromDetails(violation.Details, "tables"))) != 0
	case "workflow_detail_required":
		return !s.hasWorkflowDetailEvidence()
	default:
		return false
	}
}

// pendingRequiredSavedQueryExecution guards explicit, ordered user requests
// such as "then execute_saved_query({name:...})" from a premature executor
// final. It also covers natural live-data requests when discovery produced one
// unambiguous saved-query route. Explanatory/catalog-only requests and
// multi-candidate routes never turn into an implicit execution.
func (s *discoveryState) pendingRequiredSavedQueryExecution() string {
	name, explicit := s.requiredSavedQueryExecution()
	if name == "" || s.hasSuccessfulSavedQueryExecution(name) {
		return ""
	}
	requirement := "the live-data request has one unambiguous discovered saved-query route"
	if explicit {
		requirement = "the user explicitly required this saved-query execution"
	}
	if !s.savedQueryDetailed(name) {
		return fmt.Sprintf("%s. Continue by running this exact JavaScript now: const detail = await query_catalog({id:%q}); const execution = await execute_saved_query({name:%q}); await final({status:\"answered\", answer:\"Answer the user's request only from execution.data.\", data:execution.data, evidence:{saved_query_detail:detail.cards}});", requirement, "saved_query:"+name, name)
	}
	return fmt.Sprintf("%s. The detail is already present; continue by running this exact JavaScript now: const execution = await execute_saved_query({name:%q}); await final({status:\"answered\", answer:\"Answer the user's request only from execution.data.\", data:execution.data});", requirement, name)
}

func (s *discoveryState) pendingRequiredSavedQueryContinuation() string {
	name, _ := s.requiredSavedQueryExecution()
	if name == "" || s.hasSuccessfulSavedQueryExecution(name) {
		return ""
	}
	result := `await final("GraphJin saved-query continuation completed.", {detail, execution});`
	if !s.savedQueryDetailed(name) {
		return fmt.Sprintf(`const detail = await query_catalog({id:%q}); const execution = await execute_saved_query({name:%q}); %s`, "saved_query:"+name, name, result)
	}
	return fmt.Sprintf(`const detail = {cards:[]}; const execution = await execute_saved_query({name:%q}); %s`, name, result)
}

func (s *discoveryState) requiredSavedQueryExecution() (name string, explicit bool) {
	if s == nil {
		return "", false
	}
	name = explicitlyRequiredSavedQueryName(s.instruction)
	explicit = name != ""
	if name == "" {
		name = s.unambiguousSavedQueryForLiveData()
	}
	// A follow-up that inherits its subject from prior turns cannot be served
	// by a saved query that does not bind that subject: the referent guard
	// refuses exactly that execution, so demanding it here wedged runs between
	// two contradictory requirements. Episode same-invoice-amount rep3 authored
	// the correct scoped query and was blocked five times by this demand for a
	// saved query the other guard would never let run.
	if name != "" && !explicit {
		if refs := unresolvedHistoryReferent(s.instruction, "-", nil, s.history); len(refs) != 0 &&
			!queryBindsReference(s.savedQueryGraphQLFor(name), nil, refs) {
			return "", false
		}
	}
	return name, explicit
}

func (s *discoveryState) unambiguousSavedQueryForLiveData() string {
	if !liveDataIntent(s.instruction) || len(s.savedQueriesDiscovered) != 1 {
		return ""
	}
	for name := range s.savedQueriesDiscovered {
		return name
	}
	return ""
}

func liveDataIntent(instruction string) bool {
	lower := strings.ToLower(strings.TrimSpace(instruction))
	if lower == "" {
		return false
	}
	for _, phrase := range []string{
		"discovery only", "do not execute", "don't execute", "without executing",
		"inventory the approved saved queries", "inventory approved saved queries",
		"saved queries and workflows", "list saved queries", "list the saved queries",
		"explain the saved query", "explain saved queries",
	} {
		if strings.Contains(lower, phrase) {
			return false
		}
	}
	for _, verb := range []string{
		"analyze", "check", "compare", "decide", "determine", "find", "identify",
		"prioritize", "review", "run", "show", "summarize", "triage", "what",
	} {
		if containsWord(lower, verb) {
			return true
		}
	}
	return false
}

func containsWord(value, word string) bool {
	for _, token := range strings.FieldsFunc(value, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == word {
			return true
		}
	}
	return false
}

func (s *discoveryState) hasSuccessfulSavedQueryExecution(name string) bool {
	for _, execution := range s.executions {
		if strings.EqualFold(stringFromMap(execution, "name"), strings.TrimSpace(name)) && execution["has_data"] == true {
			return true
		}
	}
	return false
}

func explicitlyRequiredSavedQueryName(instruction string) string {
	lower := strings.ToLower(instruction)
	for _, phrase := range []string{"do not execute", "don't execute", "without executing", "discovery only"} {
		if strings.Contains(lower, phrase) {
			return ""
		}
	}
	const marker = "execute_saved_query"
	for searchFrom := 0; searchFrom < len(lower); {
		relative := strings.Index(lower[searchFrom:], marker)
		if relative < 0 {
			return ""
		}
		start := searchFrom + relative
		windowStart := start - 96
		if windowStart < 0 {
			windowStart = 0
		}
		lead := lower[windowStart:start]
		required := strings.Contains(lead, "only after") || strings.Contains(lead, "then") || strings.Contains(lead, "await") || strings.Contains(lead, "must")
		open := start + len(marker)
		for open < len(instruction) && unicode.IsSpace(rune(instruction[open])) {
			open++
		}
		if required && open < len(instruction) && instruction[open] == '(' {
			close := strings.IndexByte(instruction[open+1:], ')')
			if close >= 0 {
				if name := savedQueryNameArgument(instruction[open+1 : open+1+close]); name != "" {
					return name
				}
			}
		}
		searchFrom = start + len(marker)
	}
	return ""
}

func savedQueryNameArgument(arguments string) string {
	lower := strings.ToLower(arguments)
	nameAt := strings.Index(lower, "name")
	if nameAt < 0 {
		return ""
	}
	colon := strings.IndexByte(arguments[nameAt+len("name"):], ':')
	if colon < 0 {
		return ""
	}
	value := strings.TrimSpace(arguments[nameAt+len("name")+colon+1:])
	if len(value) < 2 || (value[0] != '\'' && value[0] != '"') {
		return ""
	}
	quote := value[0]
	end := strings.IndexByte(value[1:], quote)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(value[1 : end+1])
}

func (s *discoveryState) hasCatalogEvidence() bool {
	return s.seedOK || len(s.catalogIDs) != 0 || len(s.catalogSearches) != 0
}

// hasCatalogDetailEvidence is deliberately stricter than modelDiscoveryAction:
// broad kind/table/search listings help the model choose a route, but only an
// explicit id lookup records the exact catalog object used to author raw
// GraphQL and makes that provenance visible in the final protocol evidence.
func (s *discoveryState) hasCatalogDetailEvidence() bool {
	return s != nil && len(s.catalogDetails) != 0
}

// missingMutationEvidence returns the mutation root fields that lack
// mutation-shape evidence in this run. A target table is covered when its table
// card detail was inspected by id, it was passed through validate_where_clause,
// or a mutation_pattern detail was inspected AND the table surfaced in any
// catalog result. Watch roots require help:watches or their exact capability
// detail; other gj_* roots are governed by their dedicated gates. An
// unparseable mutation (no roots) demands generic shape evidence.
func (s *discoveryState) missingMutationEvidence(roots []string) []string {
	if len(roots) == 0 {
		if s.detailKinds["mutation_pattern"] || len(s.tablesDetailed) != 0 || len(s.tablesValidated) != 0 {
			return nil
		}
		return []string{"mutation target"}
	}
	var missing []string
	for _, root := range roots {
		root = strings.ToLower(strings.TrimSpace(root))
		if root == "" {
			continue
		}
		if root == systemRootWatch || root == systemRootWatchEvent {
			if !s.hasWatchMutationEvidence(root) {
				missing = appendUniqueString(missing, root)
			}
			continue
		}
		if strings.HasPrefix(root, "gj_") {
			continue
		}
		table, dialectRepair := s.mutationTargetTable(root)
		if dialectRepair && !s.hasCatalogDetailID("help:mutations") {
			missing = appendUniqueString(missing, root)
			continue
		}
		if s.tablesDetailed[table] || s.tablesValidated[table] {
			continue
		}
		if s.detailKinds["mutation_pattern"] && s.tableSeenInCatalog(table) {
			continue
		}
		missing = appendUniqueString(missing, root)
	}
	return missing
}

func (s *discoveryState) hasCatalogDetailID(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, detailID := range s.catalogDetails {
		if strings.EqualFold(strings.TrimSpace(detailID), id) {
			return true
		}
	}
	return false
}

func (s *discoveryState) recordDistilledSourceIDs(value any) {
	if s == nil {
		return
	}
	var visit func(any)
	visit = func(current any) {
		switch typed := normalizeValue(current).(type) {
		case map[string]any:
			if id := strings.TrimSpace(stringFromMap(typed, "id")); strings.HasPrefix(strings.ToLower(id), "source:") {
				s.distilledSourceIDs = appendUniqueString(s.distilledSourceIDs, id)
			}
			for _, child := range typed {
				visit(child)
			}
		case []any:
			for _, child := range typed {
				visit(child)
			}
		}
	}
	visit(value)
	sort.Strings(s.distilledSourceIDs)
}

func (s *discoveryState) missingDistilledSourceDetails() []string {
	if s == nil || len(s.distilledSourceIDs) < 2 {
		return nil
	}
	missing := make([]string, 0, len(s.distilledSourceIDs))
	for _, id := range s.distilledSourceIDs {
		// The distiller may select a section — source:<name>:capabilities — but the
		// catalog serves cards, and a card detail lookup returns its sections. So
		// inspecting the card satisfies the section: demanding the section id
		// verbatim left the requirement unmeetable by either the model or the
		// supply, and the guard refused 8 of 24 cross-source episodes on ids nobody
		// could fetch.
		if !s.hasCatalogDetailID(id) && !s.hasCatalogDetailID(sourceCardID(id)) {
			missing = append(missing, id)
		}
	}
	return missing
}

// sourceCardID maps a distilled source detail id onto the card that serves it:
// source:<name>:<section> -> source:<name>. Ids of other shapes pass through.
func sourceCardID(id string) string {
	trimmed := strings.TrimSpace(id)
	if !strings.HasPrefix(strings.ToLower(trimmed), "source:") {
		return trimmed
	}
	rest := trimmed[len("source:"):]
	if index := strings.Index(rest, ":"); index > 0 {
		return trimmed[:len("source:")+index]
	}
	return trimmed
}

// mutationTargetTable maps common Hasura-style mutation roots back to a table
// already surfaced by GraphJin. The mapping is used only to select evidence;
// it never rewrites or executes the model's GraphQL.
func (s *discoveryState) mutationTargetTable(root string) (string, bool) {
	root = strings.ToLower(strings.TrimSpace(root))
	if root == "" || s.catalogIDForTable(root) != "" || s.tablesDetailed[root] || s.tablesValidated[root] {
		return root, false
	}
	for _, prefix := range []string{"insert_", "update_", "delete_"} {
		if !strings.HasPrefix(root, prefix) {
			continue
		}
		candidate := strings.TrimPrefix(root, prefix)
		for _, suffix := range []string{"_by_pk", "_one", "_many"} {
			candidate = strings.TrimSuffix(candidate, suffix)
		}
		if candidate != "" && (s.catalogIDForTable(candidate) != "" || s.tablesDetailed[candidate] || s.tablesValidated[candidate]) {
			return candidate, true
		}
	}
	return root, false
}

func (s *discoveryState) hasWatchMutationEvidence(root string) bool {
	if s == nil {
		return false
	}
	root = strings.ToLower(strings.TrimSpace(root))
	for _, id := range s.catalogDetails {
		lower := strings.ToLower(strings.TrimSpace(id))
		if lower == "help:watches" || lower == root || strings.HasSuffix(lower, ":"+root) || strings.Contains(lower, ":"+root+".") {
			return true
		}
	}
	return false
}

// tableSeenInCatalog reports whether any catalog result in this run surfaced a
// table:<...> card whose id suffix matches the table name.
func (s *discoveryState) tableSeenInCatalog(table string) bool {
	for id := range s.catalogIDs {
		if tableNameFromCatalogID(id) == table {
			return true
		}
	}
	return false
}

func requiresWorkflowDetail(roots []string) bool {
	for _, root := range roots {
		if strings.EqualFold(strings.TrimSpace(root), systemRootWorkflowExec) {
			return true
		}
	}
	return false
}

func (s *discoveryState) hasWorkflowDetailEvidence() bool {
	return s != nil && (len(s.workflowsDetailed) != 0 || len(s.savedQueriesDetailed) != 0)
}

func (s *discoveryState) mutationEvidenceNext(tables []string) map[string]any {
	ids := make([]any, 0, len(tables))
	unresolved := make([]string, 0, len(tables))
	for _, table := range tables {
		if table == systemRootWatch || table == systemRootWatchEvent {
			ids = append(ids, "help:watches")
			continue
		}
		target, dialectRepair := s.mutationTargetTable(table)
		if dialectRepair && !s.hasCatalogDetailID("help:mutations") {
			ids = append(ids, "help:mutations")
		}
		// An unresolved target must not discard the ids that did resolve. A weak
		// model handed a bare "enumerate visible tables" directive obeys it
		// literally, gets a list, and retries the same rejected mutation: a list
		// is not the detail lookup this guard requires. Name every id we can and
		// enumerate only for what is genuinely unknown.
		if id := s.catalogIDForTable(target); id != "" {
			ids = append(ids, id)
			continue
		}
		unresolved = appendUniqueString(unresolved, target)
	}
	if len(ids) == 0 {
		if len(unresolved) != 0 {
			return catalogRepairNext(
				map[string]any{"kind": "table", "limit": 20},
				fmt.Sprintf("No catalog id is known for %s. Enumerate visible tables, select the exact mutation target, then inspect that table card by id — a table list alone does not satisfy this requirement.", strings.Join(unresolved, ", ")),
			)
		}
		return catalogRepairNext(
			map[string]any{"kind": "mutation_pattern", "limit": 20},
			"Inspect a mutation pattern or the exact target table detail before retrying the write.",
		)
	}
	reason := "Inspect the exact target table detail by id and author the mutation only from its returned column and mutation-shape evidence."
	if len(unresolved) != 0 {
		reason += fmt.Sprintf(" No catalog id is known for %s yet: enumerate visible tables to locate it, then inspect it by id as well.", strings.Join(unresolved, ", "))
	}
	return catalogRepairNext(map[string]any{"ids": ids}, reason)
}

func (s *discoveryState) catalogIDForTable(table string) string {
	table = strings.ToLower(strings.TrimSpace(table))
	best := ""
	for id := range s.catalogIDs {
		if tableNameFromCatalogID(id) == table && (best == "" || id < best) {
			best = id
		}
	}
	return best
}

func (s *discoveryState) workflowEvidenceNext() map[string]any {
	workflowIDs := make([]string, 0)
	for id := range s.catalogIDs {
		if strings.HasPrefix(strings.ToLower(id), "workflow:") {
			workflowIDs = append(workflowIDs, id)
		}
	}
	if len(workflowIDs) != 0 {
		sort.Strings(workflowIDs)
		ids := make([]any, len(workflowIDs))
		for i := range workflowIDs {
			ids[i] = workflowIDs[i]
		}
		return catalogRepairNext(
			map[string]any{"ids": ids},
			"Inspect the discovered workflow detail by id before retrying workflow execution.",
		)
	}
	return catalogRepairNext(
		map[string]any{"kind": "workflow", "limit": 20},
		"Enumerate visible workflows, choose the exact workflow, and inspect its detail row before retrying execution.",
	)
}

func (s *discoveryState) catalogKindSeen(kind string) bool {
	return s.catalogKinds[kind]
}

// previouslyRefusedMutationEvidence reports whether every table in missing was
// already the subject of a mutation_evidence_required refusal this run. The
// first refusal is the teaching moment; a repeat for the same targets marks a
// model that is re-sending the blocked write instead of following the repair.
func (s *discoveryState) previouslyRefusedMutationEvidence(missing []string) bool {
	if s == nil || len(missing) == 0 {
		return false
	}
	refused := map[string]bool{}
	for _, violation := range s.violations {
		if violation.Code != "mutation_evidence_required" {
			continue
		}
		for _, table := range stringListFromDetails(violation.Details, "tables") {
			refused[strings.ToLower(strings.TrimSpace(table))] = true
		}
	}
	if len(refused) == 0 {
		return false
	}
	for _, table := range missing {
		if !refused[strings.ToLower(strings.TrimSpace(table))] {
			return false
		}
	}
	return true
}

func (s *discoveryState) addViolation(code, message, tool string, blocking bool, details map[string]any) {
	s.violations = append(s.violations, protocolViolation{
		Code:     code,
		Message:  message,
		Tool:     tool,
		Blocking: blocking,
		Details:  details,
	})
}

// resolveUngroundedAnswerViolation discharges a grounding rejection after the
// answer has been restated. Violations accumulate rather than replace, so
// without this the original rejection would still be blocking when the rewritten
// answer arrives — and a perfectly grounded restatement would be refused by the
// record of the draft it replaced.
func (s *discoveryState) resolveUngroundedAnswerViolation() {
	if s == nil {
		return
	}
	for i := range s.violations {
		violation := &s.violations[i]
		if violation.Code != "ungrounded_answer_fields" || !violation.Blocking {
			continue
		}
		violation.Blocking = false
		if violation.Details == nil {
			violation.Details = map[string]any{}
		}
		violation.Details["resolved"] = true
	}
}

// ungroundedAnswerViolationTokens returns the identifiers a blocking grounding
// rejection named, so a rewrite can be told exactly what not to say again.
func (s *discoveryState) ungroundedAnswerViolationTokens() []string {
	if s == nil {
		return nil
	}
	for _, violation := range s.violations {
		if violation.Code != "ungrounded_answer_fields" || !violation.Blocking {
			continue
		}
		return stringListFromDetails(violation.Details, "tokens")
	}
	return nil
}

// onlyBlockingViolationIs reports whether every blocking violation carries the
// given code. A rewrite that cannot lift the block is not worth a model call:
// discharging grounding while a saved-metric mismatch is also blocking would
// spend a request and still finalize as blocked.
func (s *discoveryState) onlyBlockingViolationIs(code string) bool {
	if s == nil {
		return false
	}
	found := false
	for _, violation := range s.violations {
		if !violation.Blocking {
			continue
		}
		if violation.Code != code {
			return false
		}
		found = true
	}
	return found
}

func (s *discoveryState) resolveSavedQueryDetailViolation(name string) {
	name = strings.TrimSpace(name)
	for i := range s.violations {
		violation := &s.violations[i]
		if violation.Code != "saved_query_detail_required" ||
			!strings.EqualFold(strings.TrimSpace(stringFromMap(violation.Details, "name")), name) {
			continue
		}
		violation.Blocking = false
		if violation.Details == nil {
			violation.Details = map[string]any{}
		}
		violation.Details["resolved"] = true
	}
}

// resolveSuccessfulSavedQueryViolations discharges the guards a governed
// saved-query execution genuinely satisfies. It is deliberately narrower than the
// raw-GraphQL list: a saved query succeeding does not retroactively justify raw
// GraphQL authored without discovery.
func (s *discoveryState) resolveSuccessfulSavedQueryViolations() {
	for i := range s.violations {
		violation := &s.violations[i]
		if violation.Code != "history_referent_unresolved" {
			continue
		}
		violation.Blocking = false
		if violation.Details == nil {
			violation.Details = map[string]any{}
		}
		violation.Details["resolved"] = true
	}
}

func (s *discoveryState) resolveSuccessfulExecutionViolations() {
	for i := range s.violations {
		violation := &s.violations[i]
		switch violation.Code {
		case "raw_graphql_catalog_required", "raw_graphql_discovery_required", "security_runtime_discovery_required", "cross_source_detail_required", "mutation_evidence_required", "workflow_detail_required",
			// Both of these intercept one call and hand back what it needs to
			// proceed. Leaving them out of this list made them terminal: the run
			// could satisfy the guard, execute correctly, and still be forced to
			// blocked at finalization. That is what took multi-turn from 1/21 to 0/21
			// between two runs of the same suite — the guard was right about the
			// defect and wrong about being unrecoverable.
			"history_referent_unresolved", "watch_query_invalid", "observed_value_mismatch", "remote_join_path_required":
		default:
			continue
		}
		violation.Blocking = false
		if violation.Details == nil {
			violation.Details = map[string]any{}
		}
		violation.Details["resolved"] = true
	}
}

func (s *discoveryState) finalize(resp Response) Response {
	if resp.Status == "" {
		resp.Status = StatusAnswered
	}
	if s.hasPolicyFinalBlockingViolation() {
		resp = blockResponse(resp)
	}
	if resp.Status == StatusError && s.hasBlockingViolation() {
		resp = blockResponse(resp)
	}
	resp = s.recoverResolvedTerminalRefusalResponse(resp)
	resp = s.recoverCompletionLatchResponse(resp)
	resp = s.recoverLostExecutionResponse(resp)
	resp = s.recoverLostHistoryResponse(resp)
	if resp.Status == StatusAnswered {
		switch {
		case !s.seedOK:
			s.addViolation("catalog_seed_required", "agent answered without successful initial catalog discovery", "", true, nil)
			resp = blockResponse(resp)
		case !s.modelDiscoveryAction:
			s.addViolation("model_discovery_required", "agent answered without a model-driven catalog/help/detail discovery action", "", true, nil)
			resp = blockResponse(resp)
		case s.mutationAttempted && !s.mutationSucceeded:
			// The database was never changed, so an answered final would report
			// work that did not happen — the recorded episodes say "Successfully
			// recorded payment..." over a write the engine rejected, which in
			// production tells a user their payment is recorded when it is not.
			// Every earlier layer already tried to convert this run: the failure
			// carried the real column names, a corrected mutation when one was
			// certain, and the finalize bounce demanded a retry. This gate is
			// what remains when none of it was taken.
			message := "a database write this run attempted never succeeded, so the task's action did not happen"
			if s.lastMutationFailure != "" {
				message += ": " + s.lastMutationFailure
			}
			details := map[string]any{}
			if s.lastMutationRepairedQuery != "" {
				message += ". The corrected mutation was provided in the failed result's details.repaired_query and was not executed"
				details["repaired_query"] = s.lastMutationRepairedQuery
			}
			s.addViolation("mutation_execution_failed", message, "", true, details)
			resp = blockResponse(resp)
		default:
			if tokens := s.ungroundedAnswerTokens(resp.Answer); len(tokens) != 0 {
				s.addViolation("ungrounded_answer_fields",
					fmt.Sprintf("answer cites field-like identifiers absent from this run's tool evidence: %s; answer only from fields observed in execution results and state derived values in plain language", strings.Join(tokens, ", ")),
					"", true, map[string]any{"tokens": tokens})
			}
			if executed, requested := s.mismatchedSavedMetric(); executed != "" {
				s.addViolation("saved_metric_identity_mismatch",
					fmt.Sprintf("answer reports the result of saved query %q, but the request names %q; execute %q and answer from its result", executed, requested, requested),
					"", true, map[string]any{"executed": executed, "requested": requested})
			}
			if s.hasBlockingViolation() {
				resp = blockResponse(resp)
			}
		}
	}
	resp.Actions = s.actionValues()
	resp.Evidence = s.mergeEvidence(resp.Evidence)
	if resp.Next == nil {
		resp.Next = s.nextValue()
	}
	if resp.Status == StatusBlocked {
		for _, violation := range s.violations {
			if violation.Blocking {
				resp.Errors = appendProtocolError(resp.Errors, violation)
			}
		}
		if resp.Refusal == nil {
			resp.Refusal = s.buildRefusal(resp)
		}
	} else {
		resp.Refusal = nil
	}
	return resp
}

// recoverResolvedTerminalRefusalResponse removes a stale model refusal only
// when this same run subsequently satisfied that exact protocol requirement
// and its final tool action was a successful data-bearing execution. Policy
// refusals and any still-blocking requirement remain untouched.
func (s *discoveryState) recoverResolvedTerminalRefusalResponse(resp Response) Response {
	if s == nil || resp.Status != StatusBlocked || resp.Refusal == nil || resp.Refusal.PolicyFinal ||
		strings.TrimSpace(resp.Answer) == "" || s.hasBlockingViolation() ||
		s.pendingRequiredFinalization() != "" || !s.terminalExecutionSucceeded() ||
		!s.hasResolvedViolation(resp.Refusal.Code) {
		return resp
	}
	for _, item := range resp.Errors {
		code := stringFromMap(item.Extensions, "code")
		if code != "" && !s.hasResolvedViolation(code) {
			return resp
		}
	}
	resp.Status = StatusAnswered
	resp.Refusal = nil
	resp.Errors = nil
	resp.Next = nil
	return resp
}

func (s *discoveryState) terminalExecutionSucceeded() bool {
	if len(s.actions) == 0 {
		return false
	}
	action := s.actions[len(s.actions)-1]
	return action.Status == "ok" &&
		(action.Tool == toolExecuteGraphQL || action.Tool == toolExecuteSavedQuery) &&
		action.Summary["has_data"] == true && executionErrorCount(action.Summary["error_count"]) == 0
}

func (s *discoveryState) hasResolvedViolation(code string) bool {
	code = strings.TrimSpace(code)
	if code == "" {
		return false
	}
	for _, violation := range s.violations {
		if violation.Code == code && !violation.Blocking && mapValue(violation.Details)["resolved"] == true {
			return true
		}
	}
	return false
}

// recoverCompletionLatchResponse is the hard-stop safety net for a model that
// used its final actor step to repeat an already-successful execution. The
// cached result has already passed the same discovery, safety, repair, and
// database-computation guards required by the runtime auto-final path.
func (s *discoveryState) recoverCompletionLatchResponse(resp Response) Response {
	if s == nil || resp.Status != StatusError || s.completionLatchKey == "" ||
		!onlyActorLoopError(resp.Errors) || !s.answerReadyForCompletion() {
		return resp
	}
	data, ok := s.lastExecutionData()
	if !ok {
		return resp
	}
	resp.Status = StatusAnswered
	resp.Answer = "The governed GraphJin execution completed successfully. Returned data:\n\n" + executionDataExcerpt(data, 8*1024)
	resp.Data = data
	resp.Errors = nil
	resp.Refusal = nil
	resp.Next = nil
	return resp
}

// recoverLostHistoryResponse handles a narrow RLM terminal failure: the user
// explicitly asks to repeat a prior answered turn, this run re-established
// model-driven catalog evidence, but the actor loops before forwarding the
// already-loaded trail to the responder. Prior turns remain context rather
// than protocol evidence; this only returns their content verbatim and never
// authorizes a query or side effect.
func (s *discoveryState) recoverLostHistoryResponse(resp Response) Response {
	if s == nil || resp.Status != StatusError || !s.seedOK || !s.modelDiscoveryAction ||
		s.hasBlockingViolation() || resp.Refusal != nil || !explicitHistoryRepeatRequest(s.instruction) ||
		!onlyActorLoopError(resp.Errors) {
		return resp
	}
	for i := len(s.history) - 1; i >= 0; i-- {
		turn := s.history[i]
		if !strings.EqualFold(strings.TrimSpace(turn.Role), "assistant") || strings.TrimSpace(turn.Content) == "" {
			continue
		}
		if status := strings.TrimSpace(turn.Status); status != "" && status != StatusAnswered {
			continue
		}
		resp.Status = StatusAnswered
		resp.Answer = strings.TrimSpace(turn.Content)
		resp.Errors = nil
		resp.Refusal = nil
		resp.Next = nil
		return resp
	}
	return resp
}

func explicitHistoryRepeatRequest(instruction string) bool {
	lower := strings.ToLower(strings.TrimSpace(instruction))
	repeat := strings.Contains(lower, "repeat") || strings.Contains(lower, "restate") || strings.Contains(lower, "echo")
	if !repeat {
		return false
	}
	for _, phrase := range []string{"prior", "previous", "earlier", "recent trail", "task trail", "history", "last turn", "prior agent run"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func onlyActorLoopError(errors []ErrorInfo) bool {
	if len(errors) != 1 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(stringFromMap(errors[0].Extensions, "code")), "agent_actor_steps_exhausted") {
		return true
	}
	return strings.Contains(strings.ToLower(errors[0].Message), "actor loop exceeded")
}

// recoverLostExecutionResponse repairs one internally contradictory model
// response: this run completed a governed execution with data, yet the final
// actor claims that no result was provided. The protocol only takes over when
// every discovery/policy guard is satisfied and the response carries no
// structured refusal or error. This keeps real empty/error/access outcomes
// under the normal refusal path.
func (s *discoveryState) recoverLostExecutionResponse(resp Response) Response {
	if s == nil || !s.seedOK || !s.modelDiscoveryAction || s.hasBlockingViolation() ||
		len(resp.Errors) != 0 || resp.Refusal != nil || !lostExecutionEvidenceClaim(resp.Answer) {
		return resp
	}
	switch resp.Status {
	case StatusBlocked, StatusError, StatusNeedsClarification:
	default:
		return resp
	}
	data, ok := s.lastExecutionData()
	if !ok {
		return resp
	}
	resp.Status = StatusAnswered
	resp.Answer = "The governed GraphJin execution completed successfully. Returned data:\n\n" + executionDataExcerpt(data, 8*1024)
	resp.Data = data
	resp.Errors = nil
	resp.Refusal = nil
	resp.Next = nil
	return resp
}

func (s *discoveryState) lastExecutionData() (any, bool) {
	execution := mapValue(s.lastExecution)
	result := mapValue(execution["result"])
	if result == nil {
		return nil, false
	}
	data, ok := result["data"]
	return normalizeValue(data), ok && data != nil
}

func lostExecutionEvidenceClaim(answer string) bool {
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return false
	}
	plain := strings.Join(strings.FieldsFunc(answer, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}), " ")
	for _, phrase := range []string{
		"no result data",
		"no query result",
		"no execution result",
		"did not receive the result",
		"result was not provided",
		"results were not provided",
		"data was not provided",
		"unable to access the result",
		"cannot access the result",
	} {
		if strings.Contains(plain, phrase) {
			return true
		}
	}
	return strings.Contains(answer, "didn't receive the result")
}

func executionDataExcerpt(data any, maxBytes int) string {
	value := stringify(normalizeValue(data))
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return strings.TrimSpace(value[:cut]) + "..."
}

func blockResponse(resp Response) Response {
	resp.Status = StatusBlocked
	if strings.TrimSpace(resp.Answer) == "" {
		resp.Answer = "I could not answer because the required GraphJin discovery protocol was not completed."
	}
	return resp
}

// refuseUnscopedReferent decides whether an operation that ignores the retained
// subject is refused.
//
// The first one always is. A byte-identical retry is refused again, because
// repeating the same omission is not a decision to query every row — measured
// behaviour was exactly that retry, answering the unscoped question. A genuinely
// different operation is allowed through after the first refusal: re-authoring is
// evidence the message was considered, and the sampled subject could be wrong, so
// the run must never be left with no way forward.
// maxUnscopedReferentRefusals bounds how often one operation may be refused for
// ignoring the retained subject.
//
// Refusing an identical retry indefinitely was worse than letting it through.
// Measured over a run, the guard fired 31 times across 7 episodes — the model
// resent the same unscoped query until the step budget was gone, so a wrong answer
// became a guaranteed timeout with nothing to show. Being told twice is the useful
// part; a third refusal only spends the caller's remaining steps.
const maxUnscopedReferentRefusals = 2

func (s *discoveryState) refuseUnscopedReferent(identity string) bool {
	if s == nil {
		return false
	}
	if identity != "" && s.historyReferentRejected[identity] > 0 {
		return s.historyReferentRejected[identity] < maxUnscopedReferentRefusals
	}
	return len(s.historyReferentRejected) == 0
}

func (s *discoveryState) recordReferentRejection(identity string) {
	if s == nil || identity == "" {
		return
	}
	if s.historyReferentRejected == nil { // count, so the refusal can be bounded
		s.historyReferentRejected = map[string]int{}
	}
	s.historyReferentRejected[identity]++
}

// recordObservedValueLookup captures enough to localise a silent lookup without
// another run: how many cards came back at all separates a filter that returns
// nothing from cards that return without their evidence.
func (s *discoveryState) recordObservedValueLookup(table string, values map[string][]string, cardsSeen int, sampleID string) {
	if s == nil {
		return
	}
	columns := make([]string, 0, len(values))
	for column := range values {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	s.observedValueLookups = append(s.observedValueLookups, map[string]any{
		"table":               table,
		"columns_with_values": len(columns),
		"columns":             columns,
		"cards_returned":      cardsSeen,
		"first_card_id":       sampleID,
	})
}

// refuseUnobservedWrite decides whether a write using values a column has never
// held is refused, and records the refusal when it is.
//
// Each unobserved value gets two refusals — each carrying the exact corrected
// write — and the third attempt proceeds, however it is phrased. One refusal
// was measured twice, in runs 2cae6c1c and 969337b6: the model varied the
// note, kept the value, and the write landed, in 10 of 11 and then 8 of 11
// episodes. A later query-text veto meant to catch byte-identical retries was
// measured to invert the design (run dcea36f8: 5–7 refusals per episode, no
// let-through ever) because models never retry byte-identically — see the
// observedValueStrikes field comment. Two corrections is friction, not schema;
// a caller who genuinely means the new value expresses it by persisting past
// corrections that each offered the alternative, so the run is never left
// without a way forward.
func (s *discoveryState) refuseUnobservedWrite(mismatches []columnAssignment) bool {
	if s == nil || len(mismatches) == 0 {
		return false
	}
	refuse := false
	for _, mismatch := range mismatches {
		if s.observedValueStrikes[valueStrikeKey(mismatch)] < 2 {
			refuse = true
		}
	}
	if !refuse {
		return false
	}
	if s.observedValueStrikes == nil {
		s.observedValueStrikes = map[string]int{}
	}
	for _, mismatch := range mismatches {
		s.observedValueStrikes[valueStrikeKey(mismatch)]++
	}
	return true
}

// recordUnobservedWriteStrikes increments the per-(table,column,value) strike
// counters without deciding anything, for the branch where a repair forces the
// refusal past the strike cap and the counters should still tell the story.
func (s *discoveryState) recordUnobservedWriteStrikes(mismatches []columnAssignment) {
	if s == nil || len(mismatches) == 0 {
		return
	}
	if s.observedValueStrikes == nil {
		s.observedValueStrikes = map[string]int{}
	}
	for _, mismatch := range mismatches {
		s.observedValueStrikes[valueStrikeKey(mismatch)]++
	}
}

func valueStrikeKey(mismatch columnAssignment) string {
	return strings.ToLower(mismatch.Table) + "." + strings.ToLower(mismatch.Column) + "=" + strings.ToLower(mismatch.Value)
}

// parseRemoteJoinRelationshipID reads the join out of a relationship card id:
// relationship:app:main.account_health.__account_health_id->app:main.accounts.id
// names account_health as join-only under accounts. Only the synthetic
// __<table>_ key qualifies; ordinary foreign-key relationships never carry it.
func parseRemoteJoinRelationshipID(id string) (string, string, bool) {
	trimmed := strings.TrimSpace(id)
	if !strings.HasPrefix(strings.ToLower(trimmed), "relationship:") {
		return "", "", false
	}
	parts := strings.SplitN(trimmed[len("relationship:"):], "->", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	fromTable, fromColumn := tableAndColumnFromQualified(parts[0])
	parent, _ := tableAndColumnFromQualified(parts[1])
	if fromTable == "" || parent == "" {
		return "", "", false
	}
	if !strings.HasPrefix(strings.ToLower(fromColumn), "__"+strings.ToLower(fromTable)+"_") {
		return "", "", false
	}
	return fromTable, parent, true
}

// relationshipIDsInText scans free text — an edges_json payload — for relationship
// ids. Terminators are the characters that can follow an id inside JSON.
func relationshipIDsInText(text string) []string {
	// Go's JSON encoder writes > as \u003e inside string content, so a live
	// edges_json payload spells the arrow -\u003e. The integration probe caught the
	// unnormalized form slipping past both the scanner (which terminates ids on a
	// backslash) and the parser (which splits on ->).
	text = strings.ReplaceAll(text, `\u003e`, ">")
	var out []string
	terminators := "\"'\\ \t\n,}]"
	for start := 0; start < len(text); {
		index := strings.Index(text[start:], "relationship:")
		if index < 0 {
			break
		}
		index += start
		end := index
		for end < len(text) && !strings.ContainsRune(terminators, rune(text[end])) {
			end++
		}
		if end > index {
			out = appendUniqueString(out, text[index:end])
		}
		start = end + 1
	}
	return out
}

// tableAndColumnFromQualified splits db:schema.table.column into its last two
// segments.
func tableAndColumnFromQualified(qualified string) (string, string) {
	value := qualified
	if index := strings.Index(value, ":"); index >= 0 {
		value = value[index+1:]
	}
	segments := strings.Split(value, ".")
	if len(segments) < 2 {
		return "", ""
	}
	return segments[len(segments)-2], segments[len(segments)-1]
}

// topLevelRemoteJoinRoot reports the first root field that is a join-only remote
// table, with the parent it must be reached through.
func (s *discoveryState) topLevelRemoteJoinRoot(query string) (string, string, bool) {
	if s == nil || len(s.remoteJoinParents) == 0 {
		return "", "", false
	}
	for _, root := range QueryRootFields(query) {
		if parent := s.remoteJoinParents[strings.ToLower(strings.TrimSpace(root))]; parent != "" {
			return root, parent, true
		}
	}
	return "", "", false
}

func (s *discoveryState) hasBlockingViolation() bool {
	for _, violation := range s.violations {
		if violation.Blocking {
			return true
		}
	}
	return false
}

func (s *discoveryState) hasPolicyFinalBlockingViolation() bool {
	for _, violation := range s.violations {
		if violation.Blocking && policyFinalViolation(violation.Code) {
			return true
		}
	}
	return false
}

func (s *discoveryState) actionValues() any {
	out := make([]any, len(s.actions))
	for i, action := range s.actions {
		out[i] = action
	}
	return out
}

func (s *discoveryState) mergeEvidence(model any) any {
	protocol := map[string]any{
		"seed": map[string]any{
			"ok":          s.seedOK,
			"instruction": s.instruction,
			"result":      s.seedResult,
		},
		"help_topics":              s.helpTopics,
		"catalog_searches":         s.catalogSearches,
		"catalog_detail_ids":       s.catalogDetails,
		"catalog_ids":              sortedBoolKeys(s.catalogIDs),
		"catalog_kinds":            sortedBoolKeys(s.catalogKinds),
		"saved_queries_discovered": sortedBoolKeys(s.savedQueriesDiscovered),
		"saved_queries_detailed":   sortedBoolKeys(s.savedQueriesDetailed),
		"tables_detailed":          sortedBoolKeys(s.tablesDetailed),
		"tables_validated":         sortedBoolKeys(s.tablesValidated),
		"workflows_detailed":       sortedBoolKeys(s.workflowsDetailed),
		"validations":              s.validations,
		"executions":               s.executions,
		"raw_graphql":              s.rawGraphQL,
		"suggested_next":           s.suggestedNext,
		"violations":               s.violations,
	}
	if len(s.observedValueLookups) != 0 {
		protocol["observed_value_lookups"] = s.observedValueLookups
	}
	if s.forcedFinalizeUsed {
		protocol["forced_finalize"] = true
	}
	if s.ungroundedRewriteUsed {
		protocol["ungrounded_rewrite"] = true
	}
	if model == nil {
		return protocol
	}
	return map[string]any{
		"protocol": protocol,
		"model":    model,
	}
}

func (s *discoveryState) nextValue() any {
	if len(s.suggestedNext) == 0 {
		return nil
	}
	return s.suggestedNext[len(s.suggestedNext)-1]
}

func appendProtocolError(errors []ErrorInfo, violation protocolViolation) []ErrorInfo {
	for _, err := range errors {
		if err.Message == violation.Message {
			return errors
		}
	}
	return append(errors, ErrorInfo{
		Message: violation.Message,
		Extensions: map[string]any{
			"code":     violation.Code,
			"tool":     violation.Tool,
			"details":  violation.Details,
			"protocol": "graphjin_discovery",
		},
	})
}

func redactArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := map[string]any{}
	for key, value := range args {
		switch key {
		case "variables", "headers", "context":
			out[key] = "[redacted]"
		case "query":
			out[key] = truncateString(fmt.Sprint(value), actionQueryTraceLimit)
		default:
			out[key] = normalizeValue(value)
		}
	}
	return out
}

func resultSummary(tool string, args map[string]any, out any) map[string]any {
	summary := map[string]any{}
	addStructuredResultSummary(summary, out)
	if tool == "query_catalog" || tool == "graphql_help" {
		cards := catalogCards(out)
		summary["card_count"] = len(cards)
		ids := make([]string, 0, len(cards))
		kinds := map[string]bool{}
		for _, card := range cards {
			if id := stringFromMap(card, "id"); id != "" {
				ids = append(ids, id)
			}
			if kind := stringFromMap(card, "kind"); kind != "" {
				kinds[kind] = true
			}
		}
		if len(ids) != 0 {
			summary["catalog_ids"] = ids
		}
		if len(kinds) != 0 {
			summary["catalog_kinds"] = sortedBoolKeys(kinds)
		}
		if result := mapValue(out); result != nil {
			if retrieval := mapValue(result["retrieval"]); retrieval != nil {
				for _, key := range []string{"mode", "exact_match", "semantic_candidate_count", "visible_table_endpoints", "relationship_path_count", "lexical_fallback"} {
					if value, exists := retrieval[key]; exists {
						summary[key] = value
					}
				}
			}
			if coverage := anySlice(result["coverage"]); len(coverage) != 0 {
				summary["coverage_phrase_count"] = len(coverage)
			}
		}
		return summary
	}
	if tool == "execute_saved_query" || tool == "execute_graphql" {
		m := mapValue(out)
		if m != nil {
			if m["cached"] == true {
				summary["cached"] = true
			}
			if data, ok := m["data"]; ok && data != nil {
				summary["has_data"] = true
				summary["data_shape"] = dataShape(data)
				summary["database_aggregate"] = resultContainsAggregateField(data)
			}
			if trunc := mapValue(m["truncation"]); trunc != nil {
				summary["truncated"] = trunc["roots"]
			}
		}
		return summary
	}
	if tool == "validate_where_clause" {
		m := mapValue(out)
		if m != nil {
			summary["valid"] = m["valid"]
			summary["table"] = m["table"]
			summary["validated_by"] = m["validated_by"]
		}
		return summary
	}
	return summary
}

func addStructuredResultSummary(summary map[string]any, out any) {
	m := mapValue(out)
	if m == nil {
		return
	}
	errorCodes := map[string]bool{}
	recoveryCodes := map[string]bool{}
	if errs := anySlice(m["errors"]); len(errs) != 0 {
		summary["error_count"] = len(errs)
		for _, item := range errs {
			extensions := mapValue(mapValue(item)["extensions"])
			if code := stringFromMap(extensions, "code"); code != "" {
				errorCodes[code] = true
			}
			if repair := mapValue(extensions["graphjin_repair"]); repair != nil {
				if code := stringFromMap(repair, "code"); code != "" {
					recoveryCodes[code] = true
				}
				if kind := stringFromMap(repair, "kind"); kind != "" {
					recoveryCodes[kind] = true
				}
			}
		}
	}
	if recovery := mapValue(m["recovery"]); recovery != nil {
		if code := stringFromMap(recovery, "code"); code != "" {
			recoveryCodes[code] = true
		}
		if kind := stringFromMap(recovery, "kind"); kind != "" {
			recoveryCodes[kind] = true
		}
		if next := mapValue(recovery["next"]); next != nil {
			tool := stringFromMap(next, "tool")
			if tool == "" {
				tool = stringFromMap(next, "recommended_tool")
			}
			if tool != "" {
				summary["recovery_tool"] = tool
			}
		}
	}
	if len(errorCodes) != 0 {
		summary["error_codes"] = sortedBoolKeys(errorCodes)
	}
	if len(recoveryCodes) != 0 {
		summary["recovery_codes"] = sortedBoolKeys(recoveryCodes)
	}
}

func catalogCards(out any) []map[string]any {
	m := mapValue(out)
	if m == nil {
		return nil
	}
	items := anySlice(m["cards"])
	if len(items) == 0 {
		items = anySlice(m["catalog_rows"])
	}
	cards := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if card := mapValue(item); card != nil {
			cards = append(cards, card)
		}
	}
	return cards
}

func savedQueryNamesFromResult(out any) []string {
	var names []string
	for _, card := range catalogCards(out) {
		if stringFromMap(card, "kind") != "saved_query" {
			continue
		}
		name := stringFromMap(card, "name")
		if name == "" {
			name = savedQueryNameFromID(stringFromMap(card, "id"))
		}
		if name != "" {
			names = appendUniqueString(names, name)
		}
	}
	return names
}

func mapValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	if m, ok := normalizeValue(value).(map[string]any); ok {
		return m
	}
	return nil
}

func catalogFacetDigest(value any) map[string]any {
	result := mapValue(value)
	if result == nil {
		return nil
	}
	return mapValue(result["facets"])
}

func anySlice(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	default:
		if out, ok := normalizeValue(value).([]any); ok {
			return out
		}
		return nil
	}
}

func stringFromMap(m map[string]any, key string) string {
	value, _ := m[key].(string)
	return strings.TrimSpace(value)
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortedBoolKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key, ok := range values {
		if ok && key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

func canonicalSavedQueryName(name string) string {
	return strings.TrimSpace(strings.TrimPrefix(name, "saved_query:"))
}

func (s *discoveryState) markSavedQueryDiscovered(name string) {
	for _, candidate := range savedQueryNameCandidates(name) {
		s.savedQueriesDiscovered[candidate] = true
	}
}

func (s *discoveryState) markSavedQueryDetailed(name string) {
	for _, candidate := range savedQueryNameCandidates(name) {
		s.savedQueriesDetailed[candidate] = true
	}
}

func (s *discoveryState) savedQueryDetailed(name string) bool {
	for _, candidate := range savedQueryNameCandidates(name) {
		if s.savedQueriesDetailed[candidate] {
			return true
		}
	}
	return false
}

// recordMissingCatalogDetails notes the saved queries a detail lookup asked for
// and the catalog did not return, and discharges any standing demand to inspect
// them. The demand was to look the card up before executing; the lookup
// happened, and the catalog's answer was that there is no such card. Leaving the
// violation blocking made it unsatisfiable — episode ah1-001 produced the right
// answer, ran a detail lookup that returned zero cards, and was still forced to
// blocked by a guard asking for the lookup it had just performed.
func (s *discoveryState) recordMissingCatalogDetails(ids []string) {
	for _, id := range ids {
		name := savedQueryNameFromID(strings.TrimSpace(id))
		if name == "" {
			continue
		}
		if s.savedQueryDetailMissed == nil {
			s.savedQueryDetailMissed = map[string]bool{}
		}
		for _, candidate := range savedQueryNameCandidates(name) {
			s.savedQueryDetailMissed[candidate] = true
		}
		s.resolveSavedQueryDetailViolation(name)
	}
}

// savedQueryKnownAbsent reports whether this run has positive evidence that the
// named saved query does not exist — either a detail lookup that came back
// empty, or a catalog that returned saved queries and not this one. A run that
// has seen no saved queries at all knows nothing, and keeps the blocking demand:
// that is the case where the lookup is genuinely the next thing to do.

// noteInterceptedWrite records that a mutation was stopped before execution.
// Run ab4-trt measured the hole this closes: 26 action episodes made exactly
// one write call, had it consumed by an evidence supply or a guard refusal,
// and finalized "successfully closed/recorded..." over a database that never
// changed — invisible to the failed-write gate because nothing had marked the
// attempt. An intercepted write is an attempted write; only a landed one may
// be reported as done.
func (s *discoveryState) noteInterceptedWrite(query, reason string) {
	if s == nil || !ContainsMutationOperation(query) {
		return
	}
	s.mutationAttempted = true
	if !s.mutationSucceeded {
		s.lastMutationFailure = reason
	}
}

func (s *discoveryState) savedQueryKnownAbsent(name string) bool {
	candidates := savedQueryNameCandidates(name)
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if s.savedQueryDetailMissed[candidate] {
			return true
		}
	}
	if len(s.savedQueriesDiscovered) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if s.savedQueriesDiscovered[candidate] {
			return false
		}
	}
	return true
}

func (s *discoveryState) savedQueryGraphQLFor(name string) string {
	for _, candidate := range savedQueryNameCandidates(name) {
		if query := strings.TrimSpace(s.savedQueryGraphQL[candidate]); query != "" {
			return query
		}
	}
	return ""
}

func savedQueryNameCandidates(name string) []string {
	name = canonicalSavedQueryName(name)
	if name == "" {
		return nil
	}
	out := []string{name}
	if idx := strings.LastIndex(name, "."); idx >= 0 && idx+1 < len(name) {
		out = append(out, name[idx+1:])
	}
	return out
}

func savedQueryNameFromID(id string) string {
	if strings.HasPrefix(id, "saved_query:") {
		return strings.TrimSpace(strings.TrimPrefix(id, "saved_query:"))
	}
	return ""
}

func isSecurityRuntimeID(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return id == "help:security" || id == "help:runtime" || strings.Contains(id, "gj_security") || strings.Contains(id, "gj_runtime")
}

func writeLikeGraphQL(query string) bool {
	lower := strings.ToLower(query)
	return ContainsMutationOperation(query) ||
		strings.Contains(lower, "gj_config") ||
		strings.Contains(lower, "gj_workflow") ||
		strings.Contains(lower, "gj_code") ||
		strings.Contains(lower, "gj_watch")
}

func (s *discoveryState) missingCapabilityActions(query string) []string {
	if !ContainsMutationOperation(query) {
		return nil
	}
	var missing []string
	for _, item := range mutationRootActions(query) {
		action := capabilityActionForMutation(item.root, item.action)
		if action != "" && !profileHasAction(s.capabilities, action) {
			missing = appendUniqueString(missing, action)
		}
	}
	return missing
}

type mutationRootAction struct {
	root   string
	action string
}

func mutationRootActions(query string) []mutationRootAction {
	clean := graphQLStructure(query)
	var out []mutationRootAction
	for _, root := range MutationRootFields(query) {
		actions := mutationActionsForRoot(clean, root)
		if len(actions) == 0 {
			switch {
			case strings.HasPrefix(root, "insert_"):
				actions = []string{"insert"}
			case strings.HasPrefix(root, "delete_"):
				actions = []string{"delete"}
			default:
				actions = []string{"update"}
			}
		}
		for _, action := range actions {
			out = append(out, mutationRootAction{root: root, action: action})
		}
	}
	return out
}

func mutationActionsForRoot(clean, root string) []string {
	lower := strings.ToLower(clean)
	root = strings.ToLower(strings.TrimSpace(root))
	var out []string
	for offset := 0; offset < len(lower); {
		relative := strings.Index(lower[offset:], root)
		if relative < 0 {
			break
		}
		start := offset + relative
		end := start + len(root)
		offset = end
		if (start > 0 && isGraphQLNameContinue(lower[start-1])) || (end < len(lower) && isGraphQLNameContinue(lower[end])) {
			continue
		}
		open := skipGraphQLSpace(clean, end)
		if open >= len(clean) || clean[open] != '(' {
			continue
		}
		close := matchingGraphQLDelimiter(clean, open, '(', ')')
		if close < 0 {
			continue
		}
		for _, name := range topLevelGraphQLArgumentNames(clean[open+1 : close]) {
			switch name {
			case "insert":
				out = appendUniqueString(out, "insert")
			case "update":
				out = appendUniqueString(out, "update")
			case "delete":
				out = appendUniqueString(out, "delete")
			case "upsert":
				out = appendUniqueString(out, "insert")
				out = appendUniqueString(out, "update")
			}
		}
	}
	return out
}

func topLevelGraphQLArgumentNames(value string) []string {
	var out []string
	braceDepth, bracketDepth, parenDepth := 0, 0, 0
	for index := 0; index < len(value); {
		switch value[index] {
		case '{':
			braceDepth++
			index++
			continue
		case '}':
			braceDepth--
			index++
			continue
		case '[':
			bracketDepth++
			index++
			continue
		case ']':
			bracketDepth--
			index++
			continue
		case '(':
			parenDepth++
			index++
			continue
		case ')':
			parenDepth--
			index++
			continue
		}
		if braceDepth != 0 || bracketDepth != 0 || parenDepth != 0 || !isGraphQLNameStart(value[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(value) && isGraphQLNameContinue(value[index]) {
			index++
		}
		if colon := skipGraphQLSpace(value, index); colon < len(value) && value[colon] == ':' {
			out = appendUniqueString(out, strings.ToLower(value[start:index]))
		}
	}
	return out
}

func capabilityActionForMutation(root, action string) string {
	root = strings.ToLower(strings.TrimSpace(root))
	action = strings.ToLower(strings.TrimSpace(action))
	if strings.HasPrefix(root, "insert_") {
		action, root = "insert", strings.TrimPrefix(root, "insert_")
	} else if strings.HasPrefix(root, "update_") {
		action, root = "update", strings.TrimPrefix(root, "update_")
	} else if strings.HasPrefix(root, "delete_") {
		action, root = "delete", strings.TrimPrefix(root, "delete_")
	}
	for _, suffix := range []string{"_by_pk", "_one", "_many"} {
		root = strings.TrimSuffix(root, suffix)
	}
	if isFixedSystemRoot(root) {
		return root + "." + action
	}
	switch action {
	case "insert":
		return CapabilityActionDataInsert
	case "delete":
		return CapabilityActionDataDelete
	default:
		return CapabilityActionDataUpdate
	}
}

func policyFinalFailure(code, message string, details map[string]any) executeResult {
	return executeResult{Errors: []ErrorInfo{{Message: message, Extensions: map[string]any{
		"code": code, "retryable": false, "policy_final": true, "tool": toolExecuteGraphQL,
		"details": details,
	}}}}
}

func (s *discoveryState) policyFinalExecutionResult() executeResult {
	violation, _ := s.primaryBlockingViolation()
	return policyFinalFailure(violation.Code, violation.Message, violation.Details)
}

func policyFinalExecutionError(out any) (ErrorInfo, bool) {
	value := mapValue(normalizeValue(out))
	for _, item := range anySlice(value["errors"]) {
		entry := mapValue(item)
		extensions := mapValue(entry["extensions"])
		code := stringFromMap(extensions, "code")
		policyFinal, _ := extensions["policy_final"].(bool)
		if code != "" && (policyFinal || policyFinalViolation(code)) {
			return ErrorInfo{Message: stringFromMap(entry, "message"), Extensions: extensions}, true
		}
	}
	return ErrorInfo{}, false
}

func markPolicyFinalExecution(out any, code string, details map[string]any) any {
	value := cloneAnyMap(mapValue(normalizeValue(out)))
	if value == nil {
		return policyFinalFailure(code, "GraphJin policy denied the requested action", details)
	}
	for _, item := range anySlice(value["errors"]) {
		entry := mapValue(item)
		extensions := mapValue(entry["extensions"])
		if extensions == nil {
			extensions = map[string]any{}
			entry["extensions"] = extensions
		}
		extensions["code"] = code
		extensions["retryable"] = false
		extensions["policy_final"] = true
		extensions["details"] = details
	}
	return value
}

func isWatchActionReviewMutation(query string) bool {
	lower := strings.ToLower(query)
	return ContainsMutationOperation(query) &&
		strings.Contains(lower, "gj_watch") &&
		strings.Contains(lower, "action_review_json")
}

func isWatchDefinitionMutation(query string) bool {
	lower := strings.ToLower(query)
	return ContainsMutationOperation(query) &&
		strings.Contains(lower, "gj_watch") &&
		!strings.Contains(lower, "flow_review_json") &&
		!strings.Contains(lower, "action_review_json")
}

func containsStringFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func watchSubscriptionRoots(query string, args map[string]any) []string {
	subscription := watchSubscriptionText(query, args)
	if subscription == "" {
		return nil
	}
	roots := QueryRootFields(subscription)
	tables := make([]string, 0, len(roots))
	for _, root := range roots {
		// GraphJin subscriptions expose a companion <root>_cursor field for
		// resumable delivery. It is part of the selected table's contract, not
		// another application mutation target with its own catalog card.
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(root)), "_cursor") {
			continue
		}
		tables = appendUniqueString(tables, root)
	}
	return tables
}

func watchSubscriptionText(query string, args map[string]any) string {
	clean := graphQLStructure(query)
	lower := strings.ToLower(clean)
	for offset := 0; offset < len(lower); {
		relative := strings.Index(lower[offset:], systemRootWatch)
		if relative < 0 {
			return ""
		}
		start := offset + relative
		end := start + len(systemRootWatch)
		offset = end
		if (start > 0 && isGraphQLNameContinue(lower[start-1])) || (end < len(lower) && isGraphQLNameContinue(lower[end])) {
			continue
		}
		open := skipGraphQLSpace(clean, end)
		if open >= len(clean) || clean[open] != '(' {
			continue
		}
		close := matchingGraphQLDelimiter(clean, open, '(', ')')
		if close < 0 {
			return ""
		}
		if value := graphQLStringField(query, clean, open+1, close, "query", args); value != "" {
			return value
		}
		for _, argument := range []string{"insert", "update", "upsert"} {
			if input := graphQLVariableArgument(clean, open+1, close, argument, args); input != nil {
				if value, ok := input["query"].(string); ok {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	return ""
}

func graphQLStringField(raw, clean string, start, end int, field string, args map[string]any) string {
	lower := strings.ToLower(clean[start:end])
	for offset := 0; offset < len(lower); {
		relative := strings.Index(lower[offset:], field)
		if relative < 0 {
			return ""
		}
		fieldStart := start + offset + relative
		fieldEnd := fieldStart + len(field)
		offset += relative + len(field)
		if (fieldStart > start && isGraphQLNameContinue(clean[fieldStart-1])) || (fieldEnd < end && isGraphQLNameContinue(clean[fieldEnd])) {
			continue
		}
		colon := skipGraphQLSpace(clean, fieldEnd)
		if colon >= end || clean[colon] != ':' {
			continue
		}
		// graphQLStructure blanks string literals while preserving byte offsets.
		// Locate the value in the raw operation so an inline quoted subscription
		// remains visible to the watch prerequisite parser.
		value := skipGraphQLSpace(raw, colon+1)
		if value >= end {
			return ""
		}
		if raw[value] == '"' {
			stringEnd := skipGraphQLString(raw, value)
			if strings.HasPrefix(raw[value:stringEnd], `"""`) {
				return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(raw[value:stringEnd], `"""`), `"""`))
			}
			decoded, err := strconv.Unquote(raw[value:stringEnd])
			if err == nil {
				return strings.TrimSpace(decoded)
			}
			return ""
		}
		if raw[value] == '$' {
			nameStart := value + 1
			nameEnd := nameStart
			for nameEnd < end && isGraphQLNameContinue(raw[nameEnd]) {
				nameEnd++
			}
			variables, _ := normalizeValue(args["variables"]).(map[string]any)
			if result, ok := variables[raw[nameStart:nameEnd]].(string); ok {
				return strings.TrimSpace(result)
			}
		}
	}
	return ""
}

func graphQLVariableArgument(clean string, start, end int, argument string, args map[string]any) map[string]any {
	lower := strings.ToLower(clean[start:end])
	for offset := 0; offset < len(lower); {
		relative := strings.Index(lower[offset:], argument)
		if relative < 0 {
			return nil
		}
		nameStart := start + offset + relative
		nameEnd := nameStart + len(argument)
		offset += relative + len(argument)
		if (nameStart > start && isGraphQLNameContinue(clean[nameStart-1])) || (nameEnd < end && isGraphQLNameContinue(clean[nameEnd])) {
			continue
		}
		colon := skipGraphQLSpace(clean, nameEnd)
		value := skipGraphQLSpace(clean, colon+1)
		if colon >= end || clean[colon] != ':' || value >= end || clean[value] != '$' {
			continue
		}
		variableStart := value + 1
		variableEnd := variableStart
		for variableEnd < end && isGraphQLNameContinue(clean[variableEnd]) {
			variableEnd++
		}
		variables, _ := normalizeValue(args["variables"]).(map[string]any)
		return mapValue(variables[clean[variableStart:variableEnd]])
	}
	return nil
}

func watchDefinitionExecutionFailed(out any) bool {
	value := mapValue(normalizeValue(out))
	for _, item := range anySlice(value["errors"]) {
		message := strings.ToLower(stringFromMap(mapValue(item), "message"))
		for _, marker := range []string{"gj_watch subscription", "gj_watch query", "cursor pagination", "subscription probe failed"} {
			if strings.Contains(message, marker) {
				return true
			}
		}
	}
	return false
}

func (s *discoveryState) watchRepairCatalogIDs(query string, args map[string]any) []string {
	ids := []string{"help:watches"}
	for _, root := range watchSubscriptionRoots(query, args) {
		if id := s.catalogIDForTable(root); id != "" {
			ids = appendUniqueString(ids, id)
		}
	}
	return ids
}

func attachWatchQueryRepair(out any, ids []string, roots []string) any {
	value := cloneAnyMap(mapValue(normalizeValue(out)))
	if value == nil {
		return out
	}
	reason := "Inspect the exact watch contract and embedded subscription table details, then execute one distinct repaired gj_watch mutation."
	// A watch subscription must be cursor-backed. The probe reports that
	// requirement but not its shape, which leaves a weak model re-submitting the
	// same cursor-less subscription. Name the corrected form with this watch's
	// real root, the way every other repair names its arguments.
	if shape := watchCursorShape(value, roots); shape != "" {
		reason = "This watch subscription is not cursor-backed. Re-author it as " + shape + " then execute one distinct repaired gj_watch mutation."
	}
	next := catalogRepairNext(map[string]any{"ids": ids}, reason)
	for _, item := range anySlice(value["errors"]) {
		entry := mapValue(item)
		extensions := mapValue(entry["extensions"])
		if extensions == nil {
			extensions = map[string]any{}
			entry["extensions"] = extensions
		}
		extensions["code"] = "watch_query_invalid"
		extensions["retryable"] = true
		extensions["graphjin_repair"] = map[string]any{"kind": "watch_query_invalid", "next": next}
	}
	value["recovery"] = map[string]any{"code": "watch_query_invalid", "kind": "watch_query_invalid", "next": next}
	return value
}

func isAnnotationDefinitionMutation(query string, fields map[string]bool) bool {
	lower := strings.ToLower(query)
	if !ContainsMutationOperation(query) || !strings.Contains(lower, "gj_artifacts") {
		return false
	}
	if fields["target_ref"] || fields["task_id"] || fields["metadata_json"] {
		return true
	}
	if fields["content"] {
		return fields["kind"] || fields["_annotation_id"] || strings.Contains(lower, "annotation:")
	}
	return false
}

// annotationMutationInputFields inspects only gj_artifacts insert/update input
// objects. Selection-set fields and string contents do not count, and a
// variable-backed input contributes the keys of the referenced variable.
func annotationMutationInputFields(query string, args map[string]any) map[string]bool {
	fields := make(map[string]bool)
	if !ContainsMutationOperation(query) {
		return fields
	}
	clean := graphQLStructure(query)
	if containsAnnotationID(args["variables"]) {
		fields["_annotation_id"] = true
	}
	for start := 0; start < len(clean); {
		index := strings.Index(strings.ToLower(clean[start:]), "gj_artifacts")
		if index < 0 {
			break
		}
		index += start
		endName := index + len("gj_artifacts")
		if (index > 0 && isGraphQLNameContinue(clean[index-1])) || (endName < len(clean) && isGraphQLNameContinue(clean[endName])) {
			start = endName
			continue
		}
		open := skipGraphQLSpace(clean, endName)
		if open >= len(clean) || clean[open] != '(' {
			start = endName
			continue
		}
		close := matchingGraphQLDelimiter(clean, open, '(', ')')
		if close < 0 {
			break
		}
		collectAnnotationArgumentFields(clean[open+1:close], args, fields)
		start = close + 1
	}
	return fields
}

func containsAnnotationID(value any) bool {
	switch typed := normalizeValue(value).(type) {
	case string:
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(typed)), "annotation:")
	case map[string]any:
		for _, item := range typed {
			if containsAnnotationID(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if containsAnnotationID(item) {
				return true
			}
		}
	}
	return false
}

func collectAnnotationArgumentFields(arguments string, args map[string]any, fields map[string]bool) {
	for index := 0; index < len(arguments); {
		index = skipGraphQLSpace(arguments, index)
		if index >= len(arguments) {
			return
		}
		if !isGraphQLNameStart(arguments[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(arguments) && isGraphQLNameContinue(arguments[index]) {
			index++
		}
		argument := strings.ToLower(arguments[start:index])
		colon := skipGraphQLSpace(arguments, index)
		if colon >= len(arguments) || arguments[colon] != ':' || (argument != "insert" && argument != "update") {
			index = colon
			continue
		}
		value := skipGraphQLSpace(arguments, colon+1)
		if value < len(arguments) && arguments[value] == '{' {
			close := matchingGraphQLDelimiter(arguments, value, '{', '}')
			if close < 0 {
				return
			}
			collectGraphQLObjectKeys(arguments[value+1:close], fields)
			index = close + 1
			continue
		}
		if value < len(arguments) && arguments[value] == '$' {
			nameStart := value + 1
			nameEnd := nameStart
			for nameEnd < len(arguments) && isGraphQLNameContinue(arguments[nameEnd]) {
				nameEnd++
			}
			collectVariableObjectKeys(args, arguments[nameStart:nameEnd], fields)
			index = nameEnd
			continue
		}
		index = value + 1
	}
}

func collectGraphQLObjectKeys(object string, fields map[string]bool) {
	for index := 0; index < len(object); {
		if !isGraphQLNameStart(object[index]) {
			index++
			continue
		}
		start := index
		index++
		for index < len(object) && isGraphQLNameContinue(object[index]) {
			index++
		}
		if colon := skipGraphQLSpace(object, index); colon < len(object) && object[colon] == ':' {
			fields[strings.ToLower(object[start:index])] = true
		}
	}
}

func collectVariableObjectKeys(args map[string]any, name string, fields map[string]bool) {
	variables, _ := normalizeValue(args["variables"]).(map[string]any)
	input, _ := variables[name].(map[string]any)
	for key := range input {
		fields[strings.ToLower(strings.TrimSpace(key))] = true
	}
}

func graphQLStructure(query string) string {
	data := []byte(query)
	for index := 0; index < len(data); {
		switch data[index] {
		case '#':
			for index < len(data) && data[index] != '\n' {
				data[index] = ' '
				index++
			}
		case '"':
			end := skipGraphQLString(query, index)
			for position := index; position < end; position++ {
				data[position] = ' '
			}
			index = end
		default:
			index++
		}
	}
	return string(data)
}

func skipGraphQLSpace(value string, index int) int {
	for index < len(value) {
		switch value[index] {
		case ' ', '\t', '\n', '\r', ',':
			index++
		default:
			return index
		}
	}
	return index
}

func matchingGraphQLDelimiter(value string, start int, open, close byte) int {
	depth := 0
	for index := start; index < len(value); index++ {
		switch value[index] {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return index
			}
		}
	}
	return -1
}

func graphQLOperationKind(query string) string {
	if ContainsMutationOperation(query) {
		return "mutation"
	}
	lower := strings.ToLower(strings.TrimSpace(query))
	if strings.HasPrefix(lower, "subscription") {
		return "subscription"
	}
	return "query"
}

func dataShape(value any) any {
	switch v := normalizeValue(value).(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return map[string]any{"type": "object", "keys": keys}
	case []any:
		return map[string]any{"type": "array", "length": len(v)}
	default:
		return map[string]any{"type": fmt.Sprintf("%T", value)}
	}
}

func truncateString(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

// supplyCrossSourceEvidence fetches the source cards a cross-source operation
// requires and returns them once, rather than spending an actor step asking for ids
// the run already holds. It only counts evidence the catalog actually returned, so
// a stale id cannot satisfy the guard it was meant to enforce.
func (r *protocolRuntime) supplyCrossSourceEvidence(ctx context.Context, missing []string, queryArgs map[string]any) (any, bool) {
	if r == nil || r.state == nil || r.state.crossSourceEvidenceSupplied || len(missing) == 0 {
		return nil, false
	}
	// Fetch cards, not sections: a section id like source:x:capabilities returns no
	// card of its own, and asking for it verbatim is why this supply fired zero
	// times in its first measured run.
	ids := make([]any, 0, len(missing))
	seen := map[string]bool{}
	for _, id := range missing {
		card := sourceCardID(id)
		if card == "" || seen[card] {
			continue
		}
		seen[card] = true
		ids = append(ids, card)
	}
	if len(ids) == 0 {
		return nil, false
	}
	args := map[string]any{"ids": ids}
	r.addNamespace(args)
	out, err := r.base.QueryCatalog(ctx, args)
	if err != nil || len(catalogCards(out)) == 0 {
		return nil, false
	}
	r.state.recordCatalog(args, out, false)
	if len(r.state.missingDistilledSourceDetails()) != 0 {
		// The catalog answered without the cards this guard tracks. Report rather
		// than let the cross-source operation through — and leave the attempt
		// unspent, so a run where the catalog recovers can still be helped. A spent
		// flag on a failed attempt is the one-shot mistake this file has now made
		// four times.
		return nil, false
	}
	r.state.crossSourceEvidenceSupplied = true
	result := map[string]any{
		"graphjin_protocol": "cross_source_evidence_supplied",
		"message":           "GraphJin fetched the source details this cross-source operation requires. Author the query from the returned source cards, then execute it.",
		"cards":             normalizeValue(mapValue(normalizeValue(out))["cards"]),
		"sources":           missing,
		"next":              "Re-execute the cross-source query now that every selected source has been inspected.",
	}
	action := r.state.startAction("model", "execute_graphql", queryArgs)
	r.state.finishAction(action, "execute_graphql", queryArgs, result, nil)
	return result, true
}

// supplySecurityRuntimeEvidence fetches the security and runtime guidance a
// write-like operation requires and returns it once, rather than refusing so
// the model can ask for two ids that never vary. The requirement is still
// discharged only by content the catalog actually returned, so a catalog that
// stops serving the cards falls back to the refusal.
func (r *protocolRuntime) supplySecurityRuntimeEvidence(ctx context.Context, queryArgs map[string]any) (any, bool) {
	if r == nil || r.state == nil || r.state.securityRuntimeEvidenceSupplied {
		return nil, false
	}
	args := map[string]any{"ids": []any{"help:security", "help:runtime"}}
	r.addNamespace(args)
	out, err := r.base.QueryCatalog(ctx, args)
	if err != nil || len(catalogCards(out)) == 0 {
		return nil, false
	}
	r.state.recordCatalog(args, out, false)
	if !r.state.securityRuntimeEvidence {
		// The catalog answered without the guidance this guard tracks. Refuse as
		// before — and leave the attempt unspent, so a run where the catalog
		// recovers can still be helped.
		return nil, false
	}
	r.state.securityRuntimeEvidenceSupplied = true
	result := map[string]any{
		"graphjin_protocol": "security_runtime_evidence_supplied",
		"message":           "GraphJin fetched the security and runtime guidance this write-capable operation requires. Author the operation from the returned guidance, then execute it.",
		"cards":             normalizeValue(mapValue(normalizeValue(out))["cards"]),
		"next":              "Re-execute the operation now that security and runtime guidance has been inspected.",
	}
	action := r.state.startAction("model", "execute_graphql", queryArgs)
	r.state.finishAction(action, "execute_graphql", queryArgs, result, nil)
	return result, true
}

// supplyMutationEvidence discharges a missing mutation-shape prerequisite by
// fetching the target tables' catalog detail itself and returning it to the model,
// rather than naming the lookup and hoping the model performs it.
//
// It applies once per target table, and only when every missing target resolves to
// a known catalog id: a genuinely unknown or invented target still fails loudly
// through the ordinary rejection so the model must locate it. A single run-wide
// flag meant a run needing evidence for a second table never got it — the same
// one-shot mistake corrected twice elsewhere in this file.
func (r *protocolRuntime) supplyMutationEvidence(ctx context.Context, missing []string) (any, bool) {
	if r == nil || r.state == nil || len(missing) == 0 {
		return nil, false
	}
	for _, table := range missing {
		if r.state.mutationEvidenceSuppliedFor[strings.ToLower(strings.TrimSpace(table))] {
			return nil, false
		}
	}
	ids := make([]any, 0, len(missing))
	for _, table := range missing {
		target, _ := r.state.mutationTargetTable(table)
		id := r.state.catalogIDForTable(target)
		if id == "" {
			return nil, false
		}
		ids = append(ids, id)
	}
	args := map[string]any{"ids": ids}
	r.addNamespace(args)
	out, err := r.base.QueryCatalog(ctx, args)
	if err != nil {
		return nil, false
	}
	// Only count evidence the catalog actually returned, so a stale id cannot
	// satisfy the guard it was meant to enforce.
	if len(catalogCards(out)) == 0 {
		return nil, false
	}
	if r.state.mutationEvidenceSuppliedFor == nil {
		r.state.mutationEvidenceSuppliedFor = map[string]bool{}
	}
	for _, table := range missing {
		r.state.mutationEvidenceSuppliedFor[strings.ToLower(strings.TrimSpace(table))] = true
	}
	r.state.mutationEvidenceSupplied = true
	r.state.recordCatalog(args, out, false)
	return map[string]any{
		"graphjin_protocol": "mutation_shape_evidence_supplied",
		"message":           "GraphJin fetched the mutation-shape evidence this write requires. Author the mutation only from the returned column and mutation-shape evidence below, then execute it.",
		"cards":             normalizeValue(mapValue(normalizeValue(out))["cards"]),
		"tables":            missing,
		"next":              "Re-author the mutation from these returned columns and execute it once.",
	}, true
}

// watchCursorShape returns the corrected cursor-backed subscription shape when a
// watch probe rejected the query for missing cursor pagination. It substitutes
// the watch's real root so the model does not have to translate a template.
func watchCursorShape(value map[string]any, roots []string) string {
	cursorFault := false
	for _, item := range anySlice(value["errors"]) {
		message := strings.ToLower(stringFromMap(mapValue(item), "message"))
		if strings.Contains(message, "cursor pagination") {
			cursorFault = true
			break
		}
	}
	if !cursorFault {
		return ""
	}
	root := ""
	for _, candidate := range roots {
		if candidate != "" && !strings.HasPrefix(candidate, "gj_") {
			root = candidate
			break
		}
	}
	if root == "" {
		root = "<table>"
	}
	return "subscription { " + root + "(first: 25, after: $cursor) { ... } " + root + "_cursor }"
}

// catalogCardEvidence captures what a detail lookup actually told the model, so a
// stored trajectory can answer whether the guidance it needed was present. Only
// by-id catalog lookups qualify: searches return card lists whose bulk is the
// reason summaries exist, and execution results are already described by their
// summary and recovery codes.
// recoveryEvidence records the repair a call actually handed back, bounded.
//
// Action summaries carry recovery *codes* but not the payload, so "did the model
// receive the candidate ids, or the resolved subject, or nothing?" has not been
// answerable from a trajectory. Several diagnoses today turned on exactly that and
// could only be narrowed by re-deriving it from source, which is how two wrong
// readings got published earlier this week.
func recoveryEvidence(out any) map[string]any {
	recovery := mapValue(mapValue(out)["recovery"])
	if len(recovery) == 0 {
		return nil
	}
	captured := map[string]any{}
	for _, field := range []string{"kind", "instruction", "next", "did_you_mean", "missed_ids", "known_ids", "resolved", "retained_subject", "repaired_query"} {
		value, ok := recovery[field]
		if !ok || value == nil {
			continue
		}
		captured[field] = boundedEvidenceValue(value)
	}
	if len(captured) == 0 {
		return nil
	}
	return map[string]any{"recovery": captured}
}

// boundedEvidenceValue keeps a captured payload from growing an episode without
// limit; the shape matters for diagnosis, not the tail.
func boundedEvidenceValue(value any) any {
	text := stringify(normalizeValue(value))
	if len(text) <= catalogEvidenceFieldLimit {
		return normalizeValue(value)
	}
	return text[:catalogEvidenceFieldLimit] + "…"
}

func mergeActionEvidence(values ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, value := range values {
		for key, item := range value {
			out[key] = item
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func catalogCardEvidence(tool string, args map[string]any, out any) map[string]any {
	if tool != toolQueryCatalog || len(detailIDsFromArgs(args)) == 0 {
		return nil
	}
	cards := catalogCards(out)
	if len(cards) == 0 {
		return nil
	}
	evidence := make(map[string]any, len(cards))
	for _, card := range cards {
		id := stringFromMap(card, "id")
		if id == "" {
			continue
		}
		fields := map[string]any{}
		for _, key := range []string{"examples_json", "evidence_json", "safety_json", "suggested_next_json", "summary"} {
			if value := truncateString(stringFromMap(card, key), catalogEvidenceFieldLimit); value != "" {
				fields[key] = value
			}
		}
		if len(fields) != 0 {
			evidence[id] = fields
		}
		if len(evidence) >= catalogEvidenceCardLimit {
			break
		}
	}
	if len(evidence) == 0 {
		return nil
	}
	return evidence
}

const (
	// Bounds keep an episode file readable and stop a large card from dominating
	// the trajectory it is meant to explain.
	catalogEvidenceFieldLimit = 1500
	catalogEvidenceCardLimit  = 4
)

// malformedWatchSubscriptionString reports whether an inlined gj_watch
// subscription contains an unescaped double quote, which closes the string early
// and makes the surrounding mutation unparseable. A subscription supplied through
// a variable carries no inline string and is never flagged.
// closesInputStringValue reports whether what follows a closing quote is a valid
// continuation of a GraphQL input object. Commas between input fields are optional,
// so the next field's name is just as legal as a comma — treating only commas and
// braces as valid rejected every multi-line watch mutation, which is the natural
// shape a model writes and was the majority of them.
func closesInputStringValue(remainder string) bool {
	remainder = strings.TrimSpace(remainder)
	if remainder == "" || strings.HasPrefix(remainder, ",") || strings.HasPrefix(remainder, "}") || strings.HasPrefix(remainder, ")") {
		return true
	}
	// A following field is name: value. Anything else after the string — a bare
	// word, another quote, a closing paren of an inner selection — means the string
	// ended somewhere it should not have.
	name := 0
	for name < len(remainder) && (remainder[name] == '_' ||
		(remainder[name] >= 'a' && remainder[name] <= 'z') ||
		(remainder[name] >= 'A' && remainder[name] <= 'Z') ||
		(remainder[name] >= '0' && remainder[name] <= '9')) {
		name++
	}
	if name == 0 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(remainder[name:]), ":")
}

func malformedWatchSubscriptionString(query string) bool {
	lower := strings.ToLower(query)
	at := strings.Index(lower, "query:")
	if at == -1 {
		return false
	}
	rest := strings.TrimSpace(query[at+len("query:"):])
	if !strings.HasPrefix(rest, `"`) {
		return false
	}
	rest = rest[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '\\':
			i++ // an escaped character cannot close the string
		case '"':
			// Where the string closes matters: a close followed by the mutation
			// continuing is correct, anything else means it ended early.
			return !closesInputStringValue(rest[i+1:])
		}
	}
	return true // unterminated
}

// systemRootHelpIDs names the help contract each raw-queryable gj_* surface is
// documented by. Roots absent from this map get no supply and keep the normal
// discovery refusal.
var systemRootHelpIDs = map[string]string{
	systemRootWatch:      "help:watches",
	systemRootWatchEvent: "help:watches",
	"gj_task":            "help:tasks",
	"gj_artifact":        "help:artifacts",
}

// supplySystemRootDiscovery discharges the raw-GraphQL discovery prerequisite
// for a query that touches only gj_* system roots, by fetching those roots'
// help contracts itself and handing them back. The requirement survives — the
// evidence is recorded exactly as a model-driven lookup would record it — and
// it is supplied at most once per run so a genuinely stuck run still fails
// loudly.
func (r *protocolRuntime) supplySystemRootDiscovery(ctx context.Context, query string, queryArgs map[string]any) (any, bool) {
	if r == nil || r.state == nil || r.state.systemRootDiscoverySupplied {
		return nil, false
	}
	roots := QueryRootFields(query)
	for _, root := range MutationRootFields(query) {
		roots = appendUniqueString(roots, root)
	}
	if len(roots) == 0 {
		return nil, false
	}
	ids := []any{}
	seen := map[string]bool{}
	for _, root := range roots {
		fold := strings.ToLower(strings.TrimSpace(root))
		help, ok := systemRootHelpIDs[fold]
		if !ok {
			return nil, false
		}
		if !seen[help] {
			seen[help] = true
			ids = append(ids, help)
		}
		if table := r.state.catalogIDForTable(fold); table != "" && !seen[table] {
			seen[table] = true
			ids = append(ids, table)
		}
	}
	args := map[string]any{"ids": ids}
	r.addNamespace(args)
	out, err := r.base.QueryCatalog(ctx, args)
	if err != nil || len(catalogCards(out)) == 0 {
		return nil, false
	}
	r.state.recordCatalog(args, out, false)
	if !r.state.hasCatalogDetailEvidence() {
		return nil, false
	}
	r.state.systemRootDiscoverySupplied = true
	result := map[string]any{
		"graphjin_protocol": "system_root_discovery_supplied",
		"message":           "GraphJin fetched the system-root contract this operation requires. Author the operation from the returned guidance, then execute it.",
		"cards":             normalizeValue(mapValue(normalizeValue(out))["cards"]),
		"next":              "Re-execute the operation now that the system-root contract has been inspected.",
	}
	action := r.state.startAction("model", "execute_graphql", queryArgs)
	r.state.finishAction(action, "execute_graphql", queryArgs, result, nil)
	return result, true
}

// suppliedColumnSummary renders the column names of just-supplied mutation
// targets for an exception message — the one channel a straight-line executor
// program is guaranteed to read.
func (r *protocolRuntime) suppliedColumnSummary(ctx context.Context, tables []string) string {
	parts := make([]string, 0, len(tables))
	for _, table := range tables {
		if columns := r.observedColumnNames(ctx, table); len(columns) != 0 {
			parts = append(parts, table+": "+strings.Join(columns, ", "))
		}
	}
	if len(parts) == 0 {
		return "see this run's catalog evidence"
	}
	return strings.Join(parts, "; ")
}
