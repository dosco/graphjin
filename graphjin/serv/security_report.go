package serv

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/featurecap"
	"github.com/dosco/graphjin/core/v3/sourcecap"
	"github.com/dosco/graphjin/serv/v3/internal/util"
	"github.com/spf13/viper"
)

const (
	modeDev     = "dev"
	modeProd    = "prod"
	modeAgentic = "agentic"

	securityKindSummary = "summary"
	securityKindPolicy  = "policy"
	securityKindFinding = "finding"

	securityScopeRuntime = "runtime"
	securityScopeConfig  = "config"

	securityStatusPass      = "pass"
	securityStatusFinding   = "finding"
	securityStatusInfo      = "info"
	securityStatusLoadError = "load_error"

	securityEffectiveAllow     = "allow"
	securityEffectiveBlock     = "block"
	securityEffectiveReadOnly  = "read_only"
	securityEffectiveReadWrite = "read_write"
)

type securityPolicyEval struct {
	ID               string
	Scope            string
	ConfigID         string
	ConfigName       string
	ConfigFile       string
	ConfigPath       string
	ConfigInherits   string
	ConfigActive     bool
	Layer            string
	Source           string
	SourceKind       string
	Surface          string
	Transport        string
	DatabaseName     string
	TableName        string
	ColumnName       string
	Role             string
	Audience         string
	Capability       string
	Action           string
	Title            string
	Summary          string
	Mode             string
	DefaultEffective string
	Effective        string
	DefaultAllowed   bool
	EffectiveAllowed bool
	OverrideKey      string
	OverrideValue    string
	OverrideExplicit bool
	OverrideSource   string
	WeakensDefault   bool
	ReadOnly         bool
	RiskSeverity     string
	Confidence       string
	Status           string
	Reason           string
	Recommendation   string
	Evidence         map[string]any
	Details          map[string]any
	Safety           map[string]any
}

type securityReportContext struct {
	Service        *graphjinService
	Conf           *Config
	Scope          string
	ConfigID       string
	ConfigName     string
	ConfigFile     string
	ConfigPath     string
	ConfigInherits string
	ConfigActive   bool
	Mode           string
	IDPrefix       string
	SummaryID      string
	Runtime        bool
	LoadErr        error
}

func securityNanoRows(s *graphjinService) []core.NanoRow {
	now := nowNanoTimestamp()
	contexts := securityReportContexts(s, now)

	rows := make([]core.NanoRow, 0, len(contexts)*24)
	for _, ctx := range contexts {
		if ctx.LoadErr != nil {
			rows = append(rows, securityConfigLoadErrorRow(ctx, now))
			continue
		}
		policies := securityPolicyEvaluationsForContext(ctx)
		policyRows := make([]core.NanoRow, 0, len(policies))
		for _, policy := range policies {
			policyRows = append(policyRows, securityPolicyNanoRow(policy, now))
		}
		findingRows := securityFindingNanoRows(ctx, policies, now)
		rows = append(rows, securitySummaryNanoRow(ctx, len(policyRows), findingRows, now))
		rows = append(rows, policyRows...)
		rows = append(rows, findingRows...)
		if ctx.Runtime {
			rows = append(rows, securityRuntimeInfoRows(ctx, now)...)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i]["id"] == "summary" {
			return true
		}
		if rows[j]["id"] == "summary" {
			return false
		}
		return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"])
	})
	return rows
}

func securityReportContexts(s *graphjinService, now string) []securityReportContext {
	ctxs := []securityReportContext{securityRuntimeContext(s, now)}
	ctxs = append(ctxs, securityConfigAuditContexts(s)...)
	return ctxs
}

func securityRuntimeContext(s *graphjinService, _ string) securityReportContext {
	var conf *Config
	if s != nil {
		conf = s.conf
	}
	mode := effectiveMode(conf)
	return securityReportContext{
		Service:      s,
		Conf:         conf,
		Scope:        securityScopeRuntime,
		ConfigID:     "active",
		ConfigName:   securityConfigDisplayName(conf, "active"),
		ConfigFile:   securityActiveConfigFile(conf),
		ConfigPath:   securityConfigPath(conf),
		ConfigActive: true,
		Mode:         mode,
		SummaryID:    "summary",
		Runtime:      true,
	}
}

func securityConfigAuditContexts(s *graphjinService) []securityReportContext {
	if s == nil || s.conf == nil {
		return nil
	}
	configPath := securityConfigPath(s.conf)
	files, err := securityConfigFileNames(s)
	if err != nil {
		return []securityReportContext{{
			Scope:      securityScopeConfig,
			ConfigID:   "config_scan",
			ConfigName: "config scan",
			ConfigPath: configPath,
			Mode:       effectiveMode(s.conf),
			IDPrefix:   "config:config_scan:",
			SummaryID:  "config:config_scan:summary",
			LoadErr:    err,
		}}
	}
	ctxs := make([]securityReportContext, 0, len(files))
	activeFile := filepath.Base(securityActiveConfigFile(s.conf))
	for _, file := range files {
		conf, inherited, err := readSecurityConfigFile(file, configPath, s.fs)
		id := securityConfigID(file)
		ctx := securityReportContext{
			Conf:           conf,
			Scope:          securityScopeConfig,
			ConfigID:       id,
			ConfigName:     securityConfigDisplayName(conf, id),
			ConfigFile:     file,
			ConfigPath:     configPath,
			ConfigInherits: inherited,
			ConfigActive:   activeFile != "" && strings.EqualFold(filepath.Base(file), activeFile),
			Mode:           effectiveMode(conf),
			IDPrefix:       "config:" + securityIDPart(id) + ":",
			SummaryID:      "config:" + securityIDPart(id) + ":summary",
			LoadErr:        err,
		}
		if err == nil {
			ctx.Mode = effectiveMode(conf)
			ctx.ConfigName = securityConfigDisplayName(conf, id)
		}
		ctxs = append(ctxs, ctx)
	}
	sort.SliceStable(ctxs, func(i, j int) bool {
		return ctxs[i].ConfigID < ctxs[j].ConfigID
	})
	return ctxs
}

func securityConfigFileNames(s *graphjinService) ([]string, error) {
	if s != nil && s.fs != nil {
		files, err := s.fs.List(".")
		if err != nil {
			return nil, err
		}
		return filterSecurityConfigFiles(files), nil
	}
	configPath := "."
	if s != nil && s.conf != nil {
		configPath = securityConfigPath(s.conf)
	}
	entries, err := os.ReadDir(configPath)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			files = append(files, entry.Name())
		}
	}
	return filterSecurityConfigFiles(files), nil
}

func filterSecurityConfigFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, file := range files {
		switch strings.ToLower(filepath.Ext(file)) {
		case ".yml", ".yaml", ".json", ".toml":
			out = append(out, file)
		}
	}
	sort.Strings(out)
	return out
}

func readSecurityConfigFile(file, configPath string, fs core.FS) (*Config, string, error) {
	if fs == nil {
		conf, err := ReadInConfig(filepath.Join(configPath, file))
		if err != nil {
			return nil, "", err
		}
		inherited := ""
		if conf != nil && conf.viper != nil {
			inherited = conf.viper.GetString("inherits")
		}
		if err := prepareSecurityAuditConfig(conf); err != nil {
			return conf, inherited, err
		}
		return conf, inherited, nil
	}

	v, inherited, err := readSecurityViperFromFS(file, fs)
	if err != nil {
		return nil, inherited, err
	}
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GJ_") || strings.HasPrefix(e, "SJ_") {
			kv := strings.SplitN(e, "=", 2)
			util.SetKeyValue(v, kv[0], kv[1])
		}
	}
	conf := &Config{viper: v}
	conf.ConfigPath = configPath
	if err := normalizeCatalogAutoBools(v); err != nil {
		return conf, inherited, err
	}
	if err := v.Unmarshal(conf, capabilityMapDecodeOption()); err != nil {
		return conf, inherited, fmt.Errorf("failed to decode config, %v", err)
	}
	conf.ConfigPath = configPath
	if err := prepareSecurityAuditConfig(conf); err != nil {
		return conf, inherited, err
	}
	return conf, inherited, nil
}

func readSecurityViperFromFS(file string, fs core.FS) (*viper.Viper, string, error) {
	data, err := fs.Get(file)
	if err != nil {
		return nil, "", err
	}
	child := newViperWithDefaults()
	child.SetConfigType(securityConfigType(file))
	if err := child.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, "", err
	}
	inherited := strings.TrimSpace(child.GetString("inherits"))
	if inherited == "" {
		return child, "", nil
	}

	parentData, err := fs.Get(inherited)
	if err != nil {
		return nil, inherited, err
	}
	parent := newViperWithDefaults()
	parent.SetConfigType(securityConfigType(inherited))
	if err := parent.ReadConfig(bytes.NewReader(parentData)); err != nil {
		return nil, inherited, err
	}
	if parentInherited := strings.TrimSpace(parent.GetString("inherits")); parentInherited != "" {
		return nil, inherited, fmt.Errorf("inherited config '%s' cannot itself inherit '%s'", inherited, parentInherited)
	}
	parent.SetConfigType(securityConfigType(file))
	if err := parent.MergeConfig(bytes.NewReader(data)); err != nil {
		return nil, inherited, err
	}
	return parent, inherited, nil
}

func securityConfigType(file string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(file)), ".")
	if ext == "yml" {
		return "yaml"
	}
	return ext
}

func prepareSecurityAuditConfig(conf *Config) error {
	if conf == nil {
		return nil
	}
	if err := validateServiceIsSourcesUsedConfig(conf); err != nil {
		return err
	}
	applySourceCapabilitySourceDefaults(conf)
	if err := normalizeServiceSources(conf); err != nil {
		return err
	}
	if conf.DBType == "" {
		conf.DBType = conf.DB.Type
	}
	if conf.Auth.Type == "" || conf.Auth.Type == "none" {
		conf.DefaultBlock = false
	}
	conf.Core.Production = conf.Serv.Production
	applySecurityDevMCPDefaults(conf)
	applySourceCapabilityMCPDefaults(conf)
	return nil
}

func applySecurityDevMCPDefaults(conf *Config) {
	if conf == nil || conf.Serv.Production || conf.mcpDisabled() || conf.viper == nil {
		return
	}
	defaults := map[string]*bool{
		"mcp.allow_raw_queries":        &conf.MCP.AllowRawQueries,
		"mcp.allow_mutations":          &conf.MCP.AllowMutations,
		"mcp.allow_config_updates":     &conf.MCP.AllowConfigUpdates,
		"mcp.allow_schema_reload":      &conf.MCP.AllowSchemaReload,
		"mcp.allow_schema_updates":     &conf.MCP.AllowSchemaUpdates,
		"mcp.allow_workflow_updates":   &conf.MCP.AllowWorkflowUpdates,
		"mcp.allow_workflow_execution": &conf.MCP.AllowWorkflowExecution,
		"mcp.allow_dev_tools":          &conf.MCP.AllowDevTools,
	}
	for key, target := range defaults {
		if !conf.viper.IsSet(key) {
			*target = true
		}
	}
}

func securityConfigID(file string) string {
	base := strings.TrimSpace(filepath.Base(file))
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return securityIDPart(base)
}

func securityConfigDisplayName(conf *Config, fallback string) string {
	if conf != nil && strings.TrimSpace(conf.Serv.AppName) != "" {
		return strings.TrimSpace(conf.Serv.AppName)
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "active"
}

func securityConfigPath(conf *Config) string {
	if conf == nil {
		return "."
	}
	if strings.TrimSpace(conf.Serv.ConfigPath) != "" {
		return strings.TrimSpace(conf.Serv.ConfigPath)
	}
	if strings.TrimSpace(conf.ConfigPath) != "" {
		return strings.TrimSpace(conf.ConfigPath)
	}
	return "."
}

func securityActiveConfigFile(conf *Config) string {
	if conf == nil || conf.viper == nil {
		return ""
	}
	if file := strings.TrimSpace(conf.viper.ConfigFileUsed()); file != "" {
		return file
	}
	return ""
}

func effectiveMode(conf *Config) string {
	if conf != nil {
		switch strings.ToLower(strings.TrimSpace(conf.Core.Mode)) {
		case "dev", "development":
			return modeDev
		case "prod", "production":
			return modeProd
		case "agent", "agentic":
			return modeAgentic
		}
		if conf.Serv.Production || conf.Core.Production {
			return modeProd
		}
	}
	return modeDev
}

func securityPolicyEvaluations(conf *Config, mode string) []securityPolicyEval {
	return securityPolicyEvaluationsForContext(securityReportContext{
		Conf:         conf,
		Scope:        securityScopeRuntime,
		ConfigID:     "active",
		ConfigName:   securityConfigDisplayName(conf, "active"),
		ConfigFile:   securityActiveConfigFile(conf),
		ConfigPath:   securityConfigPath(conf),
		ConfigActive: true,
		Mode:         mode,
		Runtime:      true,
	})
}

func securityPolicyEvaluationsForContext(ctx securityReportContext) []securityPolicyEval {
	conf := ctx.Conf
	mode := ctx.Mode
	if mode == "" {
		mode = effectiveMode(conf)
	}
	production := conf != nil && (conf.Serv.Production || conf.Core.Production)
	prodSecurity := production && conf != nil && !conf.Core.DisableProdSecurity
	mcpEnabled := conf != nil && !conf.mcpDisabled()
	systemControlPlane := conf != nil && conf.systemControlPlaneEnabled()
	workflowsEnabled := conf != nil && conf.workflowsEnabled()
	catalogEnabled := conf != nil && conf.catalogToolsEnabled()
	runtimeRegistered := conf != nil && conf.runtimeRootRegistered()
	configReadOnly := controlPlaneTableReadOnly(conf, "", "gj_config")
	workflowReadOnly := controlPlaneTableReadOnly(conf, "", "gj_workflow")
	workflowExecutionReadOnly := controlPlaneTableReadOnly(conf, "", "gj_workflow_execution")
	corsWildcard := securityHasWildcard(confAllowedOrigins(conf))
	uploadEnabled := conf != nil && conf.Serv.Uploads.Enabled
	watchWebhookPolicy := securityWatchWebhookPolicy(conf, mode)

	rows := []securityPolicyEval{
		newSecurityPolicy(mode, "core.app_data", "core", "app", "database", "app_data", "query",
			"Application data access",
			"Application data remains governed by table roles, allow-list behavior, and data-source read_only settings.",
			defaultAllow(mode, true, true, true), conf != nil,
			"databases/sources/roles", securityConfiguredDatabaseSummary(conf),
			false, "medium",
			"The app data plane is expected to stay available in all modes while production protections are enforced separately.",
			"Use table role filters/blocklists/read_only settings for app data restrictions."),
		newSecurityPolicy(mode, "core.dynamic_graphql", "core", "system", featurecap.KindSystem, "dynamic_graphql", "query",
			"Dynamic GraphQL queries",
			"Controls whether raw client GraphQL can compile outside the allow-list.",
			defaultAllow(mode, true, false, false), !prodSecurity,
			"disable_production_security", fmt.Sprint(conf != nil && conf.Core.DisableProdSecurity),
			configBoolExplicit(conf, "disable_production_security"), "critical",
			"Production and agentic deployments should enforce the allow-list for app data.",
			"Enable production mode and keep disable_production_security false."),
		newSecurityPolicy(mode, "core.anonymous_access", "core", "system", featurecap.KindSystem, "anonymous_access", "query",
			"Anonymous app access",
			"Controls whether unauthenticated requests can use the anonymous role by default.",
			defaultAllow(mode, true, false, false), conf != nil && !conf.DefaultBlock,
			"default_block", fmt.Sprint(conf != nil && conf.DefaultBlock),
			configBoolExplicit(conf, "default_block"), "high",
			"Prod and agentic deployments are intended for authenticated users; anonymous access broadens every exposed surface.",
			"Configure authentication and keep default_block true unless anonymous access is a deliberate public API requirement."),
		newSecurityPolicy(mode, "core.introspection", "core", "system", featurecap.KindSystem, "introspection", "read",
			"GraphQL introspection export",
			"Controls whether GraphJin writes an introspection JSON file at startup.",
			defaultAllow(mode, true, false, false), conf != nil && !production && conf.Core.EnableIntrospection,
			"enable_introspection", fmt.Sprint(conf != nil && conf.Core.EnableIntrospection),
			configBoolExplicit(conf, "enable_introspection"), "medium",
			"Introspection metadata can reveal schema shape and should stay off outside development.",
			"Keep enable_introspection disabled in prod and agentic modes."),
		newSecurityPolicy(mode, "serve.catalog_read", "serve", featurecap.KindSystem, featurecap.KindSystem, "catalog", "read",
			"Catalog read access",
			"Controls whether normal authenticated users can read gj_catalog for schema and workflow discovery.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyCatalogRead), securitySystemReadAllowed(conf, "gj_catalog", mode, "user", catalogEnabled),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyCatalogRead, "roles[user].tables.gj_catalog.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_catalog", featurecap.KindSystem, featurecap.KeyCatalogRead),
			securitySystemReadExplicit(conf, "user", "gj_catalog", featurecap.KindSystem, featurecap.KeyCatalogRead), "medium",
			"Catalog discovery is the public, read-only GraphJin discovery surface by default.",
			"Restrict gj_catalog only when schema and capability discovery must not be anonymous."),
		newSecurityPolicy(mode, "serve.catalog_read_anon", "serve", "system", featurecap.KindSystem, "catalog", "read",
			"Anonymous catalog read access",
			"Controls whether unauthenticated users can read gj_catalog.",
			true, securitySystemReadAllowed(conf, "gj_catalog", mode, "anon", catalogEnabled),
			"roles[anon].tables.gj_catalog.query.block", securityRoleQueryValue(conf, "anon", "gj_catalog"),
			securityRoleTableExplicit(conf, "anon", "gj_catalog"), "high",
			"Anonymous gj_catalog access exposes bounded schema, capability, and workflow-discovery metadata.",
			"Leave gj_catalog public for agent discovery, or explicitly set the root to authenticated/admin if discovery metadata is sensitive."),
		newSecurityPolicy(mode, "serve.security_read", "serve", featurecap.KindSystem, featurecap.KindSystem, "security_audit", "read",
			"Security audit read access",
			"Controls whether normal authenticated users can read detailed gj_security rows.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeySecurityRead), securitySystemReadAllowed(conf, "gj_security", mode, "user", catalogEnabled || systemControlPlane || workflowsEnabled),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeySecurityRead, "roles[user].tables.gj_security.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_security", featurecap.KindSystem, featurecap.KeySecurityRead),
			securitySystemReadExplicit(conf, "user", "gj_security", featurecap.KindSystem, featurecap.KeySecurityRead), "high",
			"Detailed findings expose audit evidence, config posture, and privileged recommendations.",
			"Grant gj_security read only through an explicit authenticated role or system feature capability; normal agentic users should discover safe actions through gj_catalog."),
		newSecurityPolicy(mode, "serve.config_read", "serve", featurecap.KindSystem, featurecap.KindSystem, "config", "read",
			"Config read access",
			"Controls whether normal authenticated users can read gj_config.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyConfigRead), securitySystemReadAllowed(conf, "gj_config", mode, "user", systemControlPlane),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyConfigRead, "roles[user].tables.gj_config.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_config", featurecap.KindSystem, featurecap.KeyConfigRead),
			securitySystemReadExplicit(conf, "user", "gj_config", featurecap.KindSystem, featurecap.KeyConfigRead), "critical",
			"Configuration rows can reveal database names, role rules, enabled tools, and redacted secret locations.",
			"Grant gj_config read only through an explicit authenticated role or system feature capability."),
		newSecurityPolicy(mode, "serve.runtime_read", "serve", featurecap.KindSystem, featurecap.KindSystem, "runtime", "read",
			"Runtime status read access",
			"Controls whether normal authenticated users can read compact gj_runtime status and recent redacted events.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyRuntimeRead), securitySystemReadAllowed(conf, "gj_runtime", mode, "user", runtimeRegistered),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyRuntimeRead, "roles[user].tables.gj_runtime.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_runtime", featurecap.KindSystem, featurecap.KeyRuntimeRead),
			securitySystemReadExplicit(conf, "user", "gj_runtime", featurecap.KindSystem, featurecap.KeyRuntimeRead), "medium",
			"gj_runtime is decision support for agentic clients; it exposes bounded operational summaries, not audit history.",
			"Keep gj_runtime open in dev for local inspection; restrict it to authenticated/admin callers outside development."),
		newSecurityPolicy(mode, "serve.runtime_read_anon", "serve", featurecap.KindSystem, featurecap.KindSystem, "runtime", "read",
			"Anonymous runtime status read access",
			"Controls whether unauthenticated users can read gj_runtime.",
			mode == modeDev, securitySystemReadAllowed(conf, "gj_runtime", mode, "anon", runtimeRegistered),
			"roles[anon].tables.gj_runtime.query.block", securityRoleQueryValue(conf, "anon", "gj_runtime"),
			securityRoleTableExplicit(conf, "anon", "gj_runtime"), "high",
			"Runtime state can reveal degraded infrastructure components.",
			"Anonymous gj_runtime is acceptable for local dev; keep it blocked in prod and agentic deployments."),
		newSecurityPolicy(mode, "serve.workflow_read", "serve", featurecap.KindWorkflows, featurecap.KindWorkflows, "workflow", "read",
			"Workflow definition read access",
			"Controls whether normal authenticated users can read gj_workflow definitions, including workflow code.",
			featureCapabilityDefault(mode, featurecap.KindWorkflows, featurecap.KeyWorkflowRead), securitySystemReadAllowed(conf, "gj_workflow", mode, "user", workflowsEnabled),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowRead, "roles[user].tables.gj_workflow.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_workflow", featurecap.KindWorkflows, featurecap.KeyWorkflowRead),
			securitySystemReadExplicit(conf, "user", "gj_workflow", featurecap.KindWorkflows, featurecap.KeyWorkflowRead), "high",
			"Workflow definitions include code and implementation details; agentic end users should execute approved workflows without reading code by default.",
			"Grant gj_workflow read only to authenticated users who are allowed to inspect workflow code; expose workflow capabilities through gj_catalog."),
		newSecurityPolicy(mode, "serve.workflow_execution_read", "serve", "workflows", featurecap.KindWorkflows, "workflow_execution", "read",
			"Workflow execution read access",
			"Controls whether normal authenticated users can query gj_workflow_execution as a read root.",
			defaultAllow(mode, false, false, false), securitySystemReadAllowed(conf, "gj_workflow_execution", mode, "user", workflowsEnabled),
			"roles[user].tables.gj_workflow_execution.query.block", securityRoleQueryValue(conf, "user", "gj_workflow_execution"),
			securityRoleTableExplicit(conf, "user", "gj_workflow_execution"), "medium",
			"Workflow execution is an insert-shaped action and does not store durable run history.",
			"Keep gj_workflow_execution query blocked; use insert mutations for approved execution."),
		newSecurityPolicy(mode, "serve.config_write", "serve", featurecap.KindSystem, featurecap.KindSystem, "config", "write",
			"Config writes",
			"Controls gj_config mutations that update GraphJin configuration.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyConfigWrite), systemControlPlane && !configReadOnly && securitySourceCapabilityOrFallback(conf, featurecap.KindSystem, featurecap.KeyConfigWrite, conf != nil && conf.MCP.AllowConfigUpdates),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyConfigWrite, "mcp.allow_config_updates"), securitySourceOrFallbackValue(conf, featurecap.KindSystem, featurecap.KeyConfigWrite, conf != nil && conf.MCP.AllowConfigUpdates),
			securitySourceOrFallbackExplicit(conf, featurecap.KindSystem, featurecap.KeyConfigWrite, mcpBoolExplicit(conf, "mcp.allow_config_updates", conf != nil && conf.MCP.AllowConfigUpdates)), "high",
			"Config writes can alter databases, roles, and control-plane permissions.",
			"Only enable config.write for trusted operators; leave it disabled when configuration mutation is not intended."),
		newSecurityPolicy(mode, "serve.workflow_write", "serve", featurecap.KindWorkflows, featurecap.KindWorkflows, "workflow", "write",
			"Workflow writes",
			"Controls gj_workflow mutations that create, update, or delete saved workflows.",
			featureCapabilityDefault(mode, featurecap.KindWorkflows, featurecap.KeyWorkflowWrite), workflowsEnabled && !workflowReadOnly && securitySourceCapabilityOrFallback(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowWrite, conf != nil && conf.MCP.AllowWorkflowUpdates),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowWrite, "mcp.allow_workflow_updates"), securitySourceOrFallbackValue(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowWrite, conf != nil && conf.MCP.AllowWorkflowUpdates),
			securitySourceOrFallbackExplicit(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowWrite, mcpBoolExplicit(conf, "mcp.allow_workflow_updates", conf != nil && conf.MCP.AllowWorkflowUpdates)), "high",
			"Workflow writes can persist code that runs inside GraphJin.",
			"Enable workflows.write only for trusted agents and review saved workflow code."),
		newSecurityPolicy(mode, "serve.workflow_execute", "serve", featurecap.KindWorkflows, featurecap.KindWorkflows, "workflow", "execute",
			"Workflow execution",
			"Controls authenticated-user gj_workflow_execution insert mutations that execute saved workflows.",
			featureCapabilityDefault(mode, featurecap.KindWorkflows, featurecap.KeyWorkflowExecute), securityWorkflowExecutionInsertAllowed(conf, mode, "user", workflowsEnabled, workflowExecutionReadOnly),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowExecute, "tables[].read_only"), securitySourceOrFallbackValue(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowExecute, !workflowExecutionReadOnly),
			securitySourceCapabilityExplicit(conf, featurecap.KindWorkflows, featurecap.KeyWorkflowExecute) || controlPlaneTableReadOnlyExplicit(conf, "", "gj_workflow_execution") || securityRoleTableExplicit(conf, "user", "gj_workflow_execution"), "high",
			"Workflow execution can run code and access configured data sources.",
			"Keep workflow execution limited to trusted deployments; disable workflows.execute to remove execution entirely."),
		newSecurityPolicy(mode, "serve.workflow_execute_anon", "serve", "workflows", featurecap.KindWorkflows, "workflow", "execute",
			"Anonymous workflow execution",
			"Controls unauthenticated gj_workflow_execution insert mutations.",
			defaultAllow(mode, true, false, false), securityWorkflowExecutionInsertAllowed(conf, mode, "anon", workflowsEnabled, workflowExecutionReadOnly),
			"roles[anon].tables.gj_workflow_execution.insert.block", securityRoleMutationValue(conf, "anon", "gj_workflow_execution", "insert"),
			securityRoleTableExplicit(conf, "anon", "gj_workflow_execution"), "critical",
			"Approved agentic workflow execution is for authenticated company users; anonymous execution can run code without accountability.",
			"Keep anon gj_workflow_execution insert blocked and authenticate agentic users."),
		newSecurityPolicy(mode, "serve.legacy_execute_workflow_tool", "serve", "system", featurecap.KindSystem, "legacy_workflow_execution", "execute",
			"Legacy workflow execution config",
			"mcp.allow_workflow_execution is retained for compatibility; MCP no longer registers execute_workflow. GraphQL gj_workflow_execution is controlled by the workflow feature capability and root access policy.",
			defaultAllow(mode, true, false, false), false,
			"mcp.allow_workflow_execution", fmt.Sprint(conf != nil && conf.MCP.AllowWorkflowExecution),
			mcpBoolExplicit(conf, "mcp.allow_workflow_execution", conf != nil && conf.MCP.AllowWorkflowExecution), "medium",
			"Legacy MCP execution has been removed; use catalog-discovered GraphQL control-plane mutations.",
			"Use gj_workflow_execution with authenticated users and read_only policy controls."),
		newSecurityPolicy(mode, "serve.schema_reload", "serve", featurecap.KindSystem, featurecap.KindSystem, "schema", "reload",
			"Schema reload",
			"Controls MCP schema reload operations.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeySchemaReload), mcpEnabled && securitySourceCapabilityOrFallback(conf, featurecap.KindSystem, featurecap.KeySchemaReload, conf != nil && conf.MCP.AllowSchemaReload),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeySchemaReload, "mcp.allow_schema_reload"), securitySourceOrFallbackValue(conf, featurecap.KindSystem, featurecap.KeySchemaReload, conf != nil && conf.MCP.AllowSchemaReload),
			securitySourceOrFallbackExplicit(conf, featurecap.KindSystem, featurecap.KeySchemaReload, mcpBoolExplicit(conf, "mcp.allow_schema_reload", conf != nil && conf.MCP.AllowSchemaReload)), "medium",
			"Schema reload changes discovery state and should be explicit outside development.",
			"Enable schema reload only when a trusted authenticated user or agent needs fresh metadata."),
		newSecurityPolicy(mode, "serve.schema_write", "serve", featurecap.KindSystem, featurecap.KindSystem, "schema", "write",
			"Schema writes",
			"Controls MCP schema update operations that can apply DDL.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeySchemaWrite), mcpEnabled && securitySourceCapabilityOrFallback(conf, featurecap.KindSystem, featurecap.KeySchemaWrite, conf != nil && conf.MCP.AllowSchemaUpdates),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeySchemaWrite, "mcp.allow_schema_updates"), securitySourceOrFallbackValue(conf, featurecap.KindSystem, featurecap.KeySchemaWrite, conf != nil && conf.MCP.AllowSchemaUpdates),
			securitySourceOrFallbackExplicit(conf, featurecap.KindSystem, featurecap.KeySchemaWrite, mcpBoolExplicit(conf, "mcp.allow_schema_updates", conf != nil && conf.MCP.AllowSchemaUpdates)), "high",
			"Schema writes can alter application databases.",
			"Only enable mcp.allow_schema_updates for trusted migration sessions and prefer preview before apply."),
		newSecurityPolicy(mode, "serve.dev_tools", "serve", featurecap.KindSystem, featurecap.KindSystem, "dev_tools", "read",
			"Development tools",
			"Controls advanced MCP tools that expose SQL, relationship graphs, and role permissions.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyDevToolsRead), mcpEnabled && securitySourceCapabilityOrFallback(conf, featurecap.KindSystem, featurecap.KeyDevToolsRead, conf != nil && conf.MCP.AllowDevTools),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyDevToolsRead, "mcp.allow_dev_tools"), securitySourceOrFallbackValue(conf, featurecap.KindSystem, featurecap.KeyDevToolsRead, conf != nil && conf.MCP.AllowDevTools),
			securitySourceOrFallbackExplicit(conf, featurecap.KindSystem, featurecap.KeyDevToolsRead, mcpBoolExplicit(conf, "mcp.allow_dev_tools", conf != nil && conf.MCP.AllowDevTools)), "medium",
			"Development tools may reveal operational details useful for debugging and auditing.",
			"Keep dev tools disabled in prod unless a trusted audit workflow needs them."),
		newSecurityPolicy(mode, "serve.raw_queries", "serve", featurecap.KindSystem, featurecap.KindSystem, "raw_graphql", "query",
			"MCP raw GraphQL queries",
			"Controls compatibility MCP tools that submit arbitrary GraphQL text.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyRawGraphQLQuery), mcpEnabled && securitySourceCapabilityOrFallback(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLQuery, conf != nil && conf.MCP.AllowRawQueries),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLQuery, "mcp.allow_raw_queries"), securitySourceOrFallbackValue(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLQuery, conf != nil && conf.MCP.AllowRawQueries),
			securitySourceOrFallbackExplicit(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLQuery, mcpBoolExplicit(conf, "mcp.allow_raw_queries", conf != nil && conf.MCP.AllowRawQueries)), "medium",
			"Raw GraphQL should not bypass production allow-list expectations.",
			"Prefer catalog-guided saved workflows or production allow-listed operations."),
		newSecurityPolicy(mode, "serve.raw_mutations", "serve", featurecap.KindSystem, featurecap.KindSystem, "raw_graphql", "mutate",
			"MCP raw GraphQL mutations",
			"Controls compatibility MCP tools that can submit arbitrary GraphQL mutations.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyRawGraphQLMutate), mcpEnabled && securitySourceCapabilityOrFallback(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLMutate, conf != nil && conf.MCP.AllowMutations),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLMutate, "mcp.allow_mutations"), securitySourceOrFallbackValue(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLMutate, conf != nil && conf.MCP.AllowMutations),
			securitySourceOrFallbackExplicit(conf, featurecap.KindSystem, featurecap.KeyRawGraphQLMutate, mcpBoolExplicit(conf, "mcp.allow_mutations", conf != nil && conf.MCP.AllowMutations)), "high",
			"Raw mutations can bypass the intended workflow/catalog action path.",
			"Disable raw MCP mutations outside dev; expose approved mutations as saved operations or workflows."),
		newSecurityPolicy(mode, "serve.legacy_discovery", "serve", featurecap.KindSystem, featurecap.KindSystem, "legacy_discovery", "read",
			"Legacy MCP discovery",
			"Controls older MCP discovery tools and legacy helper endpoints.",
			featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyLegacyDiscoveryRead), mcpEnabled && conf != nil && conf.legacyMCPToolsEnabled(),
			securitySourceCapabilityOverrideKey(conf, featurecap.KindSystem, featurecap.KeyLegacyDiscoveryRead, "mcp.legacy_discovery"), securitySourceOrFallbackValue(conf, featurecap.KindSystem, featurecap.KeyLegacyDiscoveryRead, conf != nil && conf.MCP.LegacyDiscovery),
			securitySourceOrFallbackExplicit(conf, featurecap.KindSystem, featurecap.KeyLegacyDiscoveryRead, mcpBoolExplicit(conf, "mcp.legacy_discovery", conf != nil && conf.MCP.LegacyDiscovery)), "medium",
			"Legacy discovery can expose broader schema and helper surfaces than the catalog-first path.",
			"Prefer gj_catalog for discovery and enable legacy discovery only for compatible clients."),
		newSecurityPolicy(mode, "serve.web_ui", "serve", "system", featurecap.KindSystem, "admin_ui", "read",
			"Web UI",
			"Controls the built-in GraphJin Console HTTP surface.",
			defaultAllow(mode, true, false, true), conf != nil && conf.Serv.WebUI,
			"web_ui", fmt.Sprint(conf != nil && conf.Serv.WebUI),
			configBoolExplicit(conf, "web_ui"), "medium",
			"The console is expected in dev and governed agentic deployments; strict prod remains opt-in.",
			"Keep web_ui false in prod unless the console is intentionally exposed behind authentication."),
		newSecurityPolicy(mode, "serve.cors_wildcard", "serve", "system", featurecap.KindSystem, "cors", "allow",
			"Wildcard CORS origins",
			"Controls whether HTTP CORS allows every origin.",
			defaultAllow(mode, true, false, false), corsWildcard,
			"cors_allowed_origins", strings.Join(confAllowedOrigins(conf), ","),
			configBoolExplicit(conf, "cors_allowed_origins"), "medium",
			"Wildcard CORS increases browser-origin exposure for authenticated APIs.",
			"Set cors_allowed_origins to the exact application origins in prod and agentic deployments."),
		newSecurityPolicy(mode, "core.log_vars", "core", "system", featurecap.KindSystem, "request_variables", "log",
			"Variable logging",
			"Controls whether GraphQL variables are logged.",
			defaultAllow(mode, true, false, false), conf != nil && conf.Core.LogVars,
			"log_vars", fmt.Sprint(conf != nil && conf.Core.LogVars),
			configBoolExplicit(conf, "log_vars"), "high",
			"Variables can contain user data, tokens, or secrets.",
			"Keep log_vars false outside development and rely on structured redacted audit evidence."),
		newSecurityPolicy(mode, "serve.tracing", "serve", "system", featurecap.KindSystem, "tracing", "emit",
			"Request tracing",
			"Controls whether service request tracing is enabled.",
			defaultAllow(mode, true, true, true), conf != nil && conf.Serv.EnableTracing,
			"enable_tracing", fmt.Sprint(conf != nil && conf.Serv.EnableTracing),
			configBoolExplicit(conf, "enable_tracing"), "low",
			"Tracing is acceptable when exporters and payloads are configured to avoid sensitive data.",
			"Keep trace payloads minimal and avoid variable logging in production traces."),
		newSecurityPolicy(mode, "serve.uploads", "serve", "system", featurecap.KindSystem, "uploads", "write",
			"Multipart uploads",
			"Controls multipart GraphQL upload support.",
			defaultAllow(mode, true, false, false), uploadEnabled,
			"uploads.enabled", fmt.Sprint(uploadEnabled),
			configBoolExplicit(conf, "uploads.enabled"), "medium",
			"Uploads add file parsing, storage, and size-limit risk to the GraphQL endpoint.",
			"Enable uploads only with explicit max_size, allowed_mime, and a reviewed storage backend."),
		watchWebhookPolicy,
	}

	rows = append(rows, securitySourcePolicyEvaluations(conf, mode)...)
	rows = append(rows, securitySourceAccessPolicyEvaluations(conf, mode)...)
	rows = append(rows, securitySourceAccessSchemaFindings(ctx, mode)...)
	rows = append(rows, securityOpenAPIOperationPolicyEvaluations(ctx, mode)...)
	for i := range rows {
		securityApplyReportContext(&rows[i], ctx)
		rows[i].WeakensDefault = securityWeakensDefault(rows[i])
		rows[i].ReadOnly = rows[i].ReadOnly || rows[i].Effective == securityEffectiveReadOnly
		if rows[i].Status == "" {
			if rows[i].WeakensDefault {
				rows[i].Status = securityStatusFinding
			} else {
				rows[i].Status = securityStatusPass
			}
		}
		if rows[i].Confidence == "" {
			rows[i].Confidence = "high"
		}
		rows[i].Evidence = securityEvidence(rows[i], production, prodSecurity)
		rows[i].Details = securityPolicyDetails(rows[i])
		rows[i].Safety = map[string]any{
			"default_effective": rows[i].DefaultEffective,
			"effective":         rows[i].Effective,
			"weakens_default":   rows[i].WeakensDefault,
		}
	}
	return rows
}

func securityOpenAPIOperationPolicyEvaluations(ctx securityReportContext, mode string) []securityPolicyEval {
	if !ctx.Runtime || ctx.Service == nil || ctx.Service.gj == nil {
		return nil
	}
	md, err := ctx.Service.gj.MetadataSnapshot(ctx.Service.metadataSnapshotExcludes()...)
	if err != nil || md == nil {
		return nil
	}
	return securityOpenAPIOperationPolicies(ctx.Conf, mode, md.APIOperations)
}

func securityOpenAPIOperationPolicies(conf *Config, mode string, operations []core.MetadataAPIOperation) []securityPolicyEval {
	if conf == nil || len(operations) == 0 {
		return nil
	}
	rows := make([]securityPolicyEval, 0, len(operations))
	for _, op := range operations {
		method := strings.ToUpper(strings.TrimSpace(op.Method))
		capability, action := sourcecap.KeyAPIRead, sourcecap.ActionRead
		switch method {
		case "POST", "PUT", "PATCH":
			capability, action = sourcecap.KeyAPIWrite, sourcecap.ActionWrite
		case "DELETE":
			capability, action = sourcecap.KeyAPIDelete, sourcecap.ActionDelete
		}

		source, sourceFound := conf.Core.OpenAPISourceByName(op.SourceName)
		capabilityAllowed := false
		accessMode := core.AccessModeBlocked
		readOnlyBlocked := sourceFound && source.ReadOnly && action != sourcecap.ActionRead
		if sourceFound {
			capabilityAllowed, _ = conf.sourceCapabilityForSource(source, capability)
			access := conf.Core.EffectiveSourceAccess(source)
			switch action {
			case sourcecap.ActionWrite:
				accessMode = access.Write
			case sourcecap.ActionDelete:
				accessMode = access.Delete
			default:
				accessMode = access.Read
			}
		}
		rolesConfigured := action == sourcecap.ActionRead || len(op.AllowedRoles) != 0
		effectiveAllowed := op.Active && sourceFound && capabilityAllowed &&
			normalizeSourceAccessAllowed(accessMode) && !readOnlyBlocked && rolesConfigured
		defaultAllowed := action == sourcecap.ActionRead && op.Active && sourceCapabilityDefault(mode, sourcecap.KindAPI, capability)

		blockedBy := "caller_identity"
		switch {
		case !op.Active:
			blockedBy = "classification"
		case !sourceFound:
			blockedBy = "source"
		case readOnlyBlocked:
			blockedBy = "source.read_only"
		case !capabilityAllowed:
			blockedBy = "capability"
		case !normalizeSourceAccessAllowed(accessMode):
			blockedBy = "access"
		case !rolesConfigured:
			blockedBy = "allowed_roles"
		}

		id := "openapi.operation." + securityIDPart(op.SourceName) + "." + securityIDPart(op.SpecKey) + "." + securityIDPart(op.OperationID)
		overrideKey := fmt.Sprintf("sources[%s].specs[%s].operations[%s]", op.SourceName, op.SpecKey, op.OperationID)
		riskLevel := strings.TrimSpace(op.RiskLevel)
		if riskLevel == "" {
			riskLevel = securitySourceCapabilitySeverity(sourcecap.KindAPI, capability)
		}
		row := newSecurityPolicy(mode, id, "core", op.SourceName, sourcecap.KindAPI, capability, action,
			fmt.Sprintf("%s %s", method, op.OperationID),
			"Shows the effective runtime posture for one classified OpenAPI operation.",
			defaultAllowed, effectiveAllowed,
			overrideKey, fmt.Sprint(op.Active),
			action != sourcecap.ActionRead, riskLevel,
			"OpenAPI operations are gated by classification, owning-source capability, source access, read-only policy, and mutation role allowlists.",
			"Keep non-GET operations unexposed unless required, use narrow allowed_roles, and review the source capability and access mode.")
		// An operation is a discrete allow/block decision. Avoid the source-level
		// read_only/read_write rendering used for broad write capabilities.
		row.DefaultEffective = securityEffectiveBlock
		if defaultAllowed {
			row.DefaultEffective = securityEffectiveAllow
		}
		row.Effective = securityEffectiveBlock
		if effectiveAllowed {
			row.Effective = securityEffectiveAllow
		}
		row.TableName = op.RootName
		row.ReadOnly = readOnlyBlocked
		row.Details = map[string]any{
			"spec":                     op.SpecKey,
			"operation_id":             op.OperationID,
			"root":                     op.RootName,
			"method":                   method,
			"path":                     op.Path,
			"classification":           op.Mode,
			"active":                   op.Active,
			"skip_reason":              op.SkipReason,
			"blocked_by":               blockedBy,
			"access_mode":              accessMode,
			"capability_allowed":       capabilityAllowed,
			"allowed_roles":            op.AllowedRoles,
			"success_statuses":         op.SuccessStatuses,
			"retry_on_auth_failure":    op.RetryEnabled,
			"request_media_type":       op.RequestMediaType,
			"agent_read_only":          conf.Agent.ReadOnly,
			"mcp_mutations_enabled":    conf.MCP.AllowMutations,
			"cross_resource_atomicity": false,
		}
		rows = append(rows, row)
	}
	return rows
}

func securityWatchWebhookPolicy(conf *Config, mode string) securityPolicyEval {
	watchesEnabled := conf != nil && conf.Core.Watches.Enabled
	cfg := core.WatchesConfig{}
	if conf != nil {
		cfg = conf.Core.EffectiveWatchesConfig()
	}
	invalid := securityInvalidWatchWebhookAllowEntries(cfg.WebhookAllow)
	empty := len(cfg.WebhookAllow) == 0
	safe := !watchesEnabled || (!empty && len(invalid) == 0)
	row := newSecurityPolicy(mode, "serve.watch_webhook_egress", "serve", "system", featurecap.KindSystem, "watch_webhook", "egress",
		"Watch webhook egress",
		"Controls outbound HTTP delivery for watch events.",
		true, safe,
		"watches.webhook_allow", strings.Join(cfg.WebhookAllow, ","),
		watchesEnabled, "high",
		"Watch webhooks send event payloads to external HTTP destinations and must stay constrained to exact allowlisted origins.",
		"Set watches.webhook_allow to exact http(s) origins, including port when non-default; leave it empty only when webhook delivery is not used.")
	row.Transport = "http"
	row.Surface = "egress"
	row.Details = map[string]any{
		"watches_enabled": watchesEnabled,
		"allowlist":       cfg.WebhookAllow,
		"empty_allowlist": empty,
		"invalid_entries": invalid,
		"required_shape":  "http(s)://host[:port][/optional/path-prefix]",
	}
	if watchesEnabled && (empty || len(invalid) != 0) {
		row.Status = securityStatusFinding
	}
	return row
}

func securityInvalidWatchWebhookAllowEntries(allow []string) []string {
	var invalid []string
	for _, entry := range allow {
		raw := strings.TrimSpace(entry)
		if raw == "" {
			invalid = append(invalid, entry)
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || strings.TrimSpace(u.Host) == "" || u.User != nil ||
			strings.ContainsAny(u.Hostname(), "* ") {
			invalid = append(invalid, entry)
		}
	}
	return invalid
}

func securitySourcePolicyEvaluations(conf *Config, mode string) []securityPolicyEval {
	if conf == nil {
		return nil
	}
	var rows []securityPolicyEval
	for _, source := range conf.Core.Sources {
		kind := source.CanonicalKind()
		if kind == "" {
			continue
		}
		name := strings.TrimSpace(source.Name)
		if name == "" {
			name = kind
		}
		for _, def := range sourcecap.Definitions(kind) {
			defaultAllowed := def.Default(mode)
			configuredAllowed, explicit := conf.sourceCapabilityForSource(source, def.Key)
			effectiveAllowed := configuredAllowed
			readOnlyBlocked := sourceCapabilityReadOnlyBlocked(source, def.Key)
			if readOnlyBlocked {
				effectiveAllowed = false
			}
			action := def.Action
			id := "source." + securityIDPart(name) + "." + securityIDPart(def.Key)
			policy := newSecurityPolicy(mode, id, "core", name, kind, def.Key, action,
				fmt.Sprintf("%s %s", name, def.Key),
				def.Summary,
				defaultAllowed, effectiveAllowed,
				fmt.Sprintf("sources[%s].capabilities.%s", name, def.Key), fmt.Sprint(configuredAllowed),
				explicit, def.Severity,
				def.Reason,
				securitySourceCapabilityRecommendation(name, def))
			policy.ReadOnly = readOnlyBlocked
			if readOnlyBlocked {
				if action == "write" || action == "delete" || action == "watch" {
					policy.Effective = securityEffectiveReadOnly
				} else {
					policy.Effective = securityEffectiveBlock
				}
			}
			if !explicit {
				policy.OverrideValue = "default"
				policy.OverrideExplicit = false
				policy.OverrideSource = securityOverrideSource(false)
			}
			rows = append(rows, policy)
		}
	}
	if conf.Core.IsSourcesUsed() {
		for _, kind := range featurecap.Kinds() {
			for _, def := range featurecap.Definitions(kind) {
				effective := conf.effectiveFeatureCapability(kind, def.Key)
				value, explicit := conf.featureCapabilityConfigured(kind, def.Key)
				if !explicit {
					value = effective
				}
				policy := newSecurityPolicy(mode, "feature."+securityIDPart(kind)+"."+securityIDPart(def.Key), "core", kind, kind, def.Key, def.Action,
					fmt.Sprintf("%s %s", kind, def.Key), def.Summary,
					def.Default(mode), effective,
					fmt.Sprintf("%s.capabilities.%s", kind, def.Key), fmt.Sprint(value),
					explicit, def.Severity, def.Reason, def.Recommendation)
				policy.Details = map[string]any{"provenance": "mode_default"}
				if explicit {
					policy.Details["provenance"] = "config_override"
				}
				rows = append(rows, policy)
			}
		}
		return rows
	}
	for name, dbConf := range conf.Core.Databases {
		if !strings.EqualFold(dbConf.Type, "codesql") {
			continue
		}
		id := "database." + securityIDPart(name) + ".write"
		rows = append(rows, newSecurityPolicy(mode, id, "core", name, sourcecap.KindCode, sourcecap.KeyCodeWrite, sourcecap.ActionWrite,
			fmt.Sprintf("%s CodeSQL writes", name),
			"Controls write/watch behavior for the CodeSQL database.",
			defaultAllow(mode, true, false, false), !dbConf.ReadOnly,
			"databases[].read_only", fmt.Sprint(dbConf.ReadOnly),
			true, "high",
			"CodeSQL indexes source code and should be read-only outside development unless a trusted agent is editing code.",
			"Set read_only: true on production and agentic CodeSQL databases unless source mutation is explicitly needed."))
	}
	return rows
}

func securitySourceAccessPolicyEvaluations(conf *Config, mode string) []securityPolicyEval {
	if conf == nil || !conf.Core.IsSourcesUsed() {
		return nil
	}
	identity := conf.Core.EffectiveIdentityConfig()
	rows := []securityPolicyEval{
		newSecurityPolicy(mode, "system_access.identity", "core", featurecap.KindSystem, featurecap.KindSystem, "identity", "read",
			"Request identity",
			"Shows request-wide identity claim names used for generated source access rules.",
			true, true,
			"identity", "configured",
			true, "medium",
			"Identity must be request-wide, not source-specific, so all generated access rules resolve the same user/account context.",
			"Keep JWT claims minimal and add identity.query only when the database enrichment is needed."),
	}
	rows[0].Details = map[string]any{
		"user_id_claim":          identity.UserIDClaim,
		"role_claims":            identity.RoleClaims,
		"namespace_claim":        identity.NamespaceClaim,
		"admin_roles":            identity.AdminRoles,
		"identity_query_enabled": strings.TrimSpace(identity.Query) != "",
	}

	for _, source := range conf.Core.Sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			continue
		}
		kind := source.CanonicalKind()
		access := conf.Core.EffectiveSourceAccess(source)
		if kind == sourcecap.KindDatabase {
			rows = append(rows, sourceAccessDefaultPolicy(mode, name, kind, "read", access.Read, true))
			rows = append(rows, sourceAccessDefaultPolicy(mode, name, kind, "write", access.Write, sourceWriteModeSafe(access.Write)))
			rows = append(rows, sourceAccessDefaultPolicy(mode, name, kind, "delete", access.Delete, sourceDeleteModeSafe(mode, access.Delete)))
			rows[len(rows)-3].Details = sourceAccessDetails(access)
			rows[len(rows)-2].Details = sourceAccessDetails(access)
			rows[len(rows)-1].Details = sourceAccessDetails(access)
			rows = append(rows, sourceAccessClassificationPolicies(mode, name, kind, access)...)
		} else if kind == sourcecap.KindAPI {
			start := len(rows)
			rows = append(rows, sourceAccessDefaultPolicy(mode, name, kind, "read", access.Read, true))
			rows = append(rows, sourceAccessDefaultPolicy(mode, name, kind, "write", access.Write, !source.ReadOnly))
			rows = append(rows, sourceAccessDefaultPolicy(mode, name, kind, "delete", access.Delete, !source.ReadOnly))
			for i, capability := range []string{sourcecap.KeyAPIRead, sourcecap.KeyAPIWrite, sourcecap.KeyAPIDelete} {
				details := sourceAccessDetails(access)
				enabled, explicit := conf.sourceCapabilityForSource(source, capability)
				details["capability"] = capability
				details["capability_enabled"] = enabled
				details["capability_explicit"] = explicit
				details["source_read_only"] = source.ReadOnly
				if capability != sourcecap.KeyAPIRead && source.ReadOnly {
					details["blocked_by"] = "sources[].read_only"
				} else if !enabled {
					details["blocked_by"] = "sources[].capabilities." + capability
				} else {
					details["blocked_by"] = ""
				}
				rows[start+i].Details = details
			}
		}
	}
	for root, accessMode := range conf.Core.EffectiveSystemRootAccess() {
		safe := systemRootAccessSafe(root, accessMode)
		row := newSecurityPolicy(mode, "system_access.root."+securityIDPart(root), "core", featurecap.KindSystem, featurecap.KindSystem, root, "read",
			fmt.Sprintf("%s root access", root),
			"Shows caller access policy for a built-in GraphJin system root.",
			safe, normalizeSourceAccessAllowed(accessMode),
			fmt.Sprintf("system.root_access.%s", root), accessMode,
			true, systemRootRisk(root),
			"Sensitive gj_* roots should be admin-only; account-scoped roots must enforce account context in their handler.",
			"Set sensitive roots such as gj_security, gj_runtime, and gj_config to admin.")
		row.TableName = root
		provenance := "mode_default"
		if _, explicit := conf.Core.System.RootAccess[root]; explicit {
			provenance = "config_override"
		}
		row.Details = map[string]any{"access_mode": accessMode, "admin_roles": identity.AdminRoles, "provenance": provenance}
		rows = append(rows, row)
	}
	if conf.Core.Artifacts.Enabled {
		cfg := conf.Core.EffectiveArtifactsConfig()
		source, ok := conf.Core.SourceByName(cfg.Source)
		validSource := ok && source.CanonicalKind() == sourcecap.KindDatabase && !source.ReadOnly && artifactSecuritySourceSQL(source)
		row := newSecurityPolicy(mode, "source_access.artifacts", "core", cfg.Source, "database", "gj_artifacts", "write",
			"Artifact store",
			"Shows the durable SQL source used by gj_artifacts and whether auto-init is enabled.",
			true, validSource,
			"artifacts", cfg.Source,
			true, "high",
			"gj_artifacts must be backed by a writable SQL database source; config-folder globals stay read-only.",
			"Use a writable database source for artifacts and keep globals_path read-only through configuration review.")
		row.TableName = artifactsRootTable
		row.Details = map[string]any{
			"enabled":      cfg.Enabled,
			"source":       cfg.Source,
			"schema":       cfg.Schema,
			"auto_init":    cfg.AutoInitEnabled(),
			"globals_path": cfg.GlobalsPath,
			"valid_source": validSource,
		}
		rows = append(rows, row)
	}
	return rows
}

func artifactSecuritySourceSQL(source core.SourceConfig) bool {
	switch strings.ToLower(strings.TrimSpace(source.Type)) {
	case "mongodb", "nanodb":
		return false
	default:
		return true
	}
}

func sourceAccessDefaultPolicy(mode, source, kind, action, accessMode string, safe bool) securityPolicyEval {
	effectiveAllowed := normalizeSourceAccessAllowed(accessMode)
	defaultAllowed := effectiveAllowed
	if action == "write" || action == "delete" {
		defaultAllowed = effectiveAllowed && safe
	}
	row := newSecurityPolicy(mode, "source_access."+securityIDPart(source)+"."+action, "core", source, kind, "access."+action, action,
		fmt.Sprintf("%s default %s access", source, action),
		"Shows source-level access default compiled into generated role table rules.",
		defaultAllowed, effectiveAllowed,
		fmt.Sprintf("sources[%s].access.%s", source, action), accessMode,
		true, sourceAccessRisk(action),
		"Source-mode access defaults are generated into the existing qcode role enforcement path.",
		"Use account or owner for row-scoped reads, keep writes explicit, and keep delete blocked outside development.")
	row.Details = map[string]any{"access_mode": accessMode}
	return row
}

func sourceAccessClassificationPolicies(mode, source, kind string, access core.SourceAccessConfig) []securityPolicyEval {
	var rows []securityPolicyEval
	add := func(name string, tables []string, accessMode, summary string) {
		if len(tables) == 0 {
			return
		}
		row := newSecurityPolicy(mode, "source_access."+securityIDPart(source)+"."+name, "core", source, kind, "classification."+name, "read",
			fmt.Sprintf("%s %s tables", source, name),
			summary,
			true, true,
			fmt.Sprintf("sources[%s].access.%s_tables", source, name), strings.Join(tables, ","),
			true, "medium",
			"Table classifications are exceptions to source defaults and are compiled into generated role rules.",
			"Keep public lists to immutable reference data, admin lists to audit/control data, and blocked lists to internal-only tables.")
		row.Details = map[string]any{"tables": tables, "access_mode": accessMode}
		rows = append(rows, row)
	}
	add("public", access.PublicTables, core.AccessModePublic, "Read-only shared/reference tables with no account filter.")
	add("admin", access.AdminTables, core.AccessModeAdmin, "Read-only admin tables.")
	add("blocked", access.BlockedTables, core.AccessModeBlocked, "Fully blocked tables hidden from normal discovery.")
	return rows
}

func securitySourceAccessSchemaFindings(ctx securityReportContext, mode string) []securityPolicyEval {
	if !ctx.Runtime || ctx.Service == nil || ctx.Service.gj == nil || ctx.Conf == nil || !ctx.Conf.Core.IsSourcesUsed() {
		return nil
	}
	snapshot, err := ctx.Service.gj.MetadataSnapshot(ctx.Service.metadataSnapshotExcludesFor(ctx.Service.metadataDB, &ctx.Conf.Core, ctx.Service.managedDBs)...)
	if err != nil || snapshot == nil {
		return nil
	}

	columns := make(map[string]map[string]struct{}, len(snapshot.Tables))
	for _, col := range snapshot.Columns {
		key := sourceAccessMetadataTableKey(col.DatabaseName, col.SchemaName, col.TableName)
		if columns[key] == nil {
			columns[key] = make(map[string]struct{})
		}
		columns[key][strings.ToLower(strings.TrimSpace(col.ColumnName))] = struct{}{}
	}

	var rows []securityPolicyEval
	for _, source := range ctx.Conf.Core.Sources {
		name := strings.TrimSpace(source.Name)
		if name == "" || source.CanonicalKind() != sourcecap.KindDatabase {
			continue
		}
		access := ctx.Conf.Core.EffectiveSourceAccess(source)
		for _, table := range snapshot.Tables {
			if table.DatabaseName != name || table.Type == "remote" || table.Type == "managed" {
				continue
			}
			if sourceAccessMetadataTableListed(access.PublicTables, table) ||
				sourceAccessMetadataTableListed(access.AdminTables, table) ||
				sourceAccessMetadataTableListed(access.BlockedTables, table) ||
				sourceAccessMetadataIsArtifactPhysicalTable(ctx.Conf, source, table) {
				continue
			}
			actions := sourceAccessNamespaceActions(access)
			if len(actions) == 0 {
				continue
			}
			namespaceColumn := strings.ToLower(strings.TrimSpace(access.NamespaceColumn))
			if namespaceColumn == "" {
				namespaceColumn = "account_id"
			}
			if _, ok := columns[sourceAccessMetadataTableKey(table.DatabaseName, table.SchemaName, table.TableName)][namespaceColumn]; ok {
				continue
			}

			effectiveAllowed := !strings.EqualFold(strings.TrimSpace(access.MissingNamespaceColumn), core.MissingNamespaceBlock)
			row := newSecurityPolicy(mode,
				"source_access."+securityIDPart(name)+"."+securityIDPart(table.TableName)+".missing_namespace",
				"core", name, sourcecap.KindDatabase, "access.namespace_column", "read",
				"Account access table missing namespace column",
				"Account-scoped source access needs a namespace column on every unclassified table it protects.",
				false, effectiveAllowed,
				fmt.Sprintf("sources[%s].access.namespace_column", name), access.NamespaceColumn,
				true, "high",
				"Generated account filters cannot safely scope this table because the configured namespace column is not present.",
				"Add the namespace column, classify the table as public/admin/blocked, or switch the source/table access mode.")
			row.Status = securityStatusFinding
			row.DatabaseName = table.DatabaseName
			row.TableName = table.TableName
			row.ColumnName = access.NamespaceColumn
			row.Details = map[string]any{
				"source":                   name,
				"database_name":            table.DatabaseName,
				"schema_name":              table.SchemaName,
				"table_name":               table.TableName,
				"namespace_column":         access.NamespaceColumn,
				"missing_namespace_column": access.MissingNamespaceColumn,
				"actions":                  actions,
				"effective_behavior":       sourceAccessMissingNamespaceBehavior(access),
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func sourceAccessNamespaceActions(access core.SourceAccessConfig) []string {
	var actions []string
	if strings.EqualFold(strings.TrimSpace(access.Read), core.AccessModeAccount) {
		actions = append(actions, "read")
	}
	if strings.EqualFold(strings.TrimSpace(access.Write), core.AccessModeAccount) {
		actions = append(actions, "write")
	}
	if strings.EqualFold(strings.TrimSpace(access.Delete), core.AccessModeAccount) {
		actions = append(actions, "delete")
	}
	return actions
}

func sourceAccessMissingNamespaceBehavior(access core.SourceAccessConfig) string {
	if strings.EqualFold(strings.TrimSpace(access.MissingNamespaceColumn), core.MissingNamespaceBlock) {
		return "blocked"
	}
	return "allowed"
}

func sourceAccessMetadataTableKey(database, schema, table string) string {
	return strings.ToLower(strings.TrimSpace(database)) + ":" +
		strings.ToLower(strings.TrimSpace(schema)) + ":" +
		strings.ToLower(strings.TrimSpace(table))
}

func sourceAccessMetadataTableListed(list []string, table core.MetadataTable) bool {
	for _, item := range list {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(table.TableName))
		schemaName := strings.ToLower(strings.TrimSpace(table.SchemaName))
		databaseName := strings.ToLower(strings.TrimSpace(table.DatabaseName))
		if item == name ||
			item == schemaName+"."+name ||
			item == databaseName+":"+name ||
			item == databaseName+":"+schemaName+"."+name {
			return true
		}
	}
	return false
}

func sourceAccessMetadataIsArtifactPhysicalTable(conf *Config, source core.SourceConfig, table core.MetadataTable) bool {
	if conf == nil || !conf.Core.Artifacts.Enabled {
		return false
	}
	cfg := conf.Core.EffectiveArtifactsConfig()
	if cfg.Source != source.Name || table.DatabaseName != source.Name {
		return false
	}
	schema := strings.ToLower(strings.TrimSpace(cfg.Schema))
	tableName := strings.ToLower(strings.TrimSpace(table.TableName))
	schemaName := strings.ToLower(strings.TrimSpace(table.SchemaName))
	return (schemaName == schema && tableName == "artifacts") ||
		tableName == schema+"_artifacts"
}

func sourceAccessDetails(access core.SourceAccessConfig) map[string]any {
	return map[string]any{
		"read":                     access.Read,
		"write":                    access.Write,
		"delete":                   access.Delete,
		"namespace_column":         access.NamespaceColumn,
		"owner_column":             access.OwnerColumn,
		"missing_namespace_column": access.MissingNamespaceColumn,
		"public_tables":            access.PublicTables,
		"admin_tables":             access.AdminTables,
		"blocked_tables":           access.BlockedTables,
	}
}

func normalizeSourceAccessAllowed(accessMode string) bool {
	return !strings.EqualFold(strings.TrimSpace(accessMode), core.AccessModeBlocked)
}

func sourceWriteModeSafe(accessMode string) bool {
	return strings.EqualFold(strings.TrimSpace(accessMode), core.AccessModeBlocked) ||
		strings.EqualFold(strings.TrimSpace(accessMode), core.AccessModeAdmin)
}

func sourceDeleteModeSafe(mode, accessMode string) bool {
	return strings.EqualFold(strings.TrimSpace(accessMode), core.AccessModeBlocked) ||
		(mode == modeDev && !strings.EqualFold(strings.TrimSpace(accessMode), core.AccessModePublic))
}

func sourceAccessRisk(action string) string {
	switch action {
	case "delete":
		return "critical"
	case "write":
		return "high"
	default:
		return "medium"
	}
}

func systemRootAccessSafe(root, accessMode string) bool {
	root = strings.ToLower(strings.TrimSpace(root))
	accessMode = strings.ToLower(strings.TrimSpace(accessMode))
	switch root {
	case "gj_security", "gj_runtime", "gj_config":
		return accessMode == core.AccessModeAdmin || accessMode == core.AccessModeBlocked
	case "gj_artifacts", "gj_watch", "gj_watch_event", "gj_task", "gj_task_entry", "gj_workflow", "gj_workflow_execution":
		return accessMode == core.AccessModeOwner || accessMode == core.AccessModeAccount || accessMode == core.AccessModeAdmin || accessMode == core.AccessModeBlocked
	case "gj_catalog":
		return accessMode == core.AccessModeAuthenticated || accessMode == core.AccessModeAccount || accessMode == core.AccessModeAdmin || accessMode == core.AccessModePublic
	default:
		return accessMode != core.AccessModePublic
	}
}

func systemRootRisk(root string) string {
	switch strings.ToLower(strings.TrimSpace(root)) {
	case "gj_security", "gj_config":
		return "critical"
	case "gj_runtime", "gj_workflow", "gj_watch", "gj_watch_event", "gj_task", "gj_task_entry":
		return "high"
	default:
		return "medium"
	}
}

func securityActionForSourceCapability(capability string) string {
	for _, kind := range sourcecap.Kinds() {
		if def, ok := sourcecap.Lookup(kind, capability); ok {
			return def.Action
		}
	}
	switch {
	case strings.HasSuffix(capability, ".read"):
		return "read"
	case strings.HasSuffix(capability, ".write"):
		return "write"
	case strings.HasSuffix(capability, ".delete"):
		return "delete"
	case strings.HasSuffix(capability, ".watch"):
		return "watch"
	case strings.HasSuffix(capability, ".execute"):
		return "execute"
	case strings.HasSuffix(capability, ".reload"):
		return "reload"
	case strings.HasSuffix(capability, ".query"):
		return "query"
	case strings.HasSuffix(capability, ".mutate"):
		return "mutate"
	default:
		return "use"
	}
}

func sourceCapabilityReadOnlyBlocked(source core.SourceConfig, capability string) bool {
	if !source.ReadOnly {
		return false
	}
	if def, ok := sourcecap.Lookup(source.CanonicalKind(), capability); ok {
		return def.ReadOnlyBlocks
	}
	return false
}

func securitySourceCapabilitySeverity(kind, capability string) string {
	if def, ok := sourcecap.Lookup(kind, capability); ok {
		return def.Severity
	}
	if strings.HasSuffix(capability, ".write") || strings.HasSuffix(capability, ".delete") {
		return "high"
	}
	return "medium"
}

func securitySourceCapabilityReason(kind, capability string) string {
	if def, ok := sourcecap.Lookup(kind, capability); ok {
		return def.Reason
	}
	return "Source capabilities control authenticated user access to this source surface."
}

func securitySourceCapabilityRecommendation(sourceName string, def sourcecap.Definition) string {
	if strings.TrimSpace(def.Recommendation) == "" {
		return fmt.Sprintf("Review sources[%s].capabilities.%s and set the least permissive value that supports the deployment.", sourceName, def.Key)
	}
	return fmt.Sprintf("Set sources[%s].capabilities.%s: %s", sourceName, def.Key, def.Recommendation)
}

func newSecurityPolicy(mode, id, layer, source, sourceKind, capability, action, title, summary string,
	defaultAllowed, effectiveAllowed bool,
	overrideKey, overrideValue string,
	overrideExplicit bool,
	riskSeverity, reason, recommendation string,
) securityPolicyEval {
	defaultEffective := securityEffectiveBlock
	if defaultAllowed {
		defaultEffective = securityEffectiveAllow
	}
	effective := securityEffectiveBlock
	if effectiveAllowed {
		effective = securityEffectiveAllow
	}
	if action == "write" && capability != "config" && capability != "schema" && capability != "workflow" {
		if defaultAllowed {
			defaultEffective = securityEffectiveReadWrite
		} else {
			defaultEffective = securityEffectiveReadOnly
		}
		if effectiveAllowed {
			effective = securityEffectiveReadWrite
		} else {
			effective = securityEffectiveReadOnly
		}
	}
	return securityPolicyEval{
		ID:               "policy:" + id,
		Layer:            layer,
		Source:           source,
		SourceKind:       sourceKind,
		Surface:          securitySurfaceFor(sourceKind, capability),
		Transport:        securityTransportFor(sourceKind, capability),
		TableName:        securityTableFor(capability, action),
		Capability:       capability,
		Action:           action,
		Title:            title,
		Summary:          summary,
		Mode:             mode,
		DefaultEffective: defaultEffective,
		Effective:        effective,
		DefaultAllowed:   defaultAllowed,
		EffectiveAllowed: effectiveAllowed,
		OverrideKey:      overrideKey,
		OverrideValue:    overrideValue,
		OverrideExplicit: overrideExplicit,
		OverrideSource:   securityOverrideSource(overrideExplicit),
		RiskSeverity:     riskSeverity,
		Confidence:       "high",
		Reason:           reason,
		Recommendation:   recommendation,
	}
}

func securityApplyReportContext(row *securityPolicyEval, ctx securityReportContext) {
	if row == nil {
		return
	}
	if row.Scope == "" {
		row.Scope = ctx.Scope
	}
	if row.ConfigID == "" {
		row.ConfigID = ctx.ConfigID
	}
	if row.ConfigName == "" {
		row.ConfigName = ctx.ConfigName
	}
	if row.ConfigFile == "" {
		row.ConfigFile = ctx.ConfigFile
	}
	if row.ConfigPath == "" {
		row.ConfigPath = ctx.ConfigPath
	}
	if row.ConfigInherits == "" {
		row.ConfigInherits = ctx.ConfigInherits
	}
	row.ConfigActive = ctx.ConfigActive
	if row.Mode == "" {
		row.Mode = ctx.Mode
	}
	if row.Audience == "" {
		row.Audience = securityAudienceFor(row.Mode, row.Role)
	}
	if row.Role == "" && strings.HasPrefix(row.OverrideKey, "roles[") {
		row.Role = securityRoleFromOverrideKey(row.OverrideKey)
	}
	if row.Role == "" && strings.Contains(row.ID, "_anon") {
		row.Role = "anon"
		row.Audience = securityAudienceFor(row.Mode, row.Role)
	}
	if row.Role == "" && strings.HasPrefix(row.OverrideKey, "roles[user]") {
		row.Role = "user"
		row.Audience = securityAudienceFor(row.Mode, row.Role)
	}
	if ctx.IDPrefix != "" && !strings.HasPrefix(row.ID, ctx.IDPrefix) {
		row.ID = ctx.IDPrefix + row.ID
	}
}

func securityOverrideSource(explicit bool) string {
	if explicit {
		return "config"
	}
	return "secure_default"
}

func securityRoleFromOverrideKey(key string) string {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, "roles[") {
		return ""
	}
	key = strings.TrimPrefix(key, "roles[")
	idx := strings.Index(key, "]")
	if idx < 0 {
		return ""
	}
	return key[:idx]
}

func securityAudienceFor(mode, role string) string {
	if role == "anon" {
		return "anonymous"
	}
	switch mode {
	case modeAgentic:
		return "company_end_user"
	case modeProd:
		return "authenticated_user"
	default:
		return "developer"
	}
}

func securitySurfaceFor(sourceKind, capability string) string {
	switch sourceKind {
	case "code", "file", "api", "database":
		return sourceKind
	case "workflow":
		return "control_plane"
	case "graphjin":
		switch capability {
		case "anonymous_access":
			return "auth"
		case "admin_ui", "cors", "uploads", "tracing":
			return "http"
		case "request_variables":
			return "logging"
		case "raw_graphql", "schema", "dev_tools", "legacy_discovery", "legacy_workflow_execution":
			return "mcp"
		default:
			return "control_plane"
		}
	default:
		if capability != "" {
			return capability
		}
		return sourceKind
	}
}

func securityTransportFor(sourceKind, capability string) string {
	switch sourceKind {
	case "workflow":
		return "graphql"
	case "code", "file", "api", "database":
		return "source"
	case "graphjin":
		switch capability {
		case "raw_graphql", "schema", "dev_tools", "legacy_discovery", "legacy_workflow_execution":
			return "mcp"
		case "admin_ui", "cors", "uploads", "tracing", "anonymous_access":
			return "http"
		default:
			return "graphql"
		}
	default:
		if capability == "dynamic_graphql" || capability == "app_data" {
			return "graphql"
		}
		return ""
	}
}

func securityTableFor(capability, action string) string {
	switch capability {
	case "catalog":
		return "gj_catalog"
	case "security_audit":
		return "gj_security"
	case "config":
		return "gj_config"
	case "runtime":
		return "gj_runtime"
	case "workflow":
		if action == "execute" {
			return "gj_workflow_execution"
		}
		return "gj_workflow"
	case "workflow_execution":
		return "gj_workflow_execution"
	}
	return ""
}

func defaultAllow(mode string, dev, prod, agentic bool) bool {
	switch mode {
	case modeProd:
		return prod
	case modeAgentic:
		return agentic
	default:
		return dev
	}
}

func mcpBoolExplicit(conf *Config, key string, value bool) bool {
	return conf != nil && conf.settingExplicit(key)
}

func configBoolExplicit(conf *Config, key string) bool {
	if key == "web_ui" {
		return conf != nil && conf.webUIExplicit
	}
	return conf != nil && conf.settingExplicit(key)
}

func securitySourceCapabilityOverrideKey(conf *Config, kind, capability, fallback string) string {
	if conf == nil {
		return fallback
	}
	if _, explicit := conf.featureCapabilityConfigured(kind, capability); explicit {
		return fmt.Sprintf("%s.capabilities.%s", kind, capability)
	}
	return fallback
}

func securitySourceCapabilityExplicit(conf *Config, kind, capability string) bool {
	_, explicit := conf.featureCapabilityConfigured(kind, capability)
	return explicit
}

func securitySystemReadOverrideValue(conf *Config, role, table, kind, capability string) string {
	if securityRoleTableExplicit(conf, role, table) {
		return securityRoleQueryValue(conf, role, table)
	}
	if value, explicit := conf.featureCapabilityConfigured(kind, capability); explicit {
		return fmt.Sprint(value)
	}
	return "default"
}

func securitySystemReadExplicit(conf *Config, role, table, kind, capability string) bool {
	return securityRoleTableExplicit(conf, role, table) || securitySourceCapabilityExplicit(conf, kind, capability)
}

func securitySourceCapabilityOrFallback(conf *Config, kind, capability string, fallback bool) bool {
	if conf == nil {
		return false
	}
	if value, explicit := conf.featureCapabilityConfigured(kind, capability); explicit {
		return value
	}
	if conf.Core.IsSourcesUsed() {
		return conf.effectiveFeatureCapability(kind, capability)
	}
	return fallback
}

func securitySourceOrFallbackValue(conf *Config, kind, capability string, fallback bool) string {
	if value, explicit := conf.featureCapabilityConfigured(kind, capability); explicit {
		return fmt.Sprint(value)
	}
	return fmt.Sprint(fallback)
}

func securitySourceOrFallbackExplicit(conf *Config, kind, capability string, fallbackExplicit bool) bool {
	return securitySourceCapabilityExplicit(conf, kind, capability) || fallbackExplicit
}

func securitySystemReadAllowed(conf *Config, table, mode, role string, sourceEnabled bool) bool {
	return sourceEnabled && systemRootAllowed(conf, table, systemActionRead, role)
}

func defaultSystemReadAllowed(mode, role, table string) bool {
	if role != "user" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_catalog":
		return featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyCatalogRead)
	case "gj_security":
		return featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeySecurityRead)
	case "gj_config":
		return featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyConfigRead)
	case "gj_runtime":
		return featureCapabilityDefault(mode, featurecap.KindSystem, featurecap.KeyRuntimeRead)
	case "gj_workflow":
		return featureCapabilityDefault(mode, featurecap.KindWorkflows, featurecap.KeyWorkflowRead)
	default:
		return false
	}
}

func securityWorkflowExecutionInsertAllowed(conf *Config, mode, role string, workflowsEnabled, tableReadOnly bool) bool {
	if !workflowsEnabled || tableReadOnly || conf == nil {
		return false
	}
	return systemRootAllowed(conf, "gj_workflow_execution", systemActionInsert, role)
}

func securityRoleTableExplicit(conf *Config, role, table string) bool {
	_, ok := securityRoleTable(conf, role, table)
	return ok
}

func securityRoleTable(conf *Config, role, table string) (core.RoleTable, bool) {
	if conf == nil {
		return core.RoleTable{}, false
	}
	database := securitySystemDatabase(conf)
	for _, r := range conf.Core.Roles {
		if !strings.EqualFold(r.Name, role) {
			continue
		}
		for _, rt := range r.Tables {
			if rt.Database != "" && database != "" && !strings.EqualFold(rt.Database, database) {
				continue
			}
			if strings.EqualFold(rt.Name, table) || strings.EqualFold(rt.Schema+"."+rt.Name, table) {
				return rt, true
			}
		}
	}
	return core.RoleTable{}, false
}

func securitySystemDatabase(conf *Config) string {
	return ""
}

func securityRoleQueryValue(conf *Config, role, table string) string {
	rt, ok := securityRoleTable(conf, role, table)
	if !ok {
		return "default"
	}
	if rt.Query != nil && rt.Query.Block {
		return "block"
	}
	return "allow"
}

func securityRoleMutationValue(conf *Config, role, table, action string) string {
	rt, ok := securityRoleTable(conf, role, table)
	if !ok {
		return "default"
	}
	blocked := false
	switch action {
	case "insert":
		blocked = rt.Insert != nil && rt.Insert.Block
	case "update":
		blocked = rt.Update != nil && rt.Update.Block
	case "upsert":
		blocked = rt.Upsert != nil && rt.Upsert.Block
	case "delete":
		blocked = rt.Delete != nil && rt.Delete.Block
	}
	if blocked {
		return "block"
	}
	return "allow"
}

func confAllowedOrigins(conf *Config) []string {
	if conf == nil {
		return nil
	}
	return conf.Serv.AllowedOrigins
}

func securityHasWildcard(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "*" {
			return true
		}
	}
	return false
}

func securityConfiguredDatabaseSummary(conf *Config) string {
	if conf == nil {
		return "none"
	}
	if conf.Core.IsSourcesUsed() {
		return fmt.Sprintf("%d sources", len(conf.Core.Sources))
	}
	if len(conf.Core.Databases) != 0 {
		return fmt.Sprintf("%d databases", len(conf.Core.Databases))
	}
	if conf.DBType != "" || conf.DB.Type != "" {
		return "legacy database"
	}
	return "none"
}

func securityWeakensDefault(row securityPolicyEval) bool {
	if !row.DefaultAllowed && row.EffectiveAllowed {
		return true
	}
	return row.DefaultEffective == securityEffectiveReadOnly && row.Effective == securityEffectiveReadWrite
}

func securityEvidence(row securityPolicyEval, production, prodSecurity bool) map[string]any {
	return map[string]any{
		"scope":             row.Scope,
		"config_id":         row.ConfigID,
		"config_name":       row.ConfigName,
		"config_file":       row.ConfigFile,
		"config_path":       row.ConfigPath,
		"config_inherits":   row.ConfigInherits,
		"config_active":     row.ConfigActive,
		"mode":              row.Mode,
		"layer":             row.Layer,
		"surface":           row.Surface,
		"transport":         row.Transport,
		"database_name":     row.DatabaseName,
		"source":            row.Source,
		"source_kind":       row.SourceKind,
		"table_name":        row.TableName,
		"column_name":       row.ColumnName,
		"role":              row.Role,
		"audience":          row.Audience,
		"capability":        row.Capability,
		"action":            row.Action,
		"enforcement":       securityCapabilityEnforcement(row),
		"production":        production,
		"production_secure": prodSecurity,
		"override_key":      row.OverrideKey,
		"override_value":    row.OverrideValue,
		"override_explicit": row.OverrideExplicit,
		"override_source":   row.OverrideSource,
		"read_only":         row.ReadOnly,
		"status":            row.Status,
	}
}

func securityPolicyDetails(row securityPolicyEval) map[string]any {
	out := map[string]any{
		"title":              row.Title,
		"summary":            row.Summary,
		"mode_definition":    modeDefinition(row.Mode),
		"matrix_expectation": row.DefaultEffective,
		"default_effective":  row.DefaultEffective,
		"effective":          row.Effective,
		"evaluated_condition": map[string]any{
			"capability":  row.Capability,
			"action":      row.Action,
			"role":        row.Role,
			"surface":     row.Surface,
			"enforcement": securityCapabilityEnforcement(row),
		},
		"reason":         row.Reason,
		"recommendation": row.Recommendation,
	}
	for k, v := range row.Details {
		out[k] = v
	}
	return out
}

func securityCapabilityEnforcement(row securityPolicyEval) string {
	if def, ok := sourcecap.Lookup(row.SourceKind, row.Capability); ok {
		return def.Enforcement
	}
	if def, ok := featurecap.Lookup(row.SourceKind, row.Capability); ok {
		return def.Enforcement
	}
	switch row.SourceKind {
	case featurecap.KindSystem, featurecap.KindWorkflows:
		return "runtime"
	case "code", "file", "api":
		return "config_audit"
	case "database":
		return "existing_policy"
	default:
		if row.Layer == "runtime" {
			return "runtime"
		}
		return "config_audit"
	}
}

func modeDefinition(mode string) string {
	switch mode {
	case modeProd:
		return "Strict production: allow-list/security defaults are enforced and system discovery/control-plane surfaces are blocked unless explicitly granted."
	case modeAgentic:
		return "Agentic mode for ordinary company end users: production-oriented source and control-plane defaults apply; gj_catalog and approved workflow execution can be available, while detailed audit/config/workflow-code surfaces require explicit grants."
	default:
		return "Development: discovery and audit surfaces favor iteration for developers."
	}
}

func securityPolicyNanoRow(policy securityPolicyEval, now string) core.NanoRow {
	row := core.NanoRow{
		"id":                policy.ID,
		"kind":              securityKindPolicy,
		"report":            securityKindPolicy,
		"scope":             policy.Scope,
		"config_id":         policy.ConfigID,
		"config_name":       policy.ConfigName,
		"config_file":       policy.ConfigFile,
		"config_path":       policy.ConfigPath,
		"config_inherits":   policy.ConfigInherits,
		"config_active":     policy.ConfigActive,
		"mode":              policy.Mode,
		"layer":             policy.Layer,
		"surface":           policy.Surface,
		"transport":         policy.Transport,
		"database_name":     policy.DatabaseName,
		"source":            policy.Source,
		"source_kind":       policy.SourceKind,
		"table_name":        policy.TableName,
		"column_name":       policy.ColumnName,
		"role":              policy.Role,
		"audience":          policy.Audience,
		"capability":        policy.Capability,
		"action":            policy.Action,
		"title":             policy.Title,
		"summary":           policy.Summary,
		"effective":         policy.Effective,
		"default_effective": policy.DefaultEffective,
		"effective_allowed": policy.EffectiveAllowed,
		"default_allowed":   policy.DefaultAllowed,
		"override_key":      policy.OverrideKey,
		"override_value":    policy.OverrideValue,
		"override_explicit": policy.OverrideExplicit,
		"override_source":   policy.OverrideSource,
		"weakens_default":   policy.WeakensDefault,
		"read_only":         policy.ReadOnly,
		"severity":          policy.RiskSeverity,
		"severity_rank":     securitySeverityRank(policy.RiskSeverity),
		"confidence":        policy.Confidence,
		"status":            policy.Status,
		"recommendation":    policy.Recommendation,
		"evidence_json":     policy.Evidence,
		"details_json":      policy.Details,
		"safety_json":       policy.Safety,
		"created_at":        now,
		"updated_at":        now,
		"search_rank":       0,
	}
	row["search_vector"] = securitySearchVector(row)
	return row
}

func securityFindingNanoRows(ctx securityReportContext, policies []securityPolicyEval, now string) []core.NanoRow {
	var rows []core.NanoRow
	for _, policy := range policies {
		if !policy.WeakensDefault && policy.Status != securityStatusFinding {
			continue
		}
		severity := policy.RiskSeverity
		if severity == "" {
			severity = "medium"
		}
		rows = append(rows, securityFindingNanoRow(
			securityFindingID(policy, severity),
			policy.Mode,
			severity,
			policy.Title,
			policy.Reason,
			policy.Recommendation,
			policy,
			now,
		))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"])
	})
	return rows
}

func securityFindingID(policy securityPolicyEval, severity string) string {
	id := policy.ID
	prefix := ""
	if idx := strings.LastIndex(id, "policy:"); idx >= 0 {
		prefix = id[:idx]
		id = strings.TrimPrefix(id[idx:], "policy:")
	} else {
		id = strings.TrimPrefix(id, "policy:")
	}
	return prefix + "finding:" + securityIDPart(severity) + ":" + securityIDPart(id)
}

func securityFindingNanoRow(id, mode, severity, title, reason, recommendation string, policy securityPolicyEval, now string) core.NanoRow {
	row := core.NanoRow{
		"id":                id,
		"kind":              securityKindFinding,
		"report":            securityKindFinding,
		"scope":             policy.Scope,
		"config_id":         policy.ConfigID,
		"config_name":       policy.ConfigName,
		"config_file":       policy.ConfigFile,
		"config_path":       policy.ConfigPath,
		"config_inherits":   policy.ConfigInherits,
		"config_active":     policy.ConfigActive,
		"mode":              mode,
		"layer":             policy.Layer,
		"surface":           policy.Surface,
		"transport":         policy.Transport,
		"database_name":     policy.DatabaseName,
		"source":            policy.Source,
		"source_kind":       policy.SourceKind,
		"table_name":        policy.TableName,
		"column_name":       policy.ColumnName,
		"role":              policy.Role,
		"audience":          policy.Audience,
		"capability":        policy.Capability,
		"action":            policy.Action,
		"title":             title,
		"summary":           reason,
		"effective":         policy.Effective,
		"default_effective": policy.DefaultEffective,
		"effective_allowed": policy.EffectiveAllowed,
		"default_allowed":   policy.DefaultAllowed,
		"override_key":      policy.OverrideKey,
		"override_value":    policy.OverrideValue,
		"override_explicit": policy.OverrideExplicit,
		"override_source":   policy.OverrideSource,
		"weakens_default":   policy.WeakensDefault,
		"read_only":         policy.ReadOnly,
		"severity":          severity,
		"severity_rank":     securitySeverityRank(severity),
		"confidence":        policy.Confidence,
		"status":            securityStatusFinding,
		"reason":            reason,
		"recommendation":    recommendation,
		"evidence_json":     policy.Evidence,
		"details_json": map[string]any{
			"policy_id":         policy.ID,
			"default_effective": policy.DefaultEffective,
			"effective":         policy.Effective,
		},
		"safety_json": map[string]any{
			"requires_review": true,
			"severity":        severity,
		},
		"created_at":  now,
		"updated_at":  now,
		"search_rank": 0,
	}
	row["search_vector"] = securitySearchVector(row)
	return row
}

func securitySummaryNanoRow(ctx securityReportContext, policyCount int, findings []core.NanoRow, now string) core.NanoRow {
	counts := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, finding := range findings {
		severity := fmt.Sprint(finding["severity"])
		if _, ok := counts[severity]; !ok {
			counts[severity] = 0
		}
		counts[severity]++
	}
	conf := ctx.Conf
	mode := ctx.Mode
	production := conf != nil && (conf.Serv.Production || conf.Core.Production)
	id := ctx.SummaryID
	if id == "" {
		id = "summary"
	}
	scope := ctx.Scope
	if scope == "" {
		scope = securityScopeRuntime
	}
	row := core.NanoRow{
		"id":              id,
		"kind":            securityKindSummary,
		"report":          securityKindSummary,
		"scope":           scope,
		"config_id":       ctx.ConfigID,
		"config_name":     ctx.ConfigName,
		"config_file":     ctx.ConfigFile,
		"config_path":     ctx.ConfigPath,
		"config_inherits": ctx.ConfigInherits,
		"config_active":   ctx.ConfigActive,
		"mode":            mode,
		"layer":           "system",
		"surface":         "security_audit",
		"transport":       "graphql",
		"source":          "system",
		"source_kind":     featurecap.KindSystem,
		"audience":        securityAudienceFor(mode, ""),
		"capability":      "summary",
		"action":          "read",
		"title":           "GraphJin security summary",
		"summary":         "Effective security posture for GraphJin core, service, and agent-facing control-plane surfaces.",
		"status":          securityStatusInfo,
		"confidence":      "high",
		"summary_json": map[string]any{
			"scope":             scope,
			"config_id":         ctx.ConfigID,
			"config_name":       ctx.ConfigName,
			"config_file":       ctx.ConfigFile,
			"config_path":       ctx.ConfigPath,
			"config_inherits":   ctx.ConfigInherits,
			"config_active":     ctx.ConfigActive,
			"mode":              mode,
			"production":        production,
			"policy_rows":       policyCount,
			"finding_rows":      len(findings),
			"findings":          counts,
			"agentic_audience":  "company end users using an approved agentic deployment",
			"system_read_model": "normal role/table permissions",
			"generated_at":      now,
		},
		"details_json": securitySummaryDetails(),
		"examples_json": []map[string]string{
			{"name": "summary", "query": `query { gj_security(id: "summary") { id kind mode summary_json } }`},
			{"name": "high critical findings across configs", "query": `query { gj_security(where: { kind: { eq: "finding" }, severity: { in: ["high", "critical"] } }, order_by: { severity_rank: desc }) { id scope config_id mode severity title recommendation evidence_json } }`},
			{"name": "prod config findings", "query": `query { gj_security(where: { scope: { eq: "config" }, mode: { eq: "prod" }, kind: { eq: "finding" } }) { id config_id severity title recommendation } }`},
			{"name": "agentic effective policy", "query": `query { gj_security(where: { kind: { eq: "policy" }, mode: { eq: "agentic" } }) { id scope config_id capability action default_effective effective weakens_default } }`},
			{"name": "explicit override review", "query": `query { gj_security(where: { override_explicit: { eq: true } }) { id scope config_id mode override_key default_effective effective weakens_default } }`},
		},
		"safety_json": map[string]any{
			"read_only": true,
			"note":      "gj_security is evidence for audit and planning; change enforcement through normal config, source, and role/table permissions.",
			"agentic":   "Normal agentic users should use gj_catalog and approved workflow execution; detailed gj_security, gj_config, and gj_workflow.code require an explicit authenticated grant.",
		},
		"created_at":  now,
		"updated_at":  now,
		"search_rank": 0,
	}
	row["search_vector"] = securitySearchVector(row)
	return row
}

func securityConfigLoadErrorRow(ctx securityReportContext, now string) core.NanoRow {
	message := ""
	if ctx.LoadErr != nil {
		message = ctx.LoadErr.Error()
	}
	id := ctx.IDPrefix + "finding:high:load_error"
	if ctx.IDPrefix == "" {
		id = "config:" + securityIDPart(ctx.ConfigID) + ":finding:high:load_error"
	}
	row := core.NanoRow{
		"id":                id,
		"kind":              securityKindFinding,
		"report":            securityKindFinding,
		"scope":             securityScopeConfig,
		"config_id":         ctx.ConfigID,
		"config_name":       ctx.ConfigName,
		"config_file":       ctx.ConfigFile,
		"config_path":       ctx.ConfigPath,
		"config_inherits":   ctx.ConfigInherits,
		"config_active":     ctx.ConfigActive,
		"mode":              ctx.Mode,
		"layer":             "config",
		"surface":           "config",
		"transport":         "filesystem",
		"source":            "config",
		"source_kind":       featurecap.KindSystem,
		"capability":        "load",
		"action":            "parse",
		"title":             "Config could not be loaded",
		"summary":           message,
		"effective":         securityEffectiveBlock,
		"default_effective": securityEffectiveAllow,
		"weakens_default":   false,
		"severity":          "high",
		"severity_rank":     securitySeverityRank("high"),
		"confidence":        "high",
		"status":            securityStatusLoadError,
		"reason":            "The security audit cannot evaluate a config file that fails parsing, inheritance, normalization, or validation.",
		"recommendation":    "Fix the config load error, then rerun gj_security and review resulting policy/finding rows.",
		"evidence_json": map[string]any{
			"config_file": ctx.ConfigFile,
			"config_path": ctx.ConfigPath,
			"error":       message,
		},
		"details_json": map[string]any{
			"load_error": message,
		},
		"safety_json": map[string]any{
			"requires_review": true,
			"read_only":       true,
		},
		"created_at":  now,
		"updated_at":  now,
		"search_rank": 0,
	}
	row["search_vector"] = securitySearchVector(row)
	return row
}

func securityRuntimeInfoRows(ctx securityReportContext, now string) []core.NanoRow {
	s := ctx.Service
	if s == nil {
		return nil
	}
	var rows []core.NanoRow
	add := func(idPart, title, capability string, evidence map[string]any) {
		row := core.NanoRow{
			"id":                "runtime:info:" + securityIDPart(idPart),
			"kind":              securityKindPolicy,
			"report":            "runtime",
			"scope":             securityScopeRuntime,
			"config_id":         ctx.ConfigID,
			"config_name":       ctx.ConfigName,
			"config_file":       ctx.ConfigFile,
			"config_path":       ctx.ConfigPath,
			"config_active":     true,
			"mode":              ctx.Mode,
			"layer":             "runtime",
			"surface":           "evidence",
			"transport":         "runtime",
			"source":            "system",
			"source_kind":       featurecap.KindSystem,
			"audience":          "authenticated_user",
			"capability":        capability,
			"action":            "observe",
			"title":             title,
			"summary":           title,
			"effective":         securityEffectiveAllow,
			"default_effective": securityEffectiveAllow,
			"weakens_default":   false,
			"confidence":        "high",
			"status":            securityStatusInfo,
			"evidence_json":     evidence,
			"details_json":      evidence,
			"safety_json": map[string]any{
				"read_only": true,
				"runtime":   true,
			},
			"created_at":  now,
			"updated_at":  now,
			"search_rank": 0,
		}
		row["search_vector"] = securitySearchVector(row)
		rows = append(rows, row)
	}

	tools := []string(nil)
	if s.conf != nil {
		tools = mcpToolList(s.conf)
	}
	add("mcp_tools", "Actual MCP tool list", "mcp_tools", map[string]any{
		"tools":    tools,
		"mcp_on":   s.conf != nil && !s.conf.mcpDisabled(),
		"tool_cnt": len(tools),
	})

	if s.systemNanoDB != nil {
		if snap := s.systemNanoDB.Snapshot(); snap != nil {
			tables := make([]map[string]any, 0, len(snap.Tables))
			for _, table := range snap.Tables {
				tables = append(tables, map[string]any{
					"name":    table.Name,
					"schema":  table.Schema,
					"columns": len(table.Columns),
					"rows":    len(table.Rows),
				})
			}
			add("nanodb_tables", "System NanoDB table snapshot", "nanodb", map[string]any{
				"database_name": s.metadataDB,
				"read_only":     true,
				"tables":        tables,
			})
		}
	}

	if s.conf != nil && s.conf.workflowsEnabled() {
		snap := s.workflowSnapshot(s.workflowTimeoutSeconds())
		hashes := make(map[string]string, len(snap.workflows))
		runtimes := make(map[string]string, len(snap.workflows))
		for _, wf := range snap.workflows {
			hashes[wf.Name] = wf.SourceHash
			runtimes[wf.Name] = wf.Runtime
		}
		add("workflows", "Workflow runtime snapshot", "workflow", map[string]any{
			"workflow_count":                   len(snap.workflows),
			"workflow_revision":                snap.revision,
			"workflow_timeout_seconds":         snap.timeout,
			"workflow_source_hashes":           hashes,
			"workflow_runtimes":                runtimes,
			"gj_workflow_execution_executable": securityWorkflowExecutionInsertAllowed(s.conf, ctx.Mode, "user", true, controlPlaneTableReadOnly(s.conf, s.metadataDB, "gj_workflow_execution")),
		})
	}

	if len(s.managedDBs) != 0 {
		codeDBs := make(map[string]any, len(s.managedDBs))
		for name, managed := range s.managedDBs {
			codeDBs[name] = map[string]any{
				"watch":     managed.watch,
				"read_only": managed.readOnly,
				"connected": managed.handle != nil,
			}
		}
		add("codesql", "CodeSQL runtime state", "codesql", map[string]any{
			"databases": codeDBs,
		})
	}

	if s.gj != nil {
		if md, err := s.gj.MetadataSnapshot(s.metadataSnapshotExcludesFor(s.metadataDB, &s.conf.Core, s.managedDBs)...); err == nil && md != nil {
			add("metadata_counts", "Metadata snapshot counts", "metadata", map[string]any{
				"databases":     len(md.Databases),
				"tables":        len(md.Tables),
				"columns":       len(md.Columns),
				"relationships": len(md.Relationships),
				"functions":     len(md.Functions),
				"indexes":       len(md.Indexes),
			})
		} else if err != nil {
			add("metadata_error", "Metadata snapshot error", "metadata", map[string]any{
				"error": err.Error(),
			})
		}
	}

	if s.runtimeCore != nil {
		dbs := make(map[string]any, len(s.runtimeCore.Databases))
		for name, dbConf := range s.runtimeCore.Databases {
			if name == s.managedArtifactDB {
				continue
			}
			_, connected := s.dbs[name]
			dbs[name] = map[string]any{
				"type":         dbConf.Type,
				"managed_type": dbConf.ManagedType,
				"read_only":    dbConf.ReadOnly,
				"connected":    connected || name == s.metadataDB,
			}
		}
		tables := make(map[string]any, len(s.runtimeCore.Tables))
		for _, table := range s.runtimeCore.Tables {
			if table.Database == s.managedArtifactDB || table.Source == s.managedArtifactDB {
				continue
			}
			key := table.Name
			if table.Database != "" {
				key = table.Database + "." + key
			}
			tables[key] = map[string]any{
				"database":  table.Database,
				"source":    table.Source,
				"table":     table.Table,
				"read_only": table.ReadOnly,
			}
		}
		add("runtime_core", "Runtime core database and table state", "runtime_core", map[string]any{
			"databases": dbs,
			"tables":    tables,
		})
	}

	return rows
}

func securitySummaryDetails() map[string]any {
	return map[string]any{
		"modes": map[string]string{
			modeDev:     "Development defaults favor iteration and discovery.",
			modeProd:    "Production defaults enforce allow-lists and block agent/system write surfaces unless explicitly opened.",
			modeAgentic: "Governed agentic deployment for company users: production-oriented source and control-plane defaults apply while selected catalog/workflow execution surfaces can be available.",
		},
		"kinds": []string{securityKindSummary, securityKindPolicy, securityKindFinding},
		"columns": map[string]string{
			"scope":             "runtime rows describe the active process; config rows describe resolved config files after inheritance/env overlays.",
			"config_id":         "Stable config identifier, usually the config filename without extension.",
			"config_active":     "True when the config row corresponds to the active runtime config file.",
			"surface":           "High-level surface such as app data, control_plane, mcp, http, auth, code, file, api, or database.",
			"role":              "Role evaluated for role/table-controlled surfaces.",
			"effective":         "The resolved current behavior.",
			"default_effective": "The secure default for the active deployment mode.",
			"weakens_default":   "True when current config is more permissive than the secure default.",
			"status":            "pass, finding, info, or load_error.",
			"severity":          "Finding severity: low, medium, high, or critical.",
		},
	}
}

func securitySeverityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func securitySearchVector(row core.NanoRow) string {
	parts := []string{
		fmt.Sprint(row["kind"]),
		fmt.Sprint(row["report"]),
		fmt.Sprint(row["scope"]),
		fmt.Sprint(row["config_id"]),
		fmt.Sprint(row["config_name"]),
		fmt.Sprint(row["config_file"]),
		fmt.Sprint(row["status"]),
		fmt.Sprint(row["mode"]),
		fmt.Sprint(row["layer"]),
		fmt.Sprint(row["surface"]),
		fmt.Sprint(row["transport"]),
		fmt.Sprint(row["database_name"]),
		fmt.Sprint(row["source"]),
		fmt.Sprint(row["source_kind"]),
		fmt.Sprint(row["table_name"]),
		fmt.Sprint(row["column_name"]),
		fmt.Sprint(row["role"]),
		fmt.Sprint(row["audience"]),
		fmt.Sprint(row["capability"]),
		fmt.Sprint(row["action"]),
		fmt.Sprint(row["title"]),
		fmt.Sprint(row["summary"]),
		fmt.Sprint(row["severity"]),
		fmt.Sprint(row["reason"]),
		fmt.Sprint(row["recommendation"]),
	}
	return strings.Join(parts, " ")
}

func securityIDPart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == ':' || r == '-'
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}
