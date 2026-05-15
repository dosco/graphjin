package serv

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

const (
	securityModeDev     = "dev"
	securityModeProd    = "prod"
	securityModeAgentic = "agentic"

	securityKindSummary = "summary"
	securityKindPolicy  = "policy"
	securityKindFinding = "finding"

	securityEffectiveAllow     = "allow"
	securityEffectiveBlock     = "block"
	securityEffectiveReadOnly  = "read_only"
	securityEffectiveReadWrite = "read_write"
)

type securityPolicyEval struct {
	ID               string
	Layer            string
	Source           string
	SourceKind       string
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
	WeakensDefault   bool
	ReadOnly         bool
	RiskSeverity     string
	Reason           string
	Recommendation   string
	Evidence         map[string]any
	Details          map[string]any
	Safety           map[string]any
}

func securityNanoRows(s *graphjinService) []core.NanoRow {
	conf := (*Config)(nil)
	if s != nil {
		conf = s.conf
	}
	mode := effectiveSecurityMode(conf)
	now := nowNanoTimestamp()

	policies := securityPolicyEvaluations(conf, mode)
	policyRows := make([]core.NanoRow, 0, len(policies))
	for _, policy := range policies {
		policyRows = append(policyRows, securityPolicyNanoRow(policy, now))
	}

	findingRows := securityFindingNanoRows(conf, policies, mode, now)
	summaryRow := securitySummaryNanoRow(conf, mode, len(policyRows), findingRows, now)

	rows := make([]core.NanoRow, 0, 1+len(policyRows)+len(findingRows))
	rows = append(rows, summaryRow)
	rows = append(rows, policyRows...)
	rows = append(rows, findingRows...)
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

func effectiveSecurityMode(conf *Config) string {
	if conf != nil {
		switch strings.ToLower(strings.TrimSpace(conf.Core.SecurityMode)) {
		case "dev", "development":
			return securityModeDev
		case "prod", "production":
			return securityModeProd
		case "agent", "agentic":
			return securityModeAgentic
		}
		if conf.Serv.Production || conf.Core.Production {
			return securityModeProd
		}
	}
	return securityModeDev
}

func securityPolicyEvaluations(conf *Config, mode string) []securityPolicyEval {
	production := conf != nil && (conf.Serv.Production || conf.Core.Production)
	prodSecurity := production && conf != nil && !conf.Core.DisableProdSecurity
	mcpEnabled := conf != nil && !conf.MCP.Disable
	graphjinControlPlane := conf != nil && conf.graphjinControlPlaneEnabled()
	workflowsEnabled := conf != nil && conf.workflowsSourceEnabled()
	catalogEnabled := conf != nil && conf.catalogToolsEnabled()
	configReadOnly := controlPlaneTableReadOnly(conf, "", "gj_config")
	workflowReadOnly := controlPlaneTableReadOnly(conf, "", "gj_workflow")
	workflowExecutionReadOnly := controlPlaneTableReadOnly(conf, "", "gj_workflow_execution")

	rows := []securityPolicyEval{
		newSecurityPolicy(mode, "core.dynamic_graphql", "core", "core", "engine", "dynamic_graphql", "query",
			"Dynamic GraphQL queries",
			"Controls whether raw client GraphQL can compile outside the allow-list.",
			defaultAllow(mode, true, false, false), !prodSecurity,
			"disable_production_security", fmt.Sprint(conf != nil && conf.Core.DisableProdSecurity),
			conf != nil && conf.Core.DisableProdSecurity, "critical",
			"Production and agentic deployments should enforce the allow-list for app data.",
			"Enable production mode and keep disable_production_security false."),
		newSecurityPolicy(mode, "core.introspection", "core", "core", "engine", "introspection", "read",
			"GraphQL introspection export",
			"Controls whether GraphJin writes an introspection JSON file at startup.",
			defaultAllow(mode, true, false, false), conf != nil && !production && conf.Core.EnableIntrospection,
			"enable_introspection", fmt.Sprint(conf != nil && conf.Core.EnableIntrospection),
			conf != nil && conf.Core.EnableIntrospection, "medium",
			"Introspection metadata can reveal schema shape and should stay off outside development.",
			"Keep enable_introspection disabled in prod and agentic modes."),
		newSecurityPolicy(mode, "serve.catalog_read", "serve", "graphjin", "graphjin", "catalog", "read",
			"Catalog read access",
			"Controls whether the GraphJin catalog source is exposed for discovery.",
			defaultAllow(mode, true, false, true), catalogEnabled,
			"sources[].catalog", fmt.Sprint(catalogEnabled),
			catalogEnabled, "medium",
			"The catalog is safe for trusted agents but may expose schema context in strict production.",
			"Disable the graphjin catalog source in strict prod unless trusted discovery is required."),
		newSecurityPolicy(mode, "serve.config_write", "serve", "graphjin", "graphjin", "config", "write",
			"Config writes",
			"Controls gj_config mutations that update GraphJin configuration.",
			defaultAllow(mode, true, false, false), graphjinControlPlane && !configReadOnly && conf.MCP.AllowConfigUpdates,
			"mcp.allow_config_updates", fmt.Sprint(conf != nil && conf.MCP.AllowConfigUpdates),
			mcpBoolExplicit(conf, "mcp.allow_config_updates", conf != nil && conf.MCP.AllowConfigUpdates), "high",
			"Config writes can alter databases, roles, and control-plane permissions.",
			"Only enable mcp.allow_config_updates in trusted sessions and keep the graphjin source read_only when writes are not intended."),
		newSecurityPolicy(mode, "serve.workflow_write", "serve", "workflows", "workflows", "workflow", "write",
			"Workflow writes",
			"Controls gj_workflow mutations that create, update, or delete saved workflows.",
			defaultAllow(mode, true, false, false), workflowsEnabled && !workflowReadOnly && conf.MCP.AllowWorkflowUpdates,
			"mcp.allow_workflow_updates", fmt.Sprint(conf != nil && conf.MCP.AllowWorkflowUpdates),
			mcpBoolExplicit(conf, "mcp.allow_workflow_updates", conf != nil && conf.MCP.AllowWorkflowUpdates), "high",
			"Workflow writes can persist code that runs inside GraphJin.",
			"Enable workflow writes only for trusted agents, review saved workflow code, or mark the workflows source or gj_workflow table read_only."),
		newSecurityPolicy(mode, "serve.workflow_execute", "serve", "workflows", "workflows", "workflow", "execute",
			"Workflow execution",
			"Controls gj_workflow_execution mutations that execute saved workflows.",
			defaultAllow(mode, true, false, true), workflowsEnabled && !workflowExecutionReadOnly,
			"tables[].read_only", fmt.Sprint(workflowExecutionReadOnly),
			controlPlaneTableReadOnlyExplicit(conf, "", "gj_workflow_execution"), "high",
			"Workflow execution can run code and access configured data sources.",
			"Keep workflow execution limited to trusted deployments; mark the workflows source or gj_workflow_execution table read_only to block execution."),
		newSecurityPolicy(mode, "serve.legacy_execute_workflow_tool", "serve", "mcp", "mcp", "legacy_workflow_execution", "execute",
			"Legacy MCP workflow execution tool",
			"Controls the execute_workflow MCP compatibility tool. GraphQL gj_workflow_execution is controlled by read_only table/source policy.",
			defaultAllow(mode, true, false, false), mcpEnabled && conf != nil && conf.legacyMCPToolsEnabled() && conf.MCP.AllowWorkflowExecution,
			"mcp.allow_workflow_execution", fmt.Sprint(conf != nil && conf.MCP.AllowWorkflowExecution),
			mcpBoolExplicit(conf, "mcp.allow_workflow_execution", conf != nil && conf.MCP.AllowWorkflowExecution), "medium",
			"Legacy MCP execution is a compatibility surface; prefer catalog-discovered GraphQL control-plane mutations.",
			"Keep mcp.legacy_discovery or mcp.allow_workflow_execution disabled unless a legacy MCP client requires execute_workflow."),
		newSecurityPolicy(mode, "serve.schema_reload", "serve", "graphjin", "graphjin", "schema", "reload",
			"Schema reload",
			"Controls MCP schema reload operations.",
			defaultAllow(mode, true, false, true), mcpEnabled && conf != nil && conf.MCP.AllowSchemaReload,
			"mcp.allow_schema_reload", fmt.Sprint(conf != nil && conf.MCP.AllowSchemaReload),
			mcpBoolExplicit(conf, "mcp.allow_schema_reload", conf != nil && conf.MCP.AllowSchemaReload), "medium",
			"Schema reload changes discovery state and should be explicit outside development.",
			"Enable schema reload only when a trusted operator or agent needs fresh metadata."),
		newSecurityPolicy(mode, "serve.schema_write", "serve", "graphjin", "graphjin", "schema", "write",
			"Schema writes",
			"Controls MCP schema update operations that can apply DDL.",
			defaultAllow(mode, true, false, false), mcpEnabled && conf != nil && conf.MCP.AllowSchemaUpdates,
			"mcp.allow_schema_updates", fmt.Sprint(conf != nil && conf.MCP.AllowSchemaUpdates),
			mcpBoolExplicit(conf, "mcp.allow_schema_updates", conf != nil && conf.MCP.AllowSchemaUpdates), "high",
			"Schema writes can alter application databases.",
			"Only enable mcp.allow_schema_updates for trusted migration sessions and prefer preview before apply."),
		newSecurityPolicy(mode, "serve.dev_tools", "serve", "mcp", "mcp", "dev_tools", "read",
			"Development tools",
			"Controls advanced MCP tools that expose SQL, relationship graphs, and role permissions.",
			defaultAllow(mode, true, false, false), mcpEnabled && conf != nil && conf.MCP.AllowDevTools,
			"mcp.allow_dev_tools", fmt.Sprint(conf != nil && conf.MCP.AllowDevTools),
			mcpBoolExplicit(conf, "mcp.allow_dev_tools", conf != nil && conf.MCP.AllowDevTools), "medium",
			"Development tools may reveal operational details useful for debugging and auditing.",
			"Keep dev tools disabled in prod unless a trusted audit workflow needs them."),
		newSecurityPolicy(mode, "serve.raw_queries", "serve", "mcp", "mcp", "raw_graphql", "query",
			"MCP raw GraphQL queries",
			"Controls compatibility MCP tools that submit arbitrary GraphQL text.",
			defaultAllow(mode, true, false, false), mcpEnabled && conf != nil && conf.MCP.AllowRawQueries,
			"mcp.allow_raw_queries", fmt.Sprint(conf != nil && conf.MCP.AllowRawQueries),
			mcpBoolExplicit(conf, "mcp.allow_raw_queries", conf != nil && conf.MCP.AllowRawQueries), "medium",
			"Raw GraphQL should not bypass production allow-list expectations.",
			"Prefer catalog-guided saved workflows or production allow-listed operations."),
	}

	rows = append(rows, securitySourcePolicyEvaluations(conf, mode)...)
	for i := range rows {
		rows[i].WeakensDefault = securityWeakensDefault(rows[i])
		rows[i].ReadOnly = rows[i].Effective == securityEffectiveReadOnly
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
		kind := strings.ToLower(strings.TrimSpace(source.Kind))
		switch kind {
		case "codesql", "filesystem":
		default:
			continue
		}
		name := strings.TrimSpace(source.Name)
		if name == "" {
			name = kind
		}
		id := "source." + securityIDPart(name) + ".write"
		rows = append(rows, newSecurityPolicy(mode, id, "core", name, kind, kind, "write",
			fmt.Sprintf("%s source writes", name),
			fmt.Sprintf("Controls write/watch behavior for the %s source.", kind),
			defaultAllow(mode, true, false, false), !source.ReadOnly,
			"sources[].read_only", fmt.Sprint(source.ReadOnly),
			true, "high",
			"Code and filesystem sources are trusted inputs and should be read-only outside development.",
			"Set read_only: true on production and agentic code/filesystem sources unless writes are explicitly required."))
	}
	if conf.Core.SourceMode() {
		return rows
	}
	for name, dbConf := range conf.Core.Databases {
		if !strings.EqualFold(dbConf.Type, "codesql") {
			continue
		}
		id := "database." + securityIDPart(name) + ".write"
		rows = append(rows, newSecurityPolicy(mode, id, "core", name, "codesql", "codesql", "write",
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
		RiskSeverity:     riskSeverity,
		Reason:           reason,
		Recommendation:   recommendation,
	}
}

func defaultAllow(mode string, dev, prod, agentic bool) bool {
	switch mode {
	case securityModeProd:
		return prod
	case securityModeAgentic:
		return agentic
	default:
		return dev
	}
}

func mcpBoolExplicit(conf *Config, key string, value bool) bool {
	if value {
		return true
	}
	return conf != nil && conf.viper != nil && conf.viper.IsSet(key)
}

func securityWeakensDefault(row securityPolicyEval) bool {
	if !row.DefaultAllowed && row.EffectiveAllowed {
		return true
	}
	return row.DefaultEffective == securityEffectiveReadOnly && row.Effective == securityEffectiveReadWrite
}

func securityEvidence(row securityPolicyEval, production, prodSecurity bool) map[string]any {
	return map[string]any{
		"mode":              row.Mode,
		"layer":             row.Layer,
		"source":            row.Source,
		"source_kind":       row.SourceKind,
		"capability":        row.Capability,
		"action":            row.Action,
		"production":        production,
		"production_secure": prodSecurity,
		"override_key":      row.OverrideKey,
		"override_value":    row.OverrideValue,
		"override_explicit": row.OverrideExplicit,
	}
}

func securityPolicyDetails(row securityPolicyEval) map[string]any {
	return map[string]any{
		"title":             row.Title,
		"summary":           row.Summary,
		"default_effective": row.DefaultEffective,
		"effective":         row.Effective,
		"reason":            row.Reason,
		"recommendation":    row.Recommendation,
	}
}

func securityPolicyNanoRow(policy securityPolicyEval, now string) core.NanoRow {
	row := core.NanoRow{
		"id":                policy.ID,
		"kind":              securityKindPolicy,
		"report":            securityKindPolicy,
		"mode":              policy.Mode,
		"layer":             policy.Layer,
		"source":            policy.Source,
		"source_kind":       policy.SourceKind,
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
		"weakens_default":   policy.WeakensDefault,
		"read_only":         policy.ReadOnly,
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

func securityFindingNanoRows(conf *Config, policies []securityPolicyEval, mode, now string) []core.NanoRow {
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
			"finding:"+securityIDPart(severity)+":"+strings.TrimPrefix(policy.ID, "policy:"),
			mode,
			severity,
			policy.Title,
			policy.Reason,
			policy.Recommendation,
			policy,
			now,
		))
	}
	if mode == securityModeAgentic && conf != nil && !conf.Serv.Production && !conf.Core.Production {
		policy := securityPolicyEval{
			ID:             "policy:core.agentic_requires_production",
			Layer:          "core",
			Source:         "core",
			SourceKind:     "engine",
			Capability:     "production_security",
			Action:         "enforce",
			Title:          "Agentic mode without production",
			Mode:           mode,
			Reason:         "Agentic mode is intended to run with production data protections enabled.",
			Recommendation: "Set production: true when security_mode is agentic.",
			Evidence: map[string]any{
				"mode":       mode,
				"production": false,
			},
		}
		rows = append(rows, securityFindingNanoRow("finding:critical:core.agentic_requires_production", mode, "critical", policy.Title, policy.Reason, policy.Recommendation, policy, now))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return fmt.Sprint(rows[i]["id"]) < fmt.Sprint(rows[j]["id"])
	})
	return rows
}

func securityFindingNanoRow(id, mode, severity, title, reason, recommendation string, policy securityPolicyEval, now string) core.NanoRow {
	row := core.NanoRow{
		"id":                id,
		"kind":              securityKindFinding,
		"report":            securityKindFinding,
		"mode":              mode,
		"layer":             policy.Layer,
		"source":            policy.Source,
		"source_kind":       policy.SourceKind,
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
		"weakens_default":   true,
		"read_only":         policy.ReadOnly,
		"severity":          severity,
		"severity_rank":     securitySeverityRank(severity),
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

func securitySummaryNanoRow(conf *Config, mode string, policyCount int, findings []core.NanoRow, now string) core.NanoRow {
	counts := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}
	for _, finding := range findings {
		severity := fmt.Sprint(finding["severity"])
		if _, ok := counts[severity]; !ok {
			counts[severity] = 0
		}
		counts[severity]++
	}
	production := conf != nil && (conf.Serv.Production || conf.Core.Production)
	row := core.NanoRow{
		"id":      "summary",
		"kind":    securityKindSummary,
		"report":  securityKindSummary,
		"mode":    mode,
		"layer":   "system",
		"source":  "graphjin",
		"title":   "GraphJin security summary",
		"summary": "Effective security posture for GraphJin core, service, and agent-facing control-plane surfaces.",
		"summary_json": map[string]any{
			"mode":         mode,
			"production":   production,
			"policy_rows":  policyCount,
			"finding_rows": len(findings),
			"findings":     counts,
			"generated_at": now,
		},
		"details_json": securitySummaryDetails(),
		"examples_json": []map[string]string{
			{"name": "summary", "query": `query { gj_security(id: "summary") { id kind mode summary_json } }`},
			{"name": "high critical findings", "query": `query { gj_security(where: { kind: { eq: "finding" }, severity: { in: ["high", "critical"] } }, order_by: { severity_rank: desc }) { id severity title recommendation evidence_json } }`},
			{"name": "effective policy", "query": `query { gj_security(where: { kind: { eq: "policy" } }) { id mode capability action default_effective effective weakens_default } }`},
		},
		"safety_json": map[string]any{
			"read_only": true,
			"note":      "gj_security is evidence for audit and planning; change enforcement through normal config and source permissions.",
		},
		"created_at":  now,
		"updated_at":  now,
		"search_rank": 0,
	}
	row["search_vector"] = securitySearchVector(row)
	return row
}

func securitySummaryDetails() map[string]any {
	return map[string]any{
		"modes": map[string]string{
			securityModeDev:     "Development defaults favor iteration and discovery.",
			securityModeProd:    "Production defaults enforce allow-lists and block agent/system write surfaces unless explicitly opened.",
			securityModeAgentic: "Governed production for trusted agents: app-data production protections stay on while selected control-plane reads/execution can be available.",
		},
		"kinds": []string{securityKindSummary, securityKindPolicy, securityKindFinding},
		"columns": map[string]string{
			"effective":         "The resolved current behavior.",
			"default_effective": "The secure default for the active security mode.",
			"weakens_default":   "True when current config is more permissive than the secure default.",
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
		fmt.Sprint(row["mode"]),
		fmt.Sprint(row["layer"]),
		fmt.Sprint(row["source"]),
		fmt.Sprint(row["source_kind"]),
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
