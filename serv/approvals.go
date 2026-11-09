package serv

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
)

// Write approvals. With agent.write_approval required, the data agent never
// runs an application-data or API write itself: it hands the final query to
// ProposeWrite, which dry-runs it inside a rolled-back transaction and stores
// it here as a pending approval. The user decides later, outside any agent
// run, through the gj_approval GraphQL root or POST /api/v1/agent/approvals/{id};
// only the exact stored query runs, as the user who asked for it.

const (
	approvalsRootTable  = "gj_approval"
	approvalStoreFields = `id thread_id run_id query variables_json namespace actions summary_json preview_json hash status result_json error account_id owner_id owner_role created_at expires_at decided_at origin watch_id event_id profile decided_by delegated_by run_as_id run_as_role`

	approvalStatusPending  = "pending"
	approvalStatusRejected = "rejected"
	approvalStatusExpired  = "expired"
	approvalStatusExecuted = "executed"
	approvalStatusFailed   = "failed"

	approvalDefaultTTL     = 24 * time.Hour
	approvalPreviewRowsMax = 20
)

var (
	errApprovalNotFound = errors.New("approval not found")
	errApprovalStale    = errors.New("approval hash does not match: the proposal changed or the hash is wrong")
)

// dryRunDatabaseTypes are the engines whose transactions roll back every
// effect of a GraphJin mutation. Anything else (APIs, NanoDB, warehouses,
// managed translations) is held without a preview: a "dry run" against an
// API would really call it.
var dryRunDatabaseTypes = map[string]bool{
	"postgres": true, "mysql": true, "mariadb": true, "sqlite": true, "oracle": true, "mssql": true,
}

// resolvedWriteApproval is the effective agent.write_approval: the configured
// value, else required in agentic mode when the artifact store can hold
// proposals, else off.
func resolvedWriteApproval(conf *Config) string {
	if conf == nil {
		return gjagent.WriteApprovalOff
	}
	if v := strings.TrimSpace(conf.Agent.WriteApproval); v != "" {
		return v
	}
	if effectiveMode(conf) == "agentic" && conf.Core.Artifacts.Enabled {
		return gjagent.WriteApprovalRequired
	}
	return gjagent.WriteApprovalOff
}

func configApprovalsEnabled(conf *Config) bool {
	return conf != nil && conf.Agent.Enabled && conf.Core.Artifacts.Enabled &&
		resolvedWriteApproval(conf) == gjagent.WriteApprovalRequired
}

func (s *graphjinService) approvalsEnabled() bool {
	return s != nil && configApprovalsEnabled(s.conf)
}

func (s *graphjinService) approvalTTL() time.Duration {
	if s == nil || s.conf == nil {
		return approvalDefaultTTL
	}
	if d, err := time.ParseDuration(strings.TrimSpace(s.conf.Agent.WriteApprovalTTL)); err == nil && d > 0 {
		return d
	}
	return approvalDefaultTTL
}

// --- run thread ---------------------------------------------------------------

type runThreadKey struct{}

type runThreadRef struct{ threadID, runID string }

// withRunThread records the thread and run a turn belongs to, so a write it
// holds is filed against them and its outcome is reported there.
func withRunThread(ctx context.Context, threadID, runID string) context.Context {
	return context.WithValue(ctx, runThreadKey{}, runThreadRef{threadID: threadID, runID: runID})
}

func runThreadFrom(ctx context.Context) runThreadRef {
	ref, _ := ctx.Value(runThreadKey{}).(runThreadRef)
	return ref
}

// Approval origins: who started the run that proposed a write.
const (
	approvalOriginChat         = "chat"
	approvalOriginRoutine      = "routine"
	approvalOriginContinuation = "continuation"
)

type runOriginKey struct{}

// runOrigin says what started a run, so a write it holds can be traced back
// to it and, after the decision, continued in its context.
type runOrigin struct {
	Origin  string
	WatchID string
	EventID string
	Profile string
	// DelegatedBy is the person a service-identity run acts for; RunAsID and
	// RunAsRole are the identity it runs as.
	DelegatedBy string
	RunAsID     string
	RunAsRole   string
}

func withRunOrigin(ctx context.Context, origin runOrigin) context.Context {
	return context.WithValue(ctx, runOriginKey{}, origin)
}

func runOriginFrom(ctx context.Context) runOrigin {
	origin, _ := ctx.Value(runOriginKey{}).(runOrigin)
	if origin.Origin == "" {
		origin.Origin = approvalOriginChat
	}
	return origin
}

// --- proposing ----------------------------------------------------------------

// approvalHash pins a proposal to its exact query and to the person who asked
// for it, so a hash cannot be reused for another write or another caller.
func approvalHash(query, variablesJSON, namespace, ownerID, ownerRole string) string {
	raw, _ := json.Marshal([]string{query, variablesJSON, namespace, ownerID, ownerRole})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ProposeWrite dry-runs a held write and stores it as a pending approval. The
// dry run goes through ExecuteGraphQL, so a write that would fail returns the
// same failure the real write would have, and nothing is stored.
func (r *serviceAgentRuntime) ProposeWrite(ctx context.Context, args map[string]any) (any, *gjagent.Approval, error) {
	s := r.service
	if !s.approvalsEnabled() {
		return nil, nil, fmt.Errorf("this write did NOT execute: write approvals need the artifact store")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil, fmt.Errorf("this write did NOT execute: a write that needs approval needs a signed-in user to approve it")
	}
	if delegator := runOriginFrom(ctx).DelegatedBy; delegator != "" {
		// A service-identity run proposes on behalf of the person who
		// delegated it: they approve it, and it runs as the service.
		ownerID = delegator
	}
	query := strings.TrimSpace(stringArg(args, "query"))
	varsJSON := "{}"
	if v, ok := args["variables"]; ok && v != nil {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, nil, err
		}
		varsJSON = string(raw)
	}
	namespace := stringArg(args, "namespace")
	actions := gjagent.MutationActionsWith(query, s.apiRootActions())

	database, table, reason, compileErrors := s.approvalDryRunTarget(ctx, query, varsJSON)
	if len(compileErrors) != 0 {
		// The write cannot compile, so it would fail: report it as the failed
		// execution it is. Never run it to find out.
		errs := make([]any, 0, len(compileErrors))
		for _, message := range compileErrors {
			errs = append(errs, map[string]any{"message": message})
		}
		return map[string]any{"errors": errs}, nil, nil
	}
	out := any(map[string]any{"data": map[string]any{}})
	preview := map[string]any{"available": false, "reason": reason}
	if reason == "" {
		dryRun, rows, changed, err := r.dryRunWrite(ctx, database, args)
		if err != nil {
			return nil, nil, err
		}
		if dryRunFailed(dryRun) {
			return dryRun, nil, nil
		}
		out = dryRun
		// rows is how many rows the write changes: the engine's own count when
		// it keeps one, else the rows the write returned, which can undercount.
		preview = map[string]any{"available": true, "rows": len(rows), "counted": changed >= 0, "sample": capRows(rows, approvalPreviewRowsMax)}
		if changed >= 0 {
			preview["rows"] = changed
		}
	}

	summary := approvalSummary(actions, table, preview)
	role := runtimeRoleClass(ctx)
	accountID, _ := identityVarString(ctx, "account_id")
	hash := approvalHash(query, varsJSON, namespace, ownerID, role)
	ref := runThreadFrom(ctx)
	now := time.Now().UTC()
	expires := now.Add(s.approvalTTL())
	actionsJSON, _ := json.Marshal(actions)
	summaryJSON, _ := json.Marshal(map[string]any{"text": summary, "table": table, "database": database, "actions": actions})
	previewJSON, _ := json.Marshal(preview)
	input := map[string]any{
		"id": "apr_" + taskRandomHex(), "thread_id": ref.threadID, "run_id": ref.runID,
		"query": query, "variables_json": varsJSON, "namespace": namespace, "actions": string(actionsJSON),
		"summary_json": string(summaryJSON), "preview_json": string(previewJSON), "hash": hash,
		"status": approvalStatusPending, "result_json": "", "error": "",
		"account_id": accountID, "owner_id": ownerID, "owner_role": role,
		"created_at": now.Format(time.RFC3339Nano), "expires_at": expires.Format(time.RFC3339Nano), "decided_at": "",
	}
	origin := runOriginFrom(ctx)
	for k, v := range map[string]string{
		"origin": origin.Origin, "watch_id": origin.WatchID, "event_id": origin.EventID, "profile": origin.Profile,
		"decided_by": "", "delegated_by": origin.DelegatedBy, "run_as_id": origin.RunAsID, "run_as_role": origin.RunAsRole,
	} {
		input[k] = v
	}
	approval := &gjagent.Approval{
		ID: stringFromAny(input["id"]), Hash: hash, Summary: summary,
		Query: query, Variables: parseJSONValue(varsJSON), Preview: preview,
		ExpiresAt: expires.Format(time.RFC3339),
	}

	// A configured autonomy rule may run the write now instead of holding it.
	decision := gjagent.AutonomyDecision{Level: gjagent.AutonomyAsk}
	// Autonomy needs the engine's own count of changed rows: max_rows must not
	// rest on a count that can be low.
	if preview["available"] == true && preview["counted"] == true && len(gjagent.MutationRootFields(query)) == 1 {
		decision = gjagent.ResolveAutonomy(s.conf.Agent.Autonomy, actions, table, intFromAny(preview["rows"]))
	}
	if decision.Level != gjagent.AutonomyAsk {
		if err := s.approvalStillPermitted(ctx, input); err == nil {
			return r.runAutonomousWrite(ctx, input, approval, decision)
		}
	}

	if _, err := s.internalStoreMutationRows(ctx, "approvals", `insert: $input`, approvalStoreFields, map[string]any{"input": input}); err != nil {
		return nil, nil, fmt.Errorf("this write did NOT execute: storing it for approval failed: %w", err)
	}
	s.notify(ctx, approvalPendingNotification(input, summary))
	return out, approval, nil
}

// runAutonomousWrite runs a write an autonomy rule allows, as the run's own
// identity, and records it as an approval decided by that rule, so every
// change the agent made on its own is on the record. With notify, the owner
// is told.
func (r *serviceAgentRuntime) runAutonomousWrite(ctx context.Context, input map[string]any, approval *gjagent.Approval, decision gjagent.AutonomyDecision) (any, *gjagent.Approval, error) {
	s := r.service
	var rc core.RequestConfig
	if ns := stringFromAny(input["namespace"]); ns != "" {
		rc.SetNamespace(ns)
	}
	res, runErr := s.gj.GraphQL(ctx, stringFromAny(input["query"]), json.RawMessage(stringFromAny(input["variables_json"])), &rc)
	status, resultJSON, errText := approvalStatusExecuted, "", ""
	out := map[string]any{"data": map[string]any{}}
	if res != nil {
		raw, _ := json.Marshal(res)
		resultJSON = string(raw)
		if len(res.Data) != 0 {
			out["data"] = parseJSONValue(string(res.Data))
		}
	}
	if runErr != nil {
		status, errText = approvalStatusFailed, runErr.Error()
	} else if res != nil && len(res.Errors) != 0 {
		status, errText = approvalStatusFailed, res.Errors[0].Message
	}
	if errText != "" {
		out["errors"] = []any{map[string]any{"message": errText}}
	}
	input["status"] = status
	input["result_json"] = resultJSON
	input["error"] = truncateUTF8(errText, 4096)
	input["decided_by"] = decision.Rule
	input["decided_at"] = storeNow()
	if _, err := s.internalStoreMutationRows(ctx, "approvals", `insert: $input`, approvalStoreFields, map[string]any{"input": input}); err != nil && s.log != nil {
		// The write ran; losing its record must not report it as not run.
		s.log.Warnf("autonomous write %s ran but its record failed: %v", input["id"], err)
	}
	approval.Status, approval.DecidedBy = status, decision.Rule
	if decision.Level == gjagent.AutonomyNotify && status == approvalStatusExecuted {
		s.notify(ctx, notification{
			OwnerID: stringFromAny(input["owner_id"]), AccountID: stringFromAny(input["account_id"]), DelegatedBy: stringFromAny(input["delegated_by"]),
			Kind: notificationAutonomousWrite, Severity: notificationSeverityInfo,
			Title: "The agent made a change on its own", Body: approval.Summary + " (allowed by " + decision.Rule + ")",
			RefKind: "approval", RefID: approval.ID, Link: approvalLink(stringFromAny(input["thread_id"])),
			DedupeKey: "autonomy:" + approval.ID,
		})
	}
	return out, approval, nil
}

// approvalPendingNotification asks the owner to decide a held write. Deciding
// it settles the notification (approvalPendingKey).
func approvalPendingNotification(row map[string]any, summary string) notification {
	id := stringFromAny(row["id"])
	return notification{
		OwnerID: stringFromAny(row["owner_id"]), AccountID: stringFromAny(row["account_id"]),
		Kind: notificationApprovalPending, Severity: notificationSeverityAction,
		Title: "A change is waiting for your approval", Body: summary,
		RefKind: "approval", RefID: id, Link: approvalLink(stringFromAny(row["thread_id"])),
		DedupeKey: approvalPendingKey(id),
	}
}

func approvalPendingKey(approvalID string) string { return "approval:" + approvalID + ":pending" }

func approvalLink(threadID string) string {
	if threadID != "" {
		return "/user/agent?thread=" + threadID
	}
	return "/user/inbox"
}

// approvalDryRunTarget names the database a write would change and its root
// table, or why it cannot be dry-run, or the errors that stop it compiling.
// It compiles the query without running it.
func (s *graphjinService) approvalDryRunTarget(ctx context.Context, query, varsJSON string) (database, table, reason string, compileErrors []string) {
	if s.gj == nil {
		return "", "", "the engine is not ready", nil
	}
	// Compile as the caller: a JWT identity carries its roles in the identity
	// roles, not the user role, and an empty role compiles as anon.
	exp, err := s.gj.ExplainQuery(query, json.RawMessage(varsJSON), s.gj.RequestRole(ctx))
	if err != nil {
		return "", "", "", []string{err.Error()}
	}
	if exp == nil {
		return "", "", "the write could not be compiled for a preview", nil
	}
	if len(exp.Tables) != 0 {
		table = exp.Tables[0].Table
	}
	if len(exp.Errors) != 0 {
		return "", table, "", exp.Errors
	}
	if exp.MultiDatabase {
		return "", table, "the write spans more than one database", nil
	}
	// An explanation without a database ran on the engine's default one,
	// which is not the metadata store when the artifacts live elsewhere.
	database = exp.Database
	if database == "" {
		database = s.gj.DefaultDatabase()
	}
	if database == "" {
		database = core.DefaultDBName
	}
	kind, ok := s.dryRunDatabaseKind(database)
	if !ok || !dryRunDatabaseTypes[kind] || s.dbs[database] == nil {
		return database, table, "this source cannot be dry-run without side effects", nil
	}
	return database, table, "", nil
}

// dryRunDatabaseKind is a configured database's engine, or false for a
// managed or unknown one.
func (s *graphjinService) dryRunDatabaseKind(database string) (string, bool) {
	dbConf, ok := s.conf.Core.Databases[database]
	if !ok && len(s.conf.Core.Databases) == 0 {
		dbConf = core.DatabaseConfig{Type: s.conf.Core.DBType}
		ok = true
	}
	if !ok || dbConf.ManagedType != "" {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(dbConf.Type)), true
}

// dryRunWrite runs the write inside a transaction it always rolls back, and
// returns the result with the rows the write returned.
func (r *serviceAgentRuntime) dryRunWrite(ctx context.Context, database string, args map[string]any) (any, []any, int64, error) {
	db := r.service.dbs[database]
	if db == nil {
		return nil, nil, -1, fmt.Errorf("this write did NOT execute: its database %q is not connected", database)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, -1, fmt.Errorf("this write did NOT execute: its preview could not start: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	dryCtx := gjagent.WithDryRun(ctx, tx)
	query := stringArg(args, "query")
	// How many rows the write changes. An update or delete is counted before
	// it runs, by its own where clause: its returned rows can undercount. An
	// insert's returned rows are the rows it inserted. Anything else is
	// unknown (-1).
	changed := int64(-1)
	if countQuery, ok := gjagent.MutationTargetCountQuery(query); ok {
		countArgs := map[string]any{"query": countQuery, "variables": args["variables"], "namespace": args["namespace"]}
		if counted, err := r.ExecuteGraphQL(dryCtx, countArgs); err == nil {
			if n, ok := countFromResult(counted); ok {
				changed = n
			}
		}
	}
	out, err := r.ExecuteGraphQL(dryCtx, args)
	if err != nil {
		return out, nil, -1, err
	}
	rows := resultRows(out)
	if insertOnly(query) {
		changed = int64(len(rows))
	}
	return out, rows, changed, nil
}

// countFromResult reads count_id from a single-root count query's result.
func countFromResult(out any) (int64, bool) {
	value := resultMap(out)
	if errs, _ := value["errors"].([]any); len(errs) != 0 {
		return 0, false
	}
	data, _ := value["data"].(map[string]any)
	for _, root := range data {
		switch v := root.(type) {
		case map[string]any:
			if _, ok := v["count_id"]; ok {
				return int64(intFromAny(v["count_id"])), true
			}
		case []any:
			if len(v) == 1 {
				if m, ok := v[0].(map[string]any); ok {
					if _, ok := m["count_id"]; ok {
						return int64(intFromAny(m["count_id"])), true
					}
				}
			}
		}
	}
	return 0, false
}

func insertOnly(query string) bool {
	actions := gjagent.MutationActions(query)
	if len(actions) == 0 {
		return false
	}
	for _, action := range actions {
		if action != gjagent.CapabilityActionDataInsert {
			return false
		}
	}
	return true
}

// dryRunFailed reports a dry run that returned errors and no data.
func dryRunFailed(out any) bool {
	value := resultMap(out)
	errs, _ := value["errors"].([]any)
	return len(errs) != 0 && len(resultRows(out)) == 0
}

func resultMap(out any) map[string]any {
	raw, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}

// resultRows flattens a mutation result's root selections into rows.
func resultRows(out any) []any {
	data, _ := resultMap(out)["data"].(map[string]any)
	var rows []any
	for _, value := range data {
		switch v := value.(type) {
		case []any:
			rows = append(rows, v...)
		case map[string]any:
			rows = append(rows, v)
		}
	}
	return rows
}

func capRows(rows []any, max int) []any {
	if len(rows) > max {
		return rows[:max]
	}
	return rows
}

// approvalSummary is the one line a person reads before deciding.
func approvalSummary(actions []string, table string, preview map[string]any) string {
	verb := "change"
	if len(actions) == 1 {
		switch actions[0] {
		case gjagent.CapabilityActionDataInsert:
			verb = "insert"
		case gjagent.CapabilityActionDataUpdate:
			verb = "update"
		case gjagent.CapabilityActionDataDelete:
			verb = "delete"
		case gjagent.CapabilityActionAPIWrite, gjagent.CapabilityActionAPIDelete:
			verb = "call"
		}
	}
	if table == "" {
		table = "the data"
	}
	if available, _ := preview["available"].(bool); !available {
		return fmt.Sprintf("%s %s (not previewed: %s)", verb, table, stringFromAny(preview["reason"]))
	}
	n := intFromAny(preview["rows"])
	noun := "rows"
	if n == 1 {
		noun = "row"
	}
	switch verb {
	case "insert":
		return fmt.Sprintf("insert %d %s into %s", n, noun, table)
	case "delete":
		return fmt.Sprintf("delete %d %s from %s", n, noun, table)
	default:
		return fmt.Sprintf("%s %d %s in %s", verb, n, noun, table)
	}
}

// --- deciding -----------------------------------------------------------------

// decideApproval applies the user's decision. Only the owner decides, only a
// pending, unexpired proposal with the matching hash, and only while the
// owner still holds the write's capability; the exact stored query then runs
// once, as the owner.
func (s *graphjinService) decideApproval(ctx context.Context, id, decision, expectedHash string) (map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("deciding an approval needs a signed-in user")
	}
	decision = strings.ToLower(strings.TrimSpace(decision))
	if decision != "approve" && decision != "reject" {
		return nil, fmt.Errorf("gj_approval decision must be approve or reject")
	}
	row, err := s.approvalRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil || stringMapValue(row, "owner_id") != ownerID {
		return nil, errApprovalNotFound
	}
	if status := approvalStatus(row, time.Now().UTC()); status != approvalStatusPending {
		if status == approvalStatusExpired {
			_, _ = s.settleApproval(ctx, id, approvalStatusExpired, "", "")
		}
		return nil, fmt.Errorf("approval %s is %s, not pending", id, status)
	}
	if strings.TrimSpace(expectedHash) == "" || expectedHash != stringMapValue(row, "hash") ||
		approvalHash(stringMapValue(row, "query"), stringMapValue(row, "variables_json"), stringMapValue(row, "namespace"),
			stringMapValue(row, "owner_id"), stringMapValue(row, "owner_role")) != expectedHash {
		return nil, errApprovalStale
	}
	if decision == "reject" {
		settled, err := s.settleApproval(ctx, id, approvalStatusRejected, "", "")
		if err == nil {
			s.continueAfterDecision(ctx, id)
		}
		return settled, err
	}
	// A write a service-identity run proposed runs as that identity; the
	// owner only decides it.
	execCtx := ctx
	if runAs := stringMapValue(row, "run_as_id"); runAs != "" {
		execCtx = s.ownerContext(ctx, runAs, stringMapValue(row, "run_as_role"), stringMapValue(row, "account_id"))
	}
	if err := s.approvalStillPermitted(execCtx, row); err != nil {
		return nil, err
	}
	// Claim it before running, so two approvals of one proposal run it once:
	// the update only applies while the row is pending, and the claim's own
	// timestamp reading back proves this decision won.
	token := storeNow()
	if _, err := s.internalStoreMutationRows(ctx, "approvals",
		`where: { id: { eq: $id }, status: { eq: "pending" } }, update: $input`, "id",
		map[string]any{"id": id, "input": map[string]any{"status": "approved", "decided_at": token}}); err != nil {
		return nil, err
	}
	if claimed, err := s.approvalRow(ctx, id); err != nil {
		return nil, err
	} else if claimed == nil || stringMapValue(claimed, "status") != "approved" || stringMapValue(claimed, "decided_at") != token {
		return nil, fmt.Errorf("approval %s was already decided", id)
	}
	var rc core.RequestConfig
	if ns := stringMapValue(row, "namespace"); ns != "" {
		rc.SetNamespace(ns)
	}
	res, runErr := s.gj.GraphQL(execCtx, stringMapValue(row, "query"), json.RawMessage(stringMapValue(row, "variables_json")), &rc)
	status, resultJSON, errText := approvalStatusExecuted, "", ""
	if res != nil {
		raw, _ := json.Marshal(res)
		resultJSON = string(raw)
	}
	if runErr != nil {
		status, errText = approvalStatusFailed, runErr.Error()
	} else if res != nil && len(res.Errors) != 0 {
		status, errText = approvalStatusFailed, res.Errors[0].Message
	}
	settled, err := s.settleApproval(ctx, id, status, resultJSON, errText)
	if err == nil {
		s.continueAfterDecision(ctx, id)
	}
	return settled, err
}

// approvalStillPermitted re-checks, at decision time, everything that let the
// write be proposed: writes are still enabled and the owner still holds each
// capability the write needs.
func (s *graphjinService) approvalStillPermitted(ctx context.Context, row map[string]any) error {
	if s.conf.Agent.ReadOnly || !s.conf.MCP.AllowMutations {
		return fmt.Errorf("writes are disabled on this server, so the approval cannot run")
	}
	var actions []string
	_ = json.Unmarshal([]byte(stringMapValue(row, "actions")), &actions)
	profile := s.agentCapabilityProfile(ctx)
	for _, action := range actions {
		granted := false
		if profile != nil {
			for _, allowed := range profile.AllowedActions {
				if strings.EqualFold(allowed, action) {
					granted = true
					break
				}
			}
		}
		if !granted {
			return fmt.Errorf("you no longer have %s, so the approval cannot run", action)
		}
	}
	return nil
}

func (s *graphjinService) approvalRow(ctx context.Context, id string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "approvals", `where: { id: { eq: $id } }, limit: 1`, approvalStoreFields, map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (s *graphjinService) settleApproval(ctx context.Context, id, status, resultJSON, errText string) (map[string]any, error) {
	input := map[string]any{"status": status, "decided_at": storeNow()}
	if resultJSON != "" {
		input["result_json"] = resultJSON
	}
	if errText != "" {
		input["error"] = truncateUTF8(errText, 4096)
	}
	rows, err := s.internalStoreMutationRows(ctx, "approvals", `where: { id: { eq: $id } }, update: $input`, approvalStoreFields,
		map[string]any{"id": id, "input": input})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errApprovalNotFound
	}
	// A decided or expired approval no longer waits for anyone.
	s.markNotificationSeenByKey(ctx, stringMapValue(rows[0], "owner_id"), approvalPendingKey(id))
	return approvalAPIRow(rows[0], time.Now().UTC()), nil
}

// approvalContinuationsInline runs continuations before decideApproval
// returns; tests set it to observe them. In production they run in the
// background, so a decision answers at once.
var approvalContinuationsInline = false

// continueAfterDecision resumes the work a held write came from. The thread
// it was proposed in gets a real turn (a run row, so a lost run is swept like
// any other), started from the decision: the agent sees the outcome and the
// original context, then verifies the effect, reports it, or proposes the
// next step. It runs as the approval's owner, never under the decider's
// request, which ends when the HTTP call does.
func (s *graphjinService) continueAfterDecision(ctx context.Context, approvalID string) {
	if !s.threadsEnabled() {
		return
	}
	ctx = context.WithoutCancel(ctx)
	row, err := s.approvalRow(ctx, approvalID)
	if err != nil || row == nil || stringMapValue(row, "thread_id") == "" {
		return
	}
	if approvalContinuationsInline {
		s.runApprovalContinuation(ctx, row)
		return
	}
	go s.runApprovalContinuation(ctx, row)
}

func (s *graphjinService) runApprovalContinuation(ctx context.Context, row map[string]any) {
	approvalID := stringMapValue(row, "id")
	threadID := stringMapValue(row, "thread_id")
	origin := runOrigin{
		Origin: approvalOriginContinuation, WatchID: stringMapValue(row, "watch_id"), EventID: stringMapValue(row, "event_id"),
		Profile: stringMapValue(row, "profile"), DelegatedBy: stringMapValue(row, "delegated_by"),
		RunAsID: stringMapValue(row, "run_as_id"), RunAsRole: stringMapValue(row, "run_as_role"),
	}
	accountID := stringMapValue(row, "account_id")
	ownerRole := stringMapValue(row, "owner_role")
	var runAs func(context.Context) context.Context
	if origin.RunAsID != "" {
		// The thread is the delegator's; the agent thinks and acts as the
		// service identity, as the run that proposed the write did.
		ownerRole = "user"
		runAs = func(c context.Context) context.Context {
			return withRunOrigin(s.ownerContext(c, origin.RunAsID, origin.RunAsRole, accountID), origin)
		}
	}
	ownerCtx := withRunOrigin(s.ownerContext(ctx, stringMapValue(row, "owner_id"), ownerRole, accountID), origin)
	agentCtx := ownerCtx
	if runAs != nil {
		agentCtx = runAs(ownerCtx)
	}

	outcome := approvalOutcomeText(row)
	if outcome == "" {
		return
	}
	req := gjagent.Request{
		Instruction: approvalContinuationInstruction(row),
		Context:     s.approvalContinuationContext(ownerCtx, row),
		Capabilities: s.agentCapabilityProfile(agentCtx),
	}
	opts := threadRunOptions{RunID: "run_c_" + approvalID, Display: approvalContinuationDisplay(row), RunAs: runAs}
	run, err := s.prepareWatchRoutineRun(ownerCtx, threadID, &req, opts)
	if err != nil {
		if s.log != nil && !errors.Is(err, errThreadNotFound) {
			s.log.Warnf("approval %s: continue in thread %s: %v", approvalID, threadID, err)
		}
		return
	}
	conf := agentConfigFromService(s.conf)
	if origin.WatchID != "" {
		// A routine's continuation is as unattended as the routine was.
		conf = watchRoutineAgentConfig(s.conf)
	}
	var runner graphjinAgentRunner = staticAgentRunner{answer: outcome}
	if built, err := s.agentEntryRunner(conf); err == nil {
		runner = continuationRunner{inner: built, fallback: outcome}
	}
	resp, _ := run.execute(ownerCtx, runner, req, s.watchRoutineSettle(req), nil)
	if origin.WatchID != "" || origin.DelegatedBy != "" {
		// The owner decided from the inbox and may not be in this thread.
		s.notify(ownerCtx, notification{
			OwnerID: stringMapValue(row, "owner_id"), AccountID: stringMapValue(row, "account_id"), DelegatedBy: origin.DelegatedBy,
			Kind: notificationRoutineAnswer, Severity: notificationSeverityInfo,
			Title: "The agent followed up on your decision", Body: truncateUTF8(strings.TrimSpace(resp.Answer), 600),
			RefKind: "thread", RefID: threadID, Link: "/user/agent?thread=" + threadID,
			DedupeKey: "continuation:" + approvalID,
		})
	}
}

// approvalSummaryText is the one-line summary of a stored approval row.
func approvalSummaryText(row map[string]any) string {
	return stringMapValue(mapFromAny(parseJSONValue(stringMapValue(row, "summary_json"))), "text")
}

// approvalOutcomeText is the plain outcome of a decision, which is also the
// reply when no model can run.
func approvalOutcomeText(row map[string]any) string {
	summary := approvalSummaryText(row)
	switch stringMapValue(row, "status") {
	case approvalStatusExecuted:
		return "Approved and applied: " + summary + "."
	case approvalStatusFailed:
		return "Approved, but the change failed and nothing was applied: " + stringMapValue(row, "error")
	case approvalStatusRejected:
		return "Rejected: " + summary + ". Nothing was changed."
	}
	return ""
}

// approvalContinuationDisplay is the thread line that starts the turn.
func approvalContinuationDisplay(row map[string]any) string {
	summary := approvalSummaryText(row)
	switch stringMapValue(row, "status") {
	case approvalStatusExecuted:
		return "Approved: " + summary
	case approvalStatusFailed:
		return "Approved, but it failed: " + summary
	default:
		return "Rejected: " + summary
	}
}

func approvalContinuationInstruction(row map[string]any) string {
	summary := approvalSummaryText(row)
	switch stringMapValue(row, "status") {
	case approvalStatusExecuted:
		return "The change you proposed earlier in this conversation (" + summary + ") was approved and has been applied. " +
			"Confirm the effect from the data if that is quick, then tell the user in a sentence or two what changed. " +
			"If the earlier plan had a next step, carry it out or propose it."
	case approvalStatusFailed:
		return "The change you proposed earlier in this conversation (" + summary + ") was approved, but it failed and nothing was applied: " +
			stringMapValue(row, "error") + ". Tell the user briefly what went wrong and what they could do."
	default:
		return "The user rejected the change you proposed earlier in this conversation (" + summary + "). Nothing was changed. " +
			"Acknowledge it in one sentence. Propose something else only if the situation clearly needs it."
	}
}

// approvalContinuationContext gives the continuation the decided approval and,
// for a routine, the watch event it ran for.
func (s *graphjinService) approvalContinuationContext(ctx context.Context, row map[string]any) map[string]any {
	approval := map[string]any{
		"id": row["id"], "status": row["status"], "summary": approvalSummaryText(row),
		"result": parseJSONValue(stringMapValue(row, "result_json")), "error": row["error"],
	}
	out := map[string]any{"_approval": approval}
	if eventID := stringMapValue(row, "event_id"); eventID != "" {
		if event, err := s.internalWatchEventStoreRow(ctx, eventID); err == nil && event != nil {
			out["_watch_events"] = []any{map[string]any{
				"id": eventID, "watch_id": stringMapValue(row, "watch_id"), "data_json": parseJSONValue(jsonMapString(event, "data_json")),
			}}
		}
	}
	return out
}

// staticAgentRunner answers with a fixed sentence: the outcome of a decision
// when no model is configured.
type staticAgentRunner struct{ answer string }

func (r staticAgentRunner) Run(context.Context, gjagent.Request) (gjagent.Response, error) {
	return gjagent.Response{Status: gjagent.StatusAnswered, Answer: r.answer}, nil
}

// continuationRunner falls back to the plain outcome when the model fails,
// so the decision always reaches the thread.
type continuationRunner struct {
	inner    graphjinAgentRunner
	fallback string
}

func (r continuationRunner) Run(ctx context.Context, req gjagent.Request) (gjagent.Response, error) {
	resp, err := r.inner.Run(ctx, req)
	if err != nil || strings.TrimSpace(resp.Answer) == "" {
		return gjagent.Response{Status: gjagent.StatusAnswered, Answer: r.fallback, TraceID: resp.TraceID, Usage: resp.Usage}, nil
	}
	return resp, nil
}

// approvalStatus is the row's status, reading a lapsed pending proposal as
// expired.
func approvalStatus(row map[string]any, now time.Time) string {
	status := stringMapValue(row, "status")
	if status == approvalStatusPending {
		if expires, err := time.Parse(time.RFC3339Nano, stringMapValue(row, "expires_at")); err == nil && now.After(expires) {
			return approvalStatusExpired
		}
	}
	return status
}

func approvalAPIRow(row map[string]any, now time.Time) map[string]any {
	return map[string]any{
		"id": row["id"], "thread_id": row["thread_id"], "run_id": row["run_id"],
		"query": row["query"], "variables": parseJSONValue(stringMapValue(row, "variables_json")), "namespace": row["namespace"],
		"summary": parseJSONValue(stringMapValue(row, "summary_json")), "preview": parseJSONValue(stringMapValue(row, "preview_json")),
		"hash": row["hash"], "status": approvalStatus(row, now),
		"result": parseJSONValue(stringMapValue(row, "result_json")), "error": row["error"],
		"created_at": row["created_at"], "expires_at": row["expires_at"], "decided_at": row["decided_at"],
		"owner_id": row["owner_id"],
		"origin": row["origin"], "watch_id": row["watch_id"], "event_id": row["event_id"], "profile": row["profile"],
		"decided_by": row["decided_by"], "delegated_by": row["delegated_by"],
	}
}

// --- GraphQL root -------------------------------------------------------------

type approvalControlPlane struct {
	service *graphjinService
}

func (h approvalControlPlane) ManagedQueryTables() []core.ManagedTable {
	if h.service == nil || !h.service.approvalsEnabled() {
		return nil
	}
	return []core.ManagedTable{managedTable(approvalsRootTable, []core.ManagedColumn{
		cpCol("id", "text", true), cpCol("thread_id", "text", false), cpCol("run_id", "text", false),
		cpCol("query", "text", false), cpCol("variables", "json", false), cpCol("namespace", "text", false),
		cpCol("summary", "json", false), cpCol("preview", "json", false), cpCol("hash", "text", false),
		cpCol("status", "text", false), cpCol("result", "json", false), cpCol("error", "text", false),
		cpCol("created_at", "text", false), cpCol("expires_at", "text", false), cpCol("decided_at", "text", false),
		cpCol("decision", "text", false), cpCol("expected_hash", "text", false),
	})}
}

func (h approvalControlPlane) ManagedMutationTables() []string {
	if h.service == nil || !h.service.approvalsEnabled() {
		return nil
	}
	return []string{approvalsRootTable}
}

func (h approvalControlPlane) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows, err := h.service.listApprovals(ctx)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRows(applyManagedQuery(rows, root), root.Fields)
	}
	return json.Marshal(out)
}

// listApprovals is the caller's own proposals, newest first. Approvals are
// personal: not even an admin reads another person's.
func (s *graphjinService) listApprovals(ctx context.Context) ([]map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil
	}
	rows, err := s.internalStoreAllRowsOrdered(ctx, "approvals", `where: { owner_id: { eq: $owner_id } }`, approvalStoreFields, "id",
		map[string]any{"owner_id": ownerID})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	out := make([]map[string]any, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		if stringMapValue(rows[i], "owner_id") == ownerID {
			out = append(out, approvalAPIRow(rows[i], now))
		}
	}
	return out, nil
}

func (h approvalControlPlane) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		if root.Operation != "update" {
			return nil, fmt.Errorf("gj_approval rows are created by the agent; decide one with update: { decision, expected_hash }")
		}
		for field := range root.Input {
			if field != "decision" && field != "expected_hash" {
				return nil, fmt.Errorf("gj_approval %s is server-managed; send only decision and expected_hash", field)
			}
		}
		id := stringWhere(root.Where, "id")
		if id == "" {
			return nil, fmt.Errorf("gj_approval update needs where: { id: { eq: ... } }")
		}
		row, err := h.service.decideApproval(ctx, id, stringInput(root.Input, "decision", ""), stringInput(root.Input, "expected_hash", ""))
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRow(row, root.Fields)
	}
	return json.Marshal(out)
}

// --- REST ---------------------------------------------------------------------

// AgentApprovals serves the caller's write approvals:
//
//	GET  /api/v1/agent/approvals       the caller's approvals, newest first
//	GET  /api/v1/agent/approvals/{id}  one approval
//	POST /api/v1/agent/approvals/{id}  decide it: {"decision": "approve"|"reject", "hash": "..."}
func (s1 *HttpService) AgentApprovals(ah auth.HandlerFunc, ns *string) http.Handler {
	return apiV1Handler(s1, ns, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if !s.approvalsEnabled() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "write approvals are disabled"})
			return
		}
		if err := s.checkGraphJinInitialized(); err != nil {
			renderErr(w, err)
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		if _, ok := artifactUserID(ctx); !ok {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "approvals require a signed-in user"})
			return
		}
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, routeAgentApprovals), "/")
		switch {
		case id == "" && r.Method == http.MethodGet:
			rows, err := s.listApprovals(ctx)
			writeApprovalResult(w, map[string]any{"approvals": rows}, err)
		case id != "" && r.Method == http.MethodGet:
			rows, err := s.listApprovals(ctx)
			for _, row := range rows {
				if stringMapValue(row, "id") == id {
					writeApprovalResult(w, row, err)
					return
				}
			}
			writeApprovalResult(w, nil, errApprovalNotFound)
		case id != "" && r.Method == http.MethodPost:
			var body struct {
				Decision string `json:"decision"`
				Hash     string `json:"hash"`
			}
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
				writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "body must be {\"decision\": \"approve\"|\"reject\", \"hash\": \"...\"}"})
				return
			}
			row, err := s.decideApproval(ctx, id, body.Decision, body.Hash)
			writeApprovalResult(w, row, err)
		default:
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "use GET to read approvals and POST /api/v1/agent/approvals/{id} to decide one"})
		}
	}), ah)
}

func writeApprovalResult(w http.ResponseWriter, data any, err error) {
	switch {
	case err == nil:
		writeJSONStatus(w, http.StatusOK, data)
	case errors.Is(err, errApprovalNotFound):
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": err.Error()})
	case errors.Is(err, errApprovalStale):
		writeJSONStatus(w, http.StatusConflict, map[string]any{"error": err.Error()})
	default:
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
	}
}

// --- storage ------------------------------------------------------------------

func approvalDDL(dbType, schema string) []string {
	table := artifactTableName(dbType, schema, "approvals")
	switch dbType {
	case "postgres", "":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
thread_id TEXT NOT NULL DEFAULT '',
run_id TEXT NOT NULL DEFAULT '',
query TEXT NOT NULL,
variables_json TEXT NOT NULL DEFAULT '',
namespace TEXT NOT NULL DEFAULT '',
actions TEXT NOT NULL DEFAULT '',
summary_json TEXT NOT NULL DEFAULT '',
preview_json TEXT NOT NULL DEFAULT '',
hash TEXT NOT NULL,
status TEXT NOT NULL DEFAULT 'pending',
result_json TEXT NOT NULL DEFAULT '',
error TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT '',
expires_at TEXT NOT NULL DEFAULT '',
decided_at TEXT NOT NULL DEFAULT ''
)`, table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, status)`, quotePGIdent("idx_graphjin_approvals_owner"), table),
		}
	case "mysql", "mariadb":
		return []string{fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
thread_id VARCHAR(191) NOT NULL DEFAULT '',
run_id VARCHAR(191) NOT NULL DEFAULT '',
query LONGTEXT NOT NULL,
variables_json LONGTEXT NOT NULL,
namespace VARCHAR(191) NOT NULL DEFAULT '',
actions TEXT NOT NULL,
summary_json LONGTEXT NOT NULL,
preview_json LONGTEXT NOT NULL,
hash VARCHAR(64) NOT NULL,
status VARCHAR(32) NOT NULL DEFAULT 'pending',
result_json LONGTEXT NOT NULL,
error TEXT NOT NULL,
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
owner_role VARCHAR(191) NOT NULL DEFAULT '',
created_at VARCHAR(64) NOT NULL DEFAULT '',
expires_at VARCHAR(64) NOT NULL DEFAULT '',
decided_at VARCHAR(64) NOT NULL DEFAULT '',
KEY idx_graphjin_approvals_owner (owner_id, status)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, table)}
	default:
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
thread_id TEXT NOT NULL DEFAULT '',
run_id TEXT NOT NULL DEFAULT '',
query TEXT NOT NULL,
variables_json TEXT NOT NULL DEFAULT '',
namespace TEXT NOT NULL DEFAULT '',
actions TEXT NOT NULL DEFAULT '',
summary_json TEXT NOT NULL DEFAULT '',
preview_json TEXT NOT NULL DEFAULT '',
hash TEXT NOT NULL,
status TEXT NOT NULL DEFAULT 'pending',
result_json TEXT NOT NULL DEFAULT '',
error TEXT NOT NULL DEFAULT '',
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
owner_role TEXT NOT NULL DEFAULT '',
created_at TEXT NOT NULL DEFAULT '',
expires_at TEXT NOT NULL DEFAULT '',
decided_at TEXT NOT NULL DEFAULT ''
)`, table),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, status)`, quoteSQLIdent("idx_graphjin_approvals_owner"), table),
		}
	}
}

// migrateApprovalSchema adds the columns that trace a held write to what
// proposed it (a chat, a routine, a continuation), who decided it, and whom a
// service-identity run acted for.
func migrateApprovalSchema(ctx context.Context, db *sql.DB, dbType, schema string) error {
	table := artifactTableName(dbType, schema, "approvals")
	definition := "TEXT NOT NULL DEFAULT ''"
	if dbType == "mysql" || dbType == "mariadb" {
		definition = "VARCHAR(191) NOT NULL DEFAULT ''"
	}
	for _, col := range []string{"origin", "watch_id", "event_id", "profile", "decided_by", "delegated_by", "run_as_id", "run_as_role"} {
		if err := ensureSQLColumn(ctx, db, dbType, table, col, definition); err != nil {
			return err
		}
	}
	return nil
}
