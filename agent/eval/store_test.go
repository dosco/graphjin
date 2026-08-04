package eval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRejectsUnsafeRunID(t *testing.T) {
	store := NewStore(t.TempDir())
	if _, err := store.WriteReport(Report{RunID: "../outside"}); err == nil {
		t.Fatal("unsafe report run_id was accepted")
	}
	if _, err := store.WriteEpisode(Episode{RunID: `..\outside`}); err == nil {
		t.Fatal("unsafe episode run_id was accepted")
	}
}

func TestStoreManifestPermissionsAndRunLock(t *testing.T) {
	store := NewStore(t.TempDir())
	manifest := RunManifest{RunID: "run-1", Status: RunStatusInterrupted, UpdatedAt: time.Unix(1, 0)}
	path, err := store.WriteManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest permissions info=%v err=%v", info, err)
	}
	lock, err := store.LockRun(context.Background(), manifest.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LockRun(context.Background(), manifest.RunID); err == nil {
		t.Fatal("concurrent run lock succeeded")
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if second, err := store.LockRun(context.Background(), manifest.RunID); err != nil {
		t.Fatalf("released lock was not reusable: %v", err)
	} else {
		_ = second.Close()
	}
}

func TestFindRunUsesStrictCompatibilityAndAutoIgnoresMismatch(t *testing.T) {
	store := NewStore(t.TempDir())
	have := compatibleTestManifest("run-old")
	if _, err := store.WriteManifest(have); err != nil {
		t.Fatal(err)
	}
	want := compatibleTestManifest("run-new")
	want.BinaryFingerprint = "new-binary"
	found, ignored, err := store.FindRun(want, ResumeAuto, "")
	if err != nil {
		t.Fatal(err)
	}
	if found != nil || len(ignored) != 1 || ignored[0] != have.RunID {
		t.Fatalf("auto match = found:%+v ignored:%v", found, ignored)
	}
	if _, mismatches, err := store.FindRun(want, ResumeExact, have.RunID); err == nil || !strings.Contains(err.Error(), "binary_fingerprint") || len(mismatches) == 0 {
		t.Fatalf("exact mismatch = fields:%v err:%v", mismatches, err)
	}
}

func TestStoreRejectsCorruptUnsupportedAndCompletedResumeState(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ManifestPath("corrupt"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadManifest("corrupt"); err == nil {
		t.Fatal("corrupt manifest was accepted")
	}
	unsupported := compatibleTestManifest("unsupported")
	unsupported.SchemaVersion = "graphjin.eval.run/v999"
	data, _ := json.Marshal(unsupported)
	if err := os.WriteFile(store.ManifestPath(unsupported.RunID), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadManifest(unsupported.RunID); err == nil {
		t.Fatal("unsupported manifest schema was accepted")
	}
	if found, ignored, err := store.FindRun(compatibleTestManifest("fresh"), ResumeAuto, ""); err != nil || found != nil || len(ignored) != 2 {
		t.Fatalf("auto resume did not safely ignore corrupt history: found=%+v ignored=%v err=%v", found, ignored, err)
	}
	completed := compatibleTestManifest("completed")
	completed.Status = RunStatusComplete
	if _, err := store.WriteManifest(completed); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.FindRun(completed, ResumeExact, completed.RunID); err == nil {
		t.Fatal("completed run was resumed")
	}
}

func TestStoreEpisodeWriteOnceAndLegacyBaselineReader(t *testing.T) {
	store := NewStore(t.TempDir())
	episode := Episode{SchemaVersion: EpisodeSchemaVersion, RunID: "run", TaskID: "task", TaskSlug: "task", Repeat: 1}
	if _, err := store.WriteEpisode(episode); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteEpisode(episode); err != nil {
		t.Fatalf("identical episode was not idempotent: %v", err)
	}
	episode.Error = "different"
	if _, err := store.WriteEpisode(episode); err == nil {
		t.Fatal("different episode overwrote a finalized slot")
	}

	legacy := Report{SchemaVersion: LegacyReportVersion, RunID: "legacy", Acceptance: Acceptance{HardPass: true, SafetyPass: true}}
	data, _ := json.Marshal(legacy)
	if err := os.WriteFile(store.BaselinePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadBaseline()
	if err != nil || loaded == nil || loaded.RunID != legacy.RunID {
		t.Fatalf("legacy baseline = %+v err=%v", loaded, err)
	}
	v2 := Report{SchemaVersion: ReportV2Version, RunID: "v2", Acceptance: Acceptance{HardPass: true, SafetyPass: true}}
	data, _ = json.Marshal(v2)
	if err := os.WriteFile(store.BaselinePath(), data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.LoadBaseline()
	if err != nil || loaded == nil || loaded.RunID != v2.RunID {
		t.Fatalf("v2 baseline = %+v err=%v", loaded, err)
	}
	if err := store.PromoteBaseline(Report{RunID: "partial", RunStatus: RunStatusInterrupted, Acceptance: Acceptance{HardPass: true, SafetyPass: true}}); err == nil {
		t.Fatal("incomplete report was promoted")
	}
	if _, err := os.Stat(filepath.Join(store.Root, "episodes", "run", episodeFilename(Episode{RunID: "run", TaskID: "task", TaskSlug: "task", Repeat: 1}))); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRecoversAttemptAccountingAfterManifestLag(t *testing.T) {
	store := NewStore(t.TempDir())
	manifest := compatibleTestManifest("lagged")
	manifest.Progress.ProviderAttempts = 0
	if _, err := store.WriteManifest(manifest); err != nil {
		t.Fatal(err)
	}
	attempt := Attempt{
		RunID: manifest.RunID, TaskID: "task", TaskSlug: "task", Repeat: 1, Attempt: 1,
		ErrorCode: "provider_timeout", Retryable: true, LatencyMS: 10, Tokens: TokenUsage{Prompt: 2, Completion: 1, Total: 3},
	}
	path, err := store.WriteAttempt(attempt)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("attempt permissions info=%v err=%v", info, err)
	}
	loaded, err := store.LoadAttempts(manifest.RunID)
	if err != nil || len(loaded) != 1 {
		t.Fatalf("attempts=%+v err=%v", loaded, err)
	}
	rebuildRunAccounting(&manifest, nil, loaded)
	if manifest.Progress.ProviderAttempts != 1 || manifest.Progress.RetryCount != 0 || manifest.ProviderUsage.TotalTokens != 3 || manifest.ProviderUsage.Complete || manifest.ProviderUsage.UnknownAttempts != 1 {
		t.Fatalf("rebuilt manifest = %+v", manifest)
	}
	attempt.Attempt = 2
	if _, err := store.WriteAttempt(attempt); err != nil {
		t.Fatalf("next attempt collided after accounting recovery: %v", err)
	}
}

func TestStoreWritesAndLoadsShareableMarkdown(t *testing.T) {
	store := NewStore(t.TempDir()).WithSecrets("super-secret")
	report := markdownTestReport()
	report.RunID = "markdown-run"
	report.Acceptance.Notices = []string{"token super-secret was configured"}
	jsonPath, err := store.WriteReport(report)
	if err != nil {
		t.Fatal(err)
	}
	markdownPath := store.ReportMarkdownPath(report.RunID)
	for _, path := range []string{jsonPath, markdownPath} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("permissions for %s: info=%v err=%v", path, info, err)
		}
	}
	markdown, err := store.LoadReportMarkdown(report.RunID)
	if err != nil || !strings.Contains(string(markdown), "[REDACTED]") || strings.Contains(string(markdown), "super-secret") {
		t.Fatalf("markdown redaction failed: %q err=%v", markdown, err)
	}
	loaded, err := store.LoadReport(report.RunID)
	if err != nil || loaded.RunID != report.RunID || loaded.RunStatus != RunStatusComplete {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
}

func TestStoreLoadsPartialReportWithoutLosingNotice(t *testing.T) {
	store := NewStore(t.TempDir())
	partial := PartialReport{SchemaVersion: ReportSchemaVersion, RunID: "partial", RunStatus: RunStatusEnvironmentFailed, EnvironmentCode: "provider_timeout", Notice: "try again"}
	if _, err := store.WritePartialReport(partial); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadReport(partial.RunID)
	if err != nil || loaded.EnvironmentCode != partial.EnvironmentCode || loaded.Notice != partial.Notice {
		t.Fatalf("partial=%+v err=%v", loaded, err)
	}
}

func TestStoreLoadReportValidation(t *testing.T) {
	store := NewStore(t.TempDir())
	if err := store.Init(); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(store.Root, "reports", name+".json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("corrupt", "{")
	write("unknown", `{"schema_version":"graphjin.eval.report/v999","run_id":"unknown"}`)
	write("mismatch", `{"schema_version":"graphjin.eval.report/v3","run_id":"other"}`)
	for _, runID := range []string{"corrupt", "unknown", "mismatch", "../outside"} {
		if _, err := store.LoadReport(runID); err == nil {
			t.Fatalf("LoadReport(%q) succeeded", runID)
		}
	}
	if markdown, err := store.LoadReportMarkdown("missing"); err != nil || markdown != nil {
		t.Fatalf("missing markdown=%q err=%v", markdown, err)
	}
}

func TestStoreListReportsSortsAndSkipsTempFiles(t *testing.T) {
	store := NewStore(t.TempDir())
	older := markdownTestReport()
	older.RunID, older.GeneratedAt = "older", time.Unix(1, 0)
	newer := markdownTestReport()
	newer.RunID, newer.GeneratedAt = "newer", time.Unix(2, 0)
	newer.Tasks = []TaskVerdict{{TaskID: "public", Slug: "private-slug"}}
	if _, err := store.WriteReport(older); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteReport(newer); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.Root, "reports", ".graphjin-eval-temp"), []byte("private-slug"), 0o600); err != nil {
		t.Fatal(err)
	}
	summaries, err := store.ListReports()
	if err != nil || len(summaries) != 2 || summaries[0].RunID != "newer" || !summaries[0].HasMarkdown {
		t.Fatalf("summaries=%+v err=%v", summaries, err)
	}
	data, _ := json.Marshal(summaries[0])
	if strings.Contains(string(data), "private-slug") || strings.Contains(string(data), "tasks") {
		t.Fatalf("summary leaked task data: %s", data)
	}
}

func compatibleTestManifest(runID string) RunManifest {
	return RunManifest{
		RunID: runID, Intent: RunIntentRun, Mode: RunModeRun, Status: RunStatusInterrupted,
		SuiteFingerprint: "suite", OracleValueHash: "oracle", DatasetFingerprint: DatasetFingerprint{CatalogHash: "catalog"},
		TaskSchemaVersion: TaskSchemaVersion, EpisodeSchemaVersion: EpisodeSchemaVersion,
		ReportSchemaVersion: ReportSchemaVersion, RewardVersion: RewardVersion, GeneratorVersion: GeneratorVersion,
		BinaryFingerprint: "binary", ServerEvalFingerprint: "server",
		Provenance: RunProvenance{Provider: "provider", Model: "model", ServerFingerprint: "server", Target: "local", Seed: 23, Repeats: 3},
	}
}
