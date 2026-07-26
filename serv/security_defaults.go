package serv

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/core/v3/featurecap"
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
	case "gj_watch", "gj_watch_event":
		return configWatchesEnabled(conf) && conf.Core.Artifacts.Source != "" && conf.artifactSourceReadOnly()
	case "gj_task", "gj_task_entry":
		return configTasksEnabled(conf) && conf.Core.Artifacts.Source != "" && conf.artifactSourceReadOnly()
	case "gj_artifacts":
		return conf.Core.Artifacts.Enabled && conf.Core.Artifacts.Source != "" && conf.artifactSourceReadOnly()
	case "gj_catalog", "gj_security", "gj_runtime":
		return true
	default:
		return false
	}
}

func defaultControlPlaneTableReadOnly(conf *Config, table string) bool {
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "gj_watch", "gj_watch_event":
		return !configWatchesEnabled(conf)
	case "gj_task", "gj_task_entry":
		return !configTasksEnabled(conf)
	case "gj_workflow":
		return !conf.effectiveFeatureCapability(featurecap.KindWorkflows, featurecap.KeyWorkflowWrite)
	case "gj_workflow_execution":
		return !conf.effectiveFeatureCapability(featurecap.KindWorkflows, featurecap.KeyWorkflowExecute)
	case "gj_config":
		return !conf.effectiveFeatureCapability(featurecap.KindSystem, featurecap.KeyConfigWrite)
	case "gj_catalog", "gj_security", "gj_runtime":
		return true
	default:
		return false
	}
}

func applySystemRoleQueryDefaults(conf *Config, runtimeCore *core.Config, database string) {
	if conf == nil || runtimeCore == nil {
		return
	}
	mode := effectiveMode(conf)

	systemTables := registeredSystemRoots(conf)
	roles := []string{"user", "anon"}
	if conf.Core.IsSourcesUsed() {
		roles = sourceModeSystemRoleNames(conf)
	}
	for _, role := range roles {
		for _, table := range systemTables {
			if conf.Core.IsSourcesUsed() {
				if systemRoleTableConfigured(&conf.Core, role, table, database) {
					continue
				}
				queryBlock := !systemRootAllowed(conf, table, systemActionRead, role)
				rt := core.RoleTable{
					Name:     table,
					Database: database,
					Query:    &core.Query{Block: queryBlock},
					Insert:   &core.Insert{Block: !systemRootAllowed(conf, table, systemActionInsert, role)},
					Update:   &core.Update{Block: !systemRootAllowed(conf, table, systemActionUpdate, role)},
					Upsert:   &core.Upsert{Block: !systemRootAllowed(conf, table, systemActionUpdate, role)},
					Delete:   &core.Delete{Block: !systemRootAllowed(conf, table, systemActionDelete, role)},
				}
				applyArtifactRoleProjectionDefaults(conf, role, &rt, queryBlock)
				applyWatchRoleProjectionDefaults(conf, role, &rt, queryBlock)
				applyTaskRoleProjectionDefaults(conf, role, &rt, queryBlock)
				appendRuntimeRoleTable(runtimeCore, role, rt)
				continue
			}
			allowed := systemReadAllowedBySource(conf, mode, role, table)
			if allowed {
				if (strings.EqualFold(table, "gj_artifacts") || strings.EqualFold(table, "gj_watch") || strings.EqualFold(table, "gj_watch_event") || strings.EqualFold(table, "gj_task") || strings.EqualFold(table, "gj_task_entry")) && !systemRoleTableConfigured(&conf.Core, role, table, database) {
					rt := core.RoleTable{
						Name:     table,
						Database: database,
						Query:    &core.Query{Block: false},
						Insert:   &core.Insert{Block: false},
						Update:   &core.Update{Block: false},
						Upsert:   &core.Upsert{Block: false},
						Delete:   &core.Delete{Block: false},
					}
					applyArtifactRoleProjectionDefaults(conf, role, &rt, false)
					applyWatchRoleProjectionDefaults(conf, role, &rt, false)
					applyTaskRoleProjectionDefaults(conf, role, &rt, false)
					appendRuntimeRoleTable(runtimeCore, role, rt)
				}
				continue
			}
			if systemRoleTableConfigured(&conf.Core, role, table, database) {
				continue
			}
			rt := core.RoleTable{Name: table, Database: database, Generated: true, Query: &core.Query{Block: true}}
			if role == "anon" {
				insertBlock := true
				if strings.EqualFold(table, "gj_workflow_execution") {
					insertBlock = !securityWorkflowExecutionInsertAllowed(conf, mode, role, conf.workflowsEnabled(), controlPlaneTableReadOnly(conf, database, table))
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

func applyArtifactRoleProjectionDefaults(conf *Config, role string, rt *core.RoleTable, block bool) {
	if rt == nil || !strings.EqualFold(rt.Name, "gj_artifacts") || block || artifactRoleIsAdmin(conf, role) {
		return
	}
	if rt.Query == nil {
		rt.Query = &core.Query{}
	}
	rt.Query.Columns = artifactPublicProjectionColumns()
	if strings.EqualFold(role, "anon") {
		rt.Query.Filters = []string{`{ visibility: { eq: "global" } }`}
		rt.Insert = &core.Insert{Block: true}
		rt.Update = &core.Update{Block: true}
		rt.Upsert = &core.Upsert{Block: true}
		rt.Delete = &core.Delete{Block: true}
		return
	}
	rt.Query.Filters = []string{`{ or: { owner_ref: { eq: $user_ref }, visibility: { eq: "global" } } }`}
}

func artifactPublicProjectionColumns() []string {
	return []string{
		"id",
		"name",
		"kind",
		"path",
		"source",
		"visibility",
		"read_only",
		"content",
		"content_truncated",
		"content_json",
		"metadata_json",
		"content_hash",
		"status",
		"revision",
		"created_at",
		"updated_at",
	}
}

func applyWatchRoleProjectionDefaults(conf *Config, role string, rt *core.RoleTable, block bool) {
	if rt == nil || block || artifactRoleIsAdmin(conf, role) {
		return
	}
	switch strings.ToLower(strings.TrimSpace(rt.Name)) {
	case "gj_watch":
		rt.Query = watchOwnerQuery(rt.Query, watchPublicProjectionColumns())
	case "gj_watch_event":
		rt.Query = watchOwnerQuery(rt.Query, watchEventPublicProjectionColumns())
	default:
		return
	}
	if strings.EqualFold(role, "anon") {
		rt.Insert = &core.Insert{Block: true}
		rt.Update = &core.Update{Block: true}
		rt.Upsert = &core.Upsert{Block: true}
		rt.Delete = &core.Delete{Block: true}
	}
}

func watchOwnerQuery(query *core.Query, columns []string) *core.Query {
	if query == nil {
		query = &core.Query{}
	}
	query.Columns = columns
	query.Filters = []string{`{ owner_ref: { eq: $user_ref } }`}
	return query
}

func watchPublicProjectionColumns() []string {
	return []string{
		"id",
		"name",
		"task_id",
		"description",
		"query",
		"saved_query_name",
		"variables_json",
		"condition_js",
		"delivery_json",
		"enrich_json",
		"evidence_json",
		"lifecycle",
		"lease_expires_at",
		"status",
		"approval",
		"enabled",
		"last_data_hash",
		"last_fired_at",
		"last_error",
		"failure_count",
		"created_at",
		"updated_at",
	}
}

func watchEventPublicProjectionColumns() []string {
	return []string{
		"id",
		"watch_id",
		"data_hash",
		"data_json",
		"data_truncated",
		"evidence_json",
		"delivery_status",
		"delivery_attempts",
		"delivery_json",
		"receipt_json",
		"enrichment_json",
		"seen",
		"seen_at",
		"created_at",
		"updated_at",
	}
}

func applyTaskRoleProjectionDefaults(conf *Config, role string, rt *core.RoleTable, block bool) {
	if rt == nil || block || artifactRoleIsAdmin(conf, role) {
		return
	}
	switch strings.ToLower(strings.TrimSpace(rt.Name)) {
	case "gj_task":
		rt.Query = watchOwnerQuery(rt.Query, taskPublicProjectionColumns())
	case "gj_task_entry":
		rt.Query = watchOwnerQuery(rt.Query, taskEntryPublicProjectionColumns())
	default:
		return
	}
	if strings.EqualFold(role, "anon") {
		rt.Insert = &core.Insert{Block: true}
		rt.Update = &core.Update{Block: true}
		rt.Upsert = &core.Upsert{Block: true}
		rt.Delete = &core.Delete{Block: true}
	}
}

func taskPublicProjectionColumns() []string {
	return []string{
		"id", "goal", "status", "outcome", "snapshot_json", "last_entry_at",
		"created_at", "updated_at", "closed_at",
	}
}

func taskEntryPublicProjectionColumns() []string {
	return []string{
		"id", "task_id", "origin", "body", "detail_json", "status", "trace_id",
		"watch_id", "created_at", "updated_at",
	}
}

func artifactRoleIsAdmin(conf *Config, role string) bool {
	if conf == nil {
		return false
	}
	for _, admin := range conf.Core.EffectiveIdentityConfig().AdminRoles {
		if strings.EqualFold(strings.TrimSpace(role), strings.TrimSpace(admin)) {
			return true
		}
	}
	return false
}

func assertWatchNanoRoleDefaults(conf *Config, runtimeCore *core.Config, database string) error {
	if conf == nil || runtimeCore == nil || !configWatchesEnabled(conf) {
		return nil
	}
	roles := []string{"user", "anon"}
	if conf.Core.IsSourcesUsed() {
		roles = sourceModeSystemRoleNames(conf)
	}
	for _, role := range roles {
		if artifactRoleIsAdmin(conf, role) {
			continue
		}
		for _, table := range []string{"gj_watch", "gj_watch_event"} {
			if !systemReadAllowedBySource(conf, effectiveMode(conf), role, table) {
				continue
			}
			rt := runtimeRoleTable(runtimeCore, role, table, database)
			if rt == nil || rt.Query == nil {
				return fmt.Errorf("%s projection requires role filters for %q", table, role)
			}
			if !watchRoleQueryHasPrivacy(rt.Query) {
				return fmt.Errorf("%s projection role %q is missing owner filters or raw-id column restrictions", table, role)
			}
		}
	}
	return nil
}

func assertTaskNanoRoleDefaults(conf *Config, runtimeCore *core.Config, database string) error {
	if conf == nil || runtimeCore == nil || !configTasksEnabled(conf) {
		return nil
	}
	roles := []string{"user", "anon"}
	if conf.Core.IsSourcesUsed() {
		roles = sourceModeSystemRoleNames(conf)
	}
	for _, role := range roles {
		if artifactRoleIsAdmin(conf, role) {
			continue
		}
		for _, table := range []string{"gj_task", "gj_task_entry"} {
			if !systemReadAllowedBySource(conf, effectiveMode(conf), role, table) {
				continue
			}
			rt := runtimeRoleTable(runtimeCore, role, table, database)
			if rt == nil || rt.Query == nil {
				return fmt.Errorf("%s projection requires role filters for %q", table, role)
			}
			if !watchRoleQueryHasPrivacy(rt.Query) {
				return fmt.Errorf("%s projection role %q is missing owner filters or raw-id column restrictions", table, role)
			}
		}
	}
	return nil
}

func assertArtifactNanoRoleDefaults(conf *Config, runtimeCore *core.Config, database string) error {
	if conf == nil || runtimeCore == nil || !conf.Core.Artifacts.Enabled {
		return nil
	}
	roles := []string{"user", "anon"}
	if conf.Core.IsSourcesUsed() {
		roles = sourceModeSystemRoleNames(conf)
	}
	for _, role := range roles {
		if artifactRoleIsAdmin(conf, role) || !systemReadAllowedBySource(conf, effectiveMode(conf), role, "gj_artifacts") {
			continue
		}
		rt := runtimeRoleTable(runtimeCore, role, "gj_artifacts", database)
		if rt == nil || rt.Query == nil {
			return fmt.Errorf("gj_artifacts projection requires role filters for %q", role)
		}
		if !artifactRoleQueryHasPrivacy(rt.Query, role) {
			return fmt.Errorf("gj_artifacts projection role %q is missing owner/global filters or raw-id column restrictions", role)
		}
	}
	return nil
}

func runtimeRoleTable(conf *core.Config, role, table, database string) *core.RoleTable {
	if conf == nil {
		return nil
	}
	for i := range conf.Roles {
		if !strings.EqualFold(conf.Roles[i].Name, role) {
			continue
		}
		for j := range conf.Roles[i].Tables {
			rt := &conf.Roles[i].Tables[j]
			if rt.Database != "" && database != "" && !strings.EqualFold(rt.Database, database) {
				continue
			}
			if strings.EqualFold(rt.Name, table) {
				return rt
			}
		}
	}
	return nil
}

func watchRoleQueryHasPrivacy(query *core.Query) bool {
	if query == nil || query.Block {
		return false
	}
	hasFilter := false
	for _, filter := range query.Filters {
		f := strings.ToLower(strings.ReplaceAll(filter, " ", ""))
		if strings.Contains(f, "owner_ref:{eq:$user_ref}") {
			hasFilter = true
		}
	}
	if !hasFilter || len(query.Columns) == 0 {
		return false
	}
	for _, col := range query.Columns {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "owner_id", "owner_role", "account_id", "owner_ref", "account_ref":
			return false
		}
	}
	return true
}

func artifactRoleQueryHasPrivacy(query *core.Query, role string) bool {
	if query == nil || query.Block {
		return false
	}
	hasFilter := false
	for _, filter := range query.Filters {
		f := strings.ToLower(strings.ReplaceAll(filter, " ", ""))
		if strings.EqualFold(role, "anon") {
			if strings.Contains(f, "visibility:{eq:\"global\"}") {
				hasFilter = true
			}
			continue
		}
		if strings.Contains(f, "owner_ref:{eq:$user_ref}") && strings.Contains(f, "visibility:{eq:\"global\"}") {
			hasFilter = true
		}
	}
	if !hasFilter {
		return false
	}
	if len(query.Columns) == 0 {
		return false
	}
	for _, col := range query.Columns {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "owner_id", "account_id", "owner_ref", "account_ref":
			return false
		}
	}
	return true
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
	return systemRootAllowed(conf, table, systemActionRead, role)
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
		return conf.catalogToolsEnabled() || conf.systemControlPlaneEnabled()
	case "gj_runtime":
		return conf != nil && conf.runtimeRootRegistered()
	case "gj_artifacts":
		return conf != nil && conf.Core.Artifacts.Enabled
	case "gj_watch", "gj_watch_event":
		return configWatchesEnabled(conf)
	case "gj_task", "gj_task_entry":
		return configTasksEnabled(conf)
	case "gj_workflow", "gj_workflow_execution":
		return conf.workflowsEnabled()
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
				return !rt.Generated
			}
		}
	}
	return false
}

func appendRuntimeRoleTable(conf *core.Config, role string, table core.RoleTable) {
	table.Generated = true
	for i := range conf.Roles {
		if strings.EqualFold(conf.Roles[i].Name, role) {
			for j, rt := range conf.Roles[i].Tables {
				if rt.Database != "" && table.Database != "" && !strings.EqualFold(rt.Database, table.Database) {
					continue
				}
				if !strings.EqualFold(rt.Name, table.Name) {
					continue
				}
				if rt.Generated {
					conf.Roles[i].Tables[j] = table
				}
				return
			}
			conf.Roles[i].Tables = append(conf.Roles[i].Tables, table)
			return
		}
	}
	conf.Roles = append(conf.Roles, core.Role{Name: role, Tables: []core.RoleTable{table}})
}
