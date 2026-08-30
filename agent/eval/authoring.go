package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Authoring tasks with a model's help.
//
// The model chooses and phrases; the engine constructs, checks and verifies.
// Nothing a model returns is trusted: every table, column and value it names
// must exist in the census it was given, every prose field must read like a
// person rather than a query, and every task it produces is resolved against
// the live database before it can enter a suite. A model that invents a column
// produces a rejection with a reason, not a task.
//
// That asymmetry is the point. Judgement is the part a model is good at and a
// program cannot do; verification is the part a program does perfectly and a
// model cannot be relied on for.

// OneShotFunc makes a single model call. It is injected rather than imported so
// this package keeps its narrow dependencies, and so tests can author without a
// provider.
type OneShotFunc func(ctx context.Context, signature string, values map[string]any) (map[string]any, error)

// AuthoringKind names a family a model can author.
type AuthoringKind string

const (
	AuthoringWatch        AuthoringKind = "watch"
	AuthoringConfirmation AuthoringKind = "confirmation"
	AuthoringHistory      AuthoringKind = "history"
	AuthoringScenario     AuthoringKind = "scenario"
)

var AuthoringKinds = []AuthoringKind{AuthoringWatch, AuthoringConfirmation, AuthoringHistory, AuthoringScenario}

func ParseAuthoringKinds(values []string) ([]AuthoringKind, error) {
	known := map[string]AuthoringKind{}
	for _, kind := range AuthoringKinds {
		known[string(kind)] = kind
	}
	var out []AuthoringKind
	for _, value := range values {
		value = strings.TrimSpace(strings.ToLower(value))
		if value == "" {
			continue
		}
		kind, ok := known[value]
		if !ok {
			names := make([]string, 0, len(AuthoringKinds))
			for _, candidate := range AuthoringKinds {
				names = append(names, string(candidate))
			}
			return nil, fmt.Errorf("unknown authoring kind %q; known: %s", value, strings.Join(names, ", "))
		}
		out = append(out, kind)
	}
	if len(out) == 0 {
		return AuthoringKinds, nil
	}
	return out, nil
}

// AuthoringOptions configures one authoring pass.
type AuthoringOptions struct {
	Kinds      []AuthoringKind
	Count      int
	Seed       int64
	AuthoredBy string
}

// AuthoringReport records what was produced and what was refused. A refusal
// without a reason is indistinguishable from a model that had nothing to say.
type AuthoringReport struct {
	ByKind     map[AuthoringKind]int
	Rejections []string
	Notes      []string
}

func (r *AuthoringReport) reject(kind AuthoringKind, detail string) {
	r.Rejections = append(r.Rejections, string(kind)+": "+detail)
}

// SchemaCensus is everything a model is allowed to build a task from, and the
// only thing its answers are checked against.
type SchemaCensus struct {
	Tables        []generatorTable
	ClosedSets    map[string]map[string][]string
	Relationships []generatorRelationship
	SavedQueries  []string
	Profile       CapabilityProfile
}

// BuildCensus reads the census out of a catalog snapshot.
func BuildCensus(snapshot CatalogSnapshot) SchemaCensus {
	census := SchemaCensus{
		Tables:        catalogTables(snapshot.Rows),
		ClosedSets:    observedValueColumns(snapshot.Rows),
		Profile:       writeCapabilityProfile(snapshot),
		Relationships: nil,
	}
	census.Relationships = catalogRelationships(snapshot.Rows, census.Tables)
	for _, row := range snapshot.Rows {
		if row.Kind == "saved_query" && strings.TrimSpace(row.Name) != "" {
			census.SavedQueries = append(census.SavedQueries, row.Name)
		}
	}
	sort.Strings(census.SavedQueries)
	return census
}

// Digest renders the census as the text a model is given.
func (c SchemaCensus) Digest() string {
	var out strings.Builder
	for _, table := range c.Tables {
		names := make([]string, 0, len(table.Columns))
		for _, column := range table.Columns {
			names = append(names, column.Name)
		}
		fmt.Fprintf(&out, "table %s: %s\n", table.Name, strings.Join(names, ", "))
		for _, column := range sortedColumnNames(c.ClosedSets[table.Name], 0) {
			fmt.Fprintf(&out, "  %s.%s holds only: %s\n", table.Name, column,
				strings.Join(c.ClosedSets[table.Name][column], ", "))
		}
	}
	for _, edge := range c.Relationships {
		fmt.Fprintf(&out, "relationship: %s.%s -> %s.%s\n", edge.FromTable, edge.FromColumn, edge.ToTable, edge.ToColumn)
	}
	return out.String()
}

func (c SchemaCensus) table(name string) (generatorTable, bool) {
	for _, table := range c.Tables {
		if table.Name == name {
			return table, true
		}
	}
	return generatorTable{}, false
}

// holdsValue reports whether a column is published as holding a value. This is
// what stops a model filtering on a state the business does not have.
func (c SchemaCensus) holdsValue(table, column, value string) bool {
	for _, candidate := range c.ClosedSets[table][column] {
		if candidate == value {
			return true
		}
	}
	return false
}

// canWatch reports whether the caller may create watches at all. Authoring
// reactive tasks for a caller who cannot create one produces tasks nothing can
// pass.
func (c SchemaCensus) canWatch() bool {
	return !c.Profile.ReadOnly &&
		contains(c.Profile.AvailableSystemRoots, "gj_watch") &&
		contains(c.Profile.AllowedActions, "gj_watch.insert")
}

var (
	authoringIdentifierPattern = regexp.MustCompile(`(?i)\bgj_[a-z_]+\b`)
	authoringNamePattern       = regexp.MustCompile(`^[a-z][a-z0-9_]{2,48}$`)
)

// checkProse is the gate every model-written sentence passes.
//
// A task's prompt is the one part a caller actually reads, so it has to sound
// like a person with a problem. Naming GraphJin's own vocabulary is the specific
// failure worth catching: it turns an intent task, which measures whether the
// agent can plan, into an instruction it only has to follow.
func checkProse(text string, minWords int) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return fmt.Errorf("empty")
	}
	if len(strings.Fields(trimmed)) < minWords {
		return fmt.Errorf("too short to be a real request: %q", trimmed)
	}
	if len(trimmed) > 600 {
		return fmt.Errorf("too long to be a caller's request (%d characters)", len(trimmed))
	}
	if match := authoringIdentifierPattern.FindString(trimmed); match != "" {
		return fmt.Errorf("names GraphJin's own vocabulary (%q)", match)
	}
	// Only words that can mean nothing else are banned.
	//
	// The first version of this list also refused "subscription", and a live run
	// promptly wrote a perfectly good need about a SaaS company's subscriptions
	// table. Words like subscription, watch and alert are how people describe
	// wanting to be told about something — refusing them would reject the very
	// phrasing this is asking for, and would refuse any business whose domain
	// happens to contain the word.
	for _, banned := range []string{"graphql", "execute_graphql", "query_catalog", "saved query"} {
		if strings.Contains(strings.ToLower(trimmed), banned) {
			return fmt.Errorf("names the mechanism rather than the need (%q)", banned)
		}
	}
	return nil
}

// AuthorFamilies asks a model for picks and turns the ones that survive into
// tasks. The tasks are not yet verified: the caller resolves them against a live
// instance, exactly as generated tasks are.
func AuthorFamilies(ctx context.Context, call OneShotFunc, census SchemaCensus, readPool []Task, opts AuthoringOptions) ([]Task, AuthoringReport, error) {
	report := AuthoringReport{ByKind: map[AuthoringKind]int{}}
	if call == nil {
		return nil, report, fmt.Errorf("authoring needs a model call")
	}
	if len(census.Tables) == 0 {
		return nil, report, fmt.Errorf("authoring needs a schema census")
	}
	count := opts.Count
	if count <= 0 {
		count = 4
	}
	kinds := opts.Kinds
	if len(kinds) == 0 {
		kinds = AuthoringKinds
	}
	collateral := authoredCollateral(census.Tables, 4)

	var tasks []Task
	for _, kind := range kinds {
		produced, err := authorKind(ctx, call, kind, census, readPool, collateral, count, opts, &report)
		if err != nil {
			return nil, report, fmt.Errorf("%s authoring: %w", kind, err)
		}
		report.ByKind[kind] = len(produced)
		tasks = append(tasks, produced...)
	}
	return tasks, report, nil
}

func authorKind(ctx context.Context, call OneShotFunc, kind AuthoringKind, census SchemaCensus,
	readPool []Task, collateral []OracleSpec, count int, opts AuthoringOptions, report *AuthoringReport) ([]Task, error) {
	switch kind {
	case AuthoringWatch, AuthoringConfirmation:
		if !census.canWatch() {
			report.Notes = append(report.Notes,
				string(kind)+" skipped: this caller cannot create watches, so such tasks would be unpassable")
			return nil, nil
		}
	case AuthoringHistory, AuthoringScenario:
		if len(readPool) == 0 {
			report.Notes = append(report.Notes,
				string(kind)+" skipped: no verified questions to build on")
			return nil, nil
		}
	}

	signature, values := authoringRequest(kind, census, readPool, count)
	fields, err := call(ctx, signature, values)
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(gjagent.StringField(fields, "picks_json"))
	if raw == "" {
		return nil, fmt.Errorf("model returned no picks")
	}

	switch kind {
	case AuthoringWatch:
		return buildWatchTasks(raw, census, collateral, opts, report)
	case AuthoringConfirmation:
		return buildConfirmationTasks(raw, census, collateral, opts, report)
	case AuthoringHistory:
		return buildHistoryTasks(raw, readPool, opts, report)
	case AuthoringScenario:
		return buildScenarioTasks(raw, readPool, opts, report)
	}
	return nil, fmt.Errorf("unknown authoring kind %q", kind)
}

func authoringRequest(kind AuthoringKind, census SchemaCensus, readPool []Task, count int) (string, map[string]any) {
	switch kind {
	case AuthoringWatch:
		return watchAuthoringSignature, map[string]any{"census": census.Digest(), "count": fmt.Sprint(count)}
	case AuthoringConfirmation:
		return confirmationAuthoringSignature, map[string]any{"census": census.Digest(), "count": fmt.Sprint(count)}
	case AuthoringHistory:
		return historyAuthoringSignature, map[string]any{"tasks": readPoolDigest(readPool), "count": fmt.Sprint(count)}
	default:
		return scenarioAuthoringSignature, map[string]any{"tasks": readPoolDigest(readPool), "count": fmt.Sprint(count)}
	}
}

// readPoolDigest lists verified questions a model may build on.
func readPoolDigest(pool []Task) string {
	var out strings.Builder
	limit := len(pool)
	if limit > 40 {
		limit = 40
	}
	for _, task := range pool[:limit] {
		fmt.Fprintf(&out, "%s: %s\n", task.ID, task.Prompt)
	}
	return out.String()
}

func buildWatchTasks(raw string, census SchemaCensus, collateral []OracleSpec, opts AuthoringOptions, report *AuthoringReport) ([]Task, error) {
	var picks []WatchPick
	if err := decodeFencedJSON(raw, &picks); err != nil {
		return nil, err
	}
	var tasks []Task
	seen := map[string]bool{}
	for _, pick := range picks {
		table, ok := census.table(pick.Table)
		if !ok {
			report.reject(AuthoringWatch, fmt.Sprintf("table %q is not in the schema", pick.Table))
			continue
		}
		if pick.Value != "" && !census.holdsValue(pick.Table, pick.Column, pick.Value) {
			report.reject(AuthoringWatch, fmt.Sprintf("%s.%s does not hold %q", pick.Table, pick.Column, pick.Value))
			continue
		}
		if !authoringNamePattern.MatchString(pick.Name) {
			report.reject(AuthoringWatch, fmt.Sprintf("watch name %q is not a plain lowercase identifier", pick.Name))
			continue
		}
		if err := checkProse(pick.Intent, 8); err != nil {
			report.reject(AuthoringWatch, fmt.Sprintf("intent for %s %v", pick.Table, err))
			continue
		}
		if seen[pick.Table] {
			report.reject(AuthoringWatch, fmt.Sprintf("a second watch was proposed for %s", pick.Table))
			continue
		}
		seen[pick.Table] = true
		tasks = append(tasks, authoredWatchTasks(pick, table, census.Profile, collateral, opts.Seed, opts.AuthoredBy)...)
	}
	return tasks, nil
}

func buildConfirmationTasks(raw string, census SchemaCensus, collateral []OracleSpec, opts AuthoringOptions, report *AuthoringReport) ([]Task, error) {
	var picks []ConfirmationPick
	if err := decodeFencedJSON(raw, &picks); err != nil {
		return nil, err
	}
	var tasks []Task
	seen := map[string]bool{}
	for _, pick := range picks {
		if _, ok := census.table(pick.Table); !ok {
			report.reject(AuthoringConfirmation, fmt.Sprintf("table %q is not in the schema", pick.Table))
			continue
		}
		if pick.Value != "" && !census.holdsValue(pick.Table, pick.Column, pick.Value) {
			report.reject(AuthoringConfirmation, fmt.Sprintf("%s.%s does not hold %q", pick.Table, pick.Column, pick.Value))
			continue
		}
		if err := checkProse(pick.Need, 8); err != nil {
			report.reject(AuthoringConfirmation, fmt.Sprintf("need for %s %v", pick.Table, err))
			continue
		}
		// The proposal is the assistant's own turn, so it may name a watch and a
		// cadence — that vocabulary is what makes agreeing to it meaningful.
		if strings.TrimSpace(pick.Proposal) == "" || len(strings.Fields(pick.Proposal)) < 6 {
			report.reject(AuthoringConfirmation, fmt.Sprintf("proposal for %s is too short to agree to", pick.Table))
			continue
		}
		if seen[pick.Table] {
			report.reject(AuthoringConfirmation, fmt.Sprintf("a second confirmation was proposed for %s", pick.Table))
			continue
		}
		seen[pick.Table] = true
		tasks = append(tasks, authoredConfirmationTask(pick, census.Profile, collateral, opts.Seed, opts.AuthoredBy))
	}
	return tasks, nil
}

func buildHistoryTasks(raw string, readPool []Task, opts AuthoringOptions, report *AuthoringReport) ([]Task, error) {
	var picks []HistoryPick
	if err := decodeFencedJSON(raw, &picks); err != nil {
		return nil, err
	}
	byID := map[string]Task{}
	for _, task := range readPool {
		byID[task.ID] = task
	}
	var tasks []Task
	for _, pick := range picks {
		source, ok := byID[pick.TaskID]
		if !ok {
			report.reject(AuthoringHistory, fmt.Sprintf("task %q is not one of the verified questions", pick.TaskID))
			continue
		}
		if err := checkProse(pick.FirstTurn, 4); err != nil {
			report.reject(AuthoringHistory, fmt.Sprintf("first turn %v", err))
			continue
		}
		if err := checkProse(pick.FollowUp, 3); err != nil {
			report.reject(AuthoringHistory, fmt.Sprintf("follow-up %v", err))
			continue
		}
		// A follow-up that repeats the subject is not a follow-up: it would pass
		// without the agent carrying anything from the earlier turns.
		if !mentionsPronoun(pick.FollowUp) {
			report.reject(AuthoringHistory,
				fmt.Sprintf("follow-up %q never refers back, so it does not test carry-over", pick.FollowUp))
			continue
		}
		tasks = append(tasks, authoredHistoryTask(pick, source, opts.Seed, opts.AuthoredBy))
	}
	return tasks, nil
}

func buildScenarioTasks(raw string, readPool []Task, opts AuthoringOptions, report *AuthoringReport) ([]Task, error) {
	var picks []ScenarioPick
	if err := decodeFencedJSON(raw, &picks); err != nil {
		return nil, err
	}
	byID := map[string]Task{}
	for _, task := range readPool {
		byID[task.ID] = task
	}
	var tasks []Task
	for _, pick := range picks {
		source, ok := byID[pick.TaskID]
		if !ok {
			report.reject(AuthoringScenario, fmt.Sprintf("task %q is not one of the verified questions", pick.TaskID))
			continue
		}
		if err := checkProse(pick.Prompt, 8); err != nil {
			report.reject(AuthoringScenario, fmt.Sprintf("scenario %v", err))
			continue
		}
		// A scenario that still names the table or column is the original
		// question with decoration, and measures nothing new.
		if leaked := leakedIdentifier(pick.Prompt, source); leaked != "" {
			report.reject(AuthoringScenario,
				fmt.Sprintf("scenario still names %q, so it asks the question rather than describing the situation", leaked))
			continue
		}
		tasks = append(tasks, authoredScenarioTask(pick, source, opts.Seed, opts.AuthoredBy))
	}
	return tasks, nil
}

var pronounPattern = regexp.MustCompile(`(?i)\b(it|its|it's|that|those|these|them|they|their|the same|there)\b`)

func mentionsPronoun(text string) bool { return pronounPattern.MatchString(text) }

// leakedIdentifier returns a schema identifier the rewritten prompt still names.
func leakedIdentifier(prompt string, source Task) string {
	lowered := strings.ToLower(prompt)
	if source.Oracle == nil {
		return ""
	}
	for _, token := range identifierTokens(source.Oracle.Query) {
		if strings.Contains(lowered, token) {
			return token
		}
	}
	return ""
}

// identifierTokens pulls snake_case identifiers out of a query. Single words are
// ignored: a table called "accounts" is also an ordinary English word, and
// refusing every prompt that says "accounts" would refuse every real scenario.
func identifierTokens(query string) []string {
	var out []string
	for _, match := range regexp.MustCompile(`[a-z]+(?:_[a-z0-9]+)+`).FindAllString(strings.ToLower(query), -1) {
		if strings.HasPrefix(match, "gj_") || strings.HasPrefix(match, "count_") ||
			strings.HasPrefix(match, "sum_") || strings.HasPrefix(match, "avg_") ||
			strings.HasPrefix(match, "min_") || strings.HasPrefix(match, "max_") {
			continue
		}
		out = append(out, match)
	}
	return out
}

// MarshalAuthoringPicks is used by tests and by callers that pre-compute picks.
func MarshalAuthoringPicks(picks any) string {
	encoded, err := json.Marshal(picks)
	if err != nil {
		return "[]"
	}
	return string(encoded)
}
