package serv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
)

// Answer feedback. A person marks an agent answer in a thread as helpful or
// wrong, optionally saying what the right answer is. It is stored as a
// feedback artifact, one per person per message, and the server fills in
// the question, the answer and the turns before it from the thread, so a
// report always carries the real conversation, never the client's copy of
// it. GET /api/v1/agent/feedback?format=corpus turns reports into draft
// lead-corpus cases: the field-report-to-eval loop AGENTS.md describes.

const (
	artifactKindFeedback = "feedback"

	feedbackRatingUp   = "up"
	feedbackRatingDown = "down"

	maxFeedbackCorrectionBytes = 4 * 1024
)

var errFeedbackMessageNotFound = errors.New("feedback needs an agent answer in one of your threads")

func (s *graphjinService) feedbackEnabled() bool {
	return s.threadsEnabled()
}

// feedbackArtifactID is fixed per person and message, so rating an answer
// again replaces the earlier rating.
func feedbackArtifactID(ownerID, messageID string) string {
	return "feedback:" + hashString(ownerID + "\x00" + messageID)[:32]
}

// upsertFeedback stores a rating for an answer. Input: target_ref (the
// message id), content_json { rating: "up" | "down" }, and content, the
// correction in the person's words.
func (h artifactControlPlane) upsertFeedback(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if !s.feedbackEnabled() {
		return nil, fmt.Errorf("answer feedback needs agent threads")
	}
	actorID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("feedback requires user identity")
	}
	if err := s.checkArtifactKindWritable(artifactKindFeedback); err != nil {
		return nil, err
	}
	for field := range root.Input {
		switch field {
		case "kind", "target_ref", "content", "content_json":
		default:
			return nil, fmt.Errorf("feedback %s is not writable; send target_ref, content_json { rating } and content", field)
		}
	}
	messageID := strings.TrimSpace(stringInput(root.Input, "target_ref", ""))
	if messageID == "" {
		return nil, fmt.Errorf("feedback needs target_ref: the id of the answer it rates")
	}
	rating := strings.ToLower(strings.TrimSpace(stringFromAny(mapFromAny(parseJSONValue(jsonStringInput(root.Input, "content_json")))["rating"])))
	if rating != feedbackRatingUp && rating != feedbackRatingDown {
		return nil, fmt.Errorf("feedback content_json.rating must be up or down")
	}
	correction := strings.TrimSpace(stringInput(root.Input, "content", ""))
	if len(correction) > maxFeedbackCorrectionBytes {
		return nil, fmt.Errorf("feedback content exceeds %d bytes", maxFeedbackCorrectionBytes)
	}
	report, err := s.feedbackReport(ctx, actorID, messageID)
	if err != nil {
		return nil, err
	}
	report["rating"] = rating
	report["correction"] = correction
	contentJSON, _ := json.Marshal(report)

	id := feedbackArtifactID(actorID, messageID)
	existing, err := s.internalArtifactStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	accountID, _ := identityVarString(ctx, "account_id")
	input := map[string]any{
		"id": id, "name": messageID, "kind": artifactKindFeedback, "path": "", "source": "database",
		"visibility": "user", "read_only": false, "account_id": accountID, "owner_id": actorID,
		"content": correction, "content_json": string(contentJSON), "metadata_json": nil,
		"content_hash": hashString(string(contentJSON)), "status": "approved", "target_ref": messageID,
		"tier": annotationTierObserved, "task_id": "", "approved_by": "", "approved_at": "",
		"revision": int64(1), "created_at": now, "updated_at": now,
	}
	var rows []map[string]any
	if existing == nil {
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `insert: $input`, artifactStoreFields, map[string]any{"input": input})
	} else {
		update := cloneMap(input)
		for _, field := range []string{"id", "account_id", "owner_id", "created_at"} {
			delete(update, field)
		}
		update["revision"] = int64MapValue(existing, "revision") + 1
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `where: { id: { eq: $id } }, update: $input`, artifactStoreFields, map[string]any{"id": id, "input": update})
	}
	if err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "artifacts"); err != nil {
		return nil, err
	}
	s.markArtifactChanged("feedback mutation")
	if len(rows) != 0 {
		return artifactProjectionRow(rows[0], s.identityRoleIsAdmin(ctx)), nil
	}
	return artifactProjectionRow(input, s.identityRoleIsAdmin(ctx)), nil
}

// feedbackReport reads the rated answer and the conversation before it from
// the thread. Only the owner's own agent answers can be rated.
func (s *graphjinService) feedbackReport(ctx context.Context, ownerID, messageID string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "messages", `where: { id: { eq: $id } }, limit: 1`, messageStoreFields, map[string]any{"id": messageID})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || stringMapValue(rows[0], "owner_id") != ownerID || stringMapValue(rows[0], "role") != "assistant" {
		return nil, errFeedbackMessageNotFound
	}
	answer := rows[0]
	threadID := stringMapValue(answer, "thread_id")
	seq := int64MapValue(answer, "seq")
	earlier, err := s.internalStoreAllRowsOrdered(ctx, "messages", `where: { thread_id: { eq: $thread_id } }`, messageStoreFields, "seq",
		map[string]any{"thread_id": threadID})
	if err != nil {
		return nil, err
	}
	var turns []map[string]any
	question := ""
	for _, row := range earlier {
		if int64MapValue(row, "seq") >= seq || stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		turns = append(turns, map[string]any{"role": stringMapValue(row, "role"), "content": stringMapValue(row, "content")})
		if stringMapValue(row, "role") == "user" {
			question = stringMapValue(row, "content")
		}
	}
	// The question being rated is the last user turn; what came before it is
	// the history a replay needs.
	if n := len(turns); n != 0 && turns[n-1]["role"] == "user" {
		turns = turns[:n-1]
	}
	response := mapFromAny(parseJSONValue(jsonMapString(answer, "response_json")))
	return map[string]any{
		"message_id": messageID, "thread_id": threadID, "run_id": stringMapValue(answer, "run_id"),
		"question": question, "answer": stringMapValue(answer, "content"),
		"answer_status": stringMapValue(answer, "status"), "trace_id": stringFromAny(response["trace_id"]),
		"history": turns, "queries": feedbackQueries(response),
	}, nil
}

// feedbackQueries are the queries the data agent ran for a rated answer, in
// order. Whoever writes the draft's oracle starts from what was really run.
func feedbackQueries(response map[string]any) []string {
	queries := []string{}
	for _, raw := range anySliceFrom(response["actions"]) {
		action := mapFromAny(raw)
		if stringFromAny(action["tool"]) != "execute_graphql" {
			continue
		}
		if query := strings.TrimSpace(stringFromAny(mapFromAny(action["args"])["query"])); query != "" {
			queries = append(queries, query)
		}
	}
	return queries
}

// listFeedback is the caller's reports; an admin reads everyone's, since
// improving the agent is an operator's job.
func (s *graphjinService) listFeedback(ctx context.Context) ([]map[string]any, error) {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, nil
	}
	admin := s.identityRoleIsAdmin(ctx)
	args := `where: { kind: { eq: "feedback" }, owner_id: { eq: $owner_id } }`
	vars := map[string]any{"owner_id": ownerID}
	if admin {
		args, vars = `where: { kind: { eq: "feedback" } }`, nil
	}
	rows, err := s.internalStoreAllRows(ctx, "artifacts", args, artifactStoreFields, vars)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if !admin && stringMapValue(row, "owner_id") != ownerID {
			continue
		}
		report := mapFromAny(parseJSONValue(jsonMapString(row, "content_json")))
		if report == nil {
			continue
		}
		report["id"] = stringMapValue(row, "id")
		report["updated_at"] = stringMapValue(row, "updated_at")
		out = append(out, report)
	}
	sort.Slice(out, func(i, j int) bool { return stringFromAny(out[i]["updated_at"]) > stringFromAny(out[j]["updated_at"]) })
	return out, nil
}

// feedbackCorpusDrafts turns wrong-answer reports into draft lead-corpus
// cases. A draft has the real conversation and the person's correction but
// no oracle: whoever moves it into agent/testdata/lead_eval_cases.json writes
// the database query that proves the right answer.
func feedbackCorpusDrafts(reports []map[string]any) []map[string]any {
	drafts := []map[string]any{}
	for _, report := range reports {
		if stringFromAny(report["rating"]) != feedbackRatingDown || strings.TrimSpace(stringFromAny(report["question"])) == "" {
			continue
		}
		var turns []string
		for _, turn := range anySliceFrom(report["history"]) {
			if t := mapFromAny(turn); stringFromAny(t["role"]) == "user" {
				turns = append(turns, stringFromAny(t["content"]))
			}
		}
		draft := map[string]any{
			"id":       "field-" + hashString(stringFromAny(report["message_id"]))[:12],
			"group":    "field",
			"prompt":   report["question"],
			"draft":    true,
			"observed": map[string]any{"answer": report["answer"], "status": report["answer_status"], "trace_id": report["trace_id"], "queries": report["queries"]},
			"expected": report["correction"],
			"todo":     "Add an oracle (a database-computed GraphQL query; observed.queries are what the agent ran) and an answer rule, then move the case into agent/testdata/lead_eval_cases.json.",
		}
		if len(turns) != 0 {
			draft["turns"] = turns
		}
		drafts = append(drafts, draft)
	}
	return drafts
}

func anySliceFrom(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []map[string]any:
		out := make([]any, 0, len(t))
		for _, m := range t {
			out = append(out, m)
		}
		return out
	}
	return nil
}

// AgentFeedback serves answer feedback:
//
//	GET /api/v1/agent/feedback                the caller's reports (an admin's: everyone's)
//	GET /api/v1/agent/feedback?format=corpus  wrong-answer reports as draft lead-corpus cases
//
// Feedback is written through gj_artifacts with kind "feedback".
func (s1 *HttpService) AgentFeedback(ah auth.HandlerFunc, ns *string) http.Handler {
	return apiV1Handler(s1, ns, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if !s.feedbackEnabled() {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "answer feedback needs agent threads"})
			return
		}
		if r.Method != http.MethodGet {
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "read feedback with GET; write it through gj_artifacts with kind feedback"})
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		if _, ok := artifactUserID(ctx); !ok {
			writeJSONStatus(w, http.StatusUnauthorized, map[string]any{"error": "feedback requires a signed-in user"})
			return
		}
		reports, err := s.listFeedback(ctx)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if r.URL.Query().Get("format") == "corpus" {
			writeJSONStatus(w, http.StatusOK, map[string]any{"cases": feedbackCorpusDrafts(reports)})
			return
		}
		writeJSONStatus(w, http.StatusOK, map[string]any{"feedback": reports})
	}), ah)
}
