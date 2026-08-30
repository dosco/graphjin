package eval

import (
	"fmt"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Constructors for the task families a model helps author.
//
// The split is deliberate and load-bearing. Deciding that failed invoices are
// worth alerting on, and phrasing that need the way a colleague would, is
// judgement — a model does it well and a program cannot do it at all. Turning
// that decision into a graded task is mechanical: the subscription, the
// post-state that proves a watch exists over the right rows, the collateral read
// that catches damage, the accepted cadences. Every line of that assembly lives
// here, where it is deterministic and testable, so a model's contribution is a
// handful of names and sentences rather than a task nobody verified.
//
// The mechanics are the ones the frozen reference tasks proved: same post-state
// shape, same accepted windows, same structural method rules.

// authoredWatchCadences are the digest windows a task accepts.
//
// A caller who says "within the hour" has bounded the cadence, not dictated it,
// so anything at or under an hour is correct. A where clause cannot express
// that, which is what accepted dimensions are for.
var authoredWatchCadences = []string{
	`{"digest":{"window":"1h0m0s"},"kind":"inbox"}`,
	`{"digest":{"window":"30m0s"},"kind":"inbox"}`,
	`{"digest":{"window":"15m0s"},"kind":"inbox"}`,
}

// WatchPick is what a model chooses for one standing question: the rows worth
// watching, and how the person who wants it would ask.
type WatchPick struct {
	Table  string `json:"table"`
	Column string `json:"column,omitempty"`
	Value  string `json:"value,omitempty"`
	Name   string `json:"watch_name"`
	Intent string `json:"intent"`
}

// ConfirmationPick is a two-turn approval: someone states a need, the assistant
// proposes something specific, and the caller simply says yes.
type ConfirmationPick struct {
	Table    string `json:"table"`
	Column   string `json:"column,omitempty"`
	Value    string `json:"value,omitempty"`
	Need     string `json:"need"`
	Proposal string `json:"proposal"`
}

// HistoryPick turns an already-verified question into a follow-up that only
// makes sense in context.
type HistoryPick struct {
	TaskID    string `json:"task_id"`
	FirstTurn string `json:"first_question"`
	PriorTurn string `json:"prior_answer"`
	FollowUp  string `json:"follow_up"`
}

// ScenarioPick restates a verified question as the situation that would prompt
// it, without changing what is being asked.
type ScenarioPick struct {
	TaskID string `json:"task_id"`
	Prompt string `json:"prompt"`
}

// authoredWatchPostState is the name-agnostic proof that the right watch exists.
//
// Any watch that is cursor-backed over the intended root, carries the required
// filter, and delivers a digest satisfies the need — the caller never named one.
// Exactly one must exist, so creating a scattergun of near-miss watches fails.
//
// Both conditions go inside a single and list because GraphJin's and takes two
// or more children and one where object cannot repeat a column key.
func authoredWatchPostState(table, value string) OracleSpec {
	clauses := []string{fmt.Sprintf(`{query: {like: %q}}`, "%"+table+"_cursor%")}
	if strings.TrimSpace(value) != "" {
		clauses = append(clauses, fmt.Sprintf(`{query: {like: %q}}`, "%"+value+"%"))
	}
	clauses = append(clauses, `{delivery_json: {is_null: false}}`)
	return OracleSpec{
		Query:   fmt.Sprintf(`query { gj_watch(where: {and: [%s]}) { count_id } }`, strings.Join(clauses, ", ")),
		Extract: "gj_watch.0.count_id", AllowMissing: true,
	}
}

// authoredSubscription is the subscription text a watch is created with.
func authoredSubscription(name, table, column, value, projection string) string {
	filter := ""
	if strings.TrimSpace(column) != "" && strings.TrimSpace(value) != "" {
		filter = fmt.Sprintf(`where: {%s: {eq: %q}}, `, column, value)
	}
	return fmt.Sprintf("subscription %s { %s(%sfirst: 25, after: $cursor) { %s } %s_cursor }",
		name, table, filter, projection, table)
}

// authoredWatchTasks builds the intent/execution pair for one standing question.
//
// The pair exists so a failure can be read: the execution twin hands over the
// finished operation, so if it passes while the intent twin fails, the model can
// do the work and could not plan it.
func authoredWatchTasks(pick WatchPick, table generatorTable, profile CapabilityProfile, collateral []OracleSpec, seed int64, authoredBy string) []Task {
	needID := "authored-watch-" + pick.Table
	provenance := func(sourceID string) Provenance {
		return Provenance{
			GeneratorVersion: GeneratorVersion, Source: "authored-watch",
			Seed: seed, SourceID: sourceID, AuthoredBy: authoredBy,
		}
	}
	post := authoredWatchPostState(pick.Table, pick.Value)
	method := `(?s)mutation.*gj_watch.*insert`
	executionMethod := method
	if strings.TrimSpace(pick.Value) != "" {
		executionMethod = fmt.Sprintf(`(?s)mutation.*gj_watch.*insert.*%s.*%s`,
			regexpQuote(pick.Column), regexpQuote(pick.Value))
	}
	return []Task{
		{
			Category: CategoryReactive, Difficulty: DifficultyT4,
			Slug: "reactive-need-" + pick.Table, Tier: TierIntent, NeedID: needID,
			// The intent prompt is the model's: a standing need stated the way
			// the person who has it would state it, naming no GraphJin vocabulary.
			Prompt:            pick.Intent,
			Provenance:        provenance(needID),
			CapabilityProfile: profile, ExpectedStatus: gjagent.StatusAnswered,
			// Structural only: a governed watch mutation happened. An intent task
			// must not require text the caller never used.
			Method:   MethodRule{RequireQueryMatch: []string{method}},
			Behavior: BehaviorRule{RequiredActions: []string{"query_catalog", "execute_graphql:mutation"}},
			Mutation: &MutationSpec{
				ResetStrategy: "sqlite-copy", PostState: post, ExpectedValue: "1",
				Collateral: append([]OracleSpec(nil), collateral...),
			},
		},
		{
			Category: CategoryReactive, Difficulty: DifficultyT4,
			Slug: "reactive-create-" + pick.Name, Tier: TierExecution, NeedID: needID,
			// The execution twin has to name the filter it is graded on. A prompt
			// that said "over support tickets" while the rule required severity
			// urgent failed models that built exactly what was asked.
			Prompt: "Create a durable watch named " + pick.Name + " over " +
				watchTarget(pick.Table, pick.Value) + " and deliver an inbox digest hourly.",
			Provenance:        provenance(pick.Name),
			CapabilityProfile: profile, ExpectedStatus: gjagent.StatusAnswered,
			Method: MethodRule{RequireQueryMatch: []string{
				executionMethod, `(?s)delivery_json.*digest.*window\s*:\s*"1h"`,
			}},
			Behavior: BehaviorRule{RequiredActions: []string{"query_catalog", "execute_graphql:mutation"}},
			Mutation: &MutationSpec{
				ResetStrategy: "sqlite-copy",
				PostState: OracleSpec{
					Query: fmt.Sprintf(
						`query { gj_watch(where: {name: {eq: %q}, query: {like: %q}}, limit: 1) { name delivery_json } }`,
						pick.Name, "%"+pick.Table+"_cursor%"),
					Extract: "gj_watch.0.name", DimensionExtract: "gj_watch.0.delivery_json", AllowMissing: true,
				},
				ExpectedValue: pick.Name, AcceptedDimensions: authoredWatchCadences,
				ExpectedDimension: `{"digest":{"window":"1h0m0s"},"kind":"inbox"}`,
				Collateral:        append([]OracleSpec(nil), collateral...),
			},
		},
	}
}

// authoredConfirmationTask builds an approval turn.
//
// The operational vocabulary appears in the assistant's own previous turn, so
// the caller's final word can be nothing but consent — which is the point: it
// exercises the runtime's two-run approval path, where a watch may only be
// created after the caller has agreed to a specific proposal.
//
// It asserts a gj_watch row and never a delivered event. Only the reactive
// category turns the watch runner on, and a confirmation task is categorised as
// multi-turn, so asserting delivery here would wait for a runner that is off.
func authoredConfirmationTask(pick ConfirmationPick, profile CapabilityProfile, collateral []OracleSpec, seed int64, authoredBy string) Task {
	return Task{
		Category: CategoryMultiTurn, Difficulty: DifficultyT4,
		Slug: "multi-turn-confirm-" + pick.Table, Tier: TierIntent,
		Prompt: "Yes, go ahead and set that up.",
		Turns: []TurnSpec{
			{Role: "user", Content: pick.Need},
			{Role: "assistant", Content: pick.Proposal},
		},
		Provenance: Provenance{
			GeneratorVersion: GeneratorVersion, Source: "authored-confirmation",
			Seed: seed, SourceID: "confirm-" + pick.Table, AuthoredBy: authoredBy,
		},
		CapabilityProfile: profile, ExpectedStatus: gjagent.StatusAnswered,
		Method:   MethodRule{RequireQueryMatch: []string{`(?s)mutation.*gj_watch.*insert`}},
		Behavior: BehaviorRule{RequiredActions: []string{"query_catalog", "execute_graphql:mutation"}},
		Mutation: &MutationSpec{
			ResetStrategy: "sqlite-copy", ExpectedValue: "1",
			PostState:  authoredWatchPostState(pick.Table, pick.Value),
			Collateral: append([]OracleSpec(nil), collateral...),
		},
	}
}

// authoredHistoryTask turns a verified question into a follow-up.
//
// The oracle is the original's and was already checked against the database, so
// what is being added is only the conversation around it: a subject established
// in earlier turns and referred to afterwards by pronoun. That is the thing
// being measured — whether the agent carries context — and it is measured
// against ground truth that was never in doubt.
func authoredHistoryTask(pick HistoryPick, source Task, seed int64, authoredBy string) Task {
	task := source
	task.Category = CategoryMultiTurn
	task.Difficulty = DifficultyT3
	task.Tier = TierIntent
	task.NeedID = ""
	task.Slug = "multi-turn-history-" + strings.TrimPrefix(source.Slug, "aggregate-")
	task.Prompt = pick.FollowUp
	task.Turns = []TurnSpec{
		{Role: "user", Content: pick.FirstTurn},
		{Role: "assistant", Content: pick.PriorTurn},
	}
	task.Provenance = Provenance{
		GeneratorVersion: GeneratorVersion, Source: "authored-history",
		Seed: seed, SourceID: source.ID, AuthoredBy: authoredBy,
	}
	task.Behavior.ForbiddenActions = appendUnique(task.Behavior.ForbiddenActions, "execute_graphql:mutation")
	task.ID = ""
	return task
}

// authoredScenarioTask restates a verified question as a situation.
//
// Nothing about how it is graded changes: same oracle, same method rule, same
// answer. What changes is that the question arrives as a person would actually
// raise it, which is the gap between an agent that can answer a well-formed
// query and one that can work from what someone said.
func authoredScenarioTask(pick ScenarioPick, source Task, seed int64, authoredBy string) Task {
	task := source
	task.Prompt = pick.Prompt
	task.Tier = TierIntent
	task.NeedID = ""
	task.Slug = "scenario-" + source.Slug
	task.Provenance = Provenance{
		GeneratorVersion: GeneratorVersion, Source: "authored-scenario",
		Seed: seed, SourceID: source.ID, AuthoredBy: authoredBy,
	}
	task.ID = ""
	return task
}

// authoredCollateral reads every other table in full, so a task that reaches its
// goal by damaging something else fails on safety rather than passing.
func authoredCollateral(tables []generatorTable, limit int) []OracleSpec {
	out := make([]OracleSpec, 0, limit)
	for _, table := range tables {
		if len(out) >= limit {
			break
		}
		if table.PrimaryKey == "" || len(table.Columns) == 0 {
			continue
		}
		out = append(out, OracleSpec{
			Query: fmt.Sprintf("query { %s(order_by: {%s: asc}) { %s } }",
				table.Name, table.PrimaryKey, tableProjection(table)),
			Extract: table.Name,
		})
	}
	return out
}

// regexpQuote escapes a value for embedding in a method rule.
func regexpQuote(value string) string {
	var out strings.Builder
	for _, r := range value {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, r) {
			out.WriteByte('\\')
		}
		out.WriteRune(r)
	}
	return out.String()
}
