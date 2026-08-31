package eval

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// resolveCount answers the eligibility read with a fixed row count.
func resolveCount(value string) func(context.Context, OracleSpec) (OracleResult, error) {
	return func(context.Context, OracleSpec) (OracleResult, error) {
		return OracleResult{Value: value}, nil
	}
}

func deliveryPick() WatchPick {
	return WatchPick{
		Table: "invoices", Column: "status", Value: "failed", Name: "failed_invoices",
		Intent: "Finance keeps finding out about payments that did not go through days later. They want to hear about new ones within the hour.",
	}
}

func authorWatchesWith(t *testing.T, resolve func(context.Context, OracleSpec) (OracleResult, error)) ([]Task, AuthoringReport) {
	t.Helper()
	tasks, report, err := AuthorFamilies(context.Background(), replyWith([]WatchPick{deliveryPick()}),
		authoringCensus(), nil, AuthoringOptions{
			Kinds: []AuthoringKind{AuthoringWatch}, AuthoredBy: "test/model", ResolveOracle: resolve,
		})
	if err != nil {
		t.Fatal(err)
	}
	return tasks, report
}

func deliveryTask(tasks []Task) (Task, bool) {
	for _, task := range tasks {
		if task.Provenance.Source == "authored-watch-delivery" {
			return task, true
		}
	}
	return Task{}, false
}

var setupQueryValue = regexp.MustCompile(`query:\s*("(?:[^"\\]|\\.)*")`)

// splitDeliverySetup separates the watch's own fields from the subscription it
// carries, so each can be read as what it is rather than as one escaped string.
func splitDeliverySetup(t *testing.T, setup string) (fields []string, subscription string) {
	t.Helper()
	match := setupQueryValue.FindStringSubmatch(setup)
	if match == nil {
		t.Fatalf("no subscription in the setup: %s", setup)
	}
	decoded, err := strconv.Unquote(match[1])
	if err != nil {
		t.Fatalf("subscription is not a readable string: %v", err)
	}
	for _, key := range regexp.MustCompile(`(\w+):`).FindAllStringSubmatch(
		strings.Replace(setup, match[1], `""`, 1), -1) {
		fields = append(fields, key[1])
	}
	return fields, decoded
}

// A watch over rows that exist can deliver, so the third task is authored.
func TestAuthoredDeliveryVariantIsBuiltWhenRowsExist(t *testing.T) {
	tasks, report := authorWatchesWith(t, resolveCount("7"))
	if len(tasks) != 3 {
		t.Fatalf("expected intent, execution and delivery, got %d (%v)", len(tasks), report.Notes)
	}
	task, ok := deliveryTask(tasks)
	if !ok {
		t.Fatalf("no delivery task: %+v", tasks)
	}
	if task.Category != CategoryReactive || task.Difficulty != DifficultyT4 {
		t.Fatalf("unexpected shape: %s/%s", task.Category, task.Difficulty)
	}
	// Delivery tasks are unpaired on purpose: they are not a tier of the need,
	// so pairing an intent twin against one would compute a planning gap that
	// does not exist.
	if task.Tier != "" || task.NeedID != "" {
		t.Fatalf("a delivery task must not join the intent/execution pair: tier=%q need=%q", task.Tier, task.NeedID)
	}
	if task.Provenance.AuthoredBy != "test/model" {
		t.Fatalf("authorship was not recorded: %+v", task.Provenance)
	}
	if task.Mutation == nil || task.Mutation.ReadyState == nil {
		t.Fatal("a delivery task waits for an event before the agent is asked anything")
	}
	if task.Mutation.ReadyValue != "false" || task.Mutation.ExpectedValue != "true" {
		t.Fatalf("the episode must start unseen and end seen: %+v", task.Mutation)
	}
	// The subscription is what the runner watches: it must be cursor-backed over
	// the right rows, or nothing is ever delivered.
	_, subscription := splitDeliverySetup(t, task.Mutation.Setup[0].Query)
	for _, want := range []string{"subscription failed_invoices", "invoices_cursor", `status: {eq: "failed"}`, "after: $cursor"} {
		if !strings.Contains(subscription, want) {
			t.Fatalf("subscription missing %q: %s", want, subscription)
		}
	}
	// Reporting what changed needs the row identified and the watched field
	// shown; anything more is noise in every delivered event.
	if !strings.Contains(subscription, "{ id status }") {
		t.Fatalf("projection should identify the row and show the watched field: %s", subscription)
	}
}

// The watch a delivery task installs must be a bare inbox watch.
//
// Anything carrying a delivery, an enrichment or a workflow needs approval
// before the runner will run it, and an unapproved watch is paused silently.
// The task would then look exactly like a model that never answered, which is
// the most expensive kind of environment bug: it is indistinguishable from the
// thing being measured.
func TestAuthoredDeliveryWatchIsBareInbox(t *testing.T) {
	tasks, _ := authorWatchesWith(t, resolveCount("7"))
	task, ok := deliveryTask(tasks)
	if !ok {
		t.Fatal("no delivery task")
	}
	setup := task.Mutation.Setup[0].Query
	for _, forbidden := range []string{"delivery_json", "enrich_json", "workflow", "actions"} {
		if strings.Contains(setup, forbidden) {
			t.Fatalf("the installed watch would need approval and be paused (%q): %s", forbidden, setup)
		}
	}
	fields, _ := splitDeliverySetup(t, setup)
	for _, field := range fields {
		switch field {
		case "insert", "name", "description", "query":
		default:
			t.Fatalf("unexpected field %q in a bare inbox watch: %s", field, setup)
		}
	}
}

// The graded mechanics are the ones the reference suite proved. Restating them
// here means a change to either has to be deliberate.
func TestAuthoredDeliveryMechanicsMatchTheReferenceSuite(t *testing.T) {
	tasks, _ := authorWatchesWith(t, resolveCount("7"))
	task, ok := deliveryTask(tasks)
	if !ok {
		t.Fatal("no delivery task")
	}
	if got, want := task.Mutation.ReadyState.Query,
		`query { gj_watch_event(where: {seen: {eq: false}}, order_by: {created_at: desc}, limit: 1) { seen } }`; got != want {
		t.Fatalf("ready state drifted:\n got %s\nwant %s", got, want)
	}
	if got, want := task.Mutation.PostState.Query,
		`query { gj_watch_event(order_by: {created_at: desc}, limit: 1) { seen } }`; got != want {
		t.Fatalf("post state drifted:\n got %s\nwant %s", got, want)
	}
	if !task.Mutation.ReadyState.AllowMissing || !task.Mutation.PostState.AllowMissing {
		t.Fatal("both reads happen before the row can exist, so both must allow it missing")
	}
	if task.Mutation.ReadyTimeoutMS != 10000 {
		t.Fatalf("ready timeout drifted: %d", task.Mutation.ReadyTimeoutMS)
	}
	if task.Mutation.Setup[0].WaitAfterMS != 1200 {
		t.Fatalf("setup wait drifted: %d", task.Mutation.Setup[0].WaitAfterMS)
	}
	if got, want := strings.Join(task.Method.RequireQueryMatch, "|"),
		`(?s)mutation.*gj_watch_event.*update.*seen\s*:\s*true`; got != want {
		t.Fatalf("method rule drifted:\n got %s\nwant %s", got, want)
	}
	for _, action := range []string{"query_catalog", "execute_graphql", "execute_graphql:mutation"} {
		if !contains(task.Behavior.RequiredActions, action) {
			t.Fatalf("behavior rule missing %q: %v", action, task.Behavior.RequiredActions)
		}
	}
}

// Every reason a delivery task cannot be authored has to be said out loud.
// A watch that can never fire produces episodes that time out and score zero
// no matter what the agent does, which reads as a model failure.
func TestAuthoredDeliveryVariantRefusesWithAReason(t *testing.T) {
	cases := map[string]struct {
		resolve func(context.Context, OracleSpec) (OracleResult, error)
		want    string
	}{
		"no live instance": {nil, "no live instance"},
		"no matching rows": {resolveCount("0"), "would never fire"},
		"read failed": {func(context.Context, OracleSpec) (OracleResult, error) {
			return OracleResult{}, errors.New("connection refused")
		}, "could not count"},
		"unreadable count": {resolveCount("lots"), "not a number"},
	}
	for name, item := range cases {
		tasks, report := authorWatchesWith(t, item.resolve)
		if len(tasks) != 2 {
			t.Fatalf("%s: expected only the intent/execution pair, got %d", name, len(tasks))
		}
		if _, ok := deliveryTask(tasks); ok {
			t.Fatalf("%s: a delivery task was built anyway", name)
		}
		if !strings.Contains(strings.Join(report.Notes, "\n"), item.want) {
			t.Fatalf("%s: refused without saying %q: %v", name, item.want, report.Notes)
		}
	}
}

// A table with no primary key has nothing to count rows by, and nothing to
// identify a delivered row with either.
func TestDeliveryEligibilityNeedsAPrimaryKey(t *testing.T) {
	if _, ok := deliveryEligibilityOracle(generatorTable{Name: "invoices"}, deliveryPick()); ok {
		t.Fatal("a table without a primary key cannot be counted")
	}
	oracle, ok := deliveryEligibilityOracle(generatorTable{Name: "invoices", PrimaryKey: "id"}, deliveryPick())
	if !ok {
		t.Fatal("a table with a primary key is countable")
	}
	if got, want := oracle.Query,
		`query { invoices(where: {status: {eq: "failed"}}) { count_id } }`; got != want {
		t.Fatalf("eligibility read:\n got %s\nwant %s", got, want)
	}
	if oracle.Extract != "invoices.0.count_id" {
		t.Fatalf("extract: %s", oracle.Extract)
	}
	// An unfiltered watch covers the whole table.
	plain, _ := deliveryEligibilityOracle(generatorTable{Name: "invoices", PrimaryKey: "id"},
		WatchPick{Table: "invoices", Name: "all_invoices"})
	if got, want := plain.Query, `query { invoices { count_id } }`; got != want {
		t.Fatalf("unfiltered eligibility read:\n got %s\nwant %s", got, want)
	}
}
