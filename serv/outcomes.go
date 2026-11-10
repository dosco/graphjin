package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
)

// Outcomes close the loop on what the agent does. A change it proposes may
// carry an expectation: a read query that measures the effect, the number to
// read from it, and what that number should do within a window. GraphJin
// measures the number before the change (the baseline), and once the change
// has run and the window has passed, measures it again and records whether
// the change did what it said: hit or miss. A miss starts a turn in the
// change's thread so the agent can look into it; every check lands on the
// owner's ledger (/api/v1/agent/reliability), which is how a routine or an
// agent earns, or loses, trust.

const (
	outcomePending  = "pending"
	outcomeChecking = "checking"
	outcomeHit      = "hit"
	outcomeMiss     = "miss"
	outcomeError    = "error"

	outcomeMaxWithinHours = 30 * 24
	outcomeSweepInterval  = time.Minute
	// A check claimed this long ago by a replica that went away is taken over.
	outcomeClaimStale = 10 * time.Minute

	notificationOutcomeHit  = "outcome_hit"
	notificationOutcomeMiss = "outcome_miss"
)

var outcomeOps = map[string]string{
	"gte": "at least", "lte": "at most", "eq": "exactly",
	"increase": "up by at least", "decrease": "down by at least",
}

// approvalExpectation is what a change should achieve.
type approvalExpectation struct {
	Query       string   `json:"query"`
	Path        string   `json:"path"`
	Op          string   `json:"op"`
	Value       float64  `json:"value"`
	WithinHours float64  `json:"within_hours"`
	Baseline    *float64 `json:"baseline,omitempty"`
}

// parseApprovalExpectation reads execute_graphql's expect argument; nil when
// there is none.
func parseApprovalExpectation(raw any) (*approvalExpectation, error) {
	if raw == nil {
		return nil, nil
	}
	if text, ok := raw.(string); ok {
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}
		raw = parseJSONValue(text)
	}
	m := mapFromAny(raw)
	if m == nil {
		return nil, fmt.Errorf("expect must be an object {query, path, op, value, within_hours}")
	}
	exp := &approvalExpectation{
		Query: strings.TrimSpace(stringFromAny(m["query"])),
		Path:  strings.TrimSpace(stringFromAny(m["path"])),
		Op:    strings.ToLower(strings.TrimSpace(stringFromAny(m["op"]))),
	}
	value, ok := watchMetricNumber(m["value"])
	if !ok {
		return nil, fmt.Errorf("expect.value must be a number")
	}
	exp.Value = value
	within, ok := watchMetricNumber(m["within_hours"])
	if !ok || within <= 0 || within > outcomeMaxWithinHours {
		return nil, fmt.Errorf("expect.within_hours must be a number from 0 to %d", outcomeMaxWithinHours)
	}
	exp.WithinHours = within
	switch {
	case exp.Query == "":
		return nil, fmt.Errorf("expect.query must be a read query that measures the change's effect")
	case gjagent.ContainsMutationOperation(exp.Query) || strings.HasPrefix(strings.ToLower(exp.Query), "subscription"):
		return nil, fmt.Errorf("expect.query must be a read query, not a mutation or subscription")
	case exp.Path == "":
		return nil, fmt.Errorf("expect.path must name one number in the query's result, such as invoices.0.count_id")
	case outcomeOps[exp.Op] == "":
		return nil, fmt.Errorf("expect.op must be gte, lte, eq, increase or decrease")
	}
	return exp, nil
}

// measure runs the expectation's query as ctx's identity and reads its number.
func (s *graphjinService) measureExpectation(ctx context.Context, exp *approvalExpectation) (float64, error) {
	data, err := s.runWatchMetricQuery(ctx, exp.Query, "", time.Now().UTC(), time.UTC)
	if err != nil {
		return 0, err
	}
	raw, err := watchMetricLookup(data, exp.Path)
	if err != nil {
		return 0, err
	}
	n, ok := watchMetricNumber(raw)
	if !ok {
		return 0, fmt.Errorf("expect.path %q is %s, not a number", exp.Path, watchMetricDescribe(raw))
	}
	return n, nil
}

// holds reports whether an observed value meets the expectation.
func (exp *approvalExpectation) holds(observed float64) bool {
	baseline := 0.0
	if exp.Baseline != nil {
		baseline = *exp.Baseline
	}
	switch exp.Op {
	case "gte":
		return observed >= exp.Value
	case "lte":
		return observed <= exp.Value
	case "eq":
		return math.Abs(observed-exp.Value) <= 1e-9*math.Max(1, math.Abs(exp.Value))
	case "increase":
		return observed-baseline >= exp.Value
	case "decrease":
		return baseline-observed >= exp.Value
	}
	return false
}

// describe says the expectation in words: "invoices.0.count_id up by at least
// 3 within 72h".
func (exp *approvalExpectation) describe() string {
	return fmt.Sprintf("%s %s %s within %sh", exp.Path, outcomeOps[exp.Op], formatOutcomeNumber(exp.Value), formatOutcomeNumber(exp.WithinHours))
}

func formatOutcomeNumber(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

func expectationFromRow(row map[string]any) *approvalExpectation {
	text := stringMapValue(row, "expect_json")
	if strings.TrimSpace(text) == "" {
		return nil
	}
	var exp approvalExpectation
	if err := json.Unmarshal([]byte(text), &exp); err != nil || exp.Query == "" {
		return nil
	}
	return &exp
}

// prepareExpectation validates a proposed change's expectation and measures
// its baseline as the caller, before anything changes. An expectation that
// cannot be measured now could never be checked later, so it is refused and
// the model fixes it or leaves it out.
func (s *graphjinService) prepareExpectation(ctx context.Context, raw any) (*approvalExpectation, string, error) {
	exp, err := parseApprovalExpectation(raw)
	if err != nil || exp == nil {
		return nil, "", err
	}
	baseline, err := s.measureExpectation(ctx, exp)
	if err != nil {
		return nil, "", fmt.Errorf("expect could not be measured: %w; fix expect.query or expect.path, or leave expect out", err)
	}
	exp.Baseline = &baseline
	raw2, _ := json.Marshal(exp)
	return exp, string(raw2), nil
}

// outcomeSchedule is the outcome columns for a change that just ran: checked
// within_hours from now.
func outcomeSchedule(expectJSON string, now time.Time) map[string]any {
	exp := expectationFromRow(map[string]any{"expect_json": expectJSON})
	if exp == nil {
		return nil
	}
	due := now.Add(time.Duration(exp.WithinHours * float64(time.Hour)))
	return map[string]any{"outcome_status": outcomePending, "outcome_due_at": notificationTime(due)}
}

// scheduleOutcomeCheck starts the clock on an approved change that ran.
func (s *graphjinService) scheduleOutcomeCheck(ctx context.Context, approvalID string) {
	row, err := s.approvalRow(ctx, approvalID)
	if err != nil || row == nil || stringMapValue(row, "status") != approvalStatusExecuted {
		return
	}
	input := outcomeSchedule(stringMapValue(row, "expect_json"), time.Now())
	if input == nil {
		return
	}
	if _, err := s.internalStoreMutationRows(context.WithoutCancel(ctx), "approvals", `where: { id: { eq: $id } }, update: $input`, "id",
		map[string]any{"id": approvalID, "input": input}); err != nil && s.log != nil {
		s.log.Warnf("approval %s: schedule outcome check: %v", approvalID, err)
	}
}

// --- the check -----------------------------------------------------------------

func (s *graphjinService) startOutcomeChecker(parent context.Context) {
	if s == nil || s.gj == nil || !s.approvalsEnabled() {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	s.addCloseFn(cancel)
	s.revisionConsumerWG.Add(1)
	go func() {
		defer s.revisionConsumerWG.Done()
		ticker := time.NewTicker(outcomeSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			if _, err := s.sweepOutcomeChecks(ctx, time.Now()); err != nil && ctx.Err() == nil && s.log != nil {
				s.log.Warnf("outcome checks: %v", err)
			}
		}
	}()
}

// sweepOutcomeChecks checks every change whose window has passed. Each check
// is claimed first, so two replicas never check one change twice.
func (s *graphjinService) sweepOutcomeChecks(ctx context.Context, now time.Time) (int, error) {
	due, err := s.internalStoreAllRows(ctx, "approvals",
		`where: { outcome_status: { eq: "pending" }, outcome_due_at: { lte: $now } }`,
		approvalStoreFields, map[string]any{"now": notificationTime(now)})
	if err != nil {
		return 0, err
	}
	// A claim whose replica went away is taken over.
	stale, err := s.internalStoreAllRows(ctx, "approvals",
		`where: { outcome_status: { eq: "checking" }, outcome_checked_at: { lt: $stale } }`,
		approvalStoreFields, map[string]any{"stale": notificationTime(now.Add(-outcomeClaimStale))})
	if err != nil {
		return 0, err
	}
	due = append(due, stale...)
	checked := 0
	for _, row := range due {
		claimed, ok := s.claimOutcomeCheck(ctx, row, now)
		if !ok {
			continue
		}
		s.checkOutcome(ctx, claimed, now)
		checked++
	}
	return checked, nil
}

func (s *graphjinService) claimOutcomeCheck(ctx context.Context, row map[string]any, now time.Time) (map[string]any, bool) {
	id := stringMapValue(row, "id")
	token := notificationTime(now) + "#" + taskRandomHex()[:8]
	prev := stringMapValue(row, "outcome_status")
	if _, err := s.internalStoreMutationRows(ctx, "approvals",
		`where: { id: { eq: $id }, outcome_status: { eq: $prev } }, update: $input`, "id",
		map[string]any{"id": id, "prev": prev, "input": map[string]any{"outcome_status": outcomeChecking, "outcome_checked_at": token}}); err != nil {
		return nil, false
	}
	current, err := s.approvalRow(ctx, id)
	if err != nil || current == nil || stringMapValue(current, "outcome_checked_at") != token {
		return nil, false
	}
	return current, true
}

// checkOutcome measures a change's effect as the identity it ran as, records
// hit or miss, tells the owner, and on a miss starts a turn in its thread.
func (s *graphjinService) checkOutcome(ctx context.Context, row map[string]any, now time.Time) {
	id := stringMapValue(row, "id")
	exp := expectationFromRow(row)
	identityID, identityRole := stringMapValue(row, "owner_id"), stringMapValue(row, "owner_role")
	if runAs := stringMapValue(row, "run_as_id"); runAs != "" {
		identityID, identityRole = runAs, stringMapValue(row, "run_as_role")
	}
	checkCtx := s.ownerContext(context.WithoutCancel(ctx), identityID, identityRole, stringMapValue(row, "account_id"))
	status, observedText := outcomeError, ""
	var observed float64
	if exp == nil {
		observedText = "the expectation could not be read"
	} else if n, err := s.measureExpectation(checkCtx, exp); err != nil {
		observedText = truncateUTF8(err.Error(), 500)
	} else {
		observed, observedText = n, formatOutcomeNumber(n)
		status = outcomeMiss
		if exp.holds(n) {
			status = outcomeHit
		}
	}
	if _, err := s.internalStoreMutationRows(ctx, "approvals", `where: { id: { eq: $id } }, update: $input`, "id",
		map[string]any{"id": id, "input": map[string]any{"outcome_status": status, "outcome_value": observedText, "outcome_checked_at": notificationTime(now)}}); err != nil {
		if s.log != nil {
			s.log.Warnf("approval %s: record outcome: %v", id, err)
		}
		return
	}
	s.reportOutcome(checkCtx, row, exp, status, observed, observedText)
}

func (s *graphjinService) reportOutcome(ctx context.Context, row map[string]any, exp *approvalExpectation, status string, observed float64, observedText string) {
	id := stringMapValue(row, "id")
	summary := approvalSummaryText(row)
	expected := "its expectation"
	if exp != nil {
		expected = exp.describe()
	}
	measured := "measured " + observedText
	if exp != nil && exp.Baseline != nil && (exp.Op == "increase" || exp.Op == "decrease") && status != outcomeError {
		measured = fmt.Sprintf("went from %s to %s", formatOutcomeNumber(*exp.Baseline), observedText)
	}
	n := notification{
		OwnerID: stringMapValue(row, "owner_id"), AccountID: stringMapValue(row, "account_id"), DelegatedBy: stringMapValue(row, "delegated_by"),
		RefKind: "approval", RefID: id, Link: approvalLink(stringMapValue(row, "thread_id")),
		DedupeKey: "outcome:" + id,
	}
	switch status {
	case outcomeHit:
		n.Kind, n.Severity, n.Title = notificationOutcomeHit, notificationSeverityInfo, "A change did what it was expected to"
		n.Body = fmt.Sprintf("%s: expected %s; %s.", summary, expected, measured)
	case outcomeMiss:
		n.Kind, n.Severity, n.Title = notificationOutcomeMiss, notificationSeverityWarning, "A change missed what it was expected to do"
		n.Body = fmt.Sprintf("%s: expected %s; %s.", summary, expected, measured)
	default:
		n.Kind, n.Severity, n.Title = notificationOutcomeMiss, notificationSeverityWarning, "A change's outcome could not be checked"
		n.Body = fmt.Sprintf("%s: expected %s; the check failed: %s.", summary, expected, observedText)
	}
	s.notify(ctx, n)
	if status != outcomeMiss {
		return
	}
	// A miss is the agent's to look into, in the conversation it came from.
	s.runApprovalTurn(ctx, row, approvalTurn{
		runID:   "run_o_" + id,
		display: "Outcome check: " + summary + " missed (" + expected + "; " + measured + ")",
		instruction: fmt.Sprintf("A change made earlier (%s) was expected to leave %s; when checked it %s, so it missed. "+
			"Find out why from the data, say what you found, and propose a correction or an undo if one is needed. "+
			"If nothing should be done, say so.", summary, expected, measured),
		fallback: "Outcome check: " + summary + " missed: expected " + expected + "; " + measured + ".",
		context:  map[string]any{"_outcome": map[string]any{"expected": exp, "observed": observed, "status": status}},
		notifyTitle: "The agent looked into a change that missed", notifyKey: "outcome-turn:" + id, notifyAlways: true,
	})
}

// --- the ledger ----------------------------------------------------------------

type reliabilityTally struct {
	Key     string   `json:"key"`
	Checked int      `json:"checked"`
	Hits    int      `json:"hits"`
	Misses  int      `json:"misses"`
	HitRate *float64 `json:"hit_rate"`
}

func (t *reliabilityTally) add(status string) {
	t.Checked++
	if status == outcomeHit {
		t.Hits++
	} else {
		t.Misses++
	}
	rate := float64(t.Hits) / float64(t.Checked)
	t.HitRate = &rate
}

// reliabilityLedger tallies the caller's checked changes: overall, per
// routine (watch), per saved agent, and per kind of change. A check that
// could not run counts as neither hit nor miss.
func (s *graphjinService) reliabilityLedger(ctx context.Context) (map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, errors.New("reliability requires a signed-in user")
	}
	rows, err := s.internalStoreAllRows(ctx, "approvals",
		`where: { owner_id: { eq: $owner_id }, outcome_status: { in: ["hit", "miss"] } }`, approvalStoreFields,
		map[string]any{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	overall := &reliabilityTally{Key: "all"}
	groups := map[string]map[string]*reliabilityTally{"routine": {}, "agent": {}, "change": {}}
	tally := func(group, key, status string) {
		if key == "" {
			return
		}
		t := groups[group][key]
		if t == nil {
			t = &reliabilityTally{Key: key}
			groups[group][key] = t
		}
		t.add(status)
	}
	for _, row := range rows {
		if stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		status := stringMapValue(row, "outcome_status")
		overall.add(status)
		tally("routine", stringMapValue(row, "watch_id"), status)
		tally("agent", stringMapValue(row, "profile"), status)
		summary := mapFromAny(parseJSONValue(stringMapValue(row, "summary_json")))
		var actions []string
		for _, a := range anySliceFrom(summary["actions"]) {
			actions = append(actions, stringFromAny(a))
		}
		tally("change", strings.Join(actions, "+")+" "+stringFromAny(summary["table"]), status)
	}
	out := map[string]any{"overall": overall}
	for group, tallies := range groups {
		list := make([]*reliabilityTally, 0, len(tallies))
		for _, t := range tallies {
			list = append(list, t)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Checked > list[j].Checked || (list[i].Checked == list[j].Checked && list[i].Key < list[j].Key) })
		out["by_"+group] = list
	}
	return out, nil
}

// AgentReliability serves GET /api/v1/agent/reliability: the caller's
// checked changes, tallied.
func (s1 *HttpService) AgentReliability(ah auth.HandlerFunc, ns *string) http.Handler {
	return apiV1Handler(s1, ns, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if !s.approvalsEnabled() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "write approvals are disabled"})
			return
		}
		if r.Method != http.MethodGet {
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "read the ledger with GET"})
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		ledger, err := s.reliabilityLedger(ctx)
		if err != nil {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
			return
		}
		writeJSONStatus(w, http.StatusOK, ledger)
	}), ah)
}

// expectationExample is an expect argument shaped for a change to table, for
// the model to adapt.
func expectationExample(table string) string {
	if table == "" {
		table = "the_table"
	}
	return fmt.Sprintf(`{query: "query { %s(where: { ...the condition the change should bring about... }) { count_id } }", path: "%s.0.count_id", op: "increase", value: 1, within_hours: 24}`, table, table)
}
