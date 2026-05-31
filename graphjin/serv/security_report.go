package serv

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
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
	if err := v.Unmarshal(conf); err != nil {
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
	graphjinControlPlane := conf != nil && conf.graphjinControlPlaneEnabled()
	workflowsEnabled := conf != nil && conf.workflowsSourceEnabled()
	catalogEnabled := conf != nil && conf.catalogToolsEnabled()
	runtimeRegistered := conf != nil && conf.runtimeRootRegistered()
	configReadOnly := controlPlaneTableReadOnly(conf, "", "gj_config")
	workflowReadOnly := controlPlaneTableReadOnly(conf, "", "gj_workflow")
	workflowExecutionReadOnly := controlPlaneTableReadOnly(conf, "", "gj_workflow_execution")
	corsWildcard := securityHasWildcard(confAllowedOrigins(conf))
	uploadEnabled := conf != nil && conf.Serv.Uploads.Enabled

	rows := []securityPolicyEval{
		newSecurityPolicy(mode, "core.app_data", "core", "app", "database", "app_data", "query",
			"Application data access",
			"Application data remains governed by table roles, allow-list behavior, and data-source read_only settings.",
			defaultAllow(mode, true, true, true), conf != nil,
			"databases/sources/roles", securityConfiguredDatabaseSummary(conf),
			false, "medium",
			"The app data plane is expected to stay available in all modes while production protections are enforced separately.",
			"Use table role filters/blocklists/read_only settings for app data restrictions."),
		newSecurityPolicy(mode, "core.dynamic_graphql", "core", "graphjin", "graphjin", "dynamic_graphql", "query",
			"Dynamic GraphQL queries",
			"Controls whether raw client GraphQL can compile outside the allow-list.",
			defaultAllow(mode, true, false, false), !prodSecurity,
			"disable_production_security", fmt.Sprint(conf != nil && conf.Core.DisableProdSecurity),
			configBoolExplicit(conf, "disable_production_security"), "critical",
			"Production and agentic deployments should enforce the allow-list for app data.",
			"Enable production mode and keep disable_production_security false."),
		newSecurityPolicy(mode, "core.anonymous_access", "core", "graphjin", "graphjin", "anonymous_access", "query",
			"Anonymous app access",
			"Controls whether unauthenticated requests can use the anonymous role by default.",
			defaultAllow(mode, true, false, false), conf != nil && !conf.DefaultBlock,
			"default_block", fmt.Sprint(conf != nil && conf.DefaultBlock),
			configBoolExplicit(conf, "default_block"), "high",
			"Prod and agentic deployments are intended for authenticated users; anonymous access broadens every exposed surface.",
			"Configure authentication and keep default_block true unless anonymous access is a deliberate public API requirement."),
		newSecurityPolicy(mode, "core.introspection", "core", "graphjin", "graphjin", "introspection", "read",
			"GraphQL introspection export",
			"Controls whether GraphJin writes an introspection JSON file at startup.",
			defaultAllow(mode, true, false, false), conf != nil && !production && conf.Core.EnableIntrospection,
			"enable_introspection", fmt.Sprint(conf != nil && conf.Core.EnableIntrospection),
			configBoolExplicit(conf, "enable_introspection"), "medium",
			"Introspection metadata can reveal schema shape and should stay off outside development.",
			"Keep enable_introspection disabled in prod and agentic modes."),
		newSecurityPolicy(mode, "serve.catalog_read", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "catalog", "read",
			"Catalog read access",
			"Controls whether normal authenticated users can read gj_catalog for schema and workflow discovery.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyCatalogRead), securitySystemReadAllowed(conf, "gj_catalog", mode, "user", catalogEnabled),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyCatalogRead, "roles[user].tables.gj_catalog.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_catalog", sourcecap.KindGraphJin, sourcecap.KeyCatalogRead),
			securitySystemReadExplicit(conf, "user", "gj_catalog", sourcecap.KindGraphJin, sourcecap.KeyCatalogRead), "medium",
			"Catalog discovery is useful for agentic company users, but strict prod blocks system discovery unless explicitly granted.",
			"Grant gj_catalog read only to the authenticated role that should use discovery."),
		newSecurityPolicy(mode, "serve.catalog_read_anon", "serve", "graphjin", "graphjin", "catalog", "read",
			"Anonymous catalog read access",
			"Controls whether unauthenticated users can read gj_catalog.",
			false, securitySystemReadAllowed(conf, "gj_catalog", mode, "anon", catalogEnabled),
			"roles[anon].tables.gj_catalog.query.block", securityRoleQueryValue(conf, "anon", "gj_catalog"),
			securityRoleTableExplicit(conf, "anon", "gj_catalog"), "high",
			"Agentic catalog discovery is for authenticated company users, not anonymous clients.",
			"Keep anon gj_catalog blocked; authenticate agentic users so they receive the user role."),
		newSecurityPolicy(mode, "serve.security_read", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "security_audit", "read",
			"Security audit read access",
			"Controls whether normal authenticated users can read detailed gj_security rows.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeySecurityRead), securitySystemReadAllowed(conf, "gj_security", mode, "user", catalogEnabled || graphjinControlPlane || workflowsEnabled),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeySecurityRead, "roles[user].tables.gj_security.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_security", sourcecap.KindGraphJin, sourcecap.KeySecurityRead),
			securitySystemReadExplicit(conf, "user", "gj_security", sourcecap.KindGraphJin, sourcecap.KeySecurityRead), "high",
			"Detailed findings expose audit evidence, config posture, and privileged recommendations.",
			"Grant gj_security read only through an explicit authenticated role or source capability; normal agentic users should discover safe actions through gj_catalog."),
		newSecurityPolicy(mode, "serve.config_read", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "config", "read",
			"Config read access",
			"Controls whether normal authenticated users can read gj_config.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyConfigRead), securitySystemReadAllowed(conf, "gj_config", mode, "user", graphjinControlPlane),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyConfigRead, "roles[user].tables.gj_config.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_config", sourcecap.KindGraphJin, sourcecap.KeyConfigRead),
			securitySystemReadExplicit(conf, "user", "gj_config", sourcecap.KindGraphJin, sourcecap.KeyConfigRead), "critical",
			"Configuration rows can reveal database names, role rules, enabled tools, and redacted secret locations.",
			"Grant gj_config read only through an explicit authenticated role or source capability."),
		newSecurityPolicy(mode, "serve.runtime_read", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "runtime", "read",
			"Runtime status read access",
			"Controls whether normal authenticated users can read compact gj_runtime status and recent redacted events.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyRuntimeRead), securitySystemReadAllowed(conf, "gj_runtime", mode, "user", runtimeRegistered),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyRuntimeRead, "roles[user].tables.gj_runtime.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_runtime", sourcecap.KindGraphJin, sourcecap.KeyRuntimeRead),
			securitySystemReadExplicit(conf, "user", "gj_runtime", sourcecap.KindGraphJin, sourcecap.KeyRuntimeRead), "medium",
			"gj_runtime is decision support for agentic clients; it exposes bounded operational summaries, not audit history.",
			"Enable runtime.read only for authenticated agentic users who should inspect current system state."),
		newSecurityPolicy(mode, "serve.runtime_read_anon", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "runtime", "read",
			"Anonymous runtime status read access",
			"Controls whether unauthenticated users can read gj_runtime.",
			false, securitySystemReadAllowed(conf, "gj_runtime", mode, "anon", runtimeRegistered),
			"roles[anon].tables.gj_runtime.query.block", securityRoleQueryValue(conf, "anon", "gj_runtime"),
			securityRoleTableExplicit(conf, "anon", "gj_runtime"), "high",
			"Runtime state is for authenticated agentic clients and can reveal degraded infrastructure components.",
			"Keep anon gj_runtime blocked; authenticate agentic users so they receive the user role."),
		newSecurityPolicy(mode, "serve.workflow_read", "serve", sourcecap.KindWorkflow, sourcecap.KindWorkflow, "workflow", "read",
			"Workflow definition read access",
			"Controls whether normal authenticated users can read gj_workflow definitions, including workflow code.",
			sourceCapabilityDefault(mode, sourcecap.KindWorkflow, sourcecap.KeyWorkflowRead), securitySystemReadAllowed(conf, "gj_workflow", mode, "user", workflowsEnabled),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowRead, "roles[user].tables.gj_workflow.query.block"), securitySystemReadOverrideValue(conf, "user", "gj_workflow", sourcecap.KindWorkflow, sourcecap.KeyWorkflowRead),
			securitySystemReadExplicit(conf, "user", "gj_workflow", sourcecap.KindWorkflow, sourcecap.KeyWorkflowRead), "high",
			"Workflow definitions include code and implementation details; agentic end users should execute approved workflows without reading code by default.",
			"Grant gj_workflow read only to authenticated users who are allowed to inspect workflow code; expose workflow capabilities through gj_catalog."),
		newSecurityPolicy(mode, "serve.workflow_execution_read", "serve", "workflow", "workflow", "workflow_execution", "read",
			"Workflow execution read access",
			"Controls whether normal authenticated users can query gj_workflow_execution as a read root.",
			defaultAllow(mode, false, false, false), securitySystemReadAllowed(conf, "gj_workflow_execution", mode, "user", workflowsEnabled),
			"roles[user].tables.gj_workflow_execution.query.block", securityRoleQueryValue(conf, "user", "gj_workflow_execution"),
			securityRoleTableExplicit(conf, "user", "gj_workflow_execution"), "medium",
			"Workflow execution is an insert-shaped action and does not store durable run history.",
			"Keep gj_workflow_execution query blocked; use insert mutations for approved execution."),
		newSecurityPolicy(mode, "serve.config_write", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "config", "write",
			"Config writes",
			"Controls gj_config mutations that update GraphJin configuration.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyConfigWrite), graphjinControlPlane && !configReadOnly && securitySourceCapabilityOrFallback(conf, sourcecap.KindGraphJin, sourcecap.KeyConfigWrite, conf != nil && conf.MCP.AllowConfigUpdates),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyConfigWrite, "mcp.allow_config_updates"), securitySourceOrFallbackValue(conf, sourcecap.KindGraphJin, sourcecap.KeyConfigWrite, conf != nil && conf.MCP.AllowConfigUpdates),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindGraphJin, sourcecap.KeyConfigWrite, mcpBoolExplicit(conf, "mcp.allow_config_updates", conf != nil && conf.MCP.AllowConfigUpdates)), "high",
			"Config writes can alter databases, roles, and control-plane permissions.",
			"Only enable mcp.allow_config_updates in trusted sessions and keep the graphjin source read_only when writes are not intended."),
		newSecurityPolicy(mode, "serve.workflow_write", "serve", sourcecap.KindWorkflow, sourcecap.KindWorkflow, "workflow", "write",
			"Workflow writes",
			"Controls gj_workflow mutations that create, update, or delete saved workflows.",
			sourceCapabilityDefault(mode, sourcecap.KindWorkflow, sourcecap.KeyWorkflowWrite), workflowsEnabled && !workflowReadOnly && securitySourceCapabilityOrFallback(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowWrite, conf != nil && conf.MCP.AllowWorkflowUpdates),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowWrite, "mcp.allow_workflow_updates"), securitySourceOrFallbackValue(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowWrite, conf != nil && conf.MCP.AllowWorkflowUpdates),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowWrite, mcpBoolExplicit(conf, "mcp.allow_workflow_updates", conf != nil && conf.MCP.AllowWorkflowUpdates)), "high",
			"Workflow writes can persist code that runs inside GraphJin.",
			"Enable workflow writes only for trusted agents, review saved workflow code, or mark the workflow source or gj_workflow table read_only."),
		newSecurityPolicy(mode, "serve.workflow_execute", "serve", sourcecap.KindWorkflow, sourcecap.KindWorkflow, "workflow", "execute",
			"Workflow execution",
			"Controls authenticated-user gj_workflow_execution insert mutations that execute saved workflows.",
			sourceCapabilityDefault(mode, sourcecap.KindWorkflow, sourcecap.KeyWorkflowExecute), securityWorkflowExecutionInsertAllowed(conf, mode, "user", workflowsEnabled, workflowExecutionReadOnly),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowExecute, "tables[].read_only"), securitySourceOrFallbackValue(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowExecute, !workflowExecutionReadOnly),
			securitySourceCapabilityExplicit(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowExecute) || controlPlaneTableReadOnlyExplicit(conf, "", "gj_workflow_execution") || securityRoleTableExplicit(conf, "user", "gj_workflow_execution"), "high",
			"Workflow execution can run code and access configured data sources.",
			"Keep workflow execution limited to trusted deployments; mark the workflow source or gj_workflow_execution table read_only to block execution."),
		newSecurityPolicy(mode, "serve.workflow_execute_anon", "serve", "workflow", "workflow", "workflow", "execute",
			"Anonymous workflow execution",
			"Controls unauthenticated gj_workflow_execution insert mutations.",
			defaultAllow(mode, true, false, false), securityWorkflowExecutionInsertAllowed(conf, mode, "anon", workflowsEnabled, workflowExecutionReadOnly),
			"roles[anon].tables.gj_workflow_execution.insert.block", securityRoleMutationValue(conf, "anon", "gj_workflow_execution", "insert"),
			securityRoleTableExplicit(conf, "anon", "gj_workflow_execution"), "critical",
			"Approved agentic workflow execution is for authenticated company users; anonymous execution can run code without accountability.",
			"Keep anon gj_workflow_execution insert blocked and authenticate agentic users."),
		newSecurityPolicy(mode, "serve.legacy_execute_workflow_tool", "serve", "graphjin", "graphjin", "legacy_workflow_execution", "execute",
			"Legacy MCP workflow execution tool",
			"Controls the execute_workflow MCP compatibility tool. GraphQL gj_workflow_execution is controlled by read_only table/source policy.",
			defaultAllow(mode, true, false, false), mcpEnabled && conf != nil && conf.legacyMCPToolsEnabled() && conf.MCP.AllowWorkflowExecution,
			"mcp.allow_workflow_execution", fmt.Sprint(conf != nil && conf.MCP.AllowWorkflowExecution),
			mcpBoolExplicit(conf, "mcp.allow_workflow_execution", conf != nil && conf.MCP.AllowWorkflowExecution), "medium",
			"Legacy MCP execution is a compatibility surface; prefer catalog-discovered GraphQL control-plane mutations.",
			"Keep mcp.legacy_discovery or mcp.allow_workflow_execution disabled unless a legacy MCP client requires execute_workflow."),
		newSecurityPolicy(mode, "serve.schema_reload", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "schema", "reload",
			"Schema reload",
			"Controls MCP schema reload operations.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeySchemaReload), mcpEnabled && securitySourceCapabilityOrFallback(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaReload, conf != nil && conf.MCP.AllowSchemaReload),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaReload, "mcp.allow_schema_reload"), securitySourceOrFallbackValue(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaReload, conf != nil && conf.MCP.AllowSchemaReload),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaReload, mcpBoolExplicit(conf, "mcp.allow_schema_reload", conf != nil && conf.MCP.AllowSchemaReload)), "medium",
			"Schema reload changes discovery state and should be explicit outside development.",
			"Enable schema reload only when a trusted authenticated user or agent needs fresh metadata."),
		newSecurityPolicy(mode, "serve.schema_write", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "schema", "write",
			"Schema writes",
			"Controls MCP schema update operations that can apply DDL.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeySchemaWrite), mcpEnabled && securitySourceCapabilityOrFallback(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaWrite, conf != nil && conf.MCP.AllowSchemaUpdates),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaWrite, "mcp.allow_schema_updates"), securitySourceOrFallbackValue(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaWrite, conf != nil && conf.MCP.AllowSchemaUpdates),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindGraphJin, sourcecap.KeySchemaWrite, mcpBoolExplicit(conf, "mcp.allow_schema_updates", conf != nil && conf.MCP.AllowSchemaUpdates)), "high",
			"Schema writes can alter application databases.",
			"Only enable mcp.allow_schema_updates for trusted migration sessions and prefer preview before apply."),
		newSecurityPolicy(mode, "serve.dev_tools", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "dev_tools", "read",
			"Development tools",
			"Controls advanced MCP tools that expose SQL, relationship graphs, and role permissions.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyDevToolsRead), mcpEnabled && securitySourceCapabilityOrFallback(conf, sourcecap.KindGraphJin, sourcecap.KeyDevToolsRead, conf != nil && conf.MCP.AllowDevTools),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyDevToolsRead, "mcp.allow_dev_tools"), securitySourceOrFallbackValue(conf, sourcecap.KindGraphJin, sourcecap.KeyDevToolsRead, conf != nil && conf.MCP.AllowDevTools),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindGraphJin, sourcecap.KeyDevToolsRead, mcpBoolExplicit(conf, "mcp.allow_dev_tools", conf != nil && conf.MCP.AllowDevTools)), "medium",
			"Development tools may reveal operational details useful for debugging and auditing.",
			"Keep dev tools disabled in prod unless a trusted audit workflow needs them."),
		newSecurityPolicy(mode, "serve.raw_queries", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "raw_graphql", "query",
			"MCP raw GraphQL queries",
			"Controls compatibility MCP tools that submit arbitrary GraphQL text.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLQuery), mcpEnabled && securitySourceCapabilityOrFallback(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLQuery, conf != nil && conf.MCP.AllowRawQueries),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLQuery, "mcp.allow_raw_queries"), securitySourceOrFallbackValue(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLQuery, conf != nil && conf.MCP.AllowRawQueries),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLQuery, mcpBoolExplicit(conf, "mcp.allow_raw_queries", conf != nil && conf.MCP.AllowRawQueries)), "medium",
			"Raw GraphQL should not bypass production allow-list expectations.",
			"Prefer catalog-guided saved workflows or production allow-listed operations."),
		newSecurityPolicy(mode, "serve.raw_mutations", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "raw_graphql", "mutate",
			"MCP raw GraphQL mutations",
			"Controls compatibility MCP tools that can submit arbitrary GraphQL mutations.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLMutate), mcpEnabled && securitySourceCapabilityOrFallback(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLMutate, conf != nil && conf.MCP.AllowMutations),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLMutate, "mcp.allow_mutations"), securitySourceOrFallbackValue(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLMutate, conf != nil && conf.MCP.AllowMutations),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindGraphJin, sourcecap.KeyRawGraphQLMutate, mcpBoolExplicit(conf, "mcp.allow_mutations", conf != nil && conf.MCP.AllowMutations)), "high",
			"Raw mutations can bypass the intended workflow/catalog action path.",
			"Disable raw MCP mutations outside dev; expose approved mutations as saved operations or workflows."),
		newSecurityPolicy(mode, "serve.legacy_discovery", "serve", sourcecap.KindGraphJin, sourcecap.KindGraphJin, "legacy_discovery", "read",
			"Legacy MCP discovery",
			"Controls older MCP discovery tools and legacy helper endpoints.",
			sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyLegacyDiscoveryRead), mcpEnabled && conf != nil && conf.legacyMCPToolsEnabled(),
			securitySourceCapabilityOverrideKey(conf, sourcecap.KindGraphJin, sourcecap.KeyLegacyDiscoveryRead, "mcp.legacy_discovery"), securitySourceOrFallbackValue(conf, sourcecap.KindGraphJin, sourcecap.KeyLegacyDiscoveryRead, conf != nil && conf.MCP.LegacyDiscovery),
			securitySourceOrFallbackExplicit(conf, sourcecap.KindGraphJin, sourcecap.KeyLegacyDiscoveryRead, mcpBoolExplicit(conf, "mcp.legacy_discovery", conf != nil && conf.MCP.LegacyDiscovery)), "medium",
			"Legacy discovery can expose broader schema and helper surfaces than the catalog-first path.",
			"Prefer gj_catalog for discovery and enable legacy discovery only for compatible clients."),
		newSecurityPolicy(mode, "serve.web_ui", "serve", "graphjin", "graphjin", "admin_ui", "read",
			"Web UI",
			"Controls the built-in web UI/admin-facing HTTP surface.",
			defaultAllow(mode, true, false, false), conf != nil && conf.Serv.WebUI,
			"web_ui", fmt.Sprint(conf != nil && conf.Serv.WebUI),
			configBoolExplicit(conf, "web_ui"), "medium",
			"Admin-style UI surfaces should not be exposed in prod or agentic deployments by default.",
			"Keep web_ui false outside local development unless protected by explicit authentication."),
		newSecurityPolicy(mode, "serve.cors_wildcard", "serve", "graphjin", "graphjin", "cors", "allow",
			"Wildcard CORS origins",
			"Controls whether HTTP CORS allows every origin.",
			defaultAllow(mode, true, false, false), corsWildcard,
			"cors_allowed_origins", strings.Join(confAllowedOrigins(conf), ","),
			configBoolExplicit(conf, "cors_allowed_origins"), "medium",
			"Wildcard CORS increases browser-origin exposure for authenticated APIs.",
			"Set cors_allowed_origins to the exact application origins in prod and agentic deployments."),
		newSecurityPolicy(mode, "core.log_vars", "core", "graphjin", "graphjin", "request_variables", "log",
			"Variable logging",
			"Controls whether GraphQL variables are logged.",
			defaultAllow(mode, true, false, false), conf != nil && conf.Core.LogVars,
			"log_vars", fmt.Sprint(conf != nil && conf.Core.LogVars),
			configBoolExplicit(conf, "log_vars"), "high",
			"Variables can contain user data, tokens, or secrets.",
			"Keep log_vars false outside development and rely on structured redacted audit evidence."),
		newSecurityPolicy(mode, "serve.tracing", "serve", "graphjin", "graphjin", "tracing", "emit",
			"Request tracing",
			"Controls whether service request tracing is enabled.",
			defaultAllow(mode, true, true, true), conf != nil && conf.Serv.EnableTracing,
			"enable_tracing", fmt.Sprint(conf != nil && conf.Serv.EnableTracing),
			configBoolExplicit(conf, "enable_tracing"), "low",
			"Tracing is acceptable when exporters and payloads are configured to avoid sensitive data.",
			"Keep trace payloads minimal and avoid variable logging in production traces."),
		newSecurityPolicy(mode, "serve.uploads", "serve", "graphjin", "graphjin", "uploads", "write",
			"Multipart uploads",
			"Controls multipart GraphQL upload support.",
			defaultAllow(mode, true, false, false), uploadEnabled,
			"uploads.enabled", fmt.Sprint(uploadEnabled),
			configBoolExplicit(conf, "uploads.enabled"), "medium",
			"Uploads add file parsing, storage, and size-limit risk to the GraphQL endpoint.",
			"Enable uploads only with explicit max_size, allowed_mime, and a reviewed storage backend."),
	}

	rows = append(rows, securitySourcePolicyEvaluations(conf, mode)...)
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
	return conf != nil && conf.viper != nil && conf.viper.IsSet(key)
}

func configBoolExplicit(conf *Config, key string) bool {
	return conf != nil && conf.viper != nil && conf.viper.IsSet(key)
}

func securitySourceCapabilityOverrideKey(conf *Config, kind, capability, fallback string) string {
	if conf == nil {
		return fallback
	}
	if _, explicit := conf.sourceCapabilityConfigured(kind, capability); explicit {
		if source, ok := conf.sourceByCanonicalKind(kind); ok && strings.TrimSpace(source.Name) != "" {
			return fmt.Sprintf("sources[%s].capabilities.%s", strings.TrimSpace(source.Name), capability)
		}
		return fmt.Sprintf("sources[%s].capabilities.%s", kind, capability)
	}
	return fallback
}

func securitySourceCapabilityExplicit(conf *Config, kind, capability string) bool {
	_, explicit := conf.sourceCapabilityConfigured(kind, capability)
	return explicit
}

func securitySystemReadOverrideValue(conf *Config, role, table, kind, capability string) string {
	if securityRoleTableExplicit(conf, role, table) {
		return securityRoleQueryValue(conf, role, table)
	}
	if value, explicit := conf.sourceCapabilityConfigured(kind, capability); explicit {
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
	if value, explicit := conf.sourceCapabilityConfigured(kind, capability); explicit {
		return value
	}
	return fallback
}

func securitySourceOrFallbackValue(conf *Config, kind, capability string, fallback bool) string {
	if value, explicit := conf.sourceCapabilityConfigured(kind, capability); explicit {
		return fmt.Sprint(value)
	}
	return fmt.Sprint(fallback)
}

func securitySourceOrFallbackExplicit(conf *Config, kind, capability string, fallbackExplicit bool) bool {
	return securitySourceCapabilityExplicit(conf, kind, capability) || fallbackExplicit
}

func securitySystemReadAllowed(conf *Config, table, mode, role string, sourceEnabled bool) bool {
	if !sourceEnabled {
		return false
	}
	if rt, ok := securityRoleTable(conf, role, table); ok {
		return rt.Query == nil || !rt.Query.Block
	}
	return systemReadAllowedBySource(conf, mode, role, table)
}

func defaultSystemReadAllowed(mode, role, table string) bool {
	if role != "user" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_catalog":
		return sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyCatalogRead)
	case "gj_security":
		return sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeySecurityRead)
	case "gj_config":
		return sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyConfigRead)
	case "gj_runtime":
		return sourceCapabilityDefault(mode, sourcecap.KindGraphJin, sourcecap.KeyRuntimeRead)
	case "gj_workflow":
		return sourceCapabilityDefault(mode, sourcecap.KindWorkflow, sourcecap.KeyWorkflowRead)
	default:
		return false
	}
}

func securityWorkflowExecutionInsertAllowed(conf *Config, mode, role string, workflowsEnabled, tableReadOnly bool) bool {
	if !workflowsEnabled || tableReadOnly {
		return false
	}
	if rt, ok := securityRoleTable(conf, role, "gj_workflow_execution"); ok {
		return rt.Insert == nil || !rt.Insert.Block
	}
	if role == "anon" && (mode == modeProd || mode == modeAgentic) {
		return false
	}
	if role == "anon" && conf != nil && conf.DefaultBlock {
		return false
	}
	if role == "user" {
		return sourceCapabilityAllowed(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowExecute)
	}
	return defaultAllow(mode, true, false, true)
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
	if conf == nil {
		return ""
	}
	name := conf.Core.CatalogDatabaseName()
	if strings.TrimSpace(name) == "" {
		return defaultMetadataDBName
	}
	return strings.TrimSpace(name)
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
	return map[string]any{
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
}

func securityCapabilityEnforcement(row securityPolicyEval) string {
	if def, ok := sourcecap.Lookup(row.SourceKind, row.Capability); ok {
		return def.Enforcement
	}
	switch row.SourceKind {
	case "graphjin", "workflow":
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
		if !policy.WeakensDefault {
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
		"weakens_default":   true,
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
		"source":          "graphjin",
		"source_kind":     "graphjin",
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
		"source_kind":       "graphjin",
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
			"source":            "graphjin",
			"source_kind":       "graphjin",
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

	if s.conf != nil && s.conf.workflowsSourceEnabled() {
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
