package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
)

// Budgets cap model usage over a rolling 24 hours: per person (their own runs
// and runs made on their behalf) and per watch routine (its runs and the
// continuations of its writes). Every agent run goes through budgetedRunner,
// which refuses a run over a cap and records what each run used in the
// agent_usage store table. A refused run is a blocked reply with the
// budget_exceeded refusal, and its owner gets a notification once a day.

const (
	defaultUserDailyTokens    = 2_000_000
	defaultRoutineDailyTokens = 200_000
	budgetWindow              = 24 * time.Hour
	usageRetention            = 48 * time.Hour
	usageStoreFields          = `id owner_id account_id delegated_by run_as_id kind ref tokens_in tokens_out llm_calls created_at`
	refusalBudgetExceeded     = "budget_exceeded"
)

func configBudgetsEnabled(conf *Config) bool {
	if conf == nil || !conf.Agent.Enabled || !conf.Core.Artifacts.Enabled {
		return false
	}
	if conf.Agent.Budgets.Enabled != nil {
		return *conf.Agent.Budgets.Enabled
	}
	return effectiveMode(conf) == "agentic"
}

func (s *graphjinService) budgetsEnabled() bool {
	return s != nil && configBudgetsEnabled(s.conf)
}

func (s *graphjinService) budgetCaps() (user, routine int64) {
	user, routine = defaultUserDailyTokens, defaultRoutineDailyTokens
	if s == nil || s.conf == nil {
		return
	}
	if v := s.conf.Agent.Budgets.UserDailyTokens; v > 0 {
		user = v
	}
	if v := s.conf.Agent.Budgets.RoutineDailyTokens; v > 0 {
		routine = v
	}
	return
}

// budgetedRunner checks the caps before a run and records its usage after.
type budgetedRunner struct {
	inner   graphjinAgentRunner
	service *graphjinService
}

func (s *graphjinService) withBudgets(runner graphjinAgentRunner) graphjinAgentRunner {
	if runner == nil || !s.budgetsEnabled() {
		return runner
	}
	return budgetedRunner{inner: runner, service: s}
}

func (r budgetedRunner) Run(ctx context.Context, req gjagent.Request) (gjagent.Response, error) {
	s := r.service
	scope := s.usageScope(ctx)
	if scope.person != "" {
		if resp, over := s.checkBudget(ctx, scope); over {
			return resp, nil
		}
	}
	resp, err := r.inner.Run(ctx, req)
	if scope.person != "" {
		s.recordUsage(ctx, scope, resp)
	}
	return resp, err
}

// usageScope is who a run counts against.
type usageScope struct {
	identity    string // the identity the run executes as
	person      string // the person it counts against: the delegator, else the identity
	accountID   string
	delegatedBy string
	kind        string // chat, routine or continuation
	ref         string // the watch, for a routine; else the thread
}

func (s *graphjinService) usageScope(ctx context.Context) usageScope {
	identity, _ := artifactUserID(ctx)
	accountID, _ := identityVarString(ctx, "account_id")
	origin := runOriginFrom(ctx)
	scope := usageScope{identity: identity, person: identity, accountID: accountID, delegatedBy: origin.DelegatedBy, kind: origin.Origin}
	if origin.DelegatedBy != "" {
		scope.person = origin.DelegatedBy
	}
	if origin.WatchID != "" {
		scope.ref = origin.WatchID
	} else {
		scope.ref = runThreadFrom(ctx).threadID
	}
	return scope
}

func (s *graphjinService) checkBudget(ctx context.Context, scope usageScope) (gjagent.Response, bool) {
	userCap, routineCap := s.budgetCaps()
	since := notificationTime(time.Now().Add(-budgetWindow))
	used, err := s.usageSince(ctx, `or: [{ owner_id: { eq: $person } }, { delegated_by: { eq: $person } }], created_at: { gt: $since }`,
		map[string]any{"person": scope.person, "since": since})
	if err == nil && used >= userCap {
		return s.budgetRefusal(ctx, scope, "user", used, userCap), true
	}
	if runOriginFrom(ctx).WatchID != "" {
		used, err := s.usageSince(ctx, `ref: { eq: $ref }, kind: { in: ["routine", "continuation"] }, created_at: { gt: $since }`,
			map[string]any{"ref": scope.ref, "since": since})
		if err == nil && used >= routineCap {
			return s.budgetRefusal(ctx, scope, "routine", used, routineCap), true
		}
	}
	// A budget that cannot be read fails open: the ledger is bookkeeping, and
	// an unreadable store must not stop every run.
	return gjagent.Response{}, false
}

// usageSince sums tokens in the ledger in the store, not in Go.
func (s *graphjinService) usageSince(ctx context.Context, where string, vars map[string]any) (int64, error) {
	root, schema, ok := s.internalStoreRoot("agent_usage")
	if !ok {
		return 0, fmt.Errorf("agent usage ledger is not available")
	}
	field := internalStoreQueryField(root, schema, "where: { "+where+" }")
	out, err := s.internalStoreGraphQL(ctx, fmt.Sprintf("query { %s { sum_tokens_in sum_tokens_out } }", field), vars)
	if err != nil {
		return 0, err
	}
	return aggregateSum(out[root], "sum_tokens_in") + aggregateSum(out[root], "sum_tokens_out"), nil
}

func aggregateSum(raw json.RawMessage, field string) int64 {
	var one map[string]any
	if json.Unmarshal(raw, &one) == nil && one != nil {
		return int64MapValue(one, field)
	}
	var rows []map[string]any
	if json.Unmarshal(raw, &rows) == nil && len(rows) != 0 {
		return int64MapValue(rows[0], field)
	}
	return 0
}

func (s *graphjinService) budgetRefusal(ctx context.Context, scope usageScope, which string, used, limit int64) gjagent.Response {
	subject := "Your agent budget"
	if which == "routine" {
		subject = "This routine's agent budget"
	}
	answer := fmt.Sprintf("%s for the last 24 hours is used up (%d of %d tokens), so this run did not start. It frees up as older runs pass the 24-hour mark.", subject, used, limit)
	s.notify(ctx, notification{
		OwnerID: scope.person, AccountID: scope.accountID, DelegatedBy: scope.delegatedBy,
		Kind: refusalBudgetExceeded, Severity: notificationSeverityWarning,
		Title: subject + " is used up for today", Body: answer,
		RefKind: which, RefID: scope.ref,
		DedupeKey: "budget:" + which + ":" + scope.ref + ":" + scope.person + ":" + time.Now().UTC().Format("2006-01-02"),
	})
	return gjagent.Response{
		Status: gjagent.StatusBlocked,
		Answer: answer,
		Refusal: &gjagent.Refusal{
			Code: refusalBudgetExceeded, BlockedAction: "agent_run",
			Because:     []string{fmt.Sprintf("%s budget: %d of %d tokens used in the last 24 hours", which, used, limit)},
			PolicyFinal: true, Retryable: true,
		},
	}
}

var lastUsagePrune atomic.Int64

// recordUsage writes the run's usage to the ledger and, at most once a
// minute, drops rows older than the window needs.
func (s *graphjinService) recordUsage(ctx context.Context, scope usageScope, resp gjagent.Response) {
	usage := gjagent.SummarizeUsage(resp.Usage)
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.LLMCalls == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	now := time.Now()
	input := map[string]any{
		"id": "use_" + taskRandomHex(), "owner_id": scope.identity, "account_id": scope.accountID,
		"delegated_by": scope.delegatedBy, "run_as_id": "", "kind": scope.kind, "ref": scope.ref,
		"tokens_in": usage.PromptTokens, "tokens_out": usage.CompletionTokens, "llm_calls": usage.LLMCalls,
		"created_at": notificationTime(now),
	}
	if scope.delegatedBy != "" {
		input["run_as_id"] = scope.identity
	}
	if _, err := s.internalStoreMutationRows(ctx, "agent_usage", `insert: $input`, "id", map[string]any{"input": input}); err != nil && s.log != nil {
		s.log.Warnf("agent usage ledger: %v", err)
	}
	if last := lastUsagePrune.Load(); now.Unix()-last >= 60 && lastUsagePrune.CompareAndSwap(last, now.Unix()) {
		_, _ = s.internalStoreMutationRows(ctx, "agent_usage", `where: { created_at: { lt: $cutoff } }, delete: true`, "id",
			map[string]any{"cutoff": notificationTime(now.Add(-usageRetention))})
	}
}

func usageDDL(dbType, schema string) []string {
	table := artifactTableName(dbType, schema, "agent_usage")
	counter := "BIGINT NOT NULL DEFAULT 0"
	text := "TEXT NOT NULL DEFAULT ''"
	switch dbType {
	case "sqlite":
		counter = "INTEGER NOT NULL DEFAULT 0"
	case "mysql", "mariadb":
		text = "VARCHAR(191) NOT NULL DEFAULT ''"
	}
	id := "TEXT PRIMARY KEY"
	if dbType == "mysql" || dbType == "mariadb" {
		id = "VARCHAR(191) PRIMARY KEY"
	}
	cols := []string{
		"id " + id, "owner_id " + text, "account_id " + text, "delegated_by " + text, "run_as_id " + text,
		"kind " + text, "ref " + text, "tokens_in " + counter, "tokens_out " + counter, "llm_calls " + counter, "created_at " + text,
	}
	quote := quoteSQLIdent
	if dbType == "postgres" || dbType == "" {
		quote = quotePGIdent
	}
	stmts := []string{fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s\n)", table, strings.Join(cols, ",\n"))}
	if dbType == "mysql" || dbType == "mariadb" {
		return []string{fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (\n%s,\nKEY idx_graphjin_agent_usage_owner (owner_id, created_at),\nKEY idx_graphjin_agent_usage_ref (ref, created_at)\n) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci", table, strings.Join(cols, ",\n"))}
	}
	stmts = append(stmts,
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, created_at)`, quote("idx_graphjin_agent_usage_owner"), table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (delegated_by, created_at)`, quote("idx_graphjin_agent_usage_delegated"), table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (ref, created_at)`, quote("idx_graphjin_agent_usage_ref"), table),
	)
	return stmts
}
