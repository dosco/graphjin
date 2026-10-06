package serv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func TestCatalogDriftConflicts(t *testing.T) {
	base := map[string]string{"schema": "a", "schema:main": "m1", "schema:logs": "l1", "config": "c1", "saved_queries": "q1"}
	cases := []struct {
		name    string
		current map[string]string
		changed []string
		want    string
	}{
		{"untouched source schema drift", map[string]string{"schema": "b", "schema:main": "m2", "schema:logs": "l1", "config": "c1", "saved_queries": "q1"}, []string{"logs"}, ""},
		{"touched source schema drift", map[string]string{"schema": "b", "schema:main": "m1", "schema:logs": "l2", "config": "c1", "saved_queries": "q1"}, []string{"logs"}, "schema:logs"},
		{"config change", map[string]string{"schema": "a", "schema:main": "m1", "schema:logs": "l1", "config": "c2", "saved_queries": "q1"}, []string{"logs"}, "config"},
		{"new source key", map[string]string{"schema": "a", "schema:main": "m1", "schema:logs": "l1", "config": "c1", "saved_queries": "q1", "source:extra": "x"}, nil, "source:extra"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := strings.Join(catalogDriftConflicts(base, tc.current, tc.changed), ","); got != tc.want {
				t.Fatalf("conflicts = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleUpdateCurrentConfig_ApplyToleratesUntouchedSchemaDrift(t *testing.T) {
	for _, tc := range []struct {
		name      string
		driftKey  string
		wantApply bool
	}{
		{"untouched source schema", "schema:main", true},
		{"config", "config", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
			logsPath := createSQLiteDBFile(t, "logs.sqlite3", true)
			ms := newSourceModeConfigMCPServer(t, map[string]string{"main": mainPath})
			args := map[string]any{
				"update_sources": []any{map[string]any{
					"name": "logs",
					"kind": sourcecap.KindDatabase,
					"type": "sqlite",
					"path": logsPath,
				}},
			}

			revision := ms.currentConfigCatalogRevision(context.Background())
			previewArgs := cloneConfigUpdateArgs(t, args)
			previewArgs["mode"] = "preview"
			previewArgs["expected_catalog_revision"] = revision
			preview := applyConfigUpdate(t, ms, previewArgs)
			if !preview.Success || preview.PreviewID == "" {
				t.Fatalf("expected valid preview, got %+v", preview)
			}

			// Simulate a catalog that moved after the preview started: the
			// preview saw an older revision of one key.
			store := ms.ensureConfigPreviewStore()
			store.mu.Lock()
			rec := store.items[preview.PreviewID]
			if _, ok := rec.BaseSourceRevisions[tc.driftKey]; !ok {
				store.mu.Unlock()
				t.Fatalf("preview base revisions lack %q: %v", tc.driftKey, rec.BaseSourceRevisions)
			}
			base := make(map[string]string, len(rec.BaseSourceRevisions))
			for key, value := range rec.BaseSourceRevisions {
				base[key] = value
			}
			base[tc.driftKey] = "older"
			rec.BaseSourceRevisions = base
			applyArgs := cloneConfigUpdateArgs(t, args)
			applyArgs["mode"] = "apply"
			applyArgs["preview_id"] = preview.PreviewID
			applyArgs["expected_catalog_revision"] = "older-revision"
			rec.BaseCatalogRevision = "older-revision"
			rec.PatchHash = configUpdatePatchHash(applyArgs)
			store.items[preview.PreviewID] = rec
			store.mu.Unlock()

			applied := applyConfigUpdate(t, ms, applyArgs)
			if applied.Applied != tc.wantApply {
				t.Fatalf("applied = %v, want %v: %+v", applied.Applied, tc.wantApply, applied)
			}
			if !tc.wantApply && !strings.Contains(applied.Message, "stale") {
				t.Fatalf("expected a stale revision error, got %+v", applied)
			}
		})
	}
}

func TestHandleUpdateCurrentConfig_AddAPISourceUsesSourceScopedReload(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`[{"id": 1, "title": "Hello"}]`))
	}))
	defer api.Close()

	mainPath := createSQLiteDBFile(t, "main.sqlite3", true)
	logsPath := createSQLiteDBFile(t, "logs.sqlite3", true)
	ms := newSourceModeConfigMCPServer(t, map[string]string{"main": mainPath, "logs": logsPath})
	oldLogs := ms.service.dbs["logs"]

	out := applySourceModeConfigUpdate(t, ms, map[string]any{
		"update_sources": []any{map[string]any{
			"name":      "forum_api",
			"kind":      sourcecap.KindAPI,
			"read_only": true,
			"access":    map[string]any{"read": "public"},
			"specs": map[string]any{
				"forum": map[string]any{
					"base_url": api.URL,
					"document": `openapi: 3.0.0
info: { title: Forum, version: "1.0" }
paths:
  /latest.json:
    get:
      operationId: listLatestTopics
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: { type: array, items: { type: object, properties: { id: { type: integer }, title: { type: string } } } }
`,
					"operations": map[string]any{
						"listLatestTopics": map[string]any{"expose_top_level": true, "expose_as": "forum_latest"},
					},
				},
			},
		}},
	})
	assertSourceScopedConfigResult(t, out, "forum_api")
	if ms.service.dbs["logs"] != oldLogs {
		t.Fatal("expected the untouched database handle to be reused")
	}

	res, err := ms.service.gj.GraphQL(context.Background(), `query { forum_latest { id title } }`, nil, &core.RequestConfig{})
	if err != nil {
		t.Fatalf("query new API operation: %v", err)
	}
	if !strings.Contains(string(res.Data), "Hello") {
		t.Fatalf("data = %s, want the API response", res.Data)
	}

	removed := applySourceModeConfigUpdate(t, ms, map[string]any{"remove_sources": []any{"forum_api"}})
	assertSourceScopedConfigResult(t, removed, "forum_api")
	if _, err := ms.service.gj.GraphQL(context.Background(), `query { forum_latest { id } }`, nil, &core.RequestConfig{}); err == nil {
		t.Fatal("expected the removed API operation to be gone")
	}
}
