package eval

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

func authoringCensus() SchemaCensus {
	snapshot := CatalogSnapshot{
		Status: AgentStatus{
			ReadOnly:             false,
			AllowedActions:       []string{"gj_watch.insert", "gj_watch_event.update", gjagent.CapabilityActionDataUpdate},
			AvailableSystemRoots: []string{"gj_watch", "gj_watch_event"},
		},
		Rows: []CatalogRow{
			{
				ID: "table:invoices", Kind: "table", TableName: "invoices",
				DetailsJSON: `[{"ColumnName":"id","Type":"integer","PrimaryKey":true},
					{"ColumnName":"status","Type":"text"},
					{"ColumnName":"amount_cents","Type":"integer"},
					{"ColumnName":"due_at","Type":"datetime"}]`,
			},
			columnRow("invoices", "status", []any{
				`where: { status: { eq: "failed" } }`, "status values: failed, paid",
			}),
		},
	}
	return BuildCensus(snapshot)
}

// replyWith answers one authoring call with canned picks, so the gates can be
// exercised without a provider.
func replyWith(picks any) OneShotFunc {
	return func(context.Context, string, map[string]any) (map[string]any, error) {
		return map[string]any{"picks_json": MarshalAuthoringPicks(picks)}, nil
	}
}

func TestAuthoredWatchBuildsBothTiersFromOnePick(t *testing.T) {
	call := replyWith([]WatchPick{{
		Table: "invoices", Column: "status", Value: "failed", Name: "failed_invoices",
		Intent: "Finance keeps finding out about payments that did not go through days later. They want to hear about new ones within the hour without checking anything.",
	}})
	tasks, report, err := AuthorFamilies(context.Background(), call, authoringCensus(), nil,
		AuthoringOptions{Kinds: []AuthoringKind{AuthoringWatch}, AuthoredBy: "test/model"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected an intent and an execution task, got %d (%+v)", len(tasks), report.Rejections)
	}
	var intent, execution Task
	for _, task := range tasks {
		switch task.Tier {
		case TierIntent:
			intent = task
		case TierExecution:
			execution = task
		}
	}
	if intent.Prompt == "" || execution.Prompt == "" {
		t.Fatalf("both tiers must exist: %+v", tasks)
	}
	if intent.NeedID == "" || intent.NeedID != execution.NeedID {
		t.Fatal("the pair must share a need so the planning gap is computable")
	}
	// The intent prompt is the caller's own words; the execution prompt has to
	// name the filter it is graded on.
	if strings.Contains(intent.Prompt, "failed_invoices") {
		t.Fatalf("the intent prompt leaked the operation: %q", intent.Prompt)
	}
	if !strings.Contains(execution.Prompt, "failed_invoices") || !strings.Contains(execution.Prompt, "failed") {
		t.Fatalf("the execution prompt must name what it is graded on: %q", execution.Prompt)
	}
	for _, task := range tasks {
		if task.Category != CategoryReactive || task.Mutation == nil {
			t.Fatalf("unexpected shape: %+v", task)
		}
		if task.Provenance.AuthoredBy != "test/model" {
			t.Fatalf("authorship was not recorded: %+v", task.Provenance)
		}
		if len(task.Mutation.Collateral) == 0 {
			t.Fatal("a write task must check that nothing else moved")
		}
	}
	// The post-state proves a cursor-backed watch over the right rows exists,
	// without naming it — any watch that satisfies the need counts.
	post := intent.Mutation.PostState.Query
	for _, want := range []string{"invoices_cursor", "failed", "delivery_json"} {
		if !strings.Contains(post, want) {
			t.Fatalf("post-state missing %q: %s", want, post)
		}
	}
}

// Every table, column and value a model names has to exist. Inventing one is
// the failure mode that produces a task nobody can pass and nobody can explain.
func TestAuthoredWatchRefusesInventedSchema(t *testing.T) {
	cases := map[string]WatchPick{
		"unknown table": {Table: "shipments", Column: "status", Value: "failed", Name: "w1",
			Intent: "Someone should be told when shipments go wrong instead of finding out later on."},
		"unobserved value": {Table: "invoices", Column: "status", Value: "cancelled", Name: "w2",
			Intent: "Someone should be told when invoices are cancelled instead of finding out later on."},
		"bad watch name": {Table: "invoices", Column: "status", Value: "failed", Name: "Failed Invoices!",
			Intent: "Someone should be told when invoices fail instead of finding out later on."},
	}
	for name, pick := range cases {
		tasks, report, err := AuthorFamilies(context.Background(), replyWith([]WatchPick{pick}), authoringCensus(), nil,
			AuthoringOptions{Kinds: []AuthoringKind{AuthoringWatch}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(tasks) != 0 {
			t.Fatalf("%s: expected a refusal, got %d tasks", name, len(tasks))
		}
		if len(report.Rejections) == 0 {
			t.Fatalf("%s: refused without saying why", name)
		}
	}
}

// An intent prompt that names the mechanism stops measuring whether the agent
// can plan and starts measuring whether it can follow instructions.
func TestAuthoredWatchRefusesProseThatNamesTheMechanism(t *testing.T) {
	for _, intent := range []string{
		"Create a gj_watch over invoices where status is failed and deliver hourly.",
		"Set up a subscription on the invoices table for failed payments each hour.",
		"Watch it.",
	} {
		tasks, report, err := AuthorFamilies(context.Background(),
			replyWith([]WatchPick{{Table: "invoices", Column: "status", Value: "failed", Name: "failed_invoices", Intent: intent}}),
			authoringCensus(), nil, AuthoringOptions{Kinds: []AuthoringKind{AuthoringWatch}})
		if err != nil {
			t.Fatal(err)
		}
		if len(tasks) != 0 {
			t.Fatalf("prose %q should have been refused", intent)
		}
		if len(report.Rejections) == 0 {
			t.Fatalf("prose %q refused without a reason", intent)
		}
	}
}

// A caller who cannot create watches would be handed tasks nothing can pass.
func TestAuthoringSkipsWatchesWhenTheCallerCannotCreateThem(t *testing.T) {
	census := authoringCensus()
	census.Profile.AllowedActions = nil
	tasks, report, err := AuthorFamilies(context.Background(), replyWith([]WatchPick{{Table: "invoices"}}), census, nil,
		AuthoringOptions{Kinds: []AuthoringKind{AuthoringWatch}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatal("watch tasks were authored for a caller who cannot create watches")
	}
	if len(report.Notes) == 0 {
		t.Fatal("the skip was not explained")
	}
}

// A confirmation asserts that a watch exists, never that an event was
// delivered: only reactive tasks turn the watch runner on, and a confirmation
// is a multi-turn task, so asserting delivery would wait for a runner that is
// switched off.
func TestAuthoredConfirmationNeverAssertsDelivery(t *testing.T) {
	call := replyWith([]ConfirmationPick{{
		Table: "invoices", Column: "status", Value: "failed",
		Need:     "We keep missing payments that fail, and nobody notices until the end of the week.",
		Proposal: "I can set up an alert called failed_invoices that sends you an hourly digest of new failures.",
	}})
	tasks, report, err := AuthorFamilies(context.Background(), call, authoringCensus(), nil,
		AuthoringOptions{Kinds: []AuthoringKind{AuthoringConfirmation}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one confirmation, got %d (%+v)", len(tasks), report.Rejections)
	}
	task := tasks[0]
	if task.Category != CategoryMultiTurn {
		t.Fatalf("category = %s", task.Category)
	}
	if strings.Contains(task.Mutation.PostState.Query, "gj_watch_event") {
		t.Fatalf("a confirmation must not assert delivery: %s", task.Mutation.PostState.Query)
	}
	if len(task.Turns) != 2 || task.Turns[0].Role != "user" || task.Turns[1].Role != "assistant" {
		t.Fatalf("unexpected turns: %+v", task.Turns)
	}
	// The final word is consent alone; the operational detail lives in the
	// assistant's prior turn.
	if !strings.HasPrefix(task.Prompt, "Yes") {
		t.Fatalf("the final turn should be agreement, got %q", task.Prompt)
	}
}

func verifiedReadTask(t *testing.T) Task {
	t.Helper()
	task := Task{
		Slug: "aggregate-unpaid", Category: CategoryAggregate, Difficulty: DifficultyT2,
		Prompt: "How many invoices are unpaid?", ExpectedStatus: gjagent.StatusAnswered,
		Provenance: Provenance{Source: "catalog-entity"},
		Oracle: &OracleSpec{
			Query:   `query { invoices(where: {status: {eq: "failed"}}) { count_id } }`,
			Extract: "invoices.0.count_id",
		},
		Answer:   AnswerRule{Kind: "number"},
		Behavior: BehaviorRule{RequiredActions: []string{"execute_graphql"}},
	}
	if err := task.Normalize(); err != nil {
		t.Fatal(err)
	}
	return task
}

// A follow-up that repeats its subject would pass without the agent carrying
// anything from the earlier turns, which is the whole thing being measured.
func TestAuthoredHistoryRequiresTheFollowUpToReferBack(t *testing.T) {
	source := verifiedReadTask(t)
	good := HistoryPick{
		TaskID: source.ID, FirstTurn: "How many invoices did we raise last month?",
		PriorTurn: "You raised 42 invoices last month.", FollowUp: "How many of those went unpaid?",
	}
	tasks, report, err := AuthorFamilies(context.Background(), replyWith([]HistoryPick{good}), authoringCensus(),
		[]Task{source}, AuthoringOptions{Kinds: []AuthoringKind{AuthoringHistory}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected one follow-up, got %d (%+v)", len(tasks), report.Rejections)
	}
	// The oracle is the original's — already checked against the database.
	if tasks[0].Oracle.Query != source.Oracle.Query {
		t.Fatal("the follow-up must keep the verified oracle")
	}
	if len(tasks[0].Turns) != 2 {
		t.Fatalf("turns missing: %+v", tasks[0].Turns)
	}

	standalone := good
	standalone.FollowUp = "How many invoices are unpaid?"
	tasks, report, err = AuthorFamilies(context.Background(), replyWith([]HistoryPick{standalone}), authoringCensus(),
		[]Task{source}, AuthoringOptions{Kinds: []AuthoringKind{AuthoringHistory}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatal("a self-contained follow-up should have been refused")
	}
	if len(report.Rejections) == 0 {
		t.Fatal("refused without a reason")
	}
}

func TestAuthoredHistoryRefusesUnknownTasks(t *testing.T) {
	tasks, report, err := AuthorFamilies(context.Background(),
		replyWith([]HistoryPick{{TaskID: "gjv1_notreal", FirstTurn: "How many invoices last month?",
			PriorTurn: "42.", FollowUp: "How many of those failed?"}}),
		authoringCensus(), []Task{verifiedReadTask(t)}, AuthoringOptions{Kinds: []AuthoringKind{AuthoringHistory}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 || len(report.Rejections) == 0 {
		t.Fatalf("an invented task id must be refused: %d tasks, %+v", len(tasks), report.Rejections)
	}
}

// A scenario that still names the schema is the original question with
// decoration on it, and measures nothing the original did not.
func TestAuthoredScenarioRefusesPromptsThatNameTheSchema(t *testing.T) {
	source := verifiedReadTask(t)
	source.Oracle.Query = `query { invoices(where: {payment_status: {eq: "failed"}}) { count_id } }`
	if err := source.Normalize(); err != nil {
		t.Fatal(err)
	}
	leaky := ScenarioPick{TaskID: source.ID, Prompt: "Before the finance review, check how many rows have payment_status failed."}
	tasks, report, err := AuthorFamilies(context.Background(), replyWith([]ScenarioPick{leaky}), authoringCensus(),
		[]Task{source}, AuthoringOptions{Kinds: []AuthoringKind{AuthoringScenario}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 || len(report.Rejections) == 0 {
		t.Fatalf("a leaky scenario must be refused: %d tasks", len(tasks))
	}

	clean := ScenarioPick{TaskID: source.ID,
		Prompt: "The finance review starts in ten minutes and someone will ask how much we failed to collect. How many are outstanding?"}
	tasks, _, err = AuthorFamilies(context.Background(), replyWith([]ScenarioPick{clean}), authoringCensus(),
		[]Task{source}, AuthoringOptions{Kinds: []AuthoringKind{AuthoringScenario}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatal("a clean scenario should have been kept")
	}
	// Only the wording changes; what is graded is untouched.
	if tasks[0].Oracle.Query != source.Oracle.Query || tasks[0].Answer.Kind != source.Answer.Kind {
		t.Fatal("a scenario must not change what is being measured")
	}
}

func TestAuthoringRefusesRepliesThatAreNotPicks(t *testing.T) {
	call := func(context.Context, string, map[string]any) (map[string]any, error) {
		return map[string]any{"picks_json": "I could not find anything suitable."}, nil
	}
	_, _, err := AuthorFamilies(context.Background(), call, authoringCensus(), nil,
		AuthoringOptions{Kinds: []AuthoringKind{AuthoringWatch}})
	if err == nil {
		t.Fatal("a reply with no JSON must fail rather than silently produce nothing")
	}
}

func TestParseAuthoringKinds(t *testing.T) {
	kinds, err := ParseAuthoringKinds([]string{"watch", "history"})
	if err != nil || len(kinds) != 2 {
		t.Fatalf("kinds = %v, err = %v", kinds, err)
	}
	if _, err := ParseAuthoringKinds([]string{"vibes"}); err == nil {
		t.Fatal("an unknown kind must be refused")
	}
	all, err := ParseAuthoringKinds(nil)
	if err != nil || len(all) != len(AuthoringKinds) {
		t.Fatalf("empty should mean every kind, got %v", all)
	}
}

func TestCensusDigestNamesOnlyWhatExists(t *testing.T) {
	digest := authoringCensus().Digest()
	for _, want := range []string{"table invoices", "invoices.status holds only: failed, paid"} {
		if !strings.Contains(digest, want) {
			t.Fatalf("digest missing %q:\n%s", want, digest)
		}
	}
	var picks []WatchPick
	if err := json.Unmarshal([]byte(MarshalAuthoringPicks([]WatchPick{{Table: "invoices"}})), &picks); err != nil {
		t.Fatal(err)
	}
}
