package eval

import (
	"context"
	"regexp"
	"strings"
	"testing"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// fileCensus is a project that serves documents alongside its database.
func fileCensus() SchemaCensus {
	snapshot := CatalogSnapshot{
		Status: AgentStatus{
			ReadOnly:             false,
			AllowedActions:       []string{"gj_watch.insert", "gj_watch_event.update", gjagent.CapabilityActionDataUpdate},
			AvailableSystemRoots: []string{"gj_watch", "gj_watch_event"},
		},
		Rows: []CatalogRow{
			{
				ID: "table:support_tickets", Kind: "table", TableName: "support_tickets",
				DetailsJSON: `[{"ColumnName":"id","Type":"integer","PrimaryKey":true},
					{"ColumnName":"status","Type":"text"},
					{"ColumnName":"severity","Type":"text"}]`,
			},
			columnRow("support_tickets", "severity", []any{
				`where: { severity: { eq: "urgent" } }`, "severity values: urgent, high, low",
			}),
			{
				ID: "table:sla_policies", Kind: "table", TableName: "sla_policies",
				ExamplesJSON: []any{
					`{ sla_policies(prefix: "", limit: 10) { key size content_type } }`,
					`{ sla_policies(key: "<key>") { key content_type text data } }`,
				},
			},
		},
	}
	return BuildCensus(snapshot)
}

func filePick() FilePick {
	return FilePick{
		FileRoot: "sla_policies", Table: "support_tickets", Column: "severity", Value: "urgent",
		PolicyTopic: "urgent ticket response", PolicyAnswer: "4 hours",
		Intent:    "Support leadership wants to know whether we are behind on the most serious tickets, and how quickly we are actually supposed to deal with them.",
		Execution: "Check the written response standard for the most serious tickets and count how many of those are open right now.",
	}
}

func authorFiles(t *testing.T, picks []FilePick) ([]Task, AuthoringReport) {
	t.Helper()
	tasks, report, err := AuthorFamilies(context.Background(), replyWith(picks), fileCensus(), nil,
		AuthoringOptions{Kinds: []AuthoringKind{AuthoringFile}, AuthoredBy: "test/model"})
	if err != nil {
		t.Fatal(err)
	}
	return tasks, report
}

func TestAuthoredFileTaskPlantsTheAnswerItGradesAgainst(t *testing.T) {
	tasks, report := authorFiles(t, []FilePick{filePick()})
	if len(tasks) != 2 {
		t.Fatalf("expected an intent and an execution task, got %d (%v)", len(tasks), report.Rejections)
	}
	if len(report.Files) != 1 {
		t.Fatalf("the document the task grades against must come back to be written: %+v", report.Files)
	}
	document := report.Files[0]
	if document.FileRoot != "sla_policies" || document.Key != "authored-policy-support_tickets.md" {
		t.Fatalf("the engine names the document, not the model: %+v", document)
	}
	// The whole point of writing the document ourselves is that the graded
	// literal and the words on disk cannot disagree.
	if !strings.Contains(document.Contents, "Requirement: 4 hours.") {
		t.Fatalf("the requirement is not planted verbatim:\n%s", document.Contents)
	}
	for _, task := range tasks {
		if task.Category != CategoryCrossSource || task.Oracle == nil {
			t.Fatalf("unexpected shape: %+v", task)
		}
		if task.Oracle.DimensionLiteral != "4 hours" {
			t.Fatalf("the task must grade against the planted requirement: %q", task.Oracle.DimensionLiteral)
		}
		if !strings.Contains(document.Contents, task.Oracle.DimensionLiteral) {
			t.Fatal("the graded literal is not in the document")
		}
		// One read spanning both sources: the count the database knows and the
		// document the rule is written in.
		for _, want := range []string{"support_tickets", "count_id", `sla_policies(key: "authored-policy-support_tickets.md"`} {
			if !strings.Contains(task.Oracle.Query, want) {
				t.Fatalf("oracle missing %q: %s", want, task.Oracle.Query)
			}
		}
		if task.ExpectedStatus != gjagent.StatusAnswered || task.Answer.Kind != "number" {
			t.Fatalf("unexpected grading: %+v", task.Answer)
		}
		if !contains(task.Behavior.ForbiddenActions, "execute_graphql:mutation") {
			t.Fatal("reading a policy must not be allowed to write")
		}
	}
}

// The intent twin measures whether the agent works out that a second source
// exists. A prompt that names the document has already answered that.
func TestAuthoredFileIntentMayNotNameTheDocument(t *testing.T) {
	pick := filePick()
	pick.Intent = "Support leadership wants the sla_policies standard for urgent tickets and the current open count."
	tasks, report := authorFiles(t, []FilePick{pick})
	if len(tasks) != 0 {
		t.Fatalf("expected a refusal, got %d tasks", len(tasks))
	}
	if !strings.Contains(strings.Join(report.Rejections, "\n"), "points at the document") {
		t.Fatalf("refused without saying why: %v", report.Rejections)
	}
}

func TestAuthoredFileRefusesWhatItCannotVerify(t *testing.T) {
	longAnswer := strings.Repeat("very ", 12) + "slow"
	cases := map[string]struct {
		mutate func(*FilePick)
		want   string
	}{
		"unknown document source": {func(p *FilePick) { p.FileRoot = "handbooks" }, "not one of this project's document sources"},
		"unknown table":           {func(p *FilePick) { p.Table = "shipments" }, "is not in the schema"},
		"unobserved value":        {func(p *FilePick) { p.Value = "catastrophic" }, "does not hold"},
		"unusable requirement":    {func(p *FilePick) { p.PolicyAnswer = longAnswer }, "one short phrase"},
		"empty requirement":       {func(p *FilePick) { p.PolicyAnswer = "  " }, "one short phrase"},
		"mechanism in execution": {func(p *FilePick) {
			p.Execution = "Run a graphql query against the tickets and read the policy file."
		}, "names the mechanism"},
	}
	for name, item := range cases {
		pick := filePick()
		item.mutate(&pick)
		tasks, report := authorFiles(t, []FilePick{pick})
		if len(tasks) != 0 {
			t.Fatalf("%s: expected a refusal, got %d tasks", name, len(tasks))
		}
		if !strings.Contains(strings.Join(report.Rejections, "\n"), item.want) {
			t.Fatalf("%s: refused without saying %q: %v", name, item.want, report.Rejections)
		}
		if len(report.Files) != 0 {
			t.Fatalf("%s: a refused pick must not leave a document behind", name)
		}
	}
}

// A project with no file source has nowhere to write half of an answer.
func TestAuthoringSkipsFileTasksWithoutADocumentSource(t *testing.T) {
	tasks, report, err := AuthorFamilies(context.Background(), replyWith([]FilePick{filePick()}),
		authoringCensus(), nil, AuthoringOptions{Kinds: []AuthoringKind{AuthoringFile}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected nothing, got %d tasks", len(tasks))
	}
	if !strings.Contains(strings.Join(report.Notes, "\n"), "serves no documents") {
		t.Fatalf("skipped without saying why: %v", report.Notes)
	}
}

// File sources are carded like tables, so a model can reasonably pick one for a
// watch. Nothing can watch documents.
func TestAuthoringRefusesToWatchADocumentSource(t *testing.T) {
	call := replyWith([]WatchPick{{
		Table: "sla_policies", Name: "policy_changes",
		Intent: "Someone should tell us when the written standards change instead of us noticing months later.",
	}})
	tasks, report, err := AuthorFamilies(context.Background(), call, fileCensus(), nil,
		AuthoringOptions{Kinds: []AuthoringKind{AuthoringWatch}, ResolveOracle: resolveCount("3")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected a refusal, got %d tasks", len(tasks))
	}
	if !strings.Contains(strings.Join(report.Rejections, "\n"), "cannot be watched") {
		t.Fatalf("refused without saying why: %v", report.Rejections)
	}
}

// The generalized pattern must be the frozen one when applied to the frozen
// root. If these ever diverge, authored file tasks and reference file tasks
// would be graded by different rules while claiming to measure the same thing.
func TestFilePolicyReadPatternMatchesTheReferenceRule(t *testing.T) {
	if got := filePolicyReadPattern("sla_policies"); got != slaPolicyFileReadPattern {
		t.Fatalf("generalized pattern drifted:\n got %s\nwant %s", got, slaPolicyFileReadPattern)
	}
}

// The argument-free selection is the form models actually write, and it is a
// genuine read. Anchoring the rule behind an argument list once scored method
// false in twelve of twelve episodes that answered correctly.
func TestFilePolicyReadPatternAcceptsEveryRealRead(t *testing.T) {
	pattern := regexp.MustCompile(filePolicyReadPattern("operations_handbook"))
	for _, query := range []string{
		`{ operations_handbook(key: "x.md", inline_data: true) { data } }`,
		`{ operations_handbook { key data } }`,
		`{ operations_handbook(key: "x.md") { key text } }`,
		`query Combined { tickets { count_id } operations_handbook { text } }`,
	} {
		if !pattern.MatchString(query) {
			t.Fatalf("a real read scored as no read: %s", query)
		}
	}
	for _, query := range []string{
		`{ operations_handbook(prefix: "") { key size } }`,
		`{ tickets { count_id } }`,
	} {
		if pattern.MatchString(query) {
			t.Fatalf("a listing that never opened the document counted as a read: %s", query)
		}
	}
}
