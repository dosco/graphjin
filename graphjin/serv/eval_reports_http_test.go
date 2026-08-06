package serv

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/core/v3"
)

func TestEvalReportsMissingDirectoryIsHonestEmptyState(t *testing.T) {
	project := t.TempDir()
	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}, Serv: Serv{ConfigPath: project}})
	req := httptest.NewRequest(http.MethodGet, routeEvalReports, nil)
	rec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var response evalReportsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(project, gjeval.DefaultStateDir)
	if response.Available || response.StateDir != want || response.Reports == nil || len(response.Reports) != 0 {
		t.Fatalf("response = %+v, want unavailable at %s", response, want)
	}
}

func TestEvalReportsListAndDetailFallback(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "eval-state")
	store := gjeval.NewStore(stateDir)
	report := evalHTTPTestReport()
	report.Tasks = []gjeval.TaskVerdict{{TaskID: "task-1", Slug: "private-task-slug"}}
	report.InvalidOracleDetails = map[string]string{"task-1": "private oracle error"}
	report.EpisodePaths = []string{"/private/episode.json"}
	if _, err := store.WriteReport(report); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.ReportMarkdownPath(report.RunID)); err != nil {
		t.Fatal(err)
	}
	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}, Serv: Serv{EvalStateDir: stateDir}})

	listReq := httptest.NewRequest(http.MethodGet, routeEvalReports, nil)
	listRec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", listRec.Code, listRec.Body.String())
	}
	var list evalReportsListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil || !list.Available || len(list.Reports) != 1 || list.Reports[0].RunID != report.RunID || list.Reports[0].HasMarkdown {
		t.Fatalf("list = %+v err=%v", list, err)
	}

	detailReq := httptest.NewRequest(http.MethodGet, routeEvalReportsPrefix+report.RunID, nil)
	detailRec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", detailRec.Code, detailRec.Body.String())
	}
	var detail evalReportDetailResponse
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.RunID != report.RunID || detail.Recall != report.Metrics.Recall || !strings.Contains(detail.Markdown, "## Results at a glance") || !strings.Contains(detail.TechnicalMarkdown, "## Headline") {
		t.Fatalf("detail = %+v", detail)
	}
	for _, private := range []string{"private-task-slug", "private oracle error", "/private/episode.json"} {
		if strings.Contains(detail.Markdown, private) {
			t.Fatalf("detail leaked %q", private)
		}
	}

	badReq := httptest.NewRequest(http.MethodGet, routeEvalReportsPrefix+"bad%2Frun", nil)
	badRec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid run id status = %d: %s", badRec.Code, badRec.Body.String())
	}
}

func TestEvalReportsPartialRunHasFriendlyProgressAndTechnicalReport(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "eval-state")
	store := gjeval.NewStore(stateDir)
	report := gjeval.PartialReport{
		SchemaVersion: gjeval.ReportSchemaVersion, RunID: "partial-google", RunStatus: gjeval.RunStatusEnvironmentFailed,
		Provenance:      gjeval.RunProvenance{Provider: "google-gemini", Repeats: 3},
		Progress:        gjeval.RunProgress{CompletedInitialSlots: 36, PlannedInitialSlots: 72},
		EnvironmentCode: "provider_quota",
	}
	if _, err := store.WritePartialReport(report); err != nil {
		t.Fatal(err)
	}
	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}, Serv: Serv{EvalStateDir: stateDir}})
	listReq := httptest.NewRequest(http.MethodGet, routeEvalReports, nil)
	listRec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(listRec, listReq)
	var list evalReportsListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil || len(list.Reports) != 1 || list.Reports[0].FriendlySummary.CompletedTestAttempts != 36 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	req := httptest.NewRequest(http.MethodGet, routeEvalReportsPrefix+report.RunID, nil)
	rec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", rec.Code, rec.Body.String())
	}
	var detail evalReportDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.FriendlySummary.QuestionCount != 24 || detail.FriendlySummary.CompletedTestAttempts != 36 || detail.FriendlySummary.PlannedTestAttempts != 72 {
		t.Fatalf("friendly summary = %+v", detail.FriendlySummary)
	}
	if !strings.Contains(detail.Markdown, "Google stopped accepting requests because of quota limits") || strings.Contains(detail.Markdown, "## Headline") {
		t.Fatalf("friendly markdown = %s", detail.Markdown)
	}
	if !strings.Contains(detail.TechnicalMarkdown, "Environment code: `provider_quota`") {
		t.Fatalf("technical markdown = %s", detail.TechnicalMarkdown)
	}
}

func TestEvalReportsRecognizesLegacySingleTechnicalMarkdown(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "eval-state")
	store := gjeval.NewStore(stateDir)
	report := evalHTTPTestReport()
	report.RunID = "legacy-single-markdown"
	if _, err := store.WriteReport(report); err != nil {
		t.Fatal(err)
	}
	technical, err := store.LoadReportTechnicalMarkdown(report.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ReportMarkdownPath(report.RunID), technical, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.ReportTechnicalMarkdownPath(report.RunID)); err != nil {
		t.Fatal(err)
	}

	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}, Serv: Serv{EvalStateDir: stateDir}})
	req := httptest.NewRequest(http.MethodGet, routeEvalReportsPrefix+report.RunID, nil)
	rec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d: %s", rec.Code, rec.Body.String())
	}
	var detail evalReportDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Markdown, gjeval.FriendlyReportMarkdownVersion) || !strings.Contains(detail.TechnicalMarkdown, gjeval.TechnicalReportMarkdownVersion) {
		t.Fatalf("legacy report was not projected into both views: %+v", detail)
	}
}

func TestEvalReportsRegeneratesMissingTechnicalMarkdown(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "eval-state")
	store := gjeval.NewStore(stateDir)
	report := evalHTTPTestReport()
	report.RunID = "missing-technical-markdown"
	if _, err := store.WriteReport(report); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.ReportTechnicalMarkdownPath(report.RunID)); err != nil {
		t.Fatal(err)
	}

	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}, Serv: Serv{EvalStateDir: stateDir}})
	req := httptest.NewRequest(http.MethodGet, routeEvalReportsPrefix+report.RunID, nil)
	rec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(rec, req)
	var detail evalReportDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail.Markdown, gjeval.FriendlyReportMarkdownVersion) || !strings.Contains(detail.TechnicalMarkdown, gjeval.TechnicalReportMarkdownVersion) {
		t.Fatalf("missing technical report was not regenerated: %+v", detail)
	}
}

func TestEvalReportsRequireOperatorAndStayOffInProduction(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "eval-state")
	if err := gjeval.NewStore(stateDir).Init(); err != nil {
		t.Fatal(err)
	}
	agentic := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "agentic"}, Serv: Serv{EvalStateDir: stateDir}})
	req := httptest.NewRequest(http.MethodGet, routeEvalReports, nil)
	rec := httptest.NewRecorder()
	agentic.EvalReports(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "operator_access_required") {
		t.Fatalf("agentic anonymous status = %d: %s", rec.Code, rec.Body.String())
	}

	production := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "prod"}, Serv: Serv{EvalStateDir: stateDir}})
	prodRec := httptest.NewRecorder()
	production.EvalReports(nil).ServeHTTP(prodRec, req)
	if prodRec.Code != http.StatusNotFound {
		t.Fatalf("production status = %d: %s", prodRec.Code, prodRec.Body.String())
	}
}

func TestEvalReportsRejectMethod(t *testing.T) {
	hs := newAgentHTTPTestService(&Config{Core: core.Config{Mode: "dev"}})
	req := httptest.NewRequest(http.MethodPost, routeEvalReports, nil)
	rec := httptest.NewRecorder()
	hs.EvalReports(nil).ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status/allow = %d/%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func evalHTTPTestReport() gjeval.Report {
	return gjeval.Report{
		SchemaVersion: gjeval.ReportSchemaVersion, RewardVersion: gjeval.RewardVersion,
		RunID: "20260803T101112.000000000Z-api", RunStatus: gjeval.RunStatusComplete, Mode: gjeval.RunModeBenchmark,
		GeneratedAt: time.Date(2026, 8, 3, 10, 11, 12, 0, time.UTC), SuiteFingerprint: "suite",
		DatasetFingerprint: gjeval.DatasetFingerprint{CatalogHash: "catalog"},
		Provenance:         gjeval.RunProvenance{Provider: "openai", Model: "gpt-test", Seed: 23, Repeats: 3, MaxSteps: 8},
		Metrics:            gjeval.Metrics{TaskCount: 1, EpisodeCount: 3, Recall: .5, PassAtK: 1, SafetyPrecision: 1},
		ProviderUsage:      gjeval.ProviderUsage{TotalTokens: 100, Complete: true},
		Acceptance:         gjeval.Acceptance{SuiteValid: true, SafetyPass: true, HardPass: true},
	}
}
