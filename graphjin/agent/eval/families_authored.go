package eval

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

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

// FilePick is a question whose answer is in two places: a count the database
// knows, and a rule somebody wrote down in a document.
//
// The model chooses which rule would plausibly be written down for this
// business and how someone would ask about it. It does not choose where the
// document lives or what it is called — the engine writes the document, so the
// rule is true by construction rather than by hope.
type FilePick struct {
	FileRoot     string `json:"file_root"`
	Table        string `json:"table"`
	Column       string `json:"column,omitempty"`
	Value        string `json:"value,omitempty"`
	PolicyTopic  string `json:"policy_topic"`
	PolicyAnswer string `json:"policy_answer"`
	Intent       string `json:"intent"`
	Execution    string `json:"execution"`
}

// AuthoredFile is a document the environment must contain before its task can
// be verified. It is written by the engine from the model's chosen answer, so
// what the document says and what the task grades cannot drift apart.
type AuthoredFile struct {
	FileRoot string
	Key      string
	Contents string
}

// filePolicyReadPattern accepts every query shape that genuinely reads a file.
//
// An explicit inline_data: true argument counts, and so does a selection — with
// or without an argument list — that includes data or text, which the file
// bridge treats as a request for inline content. Both are real reads. An
// earlier version of this rule anchored both alternatives behind an argument
// list, and the argument-free form every model actually writes scored method
// false in twelve of twelve episodes of the strongest model's run while
// returning correct answers. Widening it back is not leniency: a rule that
// fails a correct method measures the rule.
func filePolicyReadPattern(root string) string {
	return `(?s)` + regexp.QuoteMeta(root) +
		`\s*(?:\([^)]*inline_data\s*:\s*true|(?:\([^)]*\))?\s*\{[^{}]*\b(?:data|text)\b)`
}

// authoredFileKey names the document a file task is graded against. The engine
// chooses it, never the model: a key the model invented could collide with a
// real document, and a task whose ground truth depends on a file somebody else
// wrote is not verified, it is assumed.
func authoredFileKey(table string) string { return "authored-policy-" + table + ".md" }

// authoredPolicyDocument writes the document the answer lives in.
//
// It reads as an operating document because that is what makes the task real:
// the agent has to find the requirement inside prose, not parse a field. The
// answer is planted verbatim on its own line so the grading literal and the
// document cannot disagree.
func authoredPolicyDocument(pick FilePick) string {
	scope := humanize(pick.Table)
	if strings.TrimSpace(pick.Value) != "" {
		scope = pick.Value + " " + scope
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# %s\n\n", capitalizeFirst(strings.TrimSpace(pick.PolicyTopic)))
	fmt.Fprintf(&out, "This document records the standard the team operates to. "+
		"It is the authority on the question below; the database records what is "+
		"actually happening, not what is required.\n\n")
	out.WriteString("## Requirement\n\n")
	fmt.Fprintf(&out, "Requirement: %s.\n\n", strings.TrimRight(strings.TrimSpace(pick.PolicyAnswer), "."))
	out.WriteString("## Scope\n\n")
	fmt.Fprintf(&out, "Applies to %s. Anything outside that scope is handled case by case.\n\n", scope)
	out.WriteString("## Review\n\nReviewed each quarter by the operations team.\n")
	return out.String()
}

// authoredFileTasks builds the intent/execution pair for a question no single
// source can answer.
//
// The database half is counted by the oracle; the document half is graded
// against the literal the engine planted. What is measured is whether the agent
// noticed a second source existed at all — which is why the intent prompt may
// not name the file, and why the method rule requires the read rather than
// trusting the answer.
func authoredFileTasks(pick FilePick, table generatorTable, key string, profile CapabilityProfile,
	seed int64, authoredBy string) []Task {
	filter := ""
	if strings.TrimSpace(pick.Value) != "" {
		filter = fmt.Sprintf("(where: {%s: {eq: %q}})", pick.Column, pick.Value)
	}
	oracle := &OracleSpec{
		Query: fmt.Sprintf("query { %s%s { count_%s } %s(key: %q, inline_data: true) { data } }",
			table.Name, filter, table.PrimaryKey, pick.FileRoot, key),
		Extract:          fmt.Sprintf("%s.0.count_%s", table.Name, table.PrimaryKey),
		DimensionLiteral: strings.TrimSpace(pick.PolicyAnswer),
	}
	needID := "authored-file-" + pick.Table
	method := MethodRule{RequireQueryMatch: []string{table.Name, filePolicyReadPattern(pick.FileRoot)}}
	behavior := BehaviorRule{
		RequiredActions:  []string{"query_catalog", "execute_graphql"},
		ForbiddenActions: []string{"execute_graphql:mutation"},
	}
	provenance := func(sourceID string) Provenance {
		return Provenance{
			GeneratorVersion: GeneratorVersion, Source: "authored-file",
			Seed: seed, SourceID: sourceID, AuthoredBy: authoredBy,
		}
	}
	return []Task{
		{
			Category: CategoryCrossSource, Difficulty: DifficultyT4,
			Slug: "cross-source-need-" + pick.Table, Tier: TierIntent, NeedID: needID,
			// Discovering that the rule is written down somewhere, rather than
			// being told to go and read it, is the whole task.
			Prompt:            pick.Intent,
			Provenance:        provenance(needID),
			CapabilityProfile: profile, ExpectedStatus: gjagent.StatusAnswered,
			Oracle: oracle, Answer: AnswerRule{Kind: "number"},
			Method: method, Behavior: behavior,
		},
		{
			Category: CategoryCrossSource, Difficulty: DifficultyT4,
			Slug: "cross-source-file-" + pick.Table, Tier: TierExecution, NeedID: needID,
			// The execution twin says both sources out loud, so a failure here is
			// about combining them rather than about finding them.
			Prompt:            pick.Execution,
			Provenance:        provenance(pick.Table),
			CapabilityProfile: profile, ExpectedStatus: gjagent.StatusAnswered,
			Oracle: oracle, Answer: AnswerRule{Kind: "number"},
			Method: method, Behavior: behavior,
		},
	}
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

// deliveryEligibilityOracle counts the rows a watch would cover.
//
// A cursor-backed subscription delivers its first event from the initial page,
// so a watch over rows that do not exist never fires and the episode can only
// time out. The reference suite solved this for its one empty root by inserting
// a seed row; that does not generalise, because inventing a row for an
// arbitrary schema means inventing every not-null column in it. Counting first
// and refusing is the generalisation: a delivery task is authored only where
// the database already has something to deliver.
//
// The second return is false when the table has no primary key to count.
func deliveryEligibilityOracle(table generatorTable, pick WatchPick) (OracleSpec, bool) {
	if table.PrimaryKey == "" {
		return OracleSpec{}, false
	}
	filter := ""
	if strings.TrimSpace(pick.Value) != "" {
		filter = fmt.Sprintf("(where: {%s: {eq: %q}})", pick.Column, pick.Value)
	}
	return OracleSpec{
		Query:   fmt.Sprintf("query { %s%s { count_%s } }", table.Name, filter, table.PrimaryKey),
		Extract: fmt.Sprintf("%s.0.count_%s", table.Name, table.PrimaryKey),
	}, true
}

// authoredDeliverySubscription is the watch query the environment installs.
func authoredDeliverySubscription(pick WatchPick, table generatorTable) string {
	args := "first: 25, after: $cursor"
	if strings.TrimSpace(pick.Value) != "" {
		args = fmt.Sprintf("where: {%s: {eq: %q}}, %s", pick.Column, pick.Value, args)
	}
	return fmt.Sprintf("subscription %s { %s(%s) { %s } %s_cursor }",
		pick.Name, table.Name, args, deliveryProjection(pick, table), table.Name)
}

// deliveryProjection selects enough of the row to report what changed: the
// identifier, whatever names it, and the field the watch filters on.
func deliveryProjection(pick WatchPick, table generatorTable) string {
	var fields []string
	for _, candidate := range []string{table.PrimaryKey, table.LabelColumn, pick.Column} {
		if strings.TrimSpace(candidate) == "" || contains(fields, candidate) {
			continue
		}
		fields = append(fields, candidate)
	}
	return strings.Join(fields, " ")
}

// authoredDeliveryTask builds the third task in a watch's family: the standing
// watch already exists and has fired, and the work is to read the event, say
// what it means, and mark it handled.
//
// The watch it installs is a bare inbox watch — a name, a description and a
// subscription, nothing else. Anything carrying a delivery or a workflow needs
// approval before it runs, and the runner pauses what it cannot run without
// saying so, which would present as an agent that never answered.
func authoredDeliveryTask(pick WatchPick, table generatorTable, profile CapabilityProfile,
	collateral []OracleSpec, seed int64, authoredBy string) Task {
	setup := fmt.Sprintf(`mutation { gj_watch(insert: {name: %q, description: %q, query: %q}) { id name enabled } }`,
		pick.Name, "Authored watch over "+humanize(table.Name),
		authoredDeliverySubscription(pick, table))
	ready := OracleSpec{
		Query:   `query { gj_watch_event(where: {seen: {eq: false}}, order_by: {created_at: desc}, limit: 1) { seen } }`,
		Extract: "gj_watch_event.0.seen", AllowMissing: true,
	}
	post := ready
	post.Query = `query { gj_watch_event(order_by: {created_at: desc}, limit: 1) { seen } }`
	return Task{
		Category: CategoryReactive, Difficulty: DifficultyT4,
		Slug: "reactive-delivery-" + table.Name,
		Prompt: fmt.Sprintf(
			"Review the unseen event from the %s watch, report what changed, and mark that event seen after review.",
			humanize(table.Name)),
		Provenance: Provenance{
			GeneratorVersion: GeneratorVersion, Source: "authored-watch-delivery",
			Seed: seed, SourceID: pick.Name, AuthoredBy: authoredBy,
		},
		CapabilityProfile: profile, ExpectedStatus: gjagent.StatusAnswered,
		Method:   MethodRule{RequireQueryMatch: []string{`(?s)mutation.*gj_watch_event.*update.*seen\s*:\s*true`}},
		Behavior: BehaviorRule{RequiredActions: []string{"query_catalog", "execute_graphql", "execute_graphql:mutation"}},
		Mutation: &MutationSpec{
			ResetStrategy: "sqlite-copy",
			Setup:         []GraphQLStep{{Query: setup, WaitAfterMS: 1200}},
			ReadyState:    &ready, ReadyValue: "false", ReadyTimeoutMS: 10000,
			PostState: post, ExpectedValue: "true",
			Collateral: append([]OracleSpec(nil), collateral...),
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

// capitalizeFirst upper-cases the first character, counting characters rather
// than bytes so a title that starts with a non-ASCII letter is not cut in half.
func capitalizeFirst(text string) string {
	for index, r := range text {
		return string(unicode.ToUpper(r)) + text[index+utf8.RuneLen(r):]
	}
	return text
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
