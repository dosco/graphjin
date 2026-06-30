package serv

import (
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func controlPlaneTableReadOnly(conf *Config, database, table string) bool {
	if conf == nil {
		return true
	}
	if controlPlaneSourceReadOnly(conf, table) {
		return true
	}
	if readOnly, ok := configuredControlPlaneTableReadOnly(&conf.Core, database, table); ok {
		return readOnly
	}
	return defaultControlPlaneTableReadOnly(conf, table)
}

func configuredControlPlaneTableReadOnly(conf *core.Config, database, table string) (bool, bool) {
	if conf == nil {
		return false, false
	}
	for _, confTable := range conf.Tables {
		if confTable.Database != "" && database != "" && confTable.Database != database {
			continue
		}
		if strings.EqualFold(confTable.Name, table) || strings.EqualFold(confTable.Table, table) {
			return confTable.ReadOnly, true
		}
	}
	return false, false
}

func controlPlaneTableReadOnlyExplicit(conf *Config, database, table string) bool {
	if conf == nil {
		return false
	}
	if controlPlaneSourceReadOnly(conf, table) {
		return true
	}
	_, ok := configuredControlPlaneTableReadOnly(&conf.Core, database, table)
	return ok
}

func controlPlaneSourceReadOnly(conf *Config, table string) bool {
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_workflow", "gj_workflow_execution":
		return conf.workflowsSourceReadOnly()
	case "gj_artifacts":
		return conf.Core.Artifacts.Enabled && conf.Core.Artifacts.Source != "" && conf.artifactSourceReadOnly()
	case "gj_config":
		return conf.graphjinSourceReadOnly()
	case "gj_catalog", "gj_security", "gj_runtime":
		return true
	default:
		return false
	}
}

func defaultControlPlaneTableReadOnly(conf *Config, table string) bool {
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_workflow":
		return !sourceCapabilityAllowed(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowWrite)
	case "gj_workflow_execution":
		return !sourceCapabilityAllowed(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowExecute)
	case "gj_config":
		return !sourceCapabilityAllowed(conf, sourcecap.KindGraphJin, sourcecap.KeyConfigWrite)
	case "gj_catalog", "gj_security", "gj_runtime":
		return true
	default:
		return false
	}
}

func sourceCapabilityAllowed(conf *Config, kind, capability string) bool {
	if conf == nil {
		return false
	}
	if source, ok := conf.sourceByCanonicalKind(kind); ok {
		value, _ := conf.sourceCapabilityForSource(source, capability)
		return value
	}
	return sourceCapabilityDefault(effectiveMode(conf), kind, capability)
}

func applySystemRoleQueryDefaults(conf *Config, runtimeCore *core.Config, database string) {
	if conf == nil || runtimeCore == nil {
		return
	}
	mode := effectiveMode(conf)

	systemTables := []string{"gj_catalog", "gj_security", "gj_config", "gj_workflow", "gj_workflow_execution"}
	if conf.runtimeRootRegistered() {
		systemTables = append(systemTables, "gj_runtime")
	}
	if conf.Core.Artifacts.Enabled {
		systemTables = append(systemTables, "gj_artifacts")
	}
	roles := []string{"user", "anon"}
	if conf.Core.IsSourcesUsed() {
		roles = sourceModeSystemRoleNames(conf)
	}
	for _, role := range roles {
		for _, table := range systemTables {
			allowed := systemReadAllowedBySource(conf, mode, role, table)
			if conf.Core.IsSourcesUsed() {
				if systemRoleTableConfigured(runtimeCore, role, table, database) {
					continue
				}
				block := !allowed
				appendRuntimeRoleTable(runtimeCore, role, core.RoleTable{
					Name:     table,
					Database: database,
					Query:    &core.Query{Block: block},
					Insert:   &core.Insert{Block: block},
					Update:   &core.Update{Block: block},
					Upsert:   &core.Upsert{Block: block},
					Delete:   &core.Delete{Block: block},
				})
				continue
			}
			if allowed {
				continue
			}
			if systemRoleTableConfigured(&conf.Core, role, table, database) {
				continue
			}
			rt := core.RoleTable{Name: table, Database: database, Generated: true, Query: &core.Query{Block: true}}
			if role == "anon" {
				insertBlock := true
				if strings.EqualFold(table, "gj_workflow_execution") {
					insertBlock = !securityWorkflowExecutionInsertAllowed(conf, mode, role, conf.workflowsSourceEnabled(), controlPlaneTableReadOnly(conf, database, table))
				}
				rt.Insert = &core.Insert{Block: insertBlock}
				rt.Update = &core.Update{Block: true}
				rt.Upsert = &core.Upsert{Block: true}
				rt.Delete = &core.Delete{Block: true}
			} else {
				rt.Insert = &core.Insert{Block: false}
				rt.Update = &core.Update{Block: false}
				rt.Upsert = &core.Upsert{Block: false}
				rt.Delete = &core.Delete{Block: false}
			}
			appendRuntimeRoleTable(runtimeCore, role, rt)
		}
	}
}

func sourceModeSystemRoleNames(conf *Config) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(role string) {
		role = strings.TrimSpace(role)
		if role == "" {
			return
		}
		key := strings.ToLower(role)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, role)
	}
	add("user")
	add("anon")
	if conf == nil {
		return out
	}
	for _, role := range conf.Core.Roles {
		add(role.Name)
	}
	for _, role := range conf.Core.EffectiveIdentityConfig().AdminRoles {
		add(role)
	}
	return out
}

func systemReadAllowedBySource(conf *Config, mode, role, table string) bool {
	if !systemReadSourceEnabled(conf, table) {
		return false
	}
	if conf != nil && conf.Core.IsSourcesUsed() {
		if source, ok := conf.Core.GraphJinSource(); ok {
			access := conf.Core.EffectiveSourceAccess(source)
			if rootMode := access.Roots[strings.ToLower(strings.TrimSpace(table))]; rootMode != "" {
				return systemRootAccessAllowed(rootMode, role, conf.Core.EffectiveIdentityConfig().AdminRoles, mode, conf.DefaultBlock)
			}
		}
	}
	if role == "anon" {
		return mode == modeDev && conf != nil && !conf.DefaultBlock && defaultSystemReadAllowed(mode, "user", table)
	}
	if role != "user" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_catalog":
		return sourceCapabilityAllowed(conf, sourcecap.KindGraphJin, sourcecap.KeyCatalogRead)
	case "gj_security":
		return sourceCapabilityAllowed(conf, sourcecap.KindGraphJin, sourcecap.KeySecurityRead)
	case "gj_config":
		return sourceCapabilityAllowed(conf, sourcecap.KindGraphJin, sourcecap.KeyConfigRead)
	case "gj_runtime":
		return sourceCapabilityAllowed(conf, sourcecap.KindGraphJin, sourcecap.KeyRuntimeRead)
	case "gj_artifacts":
		return conf.Core.Artifacts.Enabled
	case "gj_workflow":
		return sourceCapabilityAllowed(conf, sourcecap.KindWorkflow, sourcecap.KeyWorkflowRead)
	case "gj_workflow_execution":
		return defaultSystemReadAllowed(mode, role, table)
	default:
		return false
	}
}

func systemRootAccessAllowed(accessMode, role string, adminRoles []string, mode string, defaultBlock bool) bool {
	switch strings.ToLower(strings.TrimSpace(accessMode)) {
	case core.AccessModePublic:
		return true
	case core.AccessModeAuthenticated, core.AccessModeAccount, core.AccessModeOwner:
		return !strings.EqualFold(role, "anon")
	case core.AccessModeAdmin:
		for _, admin := range adminRoles {
			if strings.EqualFold(strings.TrimSpace(admin), role) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func systemReadSourceEnabled(conf *Config, table string) bool {
	if conf == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_catalog", "gj_security", "gj_config":
		return conf.catalogToolsEnabled() || conf.graphjinControlPlaneEnabled()
	case "gj_runtime":
		return conf != nil && conf.runtimeRootRegistered()
	case "gj_artifacts":
		return conf != nil && conf.Core.Artifacts.Enabled
	case "gj_workflow", "gj_workflow_execution":
		return conf.workflowsSourceEnabled()
	default:
		return false
	}
}

func systemRoleTableConfigured(conf *core.Config, role, table, database string) bool {
	if conf == nil {
		return false
	}
	for _, r := range conf.Roles {
		if !strings.EqualFold(r.Name, role) {
			continue
		}
		for _, rt := range r.Tables {
			if rt.Database != "" && database != "" && !strings.EqualFold(rt.Database, database) {
				continue
			}
			if strings.EqualFold(rt.Name, table) {
				return true
			}
		}
	}
	return false
}

func appendRuntimeRoleTable(conf *core.Config, role string, table core.RoleTable) {
	table.Generated = true
	for i := range conf.Roles {
		if strings.EqualFold(conf.Roles[i].Name, role) {
			if !runtimeRoleTableExists(conf.Roles[i], table.Name, table.Database) {
				conf.Roles[i].Tables = append(conf.Roles[i].Tables, table)
			}
			return
		}
	}
	conf.Roles = append(conf.Roles, core.Role{Name: role, Tables: []core.RoleTable{table}})
}

func runtimeRoleTableExists(role core.Role, table, database string) bool {
	for _, rt := range role.Tables {
		if rt.Database != "" && database != "" && !strings.EqualFold(rt.Database, database) {
			continue
		}
		if strings.EqualFold(rt.Name, table) {
			return true
		}
	}
	return false
}
