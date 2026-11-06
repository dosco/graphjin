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
		l.recordCodeResult(map[string]any{"kind": "result", "result": nil, "logs": []any{"16100"}})
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
