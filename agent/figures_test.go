package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	ax "github.com/ax-llm/ax/packages/go"
)

func figureTexts(figures []Figure) map[string]string {
	out := map[string]string{}
	for _, f := range figures {
		out[f.Text] = f.Source
	}
	return out
}

// The answers below are real lead replies from the lead-corpus A/B
// (2026-11-06): a follow-up summed three MRRs the previous turn reported.
func TestFigureLedgerAttributesRealFollowUpAnswers(t *testing.T) {
	previous := "| Account | MRR |\n| :-- | --: |\n| Solvex Dynamics | $6,100.00 |\n| Harborlight Systems | $5,200.00 |\n| Meridian Robotics | $4,800.00 |"

	t.Run("a head-computed total and a cents value shown as dollars are unsourced", func(t *testing.T) {
		var l figureLedger
		l.add(FigureSourceUser, "What's the combined MRR of those three?")
		l.add(FigureSourceConversation, previous)
		got := figureTexts(l.attribute("The combined monthly recurring revenue for the top three accounts is $16,100 ($1,610,000)."))
		want := map[string]string{"$16,100": "", "$1,610,000": ""}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("figures = %v, want %v", got, want)
		}
	})

	t.Run("the same total computed in code is sourced, the cents leak still is not", func(t *testing.T) {
		var l figureLedger
		l.add(FigureSourceConversation, previous)
		l.recordCodeResult(`console.log(accounts.reduce((sum, a) => sum + a.mrr, 0))`, map[string]any{"kind": "result", "result": nil, "logs": []any{"16100"}})
		got := figureTexts(l.attribute("The combined MRR is $16,100 ($1,610,000)."))
		if got["$16,100"] != FigureSourceCode || got["$1,610,000"] != "" {
			t.Fatalf("figures = %v, want $16,100 from code and $1,610,000 unsourced", got)
		}
	})

	t.Run("restating the earlier figures is sourced from the conversation", func(t *testing.T) {
		var l figureLedger
		l.add(FigureSourceConversation, previous)
		got := figureTexts(l.attribute("Solvex Dynamics ($6,100), Harborlight Systems ($5,200), and Meridian Robotics ($4,800)."))
		for text, source := range got {
			if source != FigureSourceConversation {
				t.Fatalf("%s source = %q, want conversation (all: %v)", text, source, got)
			}
		}
		if len(got) != 3 {
			t.Fatalf("figures = %v, want the three MRRs", got)
		}
	})

	t.Run("a fresh data-agent answer is credited over the conversation", func(t *testing.T) {
		var l figureLedger
		l.add(FigureSourceConversation, previous)
		l.add(FigureSourceDataAgent, "The combined MRR of Solvex Dynamics, Harborlight Systems and Meridian Robotics is $16,100.")
		got := figureTexts(l.attribute("The combined monthly recurring revenue is **$16,100.00/mo**."))
		if got["$16,100.00"] != FigureSourceDataAgent {
			t.Fatalf("figures = %v, want $16,100.00 from the data agent", got)
		}
	})
}

// The lead's stages from a captured flash-lite follow-up (2026-11-07): the
// context stage copied the earlier answer's three MRRs into final(), the
// executor summed them with reduce() and handed {totalMRR: 16100} on with
// final(), and the reply stage formatted it. The total was computed in code.
func TestFigureLedgerCreditsTotalsComputedInCode(t *testing.T) {
	var l figureLedger
	l.add(FigureSourceConversation, "The three biggest accounts are: **Solvex Dynamics** ($6100.00/mo), **Harborlight Systems** ($5200.00/mo), **Meridian Robotics** ($4800.00/mo).")
	l.recordCodeResult(
		`await final("Calculate the combined MRR", { accounts: [{ name: "Solvex Dynamics", mrr: 6100.00 }, { name: "Harborlight Systems", mrr: 5200.00 }, { name: "Meridian Robotics", mrr: 4800.00 }] });`,
		map[string]any{"type": "final", "args": []any{"Calculate the combined MRR", map[string]any{"accounts": []any{
			map[string]any{"name": "Solvex Dynamics", "mrr": 6100}, map[string]any{"name": "Harborlight Systems", "mrr": 5200}, map[string]any{"name": "Meridian Robotics", "mrr": 4800},
		}}}},
	)
	l.recordCodeResult(
		`const accounts = inputs.distilledContext.accounts;const totalMRR = accounts.reduce((acc, curr) => acc + curr.mrr, 0);await final('Answer the user with the combined MRR.', { totalMRR, accounts });`,
		map[string]any{"type": "final", "args": []any{"Answer the user with the combined MRR.", map[string]any{"totalMRR": 16100}}},
	)
	got := figureTexts(l.attribute("The combined monthly recurring revenue for Solvex Dynamics, Harborlight Systems, and Meridian Robotics is **$16,100.00/mo**."))
	if got["$16,100.00"] != FigureSourceCode {
		t.Fatalf("figures = %v, want $16,100.00 computed in code", got)
	}
}

// From a captured run (2026-11-07): the context stage converted two MRRs from
// cents and summed them, then handed the result straight to the reply stage
// with respond().
func TestFigureLedgerCreditsEvidenceHandedOnWithRespond(t *testing.T) {
	var l figureLedger
	l.recordCodeResult(
		"const mrr1 = 480000 / 100;\nconst mrr2 = 40000 / 100;\nconst combinedMRR = mrr1 + mrr2;\nawait respond('State the combined MRR.', { combinedMRR, accounts: [{ name: 'Meridian Robotics', mrr: mrr1 }, { name: 'Quartzline Analytics', mrr: mrr2 }] });",
		map[string]any{"type": "respond", "args": []any{"State the combined MRR.", map[string]any{"combinedMRR": 5200, "accounts": []any{
			map[string]any{"name": "Meridian Robotics", "mrr": 4800}, map[string]any{"name": "Quartzline Analytics", "mrr": 400},
		}}}},
	)
	got := figureTexts(l.attribute("The combined MRR for Meridian Robotics ($4,800) and Quartzline Analytics ($400) is **$5,200**."))
	for _, text := range []string{"$4,800", "$400", "$5,200"} {
		if got[text] != FigureSourceCode {
			t.Fatalf("figures = %v, want %s computed in code", got, text)
		}
	}
}

// A number the code only wrote down — printed as a literal, or put in final()
// as a literal — was not computed, so the code does not vouch for it.
func TestFigureLedgerIgnoresLiteralsInCode(t *testing.T) {
	var l figureLedger
	l.recordCodeResult(`console.log(16100)`, map[string]any{"kind": "result", "result": nil, "logs": []any{"16100"}})
	l.recordCodeResult(`await final("answer", { total: 16100 })`, map[string]any{"type": "final", "args": []any{"answer", map[string]any{"total": 16100}}})
	l.recordCodeResult(`await respond("State the total", { total: 9999 })`, map[string]any{"type": "respond", "args": []any{"State the total", map[string]any{"total": 9999}}})
	l.recordCodeResult(`await askClarification("Which of the 2 accounts?")`, map[string]any{"type": "askClarification", "args": []any{"Which of the 2 accounts?"}})
	got := figureTexts(l.attribute("The total is $16,100, or 9,999."))
	if got["$16,100"] != "" || got["9,999"] != "" {
		t.Fatalf("figures = %v, want both unsourced", got)
	}
}

func TestFigureLedgerMatchesDisplayPrecisionOnly(t *testing.T) {
	var l figureLedger
	l.add(FigureSourceDataAgent, "Total MRR is 16123.4 dollars; churn rate 0.125; net change -2,450.")
	cases := map[string]string{
		"$16,123":    FigureSourceDataAgent, // rounded to whole dollars
		"$16.1k":     FigureSourceDataAgent, // rounded to hundreds
		"16,123.40":  FigureSourceDataAgent,
		"12.5%":      FigureSourceDataAgent, // the fraction as a percentage
		"$2,450":     FigureSourceDataAgent, // sign is prose ("down $2,450")
		"$16,200":    "",                    // a different number
		"$1,612,340": "",                    // a scale factor is not a match
		"16,123.45":  "",                    // more precision than the source had
	}
	for text, want := range cases {
		got := figureTexts(l.attribute("Value: " + text + "."))
		if got[text] != want {
			t.Errorf("%s source = %q, want %q (all: %v)", text, got[text], want, got)
		}
	}
}

// Digits that are not reported figures are never checked: dates, times, list
// markers, identifiers, years and small counts.
func TestFigureLedgerSkipsNumbersThatAreNotFigures(t *testing.T) {
	var l figureLedger
	answer := strings.Join([]string{
		"1. Renews on 2026-11-20 at 09:30 UTC (Nov 20, 2026).",
		"2. Invoice INV-1042 for Q3, plan v2.1, ticket #77, item_12.",
		"3. Top 3 accounts across 5 regions since 2024.",
	}, "\n")
	if got := l.attribute(answer); len(got) != 0 {
		t.Fatalf("figures = %v, want none", got)
	}
	got := figureTexts(l.attribute("There are 42 open tickets worth $5 and 3% of usage."))
	if _, ok := got["42"]; !ok || len(got) != 3 {
		t.Fatalf("figures = %v, want 42, $5 and 3%% checked", got)
	}
}

// Dates in a source never vouch for the numbers inside them.
func TestFigureLedgerSourcesIgnoreDateParts(t *testing.T) {
	var l figureLedger
	l.add(FigureSourceDataAgent, "Renews on 2026-11-20.")
	if got := figureTexts(l.attribute("It renews in 20 days and costs $11.")); got["20"] != "" || got["$11"] != "" {
		t.Fatalf("figures = %v, want 20 and $11 unsourced", got)
	}
}

// A lead turn that computes a follow-up total in code reports it as sourced;
// one that writes the total without delegating or computing gets a notice.
// Neither changes the answer.
func TestLeadReportsWhereEachFigureCameFrom(t *testing.T) {
	history := []Turn{
		{Role: "user", Content: "What are our three biggest accounts by MRR?"},
		{Role: "assistant", Content: "Solvex Dynamics $6,100, Harborlight Systems $5,200, Meridian Robotics $4,800.", Status: StatusAnswered},
	}
	noDelegate := func(context.Context, DelegateRequest) (Response, error) {
		t.Fatal("no question should reach the data agent")
		return Response{}, nil
	}
	run := func(code, answer string) Response {
		client := &scriptedLeadClient{responses: []ax.Value{
			leadCode(`final({ plan: "combine the three figures" })`),
			leadCode(code),
			leadCode(`final("combined")`),
			leadReply(StatusAnswered, answer),
		}}
		lead := newTestLead(t, Config{}, client, noDelegate)
		resp, err := lead.Run(context.Background(), Request{Instruction: "and combined?", History: history})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	computed := run(`console.log(6100 + 5200 + 4800)`, "Combined MRR is $16,100.")
	if got := figureTexts(computed.Figures); got["$16,100"] != FigureSourceCode {
		t.Fatalf("figures = %v, want $16,100 from code", got)
	}
	for _, n := range computed.Notices {
		if n.Kind == noticeUnverifiedFigures {
			t.Fatalf("a code-computed total raised %v", n)
		}
	}

	head := run(`console.log("adding them up")`, "Combined MRR is $16,100.")
	if head.Answer != "Combined MRR is $16,100." || head.Status != StatusAnswered {
		t.Fatalf("the check must not change the answer: %q %s", head.Answer, head.Status)
	}
	var notice *ResponseNotice
	for i := range head.Notices {
		if head.Notices[i].Kind == noticeUnverifiedFigures {
			notice = &head.Notices[i]
		}
	}
	if notice == nil || notice.Count != 1 || !strings.Contains(notice.Message, "$16,100") {
		t.Fatalf("notices = %+v, want one unverified_figures notice naming $16,100", head.Notices)
	}
	if got := head.UnverifiedFigures(); !reflect.DeepEqual(got, []string{"$16,100"}) {
		t.Fatalf("unverified = %v", got)
	}
}

// A real lead reply (saas-ops demo, 2026-11-09): the data agent answered in
// cents and said so; the lead's dollars are those values divided by 100.
func TestFigureLedgerCreditsConversionFromAStatedUnit(t *testing.T) {
	worker := "Here are the most recent paid invoices:\n\n| Due Date | Amount (Cents) |\n|---|---|\n| 2026-11-04 | 30000 |\n| 2026-11-03 | 140000 |"
	var l figureLedger
	l.add(FigureSourceDataAgent, worker)
	got := figureTexts(l.attribute("| 2026-11-04 | $300.00 |\n| 2026-11-03 | $1,400.00 |\nThat is $1,700.00 in total."))
	// The head-computed total stays unsourced: only the conversion is exact.
	want := map[string]string{"$300.00": FigureSourceDataAgent, "$1,400.00": FigureSourceDataAgent, "$1,700.00": ""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("figures = %v, want %v", got, want)
	}

	// A column name states the unit as well as prose does.
	var named figureLedger
	named.add(FigureSourceDataAgent, `{"amount_cents": 480000, "duration_ms": 2500}`)
	if got := figureTexts(named.attribute("It was $4,800 and took 2.5 seconds.")); got["$4,800"] != FigureSourceDataAgent || got["2.5"] != FigureSourceDataAgent {
		t.Fatalf("named units: %v", got)
	}

	// A title is not a unit, and with no unit named nothing is scaled.
	var title figureLedger
	title.add(FigureSourceDataAgent, "Ms Lee owes 30000.")
	if got := figureTexts(title.attribute("Ms Lee owes $30.")); got["$30"] != "" {
		t.Fatalf("a title was read as milliseconds: %v", got)
	}
	var plain figureLedger
	plain.add(FigureSourceDataAgent, "The invoice total is 30000.")
	if got := figureTexts(plain.attribute("The invoice total is $300.")); got["$300"] != "" {
		t.Fatalf("an unnamed unit was scaled: %v", got)
	}
}
