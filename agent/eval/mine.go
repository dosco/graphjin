package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3/queryshape"
)

// Mining turns questions people already asked into tests.
//
// A stored turn holds each question the lead put to the data agent, the
// answer that came back, and the query text the data agent ran. When that
// query computes one figure in the database, it is an answer key: re-run at
// test time it gives the right answer for that day's data, so a test written
// in March still marks correctly in June. Nothing here asks a model anything;
// the engine parses each query and the database runs it.

// Trust tiers. They are reported apart and never summed: an unrated answer
// that a later model disagrees with may be the one that was wrong.
const (
	MineTierConfirmed = "confirmed" // a person marked the answer right
	MineTierIncumbent = "incumbent" // nobody rated it: agreement, not correctness
)

// Why a candidate did not become a test. Each is counted, because the share
// of real questions that cannot be tested yet is what says what to build next.
const (
	MineDropNotAnswered   = "not_answered"
	MineDropRatedWrong    = "rated_wrong"
	MineDropUnsafe        = "unsafe"
	MineDropWrite         = "write"
	MineDropNoQuery       = "no_query"
	MineDropVariables     = "uses_variables"
	MineDropDated         = "dated"
	MineDropList          = "list"
	MineDropSeveral       = "several_figures"
	MineDropShape         = "shape"
	MineDropProse         = "prose"
	MineDropQueryFailed   = "query_failed"
	MineDropEmpty         = "empty_result"
	MineDropAnswerChanged = "answer_mismatch"
	MineDropDuplicate     = "duplicate"
	MineDropAsker         = "asker_unknown"
)

// MineCandidate is what one question to the data agent left behind in a stored
// turn.
type MineCandidate struct {
	ID         string `json:"id"`
	RunID      string `json:"run_id"`
	Delegate   int    `json:"delegate"`
	AskedAt    string `json:"asked_at,omitempty"`
	Origin     string `json:"origin,omitempty"`
	UserPrompt string `json:"user_prompt,omitempty"`
	Question   string `json:"question"`
	Status     string `json:"status"`
	Answer     string `json:"answer,omitempty"`
	Data       any    `json:"data,omitempty"`
	// Queries are the data agent's execute_graphql calls, in order.
	Queries        []MineQuery `json:"queries,omitempty"`
	ViolationCodes []string    `json:"violation_codes,omitempty"`
	// Rating is the person's verdict on the turn: up, down or empty.
	Rating string `json:"rating,omitempty"`
	// AskerUnknown says why the identity the question ran as cannot be
	// rebuilt, so its query cannot be re-run with the rows it saw.
	AskerUnknown string `json:"asker_unknown,omitempty"`
	// TurnQuestions is how many questions the turn asked the data agent. A
	// rating is on the whole turn, so it confirms a single question only.
	TurnQuestions int `json:"turn_questions,omitempty"`
}

// MineQuery is one execute_graphql call as the action trail kept it.
type MineQuery struct {
	Query   string `json:"query"`
	OK      bool   `json:"ok"`
	HasData bool   `json:"has_data"`
}

// MineRunner runs one read query from a candidate and returns its data. The
// server runs it as the person who asked, so the answer key sees exactly the
// rows they saw.
type MineRunner func(ctx context.Context, c MineCandidate, query string) (any, error)

// MinedCase is a test made from a real question. Its fields are the data
// corpus shape ImportCorpora reads, plus where it came from.
type MinedCase struct {
	// ID is the run id and the question's index in its turn.
	ID string `json:"id"`
	// Tier is confirmed (a person marked the answer right) or incumbent (never
	// rated: it measures agreement with the agent that answered, not
	// correctness).
	Tier string `json:"tier"`
	// Prompt is the question the lead put to the data agent.
	Prompt string `json:"prompt"`
	// Category is aggregate, ranking or discovery (a single looked-up value).
	Category       Category `json:"category"`
	ExpectedStatus string   `json:"expected_status"`
	// Oracle is the answer key: the query the data agent ran and where in its
	// result the answer is. It is re-run whenever the test runs.
	Oracle OracleSpec `json:"oracle"`
	// Answer says how an answer is compared with the key's value.
	Answer AnswerRule `json:"answer"`
	// Method says what the database must have computed.
	Method MethodRule  `json:"method"`
	Source MinedSource `json:"source"`
	// Value is what the answer key gave when the case was mined.
	Value     string `json:"value"`
	Dimension string `json:"dimension,omitempty"`
}

// MinedSource says which stored turn a test came from.
type MinedSource struct {
	RunID string `json:"run_id"`
	// Delegate is the question's 1-based index in its turn.
	Delegate int `json:"delegate"`
	// AskedAt is when the turn ran, RFC 3339.
	AskedAt string `json:"asked_at,omitempty"`
	// Origin is chat, routine, continuation or outcome.
	Origin string `json:"origin,omitempty"`
	// UserPrompt is what the person typed; the lead's question came from it.
	UserPrompt string `json:"user_prompt,omitempty"`
	// The data agent's answer is deliberately absent: it stays on the server.
}

// MineSummary counts what happened to every candidate.
type MineSummary struct {
	// Candidates is how many questions to the data agent were read.
	Candidates int `json:"candidates"`
	// Tests is how many became tests.
	Tests int `json:"tests"`
	// ByTier counts tests per tier.
	ByTier map[string]int `json:"by_tier"`
	// Dropped counts the rest per reason: not_answered, rated_wrong, unsafe,
	// write, no_query, uses_variables, dated, list, several_figures, shape,
	// prose, query_failed, empty_result, answer_mismatch, duplicate.
	Dropped map[string]int `json:"dropped"`
}

// MineReport is the result of mining a set of candidates.
type MineReport struct {
	Cases   []MinedCase `json:"cases"`
	Summary MineSummary `json:"summary"`
	// Drops says why each other question did not become a test.
	Drops []MineDrop `json:"drops"`
}

// MineDrop is one question that did not become a test, and why.
type MineDrop struct {
	ID     string `json:"id"`
	Prompt string `json:"prompt"`
	// Reason is one of the summary's dropped keys.
	Reason string `json:"reason"`
	// Detail is the specific cause: the literal date, the guard code, what the
	// answer key gave against what was said.
	Detail string `json:"detail,omitempty"`
}

// MineOutcome is one candidate's fate.
type MineOutcome struct {
	Case   *MinedCase
	Drop   string
	Detail string
}

// Mine runs every candidate through MineOne and keeps one test per question:
// a confirmed one over an incumbent, then the most recently asked.
func Mine(ctx context.Context, candidates []MineCandidate, run MineRunner) MineReport {
	report := MineReport{Summary: MineSummary{ByTier: map[string]int{}, Dropped: map[string]int{}}}
	report.Summary.Candidates = len(candidates)
	byPrompt := map[string]int{}
	for _, candidate := range candidates {
		outcome := MineOne(ctx, candidate, run)
		if outcome.Case == nil {
			report.Summary.Dropped[outcome.Drop]++
			report.Drops = append(report.Drops, MineDrop{ID: candidate.ID, Prompt: candidate.Question, Reason: outcome.Drop, Detail: truncateMineDetail(outcome.Detail)})
			continue
		}
		key := strings.ToLower(strings.Join(strings.Fields(outcome.Case.Prompt), " "))
		if at, ok := byPrompt[key]; ok {
			report.Summary.Dropped[MineDropDuplicate]++
			kept, dropped := report.Cases[at], *outcome.Case
			if MinedCaseOutranks(dropped, kept) {
				kept, dropped = dropped, kept
			}
			report.Cases[at] = kept
			report.Drops = append(report.Drops, MineDrop{ID: dropped.ID, Prompt: dropped.Prompt, Reason: MineDropDuplicate, Detail: "kept " + kept.ID})
			continue
		}
		byPrompt[key] = len(report.Cases)
		report.Cases = append(report.Cases, *outcome.Case)
	}
	sort.SliceStable(report.Cases, func(i, j int) bool { return report.Cases[i].Source.AskedAt > report.Cases[j].Source.AskedAt })
	for _, c := range report.Cases {
		report.Summary.ByTier[c.Tier]++
	}
	report.Summary.Tests = len(report.Cases)
	return report
}

func truncateMineDetail(detail string) string {
	if len(detail) > 300 {
		return detail[:300] + "…"
	}
	return detail
}

// MinedCaseOutranks says whether a should be kept over b when both test the
// same question: a confirmed test beats an incumbent one, then the newer wins.
func MinedCaseOutranks(a, b MinedCase) bool {
	if a.Tier != b.Tier {
		return a.Tier == MineTierConfirmed
	}
	return a.Source.AskedAt > b.Source.AskedAt
}

var (
	minedDateLiteral = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)
	minedCentsField  = regexp.MustCompile(`(?i)cents`)
	minedLabelFields = []string{"name", "title", "full_name", "display_name", "label", "email"}
)

// MineOne decides whether one candidate becomes a test.
func MineOne(ctx context.Context, c MineCandidate, run MineRunner) MineOutcome {
	drop := func(reason, detail string) MineOutcome { return MineOutcome{Drop: reason, Detail: detail} }
	if c.Rating == "down" {
		return drop(MineDropRatedWrong, "")
	}
	if c.AskerUnknown != "" {
		return drop(MineDropAsker, c.AskerUnknown)
	}
	if c.Status != gjagent.StatusAnswered {
		return drop(MineDropNotAnswered, c.Status)
	}
	for _, code := range c.ViolationCodes {
		// A guard that stopped something before it ran is a detour the run
		// recovered from; anything else is an effect nobody should copy.
		if !gjagent.IsBlockingGuardViolationCode(code) {
			return drop(MineDropUnsafe, code)
		}
	}
	var pick *MineQuery
	var pickShape queryshape.Shape
	for i := range c.Queries {
		q := c.Queries[i]
		shape, err := queryshape.Inspect(q.Query)
		if err != nil {
			continue
		}
		if shape.Operation == "mutation" {
			return drop(MineDropWrite, "")
		}
		if q.OK && q.HasData && shape.Operation == "query" {
			pick, pickShape = &c.Queries[i], shape
		}
	}
	if pick == nil {
		return drop(MineDropNoQuery, "")
	}
	if err := checkProse(c.Question, 3); err != nil {
		return drop(MineDropProse, err.Error())
	}
	if pickShape.Variables {
		return drop(MineDropVariables, "")
	}
	oracleQuery, oracleVariables, dated := minedRelativeDates(pick.Query, pickShape.Literals, c)
	if dated != "" {
		return drop(MineDropDated, dated)
	}
	if len(pickShape.Roots) != 1 {
		return drop(MineDropSeveral, fmt.Sprintf("%d roots", len(pickShape.Roots)))
	}
	root := pickShape.Roots[0]
	if root.Nested {
		return drop(MineDropShape, "nested selection")
	}
	aggregates, _ := queryshape.Aggregates(pick.Query)
	ranked := root.Limit == 1 && len(root.OrderBy) != 0
	// The corpus rule: an answer key is an aggregate or a single row, so it
	// never depends on how many rows a page holds.
	if len(aggregates) == 0 && root.Limit != 1 {
		return drop(MineDropList, "")
	}

	data, err := run(ctx, c, pick.Query)
	if err != nil {
		return drop(MineDropQueryFailed, err.Error())
	}
	rowPath, row, reason := minedSingleRow(data, root.Key)
	if reason != "" {
		return drop(reason, "")
	}

	measure, dimension, reason := minedMeasure(root, aggregates, ranked, row)
	if reason != "" {
		return drop(reason, "")
	}
	value := row[measure.Key]
	kind, ok := minedAnswerKind(value)
	if !ok {
		return drop(MineDropShape, fmt.Sprintf("value %v", value))
	}
	oracle := OracleSpec{Query: oracleQuery, Variables: oracleVariables, Extract: rowPath + "." + measure.Key}
	if dimension != nil {
		oracle.DimensionExtract = rowPath + "." + dimension.Key
		for _, f := range root.Fields {
			if f.Name == "id" && f.Key != dimension.Key {
				oracle.DimensionAlternateExtracts = append(oracle.DimensionAlternateExtracts, rowPath+"."+f.Key)
			}
		}
	}

	answer := AnswerRule{Kind: kind}
	isAggregate := minedIsAggregate(measure, aggregates)
	if kind == "number" {
		if minedCentsField.MatchString(measure.Name) {
			// The column says its unit; people say dollars.
			answer.AcceptScales = []float64{1, 0.01}
		}
		if n, ok := numberFromString(valueString(value)); ok && n != float64(int64(n)) {
			answer.TolerancePct = 1
		}
	}
	method := MethodRule{}
	category := CategoryDiscovery
	if isAggregate {
		method.RequireQueryMatch = append(method.RequireQueryMatch, measure.Name)
		method.ForbidFinalizeFromListOnly = kind == "number"
		category = CategoryAggregate
	}
	switch {
	case ranked && dimension != nil:
		method.RequireQueryMatch = append(method.RequireQueryMatch, "order_by")
		category = CategoryRanking
	case ranked && measure.Name == root.OrderBy[0]:
		// The top value itself ("the largest invoice"): ordering and taking
		// one row, or the matching aggregate, both compute it in the database.
		method.RequireQueryMatch = append(method.RequireQueryMatch, "order_by|(max|min)_"+measure.Name)
		category = CategoryRanking
	}

	tier := MineTierIncumbent
	if c.Rating == "up" && c.TurnQuestions <= 1 {
		tier = MineTierConfirmed
	}
	mined := &MinedCase{
		ID:             c.ID,
		Tier:           tier,
		Prompt:         strings.TrimSpace(c.Question),
		Category:       category,
		ExpectedStatus: gjagent.StatusAnswered,
		Oracle:         oracle,
		Answer:         answer,
		Method:         method,
		Source: MinedSource{
			RunID: c.RunID, Delegate: c.Delegate, AskedAt: c.AskedAt, Origin: c.Origin,
			UserPrompt: c.UserPrompt,
		},
	}
	task, err := mined.Task()
	if err != nil {
		return drop(MineDropShape, err.Error())
	}
	result, err := extractOracleResult(oracle, data, nil)
	if err != nil {
		return drop(MineDropShape, err.Error())
	}
	// The answer the agent gave must pass its own answer key. A query that
	// does not reproduce what was said was not the one the answer came from,
	// or the data has moved on since.
	if ok, why := evaluateGroundTruth(task, result, gjagent.Response{Status: c.Status, Answer: c.Answer, Data: c.Data}); !ok {
		return drop(MineDropAnswerChanged, why)
	}
	mined.Value, mined.Dimension = result.Value, result.Dimension
	return MineOutcome{Case: mined}
}

var minedAbsoluteDate = regexp.MustCompile(`(?i)\b\d{4}-\d{2}-\d{2}\b|\b\d{1,2}/\d{1,2}(/\d{2,4})?\b|\b(19|20)\d{2}\b|\b(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\.?\s+\d{1,2}\b|\b(january|february|march|april|june|july|august|september|october|november|december)\b`)

// minedRelativeDays is how far from the day a question was asked a date in its
// query may be and still be read as relative to it: a year and a month, so
// "the last 12 months" and "this year" shift while "since 2020-01-01" does not.
const minedRelativeDays = 400

// minedRelativeDates turns the literal dates in a query into offsets from the
// day the question was asked, so "renewals still to come" asked on the 19th
// still means "from today" when the test runs a month later. It does so only
// when the question names no date itself: a question about "November 2026"
// means that month for good, and is dropped rather than shifted. It returns the
// reason a query cannot be used, or "" with the query and variables to use.
func minedRelativeDates(query string, literals []string, c MineCandidate) (string, map[string]any, string) {
	var dates []string
	seen := map[string]bool{}
	for _, literal := range literals {
		if minedDateLiteral.MatchString(strings.TrimSpace(literal)) && !seen[literal] {
			seen[literal] = true
			dates = append(dates, literal)
		}
	}
	if len(dates) == 0 {
		return query, nil, ""
	}
	if match := minedAbsoluteDate.FindString(c.Question); match != "" {
		return "", nil, "the question names a date: " + match
	}
	asked, err := time.Parse(time.RFC3339, c.AskedAt)
	if err != nil {
		return "", nil, "no date the question was asked"
	}
	asked = asked.UTC().Truncate(24 * time.Hour)
	variables := map[string]any{}
	for i, literal := range dates {
		day, err := time.Parse("2006-01-02", literal)
		if err != nil {
			// A timestamp, not a day: no offset says what it meant.
			return "", nil, literal
		}
		offset := int(day.Sub(asked).Hours() / 24)
		// A date this far from the day of asking is a fixed bound ("all
		// time", say 2020-01-01), not one counted from today: it stays put.
		if offset > minedRelativeDays || offset < -minedRelativeDays {
			continue
		}
		name := "asked_" + strconv.Itoa(i)
		quoted, _ := json.Marshal(literal)
		marker, _ := json.Marshal(oracleVariableMarker(name))
		if !strings.Contains(query, string(quoted)) {
			return "", nil, literal
		}
		query = strings.ReplaceAll(query, string(quoted), string(marker))
		token := "{{today}}"
		if offset > 0 {
			token = fmt.Sprintf("{{today+%d}}", offset)
		} else if offset < 0 {
			token = fmt.Sprintf("{{today%d}}", offset)
		}
		variables[name] = token
	}
	if len(variables) == 0 {
		return query, nil, ""
	}
	return query, variables, ""
}

// minedSingleRow finds the one row a query returned under key, and the path
// to it.
func minedSingleRow(data any, key string) (string, map[string]any, string) {
	root, ok := toMap(normalizeJSON(data))[key]
	if !ok || root == nil {
		return "", nil, MineDropEmpty
	}
	if items, isList := normalizeJSON(root).([]any); isList {
		switch {
		case len(items) == 0:
			return "", nil, MineDropEmpty
		case len(items) > 1:
			return "", nil, MineDropList
		}
		row := toMap(items[0])
		if row == nil {
			return "", nil, MineDropShape
		}
		return key + ".0", row, ""
	}
	row := toMap(root)
	if row == nil {
		return "", nil, MineDropShape
	}
	return key, row, ""
}

// minedMeasure picks the figure the question is about and, for a ranking, the
// field that names the winning row.
func minedMeasure(root queryshape.Root, aggregates []queryshape.Aggregate, ranked bool, row map[string]any) (queryshape.Field, *queryshape.Field, string) {
	fields := make([]queryshape.Field, 0, len(root.Fields))
	for _, f := range root.Fields {
		if _, ok := row[f.Key]; ok {
			fields = append(fields, f)
		}
	}
	switch {
	case len(fields) == 0:
		return queryshape.Field{}, nil, MineDropShape
	case len(fields) == 1:
		return fields[0], nil, ""
	case !ranked:
		return queryshape.Field{}, nil, MineDropSeveral
	}
	var measure *queryshape.Field
	for i := range fields {
		if fields[i].Name == root.OrderBy[0] || fields[i].Key == root.OrderBy[0] {
			measure = &fields[i]
			break
		}
	}
	if measure == nil {
		return queryshape.Field{}, nil, MineDropShape
	}
	var dimension *queryshape.Field
	for _, label := range minedLabelFields {
		for i := range fields {
			if fields[i].Name == label && fields[i].Key != measure.Key {
				dimension = &fields[i]
				break
			}
		}
		if dimension != nil {
			break
		}
	}
	if dimension == nil {
		for i := range fields {
			if fields[i].Key == measure.Key || fields[i].Name == "id" {
				continue
			}
			if s, ok := row[fields[i].Key].(string); ok && strings.TrimSpace(s) != "" && dateFromValue(s) == "" {
				dimension = &fields[i]
				break
			}
		}
	}
	if dimension == nil {
		// Nothing names the row but its id: the question is about the top
		// value itself, not which row holds it.
		for _, f := range fields {
			if f.Key != measure.Key && f.Name != "id" {
				return queryshape.Field{}, nil, MineDropShape
			}
		}
		return *measure, nil, ""
	}
	if len(fields) > 3 {
		// More than the figure, the name and an id: the question asked for
		// more than one thing about the row.
		return queryshape.Field{}, nil, MineDropSeveral
	}
	return *measure, dimension, ""
}

func minedIsAggregate(measure queryshape.Field, aggregates []queryshape.Aggregate) bool {
	for _, a := range aggregates {
		if a.Column != "" && strings.EqualFold(measure.Name, a.Operation+"_"+a.Column) {
			return true
		}
	}
	return false
}

// minedAnswerKind says how an answer is compared with the value.
func minedAnswerKind(value any) (string, bool) {
	switch typed := value.(type) {
	case float64, float32, int, int64, json.Number:
		return "number", true
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return "", false
		}
		if minedDateLiteral.MatchString(text) && dateFromValue(text) != "" {
			return "date", true
		}
		if _, ok := numberFromString(text); ok {
			return "number", true
		}
		return "string", true
	}
	return "", false
}

// Task is the evaluation task a mined case becomes.
func (c MinedCase) Task() (Task, error) {
	oracle := c.Oracle
	difficulty := DifficultyT1
	if c.Category == CategoryRanking {
		difficulty = DifficultyT3
	}
	task := Task{
		Slug:              "mined-" + c.Prompt,
		Category:          c.Category,
		Difficulty:        difficulty,
		Prompt:            c.Prompt,
		Provenance:        Provenance{Source: MinedSourcePrefix + c.Tier, SourceID: c.ID},
		CapabilityProfile: CapabilityProfile{RoleClass: "user"},
		ExpectedStatus:    c.ExpectedStatus,
		Oracle:            &oracle,
		Answer:            c.Answer,
		Method:            c.Method,
		Behavior: BehaviorRule{
			RequiredActions:  []string{"query_catalog", "execute_graphql"},
			ForbiddenActions: []string{"execute_graphql:mutation"},
		},
	}
	if err := task.Normalize(); err != nil {
		return Task{}, err
	}
	return task, nil
}

// MinedSourcePrefix marks a task made from a real question; the tier follows.
const MinedSourcePrefix = "mined-"

// MinedSuite turns mined cases into a suite of their own. Mined tests are kept
// apart from generated ones: a generated suite samples its candidates, and a
// question somebody really asked should never lose a draw.
func MinedSuite(cases []MinedCase, catalogFingerprint string, now time.Time) (Suite, []string, error) {
	suite := Suite{
		SchemaVersion:      SuiteSchemaVersion,
		Name:               "Mined from real questions",
		Description:        "Questions people asked the agent, each with the database query that answered it as its answer key.",
		CreatedAt:          now.UTC(),
		CatalogFingerprint: catalogFingerprint,
		Generator:          GeneratorMeta{Version: GeneratorVersion},
	}
	var skipped []string
	for _, c := range cases {
		task, err := c.Task()
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s: %v", c.ID, err))
			continue
		}
		suite.Tasks = append(suite.Tasks, task)
	}
	return suite, skipped, nil
}
