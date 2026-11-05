package serv

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/core/v3"
)

// Threads, their messages and their runs are GraphQL roots, so a console or
// any other GUI lists, renames, archives, reads and deletes conversations
// through the graph. Rows are the caller's own (an admin reads every owner's).
// Messages and runs are written only by a turn, which still starts at
// POST /api/v1/agent or the MCP agent tool, so they are read-only here.
//
// These roots are served live from the thread store and never mirrored into
// the system NanoDB: runs renew their lease every few seconds, and a mirror
// would be rebuilt on every renewal.

const (
	threadsRootTable        = "gj_thread"
	threadMessagesRootTable = "gj_thread_message"
	threadRunsRootTable     = "gj_thread_run"
)

type threadControlPlane struct {
	service *graphjinService
}

func newThreadControlPlane(s *graphjinService) threadControlPlane {
	return threadControlPlane{service: s}
}

func (h threadControlPlane) ManagedQueryTables() []core.ManagedTable {
	if h.service == nil || !h.service.threadsEnabled() {
		return nil
	}
	return []core.ManagedTable{
		managedTable(threadsRootTable, []core.ManagedColumn{
			cpCol("id", "text", true), cpCol("title", "text", false), cpCol("profile", "text", false),
			cpCol("status", "text", false), cpCol("channel", "text", false), cpCol("active_run_id", "text", false),
			cpCol("unread", "boolean", false), cpCol("owner_ref", "text", false),
			cpCol("last_message_at", "text", false), cpCol("last_seen_at", "text", false),
			cpCol("created_at", "text", false), cpCol("updated_at", "text", false), cpCol("deleted", "boolean", false),
		}),
		managedTable(threadMessagesRootTable, []core.ManagedColumn{
			cpCol("id", "text", true), cpCol("thread_id", "text", false), cpCol("seq", "integer", false),
			cpCol("role", "text", false), cpCol("content", "text", false), cpCol("status", "text", false),
			cpCol("run_id", "text", false), cpCol("catalog_ids", "json", false), cpCol("response", "json", false),
			cpCol("created_at", "text", false),
		}),
		managedTable(threadRunsRootTable, []core.ManagedColumn{
			cpCol("id", "text", true), cpCol("thread_id", "text", false), cpCol("status", "text", false),
			cpCol("tokens_in", "integer", false), cpCol("tokens_out", "integer", false), cpCol("llm_calls", "integer", false),
			cpCol("used_skills", "json", false), cpCol("used_memories", "json", false), cpCol("error", "text", false),
			cpCol("created_at", "text", false), cpCol("updated_at", "text", false), cpCol("finished_at", "text", false),
		}),
	}
}

func (h threadControlPlane) ManagedMutationTables() []string {
	if h.service == nil || !h.service.threadsEnabled() {
		return nil
	}
	return []string{threadsRootTable, threadMessagesRootTable, threadRunsRootTable}
}

func (h threadControlPlane) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows, err := h.queryRows(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRows(rows, root.Fields)
	}
	return json.Marshal(out)
}

func (h threadControlPlane) queryRows(ctx context.Context, root core.ManagedQueryRoot) ([]map[string]any, error) {
	s := h.service
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil
	}
	admin := s.identityRoleIsAdmin(ctx)
	// Scope in the store: the caller's rows, narrowed to one thread when the
	// query names it, so reading a thread never loads every message.
	scope := map[string]any{}
	if !admin {
		scope["owner_id"] = map[string]any{"eq": "$owner_id"}
	}
	threadID := stringWhere(root.Where, "thread_id")
	var (
		table, fields, order string
		shape                func(map[string]any) map[string]any
	)
	switch root.Table {
	case threadsRootTable:
		table, fields, shape = "threads", threadStoreFields, threadAPIRow
		if id := stringWhere(root.Where, "id"); id != "" {
			threadID = id
			scope["id"] = map[string]any{"eq": "$thread_id"}
		}
	case threadMessagesRootTable:
		table, fields, order, shape = "messages", messageStoreFields, "seq", messageAPIRow
	case threadRunsRootTable:
		table, fields, shape = "runs", runStoreFields, runGraphRow
	default:
		return nil, fmt.Errorf("unsupported GraphJin thread root: %s", root.Table)
	}
	if threadID != "" && root.Table != threadsRootTable {
		scope["thread_id"] = map[string]any{"eq": "$thread_id"}
	}
	args, vars := threadScopeArgs(scope, ownerID, threadID)
	var rows []map[string]any
	var err error
	if order != "" {
		rows, err = s.internalStoreAllRowsOrdered(ctx, table, args, fields, order, vars)
	} else {
		rows, err = s.internalStoreAllRows(ctx, table, args, fields, vars)
	}
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		out = append(out, shape(row))
	}
	return applyManagedQuery(out, root), nil
}

// threadScopeArgs renders the store filter for a scope built from the
// placeholders $owner_id and $thread_id.
func threadScopeArgs(scope map[string]any, ownerID, threadID string) (string, map[string]any) {
	if len(scope) == 0 {
		return "", nil
	}
	args := "where: { "
	first := true
	for _, key := range []string{"id", "owner_id", "thread_id"} {
		cond, ok := scope[key].(map[string]any)
		if !ok {
			continue
		}
		if !first {
			args += ", "
		}
		first = false
		args += fmt.Sprintf("%s: { eq: %s }", key, cond["eq"])
	}
	args += " }"
	vars := map[string]any{}
	if _, ok := scope["owner_id"]; ok {
		vars["owner_id"] = ownerID
	}
	if threadID != "" {
		vars["thread_id"] = threadID
	}
	return args, vars
}

func runGraphRow(row map[string]any) map[string]any {
	out := runAPIRow(row)
	out["used_skills"] = parseJSONValue(jsonMapString(row, "used_skills"))
	out["used_memories"] = parseJSONValue(jsonMapString(row, "used_memories"))
	return out
}

func (h threadControlPlane) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		row, err := h.mutateRow(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRow(row, root.Fields)
	}
	return json.Marshal(out)
}

func (h threadControlPlane) mutateRow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	switch root.Table {
	case threadsRootTable:
	case threadMessagesRootTable, threadRunsRootTable:
		return nil, fmt.Errorf("%s is written only by agent turns: send the message with POST /api/v1/agent and a thread_id", root.Table)
	default:
		return nil, fmt.Errorf("unsupported GraphJin thread mutation root: %s", root.Table)
	}
	id := stringWhere(root.Where, "id")
	switch root.Operation {
	case "insert":
		for field := range root.Input {
			switch field {
			case "title", "profile":
			default:
				return nil, fmt.Errorf("gj_thread %s is server-managed", field)
			}
		}
		return s.createThread(ctx, threadSpec{Title: stringInput(root.Input, "title", ""), Profile: stringInput(root.Input, "profile", "")})
	case "update":
		if id == "" {
			return nil, fmt.Errorf("gj_thread update needs where: { id: { eq: ... } }")
		}
		input := map[string]any{}
		for field, value := range root.Input {
			switch field {
			case "title", "profile", "status":
				input[field] = value
			case "last_seen_at":
				// Marking a thread read sets last_seen_at to now; the client's
				// clock is never trusted for it.
				input["seen"] = true
			default:
				return nil, fmt.Errorf("gj_thread %s is not editable", field)
			}
		}
		return s.updateThread(ctx, id, input)
	case "delete":
		if id == "" {
			return nil, fmt.Errorf("gj_thread delete needs where: { id: { eq: ... } }")
		}
		if err := s.deleteThread(ctx, id); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "deleted": true}, nil
	default:
		return nil, fmt.Errorf("gj_thread supports insert, update and delete mutations")
	}
}
