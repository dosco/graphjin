package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	gjeval "github.com/dosco/graphjin/agent/v3/eval"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const (
	benchmarkDataVersion  = "graphjin.benchmark.data/v2"
	defaultBenchmarkSlug  = "deeporg"
	defaultBenchmarkName  = "DeepORG — The Organizational Agent Benchmark"
	defaultBenchmarkScope = "2026.1/2026.2 — governed read-only Q&A · 2027.1 — adds writes, alerts, follow-ups, multi-source · 2028.1 — intent-phrased tasks with execution twins · 2028.2 — cross-source ruler fix (v11)"
)

type benchmarkIdentity struct {
	Slug string `yaml:"slug"`
	Name string `yaml:"name"`
}

type benchmarkData struct {
	SchemaVersion string            `yaml:"schema_version"`
	Benchmark     benchmarkIdentity `yaml:"benchmark"`
	Suite         benchmarkSuite    `yaml:"suite"`
	Runs          []benchmarkEntry  `yaml:"runs"`
}

type benchmarkSuite struct {
	Generation           string            `yaml:"generation,omitempty"`
	ComparisonGeneration string            `yaml:"comparison_generation,omitempty"`
	ScopeLabel           string            `yaml:"scope_label,omitempty"`
	GenerationScopes     map[string]string `yaml:"generation_scopes,omitempty"`
	GeneratorVersion     string            `yaml:"generator_version,omitempty"`
	Identity             string            `yaml:"identity,omitempty"`
	SuiteFingerprint     string            `yaml:"suite_fingerprint,omitempty"`
	CatalogHash          string            `yaml:"catalog_hash,omitempty"`
	SeedManifestHash     string            `yaml:"seed_manifest_hash,omitempty"`
	Mode                 string            `yaml:"mode,omitempty"`
	Seed                 int64             `yaml:"seed,omitempty"`
	Repeats              int               `yaml:"repeats,omitempty"`
	MaxSteps             int               `yaml:"max_steps,omitempty"`
	Temperature          float64           `yaml:"temperature,omitempty"`
	RewardVersion        string            `yaml:"reward_version,omitempty"`
	CategoryCounts       map[string]int    `yaml:"category_counts,omitempty"`
	RollupMapVersion     string            `yaml:"rollup_map_version,omitempty"`
	RollupMap            map[string]string `yaml:"rollup_map,omitempty"`
}

type benchmarkEntry struct {
	RunID                       string             `yaml:"run_id"`
	Slug                        string             `yaml:"slug"`
	Label                       string             `yaml:"label"`
	Release                     string             `yaml:"release,omitempty"`
	Notes                       string             `yaml:"notes,omitempty"`
	Ranked                      bool               `yaml:"ranked"`
	UnrankedReason              string             `yaml:"unranked_reason,omitempty"`
	Generation                  string             `yaml:"generation"`
	GeneratedAt                 time.Time          `yaml:"generated_at"`
	Model                       string             `yaml:"model"`
	Provider                    string             `yaml:"provider,omitempty"`
	GraphJinCommit              string             `yaml:"graphjin_commit,omitempty"`
	BinaryFingerprint           string             `yaml:"binary_fingerprint,omitempty"`
	SuiteIdentity               string             `yaml:"suite_identity"`
	SuiteFingerprint            string             `yaml:"suite_fingerprint"`
	CatalogHash                 string             `yaml:"catalog_hash"`
	SeedManifestHash            string             `yaml:"seed_manifest_hash,omitempty"`
	OracleValueHash             string             `yaml:"oracle_value_hash,omitempty"`
	DataAnchor                  string             `yaml:"data_anchor,omitempty"`
	RewardVersion               string             `yaml:"reward_version"`
	UsageAccountingVersion      string             `yaml:"usage_accounting_version,omitempty"`
	Seed                        int64              `yaml:"seed"`
	Repeats                     int                `yaml:"repeats"`
	MaxSteps                    int                `yaml:"max_steps"`
	Temperature                 float64            `yaml:"temperature"`
	TaskCount                   int                `yaml:"task_count"`
	EpisodeCount                int                `yaml:"episode_count"`
	Recall                      float64            `yaml:"recall"`
	RecallCILow                 float64            `yaml:"recall_ci_low"`
	RecallCIHigh                float64            `yaml:"recall_ci_high"`
	PassAtK                     float64            `yaml:"pass_at_k"`
	PassPowerK                  float64            `yaml:"pass_power_k"`
	GroundTruthRecall           float64            `yaml:"ground_truth_recall"`
	MethodRecall                float64            `yaml:"method_recall"`
	SafetyPrecision             float64            `yaml:"safety_precision"`
	BehaviorRecall              float64            `yaml:"behavior_recall"`
	CategoryRecall              map[string]float64 `yaml:"category_recall,omitempty"`
	RollupRecall                map[string]float64 `yaml:"rollup_recall,omitempty"`
	RollupPassPower             map[string]float64 `yaml:"rollup_pass_power,omitempty"`
	MeanReward                  float64            `yaml:"mean_reward"`
	TotalTokens                 int64              `yaml:"total_tokens"`
	PromptTokens                int64              `yaml:"prompt_tokens,omitempty"`
	CompletionTokens            int64              `yaml:"completion_tokens,omitempty"`
	ProviderTotalTokens         int64              `yaml:"provider_total_tokens"`
	LatencyP50MS                float64            `yaml:"latency_p50_ms,omitempty"`
	LatencyP95MS                float64            `yaml:"latency_p95_ms,omitempty"`
	PromptPricePerMillion       float64            `yaml:"prompt_price_per_million,omitempty"`
	CompletionPricePerMillion   float64            `yaml:"completion_price_per_million,omitempty"`
	EstimatedListCostUSD        float64            `yaml:"estimated_list_cost_usd,omitempty"`
	EstimatedListCostPerTaskUSD float64            `yaml:"estimated_list_cost_per_task_usd,omitempty"`
	PricingSource               string             `yaml:"pricing_source,omitempty"`
	ScoringSuspect              bool               `yaml:"scoring_suspect,omitempty"`
	UsageIncomplete             bool               `yaml:"usage_incomplete,omitempty"`
	GuardInterventions          int                `yaml:"guard_interventions,omitempty"`
	ForbiddenAttempts           int                `yaml:"forbidden_attempts,omitempty"`
	UnsafeEffects               int                `yaml:"unsafe_effects"`
	RescoredFrom                string             `yaml:"rescored_from,omitempty"`
	Accepted                    bool               `yaml:"accepted"`
}

type evalPublishOptions struct {
	Site                      string
	Data                      string
	Benchmark                 string
	Label                     string
	Release                   string
	Notes                     string
	Force                     bool
	AllowOffSuite             bool
	AllowSuspectScoring       bool
	AllowIncompleteUsage      bool
	PromptPricePerMillion     float64
	CompletionPricePerMillion float64
	PricingSource             string
}

func evalPublishCmd(evalOpts *evalCLIOptions) *cobra.Command {
	opts := &evalPublishOptions{}
	cmd := &cobra.Command{
		Use:   "publish <run-id>",
		Short: "Publish one shareable evaluation report to the benchmark website",
		Long: `Publish one completed evaluation report to the benchmark website.

The command writes one leaderboard row and one run page, then stops. It never
runs git. Publish refuses incomplete, environment-failed, invalid-suite,
build-mismatched, empty, and scoring-suspect runs. It deliberately does not
refuse a low score: accepted=false is a benchmark result, not a reason to hide
the run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEvalPublish(cmd, evalOpts, opts, strings.TrimSpace(args[0]))
		},
	}
	cmd.Flags().StringVar(&opts.Site, "site", "website", "website root")
	cmd.Flags().StringVar(&opts.Data, "data", "", "leaderboard data file (default <site>/data/benchmarks/<benchmark>.yaml)")
	cmd.Flags().StringVar(&opts.Benchmark, "benchmark", defaultBenchmarkSlug, "public benchmark slug")
	cmd.Flags().StringVar(&opts.Label, "label", "", "leaderboard display label only; model identity uses provider and model (default: model)")
	cmd.Flags().StringVar(&opts.Release, "release", "", "GraphJin release label (default: short commit)")
	cmd.Flags().StringVar(&opts.Notes, "notes", "", "short public notes for this run")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "replace an existing row and overwrite its page")
	cmd.Flags().BoolVar(&opts.AllowOffSuite, "allow-off-suite", false, "publish a non-matching run as explicitly unranked")
	cmd.Flags().BoolVar(&opts.AllowSuspectScoring, "allow-suspect-scoring", false, "publish despite an answer/method scoring divergence warning")
	cmd.Flags().BoolVar(&opts.AllowIncompleteUsage, "allow-incomplete-usage", false, "publish a run whose token accounting has gaps; usage and cost are withheld from the row rather than understated")
	cmd.Flags().Float64Var(&opts.PromptPricePerMillion, "prompt-price-per-million", 0, "provider list price in USD per million prompt tokens")
	cmd.Flags().Float64Var(&opts.CompletionPricePerMillion, "completion-price-per-million", 0, "provider list price in USD per million completion tokens")
	cmd.Flags().StringVar(&opts.PricingSource, "pricing-source", "", "public pricing source or price-card date")
	return cmd
}

func runEvalPublish(cmd *cobra.Command, evalOpts *evalCLIOptions, opts *evalPublishOptions, runID string) error {
	benchmark, benchmarkKey, err := resolveBenchmarkIdentity(opts.Benchmark)
	if err != nil {
		return &evalExitError{Code: 2, Err: err}
	}
	stateDir, err := evalStateDirForPublish(cmd, evalOpts)
	if err != nil {
		return err
	}
	store := gjeval.NewStore(stateDir)
	stored, err := store.LoadReport(runID)
	if err != nil {
		return err
	}
	report := stored.Report
	if report.RunStatus != gjeval.RunStatusComplete {
		return &evalExitError{Code: 1, Err: fmt.Errorf("run %s is %s; only complete runs can be published", runID, report.RunStatus)}
	}
	if !report.Acceptance.SuiteValid {
		return &evalExitError{Code: 2, Err: fmt.Errorf("run %s has an invalid suite", runID)}
	}
	if report.Acceptance.EnvironmentFailure {
		return &evalExitError{Code: 3, Err: fmt.Errorf("run %s has an evaluation environment failure", runID)}
	}
	if report.Metrics.TaskCount == 0 {
		return &evalExitError{Code: 1, Err: fmt.Errorf("run %s has no scored tasks", runID)}
	}
	buildCommit := report.Provenance.GraphJinCommit
	buildFingerprint := report.Provenance.BinaryFingerprint
	buildLabel := "run"
	if report.RescoredFrom != "" {
		if report.ScoringProvenance == nil || report.ScoringProvenance.RewardVersion != report.RewardVersion {
			return &evalExitError{Code: 2, Err: fmt.Errorf("run %s has incomplete scoring provenance", runID)}
		}
		buildCommit = report.ScoringProvenance.GraphJinCommit
		buildFingerprint = report.ScoringProvenance.BinaryFingerprint
		buildLabel = "scorer"
	}
	if strings.TrimSpace(buildCommit) == "" {
		return &evalExitError{Code: 2, Err: fmt.Errorf("run %s has no graphjin_commit; rebuild and rerun before publishing", runID)}
	}
	currentFingerprint := evalBinaryFingerprint()
	if currentFingerprint == "" {
		return &evalExitError{Code: 2, Err: errors.New("cannot fingerprint the current GraphJin binary; publishing cannot verify build identity")}
	}
	if buildFingerprint == "" || buildFingerprint != currentFingerprint {
		return &evalExitError{Code: 2, Err: fmt.Errorf("run %s %s binary_fingerprint does not match the current GraphJin build; publish with the same binary that scored the report", runID, buildLabel)}
	}
	if (report.Acceptance.ScoringSuspect || gjeval.IsScoringDivergenceSuspect(report.Metrics)) && !opts.AllowSuspectScoring {
		return &evalExitError{Code: 2, Err: fmt.Errorf("run %s has a suspect answer/method scoring divergence; investigate it or use --allow-suspect-scoring to override", runID)}
	}
	if (!report.ProviderUsage.Complete || report.ProviderUsage.UnknownAttempts != 0) && !opts.AllowIncompleteUsage {
		return &evalExitError{Code: 2, Err: fmt.Errorf("run %s has incomplete provider usage accounting; publishing requires complete prompt and completion totals, or --allow-incomplete-usage to publish the scores with usage withheld", runID)}
	}
	if (opts.PromptPricePerMillion == 0) != (opts.CompletionPricePerMillion == 0) || opts.PromptPricePerMillion < 0 || opts.CompletionPricePerMillion < 0 {
		return &evalExitError{Code: 2, Err: errors.New("list pricing requires both non-negative --prompt-price-per-million and --completion-price-per-million values")}
	}

	dataPath := opts.Data
	if strings.TrimSpace(dataPath) == "" {
		dataPath = filepath.Join(opts.Site, "data", "benchmarks", benchmarkKey+".yaml")
	}
	data, err := loadBenchmarkData(dataPath, benchmark)
	if err != nil {
		return err
	}
	slug := benchmarkRunSlug(runID)
	pagePath := filepath.Join(opts.Site, "content", "benchmarks", benchmark.Slug, "runs", slug+".md")
	existing := -1
	for i := range data.Runs {
		if data.Runs[i].RunID == runID {
			existing = i
			break
		}
	}
	if existing >= 0 && !opts.Force {
		return fmt.Errorf("run %s is already published; use --force to replace it", runID)
	}
	if _, err := os.Stat(pagePath); err == nil && !opts.Force {
		return fmt.Errorf("benchmark page %s already exists; use --force to overwrite it", pagePath)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	comparisonSuite := data.Suite
	currentPublicSuite := ""
	if benchmark.Slug == defaultBenchmarkSlug {
		currentPublicSuite = gjeval.PublicBenchmark().SuiteFingerprint
	}
	advancingCohort := currentPublicSuite != "" && report.SuiteFingerprint == currentPublicSuite &&
		data.Suite.SuiteFingerprint != "" && data.Suite.SuiteFingerprint != report.SuiteFingerprint
	correctingScoring := currentPublicSuite != "" && report.SuiteFingerprint == currentPublicSuite &&
		data.Suite.SuiteFingerprint == report.SuiteFingerprint && data.Suite.RewardVersion != "" &&
		data.Suite.RewardVersion != report.RewardVersion && report.RewardVersion == gjeval.RewardVersion
	if len(data.Runs) == 0 || advancingCohort || correctingScoring {
		// With no published rows, the first run defines the metadata for the
		// current pinned cohort. This permits an intentional regenerated public
		// suite to replace the prior empty cohort without an off-suite override.
		comparisonSuite = benchmarkSuite{}
	}
	mismatches := benchmarkComparabilityMismatches(report, comparisonSuite, currentPublicSuite)
	ranked := len(mismatches) == 0
	if !ranked && !opts.AllowOffSuite {
		return fmt.Errorf("run %s does not match the public benchmark cohort (%s); use --allow-off-suite to publish it as unranked", runID, strings.Join(mismatches, ", "))
	}
	if !evalOpts.Yes {
		if !isInteractiveTTY() {
			return errors.New("benchmark publishing requires --yes in non-interactive mode")
		}
		ok, err := promptConfirm(newPromptIO(cmd.InOrStdin(), cmd.OutOrStdout()), fmt.Sprintf("Publish evaluation run %s to %s?", runID, opts.Site), false)
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("publish aborted")
		}
	}

	if (advancingCohort || correctingScoring) && ranked {
		previousGeneration := strings.TrimSpace(data.Suite.Generation)
		if previousGeneration == "" {
			previousGeneration = "previous"
		}
		for i := range data.Runs {
			if !data.Runs[i].Ranked {
				continue
			}
			data.Runs[i].Ranked = false
			if correctingScoring {
				data.Runs[i].UnrankedReason = "superseded by corrected scoring contract (" + data.Suite.RewardVersion + " → " + report.RewardVersion + ")"
			} else {
				data.Runs[i].UnrankedReason = "previous public benchmark cohort (" + previousGeneration + ")"
			}
		}
		data.Suite = benchmarkSuiteFromReport(report)
	} else if (data.Suite.Identity == "" || len(data.Runs) == 0) && ranked {
		data.Suite = benchmarkSuiteFromReport(report)
	}
	label := strings.TrimSpace(opts.Label)
	if label == "" {
		label = strings.TrimSpace(report.Provenance.Model)
	}
	if label == "" {
		label = "Unknown model"
	}
	release := strings.TrimSpace(opts.Release)
	if release == "" {
		release = shortRevision(report.Provenance.GraphJinCommit)
	}
	entry := benchmarkEntryFromReport(report, slug, label, release, opts.Notes, ranked, strings.Join(mismatches, "; "), opts)
	if ranked {
		for i := range data.Runs {
			if !data.Runs[i].Ranked {
				continue
			}
			if data.Runs[i].Generation != entry.Generation {
				data.Runs[i].Ranked = false
				data.Runs[i].UnrankedReason = "previous public benchmark cohort (" + data.Runs[i].Generation + ")"
				continue
			}
			// Same cohort, same provider/model, older GraphJin build. Display labels
			// are presentation only and may change between publishes. Legacy rows that
			// predate provider/model provenance fall back to matching the label.
			if data.Runs[i].RunID != entry.RunID && sameBenchmarkModel(data.Runs[i], entry) {
				data.Runs[i].Ranked = false
				data.Runs[i].UnrankedReason = "superseded by GraphJin build " + entry.Release
			}
		}
		data.Suite.ComparisonGeneration = entry.Generation
	}
	if benchmark.Slug == defaultBenchmarkSlug && data.Suite.ScopeLabel == "" {
		data.Suite.ScopeLabel = defaultBenchmarkScope
	}
	if benchmark.Slug == defaultBenchmarkSlug && len(data.Suite.GenerationScopes) == 0 {
		data.Suite.GenerationScopes = defaultBenchmarkGenerationScopes()
	}
	if existing >= 0 {
		data.Runs[existing] = entry
	} else {
		data.Runs = append(data.Runs, entry)
	}
	sort.Slice(data.Runs, func(i, j int) bool { return data.Runs[i].RunID < data.Runs[j].RunID })

	// The rollup mapping is frozen, versioned data the site checker validates
	// rows against; a same-generation publish must carry it too, not only the
	// cohort-advance path that rebuilds the whole suite block.
	if data.Suite.RollupMapVersion != gjeval.PublicBenchmarkRollupVersion {
		data.Suite.RollupMapVersion = gjeval.PublicBenchmarkRollupVersion
		data.Suite.RollupMap = gjeval.BenchmarkRollupMap()
	}

	markdown, err := store.LoadReportMarkdown(runID)
	if err != nil {
		return err
	}
	technicalMarkdown, err := store.LoadReportTechnicalMarkdown(runID)
	if err != nil {
		return err
	}
	if technicalMarkdown == nil && markdown != nil && !strings.Contains(string(markdown), gjeval.FriendlyReportMarkdownVersion) {
		technicalMarkdown = markdown
		markdown = nil
	}
	if markdown == nil {
		markdown = []byte(gjeval.RenderFriendlyReportMarkdown(report))
	}
	if technicalMarkdown == nil {
		technicalMarkdown = []byte(gjeval.RenderTechnicalReportMarkdown(report))
	}
	page, err := renderBenchmarkRunPage(benchmark, benchmarkKey, entry, markdown, technicalMarkdown)
	if err != nil {
		return err
	}
	yamlData, err := marshalBenchmarkData(data)
	if err != nil {
		return err
	}
	if err := atomicWritePublicFile(dataPath, yamlData); err != nil {
		return err
	}
	if err := atomicWritePublicFile(pagePath, page); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Published benchmark data: %s\nPublished run page: %s\nReview and commit these files.\n", dataPath, pagePath)
	return nil
}

func evalStateDirForPublish(cmd *cobra.Command, opts *evalCLIOptions) (string, error) {
	if opts.Demo && opts.Remote {
		return "", errors.New("--demo and --remote are mutually exclusive")
	}
	projectPath := cpath
	if opts.Demo && !flagChanged(cmd, "path") && !flagChanged(cmd, "config") {
		projectPath = demoDefaultPath
	}
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return "", err
	}
	stateDir := filepath.Join(abs, gjeval.DefaultStateDir)
	info, err := os.Stat(stateDir)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("no evaluation state at `%s`; run `graphjin eval bench --public --yes` first", stateDir)
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("evaluation state path is not a directory: %s", stateDir)
	}
	return stateDir, nil
}

func sameBenchmarkModel(a, b benchmarkEntry) bool {
	aIdentity, aOK := benchmarkModelIdentity(a)
	bIdentity, bOK := benchmarkModelIdentity(b)
	if aOK && bOK {
		return aIdentity == bIdentity
	}
	aLabel := strings.TrimSpace(a.Label)
	bLabel := strings.TrimSpace(b.Label)
	return aLabel != "" && bLabel != "" && strings.EqualFold(aLabel, bLabel)
}

func benchmarkModelIdentity(entry benchmarkEntry) (string, bool) {
	provider := canonicalBenchmarkProvider(entry.Provider)
	model := strings.ToLower(strings.TrimSpace(entry.Model))
	if provider == "" || model == "" {
		return "", false
	}
	return provider + "\x00" + model, true
}

func canonicalBenchmarkProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "gemini", "google-gemini":
		return "google-gemini"
	default:
		return provider
	}
}

func benchmarkComparabilityMismatches(report gjeval.Report, suite benchmarkSuite, officialSuiteFingerprint string) []string {
	var mismatches []string
	if report.RewardVersion != gjeval.RewardVersion {
		mismatches = append(mismatches, "reward_version")
	}
	if officialSuiteFingerprint != "" && report.SuiteFingerprint != officialSuiteFingerprint {
		mismatches = append(mismatches, "suite_fingerprint")
	}
	if suite.Identity != "" && gjeval.SuiteIdentity(report) != suite.Identity {
		want := reportFromBenchmarkSuite(suite)
		mismatches = append(mismatches, gjeval.SuiteIdentityMismatches(report, want)...)
		if len(mismatches) == 0 {
			mismatches = append(mismatches, "suite_identity")
		}
	}
	return sortedUniqueStrings(mismatches)
}

func benchmarkSuiteFromReport(report gjeval.Report) benchmarkSuite {
	categoryCounts := map[string]int{}
	for _, task := range report.Tasks {
		categoryCounts[string(task.Category)]++
	}
	return benchmarkSuite{
		Generation: gjeval.PublicBenchmarkGeneration, ComparisonGeneration: gjeval.PublicBenchmarkGeneration,
		ScopeLabel: defaultBenchmarkScope, GenerationScopes: defaultBenchmarkGenerationScopes(), GeneratorVersion: gjeval.GeneratorVersion,
		Identity: gjeval.SuiteIdentity(report), SuiteFingerprint: report.SuiteFingerprint,
		CatalogHash: report.DatasetFingerprint.CatalogHash, SeedManifestHash: report.DatasetFingerprint.SeedManifestHash,
		Mode: string(report.Mode), Seed: report.Provenance.Seed, Repeats: report.Provenance.Repeats,
		MaxSteps: report.Provenance.MaxSteps, Temperature: report.Provenance.Temperature, RewardVersion: report.RewardVersion,
		CategoryCounts: categoryCounts,
		RollupMapVersion: gjeval.PublicBenchmarkRollupVersion, RollupMap: gjeval.BenchmarkRollupMap(),
	}
}

func defaultBenchmarkGenerationScopes() map[string]string {
	return map[string]string{
		"2026.1": "governed read-only Q&A",
		"2026.2": "governed read-only Q&A",
		"2027.1": "adds writes, alerts, follow-ups, multi-source",
		"2028.1": "intent-phrased tasks with execution twins, v10 ruler",
		"2028.2": "cross-source ruler fix (v11): argument-free file reads count",
	}
}

func reportFromBenchmarkSuite(s benchmarkSuite) gjeval.Report {
	return gjeval.Report{
		Mode: gjeval.RunMode(s.Mode), SuiteFingerprint: s.SuiteFingerprint, RewardVersion: s.RewardVersion,
		DatasetFingerprint: gjeval.DatasetFingerprint{CatalogHash: s.CatalogHash, SeedManifestHash: s.SeedManifestHash},
		Provenance:         gjeval.RunProvenance{Seed: s.Seed, Repeats: s.Repeats, MaxSteps: s.MaxSteps, Temperature: s.Temperature},
	}
}

func benchmarkEntryFromReport(report gjeval.Report, slug, label, release, notes string, ranked bool, reason string, opts *evalPublishOptions) benchmarkEntry {
	// A run interrupted by provider failures loses the token counts for the
	// attempts in flight. Its scores are unaffected — every episode still ran and
	// was graded — but its receipt has holes. Publishing the partial totals would
	// understate cost silently, so usage is withheld and the row says so.
	usageIncomplete := !report.ProviderUsage.Complete || report.ProviderUsage.UnknownAttempts != 0
	promptTokens, completionTokens := report.ProviderUsage.PromptTokens, report.ProviderUsage.CompletionTokens
	providerTotal, measuredTotal := report.ProviderUsage.TotalTokens, report.Metrics.TotalTokens
	promptPrice, completionPrice := opts.PromptPricePerMillion, opts.CompletionPricePerMillion
	if usageIncomplete {
		promptTokens, completionTokens, providerTotal, measuredTotal = 0, 0, 0, 0
		promptPrice, completionPrice = 0, 0
	}
	estimatedCost := float64(promptTokens)*promptPrice/1_000_000 +
		float64(completionTokens)*completionPrice/1_000_000
	costPerTask := 0.0
	if report.Metrics.TaskCount > 0 {
		costPerTask = estimatedCost / float64(report.Metrics.TaskCount)
	}
	pricingSource := strings.TrimSpace(opts.PricingSource)
	if pricingSource == "" && (promptPrice != 0 || completionPrice != 0) {
		pricingSource = "provider list pricing"
	}
	if usageIncomplete {
		pricingSource = ""
	}
	return benchmarkEntry{
		RunID: report.RunID, Slug: slug, Label: label, Release: release, Notes: strings.TrimSpace(notes), Ranked: ranked, UnrankedReason: reason,
		Generation: gjeval.PublicBenchmarkGeneration, GeneratedAt: report.GeneratedAt, Model: report.Provenance.Model, Provider: report.Provenance.Provider,
		GraphJinCommit: report.Provenance.GraphJinCommit, BinaryFingerprint: report.Provenance.BinaryFingerprint,
		SuiteIdentity: gjeval.SuiteIdentity(report), SuiteFingerprint: report.SuiteFingerprint,
		CatalogHash: report.DatasetFingerprint.CatalogHash, SeedManifestHash: report.DatasetFingerprint.SeedManifestHash,
		OracleValueHash: report.OracleValueHash, DataAnchor: report.DatasetFingerprint.DataAnchor,
		RewardVersion: report.RewardVersion, UsageAccountingVersion: report.UsageAccountingVersion,
		Seed: report.Provenance.Seed, Repeats: report.Provenance.Repeats, MaxSteps: report.Provenance.MaxSteps, Temperature: report.Provenance.Temperature,
		TaskCount: report.Metrics.TaskCount, EpisodeCount: report.Metrics.EpisodeCount, Recall: report.Metrics.Recall,
		RecallCILow: report.Metrics.RecallCI.Low, RecallCIHigh: report.Metrics.RecallCI.High,
		PassAtK: report.Metrics.PassAtK, PassPowerK: report.Metrics.PassPowerK,
		GroundTruthRecall: report.Metrics.GroundTruthRecall, MethodRecall: report.Metrics.MethodRecall,
		SafetyPrecision: report.Metrics.SafetyPrecision, BehaviorRecall: report.Metrics.BehaviorRecall,
		CategoryRecall:  categoryRecallFromMetrics(report.Metrics.ByCategory),
		RollupRecall:    rollupMetricFromReport(report.Metrics.ByRollup, func(v gjeval.TierMetrics) float64 { return v.Recall }),
		RollupPassPower: rollupMetricFromReport(report.Metrics.ByRollup, func(v gjeval.TierMetrics) float64 { return v.PassPowerK }),
		MeanReward:      report.Metrics.MeanReward, TotalTokens: measuredTotal,
		PromptTokens: promptTokens, CompletionTokens: completionTokens,
		ProviderTotalTokens: providerTotal,
		LatencyP50MS:        report.Metrics.LatencyP50MS, LatencyP95MS: report.Metrics.LatencyP95MS,
		PromptPricePerMillion: promptPrice, CompletionPricePerMillion: completionPrice,
		EstimatedListCostUSD: estimatedCost, EstimatedListCostPerTaskUSD: costPerTask, PricingSource: pricingSource,
		ScoringSuspect:     report.Acceptance.ScoringSuspect || gjeval.IsScoringDivergenceSuspect(report.Metrics),
		UsageIncomplete:    usageIncomplete,
		GuardInterventions: report.Metrics.GuardInterventions, ForbiddenAttempts: report.Metrics.ForbiddenAttempts, UnsafeEffects: report.Metrics.UnsafeEffects,
		RescoredFrom: report.RescoredFrom, Accepted: report.Acceptance.HardPass,
	}
}

// rollupMetricFromReport projects one field of the eval-computed rollup
// metrics. The values are computed in agent/eval from whole verdict sets —
// task-weighted, with pass^k over the rollup's own episodes — never derived
// here from category numbers, which would be a mean of means.
func rollupMetricFromReport(metrics map[string]gjeval.TierMetrics, pick func(gjeval.TierMetrics) float64) map[string]float64 {
	if len(metrics) == 0 {
		return nil
	}
	out := make(map[string]float64, len(metrics))
	for name, value := range metrics {
		out[name] = pick(value)
	}
	return out
}

func categoryRecallFromMetrics(metrics map[gjeval.Category]gjeval.TierMetrics) map[string]float64 {
	if len(metrics) == 0 {
		return nil
	}
	out := make(map[string]float64, len(metrics))
	for category, value := range metrics {
		out[string(category)] = value.Recall
	}
	return out
}

func loadBenchmarkData(path string, benchmark benchmarkIdentity) (benchmarkData, error) {
	data := benchmarkData{SchemaVersion: benchmarkDataVersion, Benchmark: benchmark, Runs: []benchmarkEntry{}}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return data, nil
	}
	if err != nil {
		return data, err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&data); err != nil {
		return data, fmt.Errorf("parse benchmark data %s: %w", path, err)
	}
	if data.SchemaVersion == "" {
		data.SchemaVersion = benchmarkDataVersion
	}
	if data.SchemaVersion != benchmarkDataVersion {
		return data, fmt.Errorf("unsupported benchmark data schema_version %q", data.SchemaVersion)
	}
	if data.Benchmark.Slug != benchmark.Slug {
		return data, fmt.Errorf("benchmark data %s belongs to %q, not %q", path, data.Benchmark.Slug, benchmark.Slug)
	}
	if strings.TrimSpace(data.Benchmark.Name) == "" {
		return data, fmt.Errorf("benchmark data %s has no benchmark name", path)
	}
	if data.Runs == nil {
		data.Runs = []benchmarkEntry{}
	}
	return data, nil
}

func marshalBenchmarkData(data benchmarkData) ([]byte, error) {
	data.SchemaVersion = benchmarkDataVersion
	raw, err := yaml.Marshal(data)
	if err != nil {
		return nil, err
	}
	return append([]byte("# Generated by `graphjin eval publish`; review and commit this file.\n"), raw...), nil
}

func renderBenchmarkRunPage(benchmark benchmarkIdentity, benchmarkKey string, entry benchmarkEntry, markdown, technicalMarkdown []byte) ([]byte, error) {
	front := struct {
		Title       string    `yaml:"title"`
		Description string    `yaml:"description"`
		Date        time.Time `yaml:"date"`
		Benchmark   string    `yaml:"benchmark"`
		RunID       string    `yaml:"run_id"`
		Generation  string    `yaml:"benchmark_generation"`
		Ranked      bool      `yaml:"ranked"`
		Model       string    `yaml:"model"`
		Provider    string    `yaml:"provider,omitempty"`
		Release     string    `yaml:"release,omitempty"`
		Aliases     []string  `yaml:"aliases,omitempty"`
	}{
		Title: entry.Label + " benchmark run", Description: "Verified report from " + benchmark.Name + " for " + entry.Label + ".",
		Benchmark: benchmark.Slug,
		Date:      entry.GeneratedAt, RunID: entry.RunID, Generation: entry.Generation, Ranked: entry.Ranked,
		Model: entry.Model, Provider: entry.Provider, Release: entry.Release,
		Aliases: benchmarkRunAliases(benchmark.Slug, entry.Slug),
	}
	frontData, err := yaml.Marshal(front)
	if err != nil {
		return nil, err
	}
	friendlyBody := stripMarkdownReportTitle(string(markdown))
	technicalBody := stripMarkdownReportTitle(string(technicalMarkdown))
	body := friendlyBody
	if rollups := renderBenchmarkRollupLine(entry); rollups != "" {
		body += "\n\n## Capability rollup\n\n" + rollups
	}
	if chart := renderBenchmarkCategorySVG(entry); chart != "" {
		body += "\n\n## Results by task family\n\n" + chart
	}
	body += "\n\n---\n\n## Technical benchmark report\n\n" + technicalBody
	shortcode := fmt.Sprintf("{{< benchmark-run-meta benchmark=%q >}}", benchmarkKey)
	return []byte("---\n" + string(frontData) + "---\n\n" + shortcode + "\n\n" + body), nil
}

// renderBenchmarkRollupLine renders the frozen capability rollup as
// publish-time HTML with machine-readable values, so the site checker can
// assert the rendered numbers against the row and the row against its
// category scores. Numbers are computed in Go and baked into the page — the
// same never-template-computed rule the category SVG follows.
func renderBenchmarkRollupLine(entry benchmarkEntry) string {
	if len(entry.RollupRecall) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(`<p data-benchmark-rollups data-rollup-run="` + entry.RunID + `">`)
	first := true
	for _, name := range gjeval.BenchmarkRollupNames() {
		value, ok := entry.RollupRecall[name]
		if !ok {
			continue
		}
		if !first {
			b.WriteString(" · ")
		}
		first = false
		fmt.Fprintf(&b, `<span data-rollup=%q data-rollup-recall="%.17g">%s %.1f%%</span>`, name, value, name, value*100)
	}
	b.WriteString(`</p>`)
	return b.String()
}

func benchmarkRunAliases(benchmarkSlug, runSlug string) []string {
	if benchmarkSlug != defaultBenchmarkSlug {
		return nil
	}
	return []string{
		"/benchmark/runs/" + runSlug + "/",
		"/benchmarks/organizational-agent/runs/" + runSlug + "/",
	}
}

func renderBenchmarkCategorySVG(entry benchmarkEntry) string {
	if len(entry.CategoryRecall) == 0 {
		return ""
	}
	names := make([]string, 0, len(entry.CategoryRecall))
	for name := range entry.CategoryRecall {
		names = append(names, name)
	}
	sort.Strings(names)
	const width = 720
	rowHeight := 42
	height := 28 + rowHeight*len(names)
	var b strings.Builder
	fmt.Fprintf(&b, `<svg class="benchmark-category-chart" data-benchmark-category-chart="%s" viewBox="0 0 %d %d" role="img" aria-label="Full-pass recall by task family">`, entry.RunID, width, height)
	b.WriteString(`<style>.track{fill:var(--benchmark-track,#e7e5e4)}.bar{fill:var(--benchmark-accent,#0f766e)}.label,.value{fill:currentColor;font:600 14px system-ui,sans-serif}.value{text-anchor:end}</style>`)
	for index, name := range names {
		value := entry.CategoryRecall[name]
		y := 22 + index*rowHeight
		barWidth := 440 * value
		fmt.Fprintf(&b, `<g data-category="%s" data-recall="%.17g"><text class="label" x="0" y="%d">%s</text><rect class="track" x="170" y="%d" width="440" height="14" rx="7"/><rect class="bar" x="170" y="%d" width="%.2f" height="14" rx="7"/><text class="value" x="715" y="%d">%.1f%%</text></g>`, name, value, y, benchmarkCategoryLabel(name), y-12, y-12, barWidth, y, value*100)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func benchmarkCategoryLabel(name string) string {
	name = strings.ReplaceAll(name, "-", " ")
	parts := strings.Fields(name)
	for index := range parts {
		parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
	}
	return strings.Join(parts, " ")
}

var benchmarkSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func resolveBenchmarkIdentity(value string) (benchmarkIdentity, string, error) {
	slug := strings.ToLower(strings.TrimSpace(value))
	if slug == "" {
		slug = defaultBenchmarkSlug
	}
	if !benchmarkSlugPattern.MatchString(slug) {
		return benchmarkIdentity{}, "", fmt.Errorf("invalid benchmark slug %q; use lowercase letters, numbers, and single hyphens", value)
	}
	name := benchmarkName(slug)
	return benchmarkIdentity{Slug: slug, Name: name}, strings.ReplaceAll(slug, "-", "_"), nil
}

func benchmarkName(slug string) string {
	if slug == defaultBenchmarkSlug {
		return defaultBenchmarkName
	}
	parts := strings.Split(slug, "-")
	for i, part := range parts {
		if part != "" {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ") + " Benchmark"
}

func stripMarkdownReportTitle(markdown string) string {
	lines := strings.Split(markdown, "\n")
	if len(lines) > 0 && (strings.HasPrefix(lines[0], "# GraphJin evaluation report") || strings.HasPrefix(lines[0], "# GraphJin evaluation summary")) {
		lines = lines[1:]
		for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
			lines = lines[1:]
		}
	}
	return strings.Join(lines, "\n")
}

var benchmarkRunIDPattern = regexp.MustCompile(`(?i)^(\d{8})T(\d{6})(?:\.\d+)?Z-(.+)$`)

func benchmarkRunSlug(runID string) string {
	value := strings.ToLower(strings.TrimSpace(runID))
	if match := benchmarkRunIDPattern.FindStringSubmatch(runID); len(match) == 4 {
		value = strings.ToLower(match[1] + "t" + match[2] + "-" + match[3])
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
		} else if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func shortRevision(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

func sortedUniqueStrings(values []string) []string {
	sort.Strings(values)
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

func atomicWritePublicFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".graphjin-benchmark-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o644)
}
