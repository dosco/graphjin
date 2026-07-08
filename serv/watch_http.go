package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dosco/graphjin/auth/v3"
	"github.com/dosco/graphjin/core/v3"
)

func (s1 *HttpService) Watches(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s1, nil, s1.apiV1Watches(nil), ah)
}

func (s1 *HttpService) WatchesWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return apiV1Handler(s1, &ns, s1.apiV1Watches(&ns), ah)
}

func (s1 *HttpService) apiV1Watches(ns *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := s1.Load().(*graphjinService)
		w.Header().Set("Content-Type", "application/json")
		if err := s.checkGraphJinInitialized(); err != nil {
			renderErr(w, err)
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		cp := newWatchControlPlane(s)
		path := strings.TrimPrefix(r.URL.Path, routeWatches)
		if strings.HasPrefix(r.URL.Path, routeWatchEvents) {
			handleWatchEventsREST(ctx, w, r, cp, s)
			return
		}
		switch {
		case r.Method == http.MethodGet && (path == "" || path == "/"):
			rows, err := cp.watchRows(ctx)
			if err != nil {
				renderErr(w, err)
				return
			}
			rows = filterWatchRESTRows(rows, r)
			writeJSONStatus(w, http.StatusOK, map[string]any{"data": rows})
		case r.Method == http.MethodPost && path == "":
			input, err := readJSONMap(r)
			if err != nil {
				renderErr(w, err)
				return
			}
			row, err := cp.upsertWatch(ctx, core.ManagedMutationRoot{Operation: "insert", Input: input})
			if err != nil {
				renderErr(w, err)
				return
			}
			writeJSONStatus(w, http.StatusCreated, map[string]any{"data": row})
		case r.Method == http.MethodPatch && strings.HasPrefix(path, "/"):
			id := strings.Trim(strings.TrimPrefix(path, "/"), "/")
			input, err := readJSONMap(r)
			if err != nil {
				renderErr(w, err)
				return
			}
			row, err := mergeWatchRESTInput(ctx, s, id, input)
			if err != nil {
				renderErr(w, err)
				return
			}
			out, err := cp.upsertWatch(ctx, core.ManagedMutationRoot{Operation: "update", Input: row})
			if err != nil {
				renderErr(w, err)
				return
			}
			writeJSONStatus(w, http.StatusOK, map[string]any{"data": out})
		case r.Method == http.MethodDelete && strings.HasPrefix(path, "/"):
			id := strings.Trim(strings.TrimPrefix(path, "/"), "/")
			row, err := cp.deleteWatch(ctx, core.ManagedMutationRoot{Operation: "delete", Input: map[string]any{"id": id}})
			if err != nil {
				renderErr(w, err)
				return
			}
			writeJSONStatus(w, http.StatusOK, map[string]any{"data": row})
		case r.Method == http.MethodPost && path == "/cleanup-preview":
			var opts watchCleanupOptions
			if err := decodeOptionalJSON(r, &opts); err != nil {
				renderErr(w, err)
				return
			}
			preview, err := s.previewWatchCleanup(ctx, opts)
			if err != nil {
				renderErr(w, err)
				return
			}
			writeJSONStatus(w, http.StatusOK, preview)
		case r.Method == http.MethodPost && path == "/cleanup-apply":
			var req watchCleanupApplyRequest
			if err := decodeJSON(r, &req); err != nil {
				renderErr(w, err)
				return
			}
			result, err := s.applyWatchCleanup(ctx, req)
			if err != nil {
				renderErr(w, err)
				return
			}
			writeJSONStatus(w, http.StatusOK, map[string]any{"data": result})
		default:
			http.NotFound(w, r)
		}
	})
}

func handleWatchEventsREST(ctx context.Context, w http.ResponseWriter, r *http.Request, cp watchControlPlane, s *graphjinService) {
	path := strings.TrimPrefix(r.URL.Path, routeWatchEvents)
	switch {
	case r.Method == http.MethodGet && path == "/unseen":
		payload, err := s.unseenWatchEventsPayload(ctx)
		if err != nil {
			renderErr(w, err)
			return
		}
		writeJSONStatus(w, http.StatusOK, payload)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/seen"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/"), "/seen")
		row, err := cp.updateWatchEvent(ctx, core.ManagedMutationRoot{
			Operation: "update",
			Input:     map[string]any{"id": id, "seen": true},
		})
		if err != nil {
			renderErr(w, err)
			return
		}
		writeJSONStatus(w, http.StatusOK, map[string]any{"data": row})
	default:
		http.NotFound(w, r)
	}
}

func filterWatchRESTRows(rows []map[string]any, r *http.Request) []map[string]any {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	lifecycle := strings.TrimSpace(r.URL.Query().Get("lifecycle"))
	if status == "" && lifecycle == "" {
		return rows
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if status != "" && fmt.Sprint(row["status"]) != status {
			continue
		}
		if lifecycle != "" && fmt.Sprint(row["lifecycle"]) != lifecycle {
			continue
		}
		out = append(out, row)
	}
	return out
}

func mergeWatchRESTInput(ctx context.Context, s *graphjinService, id string, input map[string]interface{}) (map[string]interface{}, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("watch id is required")
	}
	existing, err := s.internalWatchStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("watch not found")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_watch write requires user identity")
	}
	if !s.identityRoleIsAdmin(ctx) && stringMapValue(existing, "owner_id") != ownerID {
		return nil, fmt.Errorf("gj_watch write denied")
	}
	merged := map[string]interface{}{
		"id":               id,
		"name":             stringMapValue(existing, "name"),
		"description":      stringMapValue(existing, "description"),
		"query":            stringMapValue(existing, "query"),
		"saved_query_name": stringMapValue(existing, "saved_query_name"),
		"variables_json":   jsonMapString(existing, "variables_json"),
		"condition_js":     stringMapValue(existing, "condition_js"),
		"delivery_json":    jsonMapString(existing, "delivery_json"),
		"enrich_json":      jsonMapString(existing, "enrich_json"),
		"lifecycle":        watchLifecycle(stringMapValue(existing, "lifecycle")),
		"lease_expires_at": stringMapValue(existing, "lease_expires_at"),
		"status":           watchStatus(stringMapValue(existing, "status")),
		"approval":         watchApproval(stringMapValue(existing, "approval")),
		"enabled":          boolMapValue(existing, "enabled"),
	}
	for k, v := range input {
		merged[k] = v
	}
	merged["id"] = id
	return merged, nil
}

func readJSONMap(r *http.Request) (map[string]interface{}, error) {
	var input map[string]interface{}
	if err := decodeJSON(r, &input); err != nil {
		return nil, err
	}
	if input == nil {
		input = map[string]interface{}{}
	}
	return input, nil
}

func decodeOptionalJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxReadBytes))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func decodeJSON(r *http.Request, dst any) error {
	if r.Body == nil {
		return fmt.Errorf("request body is required")
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxReadBytes))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return fmt.Errorf("request body is required")
	}
	return json.Unmarshal(data, dst)
}
