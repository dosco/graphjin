package serv

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/dosco/graphjin/auth/v3"
)

const evalReportsCapability = "eval.reports"

var evalReportRunIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

type evalReportsListResponse struct {
	Available bool                   `json:"available"`
	StateDir  string                 `json:"state_dir"`
	Reports   []gjeval.ReportSummary `json:"reports"`
	Warning   string                 `json:"warning,omitempty"`
}

type evalReportDetailResponse struct {
	Available       bool             `json:"available"`
	StateDir        string           `json:"state_dir"`
	RunID           string           `json:"run_id"`
	RunStatus       gjeval.RunStatus `json:"run_status"`
	Mode            gjeval.RunMode   `json:"mode"`
	GeneratedAt     time.Time        `json:"generated_at"`
	Provider        string           `json:"provider,omitempty"`
	Model           string           `json:"model,omitempty"`
	TaskCount       int              `json:"task_count"`
	EpisodeCount    int              `json:"episode_count"`
	Recall          float64          `json:"recall"`
	PassAtK         float64          `json:"pass_at_k"`
	GroundTruth     float64          `json:"ground_truth_recall"`
	Method          float64          `json:"method_recall"`
	Safety          float64          `json:"safety_precision"`
	Behavior        float64          `json:"behavior_recall"`
	TotalTokens     int64            `json:"total_tokens"`
	ProviderTokens  int64            `json:"provider_total_tokens"`
	Accepted        bool             `json:"accepted"`
	EnvironmentCode string           `json:"environment_code,omitempty"`
	Notice          string           `json:"notice,omitempty"`
	Markdown        string           `json:"markdown"`
}

func (s *HttpService) EvalReports(ah auth.HandlerFunc) http.Handler {
	return apiV1Handler(s, nil, s.apiV1EvalReports(nil), ah)
}

func (s *HttpService) EvalReportsWithNS(ah auth.HandlerFunc, ns string) http.Handler {
	return apiV1Handler(s, &ns, s.apiV1EvalReports(&ns), ah)
}

func (s1 *HttpService) apiV1EvalReports(_ *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]any{"error": "method_not_allowed", "message": "Only GET is supported."})
			return
		}
		s := s1.Load().(*graphjinService)
		if !evalReportsConfigured(s.conf) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "not_found", "message": "Evaluation reports are unavailable in this mode."})
			return
		}
		ctx := s.applyIdentityContext(r.Context())
		roots := consoleReadableRoots(s.conf, runtimeRoleClass(ctx))
		features := consoleEnabledFeatures(s.conf)
		if !consoleAdminWorkspaceAvailable(roots, features) {
			writeJSONStatus(w, http.StatusForbidden, map[string]any{"error": "operator_access_required", "message": "Trainer reports require operator access to the GraphJin console."})
			return
		}

		stateDir, err := resolveEvalReportsStateDir(s.conf)
		if err != nil {
			writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "state_dir_invalid", "message": err.Error()})
			return
		}
		store := gjeval.NewStore(stateDir)
		runID, detail, valid := evalReportsRequestRunID(r.URL.Path)
		if !valid {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"error": "invalid_run_id", "message": "Run IDs may contain only letters, numbers, dot, underscore, and hyphen."})
			return
		}
		if !detail {
			handleEvalReportList(w, store, stateDir)
			return
		}
		handleEvalReportDetail(w, store, stateDir, runID)
	})
}

func handleEvalReportList(w http.ResponseWriter, store *gjeval.Store, stateDir string) {
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		writeJSONStatus(w, http.StatusOK, evalReportsListResponse{Available: false, StateDir: stateDir, Reports: []gjeval.ReportSummary{}})
		return
	} else if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "state_unreadable", "message": err.Error(), "state_dir": stateDir})
		return
	}
	reports, err := store.ListReports()
	response := evalReportsListResponse{Available: true, StateDir: stateDir, Reports: reports}
	if response.Reports == nil {
		response.Reports = []gjeval.ReportSummary{}
	}
	if err != nil {
		response.Warning = err.Error()
	}
	writeJSONStatus(w, http.StatusOK, response)
}

func handleEvalReportDetail(w http.ResponseWriter, store *gjeval.Store, stateDir, runID string) {
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "report_not_found", "message": "Evaluation state directory does not exist.", "state_dir": stateDir})
		return
	}
	report, err := store.LoadReport(runID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSONStatus(w, http.StatusNotFound, map[string]any{"error": "report_not_found", "message": "Evaluation report not found."})
			return
		}
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "report_unreadable", "message": err.Error()})
		return
	}
	markdown, err := store.LoadReportMarkdown(runID)
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]any{"error": "report_unreadable", "message": err.Error()})
		return
	}
	if markdown == nil {
		if report.RunStatus == "" || report.RunStatus == gjeval.RunStatusComplete {
			markdown = []byte(gjeval.RenderReportMarkdown(report.Report))
		} else {
			markdown = []byte(gjeval.RenderPartialReportMarkdown(storedPartialReport(report)))
		}
	}
	writeJSONStatus(w, http.StatusOK, evalReportDetailResponse{
		Available: true, StateDir: stateDir, RunID: report.RunID, RunStatus: report.RunStatus, Mode: report.Mode,
		GeneratedAt: report.GeneratedAt, Provider: report.Provenance.Provider, Model: report.Provenance.Model,
		TaskCount: report.Metrics.TaskCount, EpisodeCount: report.Metrics.EpisodeCount,
		Recall: report.Metrics.Recall, PassAtK: report.Metrics.PassAtK, GroundTruth: report.Metrics.GroundTruthRecall,
		Method: report.Metrics.MethodRecall, Safety: report.Metrics.SafetyPrecision, Behavior: report.Metrics.BehaviorRecall,
		TotalTokens: report.Metrics.TotalTokens, ProviderTokens: report.ProviderUsage.TotalTokens,
		Accepted: report.Acceptance.HardPass, EnvironmentCode: report.EnvironmentCode, Notice: report.Notice,
		Markdown: string(markdown),
	})
}

func storedPartialReport(report *gjeval.StoredReport) gjeval.PartialReport {
	return gjeval.PartialReport{
		SchemaVersion: report.SchemaVersion, UsageAccountingVersion: report.UsageAccountingVersion, RewardVersion: report.RewardVersion,
		RunID: report.RunID, RunStatus: report.RunStatus, Mode: report.Mode, GeneratedAt: report.GeneratedAt,
		SuiteFingerprint: report.SuiteFingerprint, CatalogFingerprint: report.CatalogFingerprint,
		DatasetFingerprint: report.DatasetFingerprint, OracleValueHash: report.OracleValueHash,
		Provenance: report.Provenance, Progress: report.Progress, ProviderUsage: report.ProviderUsage,
		EnvironmentCode: report.EnvironmentCode, Notice: report.Notice,
	}
}

func evalReportsRequestRunID(requestPath string) (string, bool, bool) {
	if requestPath == routeEvalReports {
		return "", false, true
	}
	if !strings.HasPrefix(requestPath, routeEvalReportsPrefix) {
		return "", true, false
	}
	runID := strings.TrimPrefix(requestPath, routeEvalReportsPrefix)
	return runID, true, evalReportRunIDPattern.MatchString(runID)
}

func evalReportsConfigured(conf *Config) bool {
	if conf == nil || conf.Serv.Production || effectiveMode(conf) == modeProd {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(conf.Serv.EvalStateDir), "off")
}

func evalReportsDirectoryAvailable(conf *Config) bool {
	if !evalReportsConfigured(conf) {
		return false
	}
	stateDir, err := resolveEvalReportsStateDir(conf)
	if err != nil {
		return false
	}
	info, err := os.Stat(stateDir)
	return err == nil && info.IsDir()
}

func resolveEvalReportsStateDir(conf *Config) (string, error) {
	configured := ""
	if conf != nil {
		configured = strings.TrimSpace(conf.Serv.EvalStateDir)
	}
	base := securityConfigPath(conf)
	path := configured
	if path == "" {
		path = gjeval.DefaultStateDir
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return filepath.Abs(path)
}
