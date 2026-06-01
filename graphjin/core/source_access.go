package core

import (
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/sdata"
	"github.com/dosco/graphjin/core/v3/sourcecap"
)

func (gj *graphjinEngine) applySourceAccessRules(dbinfo *sdata.DBInfo, database string) error {
	if gj == nil || gj.conf == nil || dbinfo == nil || !gj.conf.IsSourcesUsed() {
		return nil
	}
	source, ok := gj.conf.SourceByName(database)
	if !ok {
		return nil
	}
	kind := source.CanonicalKind()
	if kind != sourcecap.KindDatabase {
		return nil
	}

	access := gj.conf.EffectiveSourceAccess(source)
	roles := sourceAccessRoleNames(gj.conf)
	adminRoles := stringSet(gj.conf.EffectiveIdentityConfig().AdminRoles)

	for i := range dbinfo.Tables {
		table := &dbinfo.Tables[i]
		if table.Type == "remote" || table.Type == "managed" {
			continue
		}
		if gj.isArtifactPhysicalTable(database, table) {
			table.Blocked = true
			continue
		}
		class := classifySourceAccessTable(access, table)
		if class.blocked {
			table.Blocked = true
			continue
		}

		tableRead := class.readMode
		tableWrite := class.writeMode
		tableDelete := class.deleteMode
		tableRead = modeForMissingNamespaceColumn(tableRead, table, access)
		tableWrite = modeForMissingNamespaceColumn(tableWrite, table, access)
		tableDelete = modeForMissingNamespaceColumn(tableDelete, table, access)
		if tableRead == AccessModeOwner && !tableHasColumn(table, access.OwnerColumn) {
			tableRead = AccessModeBlocked
		}
		if tableWrite == AccessModeOwner && !tableHasColumn(table, access.OwnerColumn) {
			tableWrite = AccessModeBlocked
		}
		if tableDelete == AccessModeOwner && !tableHasColumn(table, access.OwnerColumn) {
			tableDelete = AccessModeBlocked
		}

		for _, role := range roles {
			rt, err := sourceAccessRoleTable(role, adminRoles, table, access, tableRead, tableWrite, tableDelete)
			if err != nil {
				return err
			}
			appendRuntimeRoleTableCore(gj.conf, role, rt)
		}
	}
	return nil
}

func modeForMissingNamespaceColumn(mode string, table *sdata.DBTable, access SourceAccessConfig) string {
	if mode != AccessModeAccount || tableHasColumn(table, access.NamespaceColumn) {
		return mode
	}
	if strings.EqualFold(strings.TrimSpace(access.MissingNamespaceColumn), MissingNamespaceBlock) {
		return AccessModeBlocked
	}
	return AccessModeAuthenticated
}

func (gj *graphjinEngine) isArtifactPhysicalTable(database string, table *sdata.DBTable) bool {
	if gj == nil || gj.conf == nil || table == nil || !gj.conf.Artifacts.Enabled {
		return false
	}
	cfg := gj.conf.EffectiveArtifactsConfig()
	if cfg.Source != database {
		return false
	}
	schema := strings.ToLower(strings.TrimSpace(cfg.Schema))
	return (strings.ToLower(table.Schema) == schema &&
		(strings.EqualFold(table.Name, "artifacts") || strings.EqualFold(table.Name, "artifact_revisions"))) ||
		strings.EqualFold(table.Name, schema+"_artifacts") ||
		strings.EqualFold(table.Name, schema+"_artifact_revisions")
}

type tableAccessClass struct {
	readMode   string
	writeMode  string
	deleteMode string
	blocked    bool
}

func classifySourceAccessTable(access SourceAccessConfig, table *sdata.DBTable) tableAccessClass {
	out := tableAccessClass{
		readMode:   normalizeAccessMode(access.Read),
		writeMode:  normalizeAccessMode(access.Write),
		deleteMode: normalizeAccessMode(access.Delete),
	}
	if sourceAccessTableListed(access.BlockedTables, table) {
		out.blocked = true
		return out
	}
	if sourceAccessTableListed(access.PublicTables, table) {
		out.readMode = AccessModePublic
		out.writeMode = AccessModeBlocked
		out.deleteMode = AccessModeBlocked
		return out
	}
	if sourceAccessTableListed(access.AdminTables, table) {
		out.readMode = AccessModeAdmin
		out.writeMode = AccessModeBlocked
		out.deleteMode = AccessModeBlocked
		return out
	}
	return out
}

func sourceAccessRoleNames(conf *Config) []string {
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
	add("anon")
	add("user")
	if conf != nil {
		for _, role := range conf.Roles {
			add(role.Name)
		}
		for _, role := range conf.EffectiveIdentityConfig().AdminRoles {
			add(role)
		}
	}
	return out
}

func sourceAccessRoleTable(
	role string,
	adminRoles map[string]struct{},
	table *sdata.DBTable,
	access SourceAccessConfig,
	readMode, writeMode, deleteMode string,
) (RoleTable, error) {
	if table == nil {
		return RoleTable{}, fmt.Errorf("source access: nil table")
	}
	isAdmin := roleInSet(role, adminRoles)
	rt := RoleTable{
		Name:      table.Name,
		Schema:    table.Schema,
		Database:  table.Database,
		Generated: true,
		Query:     &Query{Block: modeBlocksRole(readMode, role, isAdmin)},
		Insert:    &Insert{Block: modeBlocksRole(writeMode, role, isAdmin)},
		Update:    &Update{Block: modeBlocksRole(writeMode, role, isAdmin)},
		Upsert:    &Upsert{Block: modeBlocksRole(writeMode, role, isAdmin)},
		Delete:    &Delete{Block: modeBlocksRole(deleteMode, role, isAdmin)},
	}
	if !rt.Query.Block {
		rt.Query.Filters = modeFilters(readMode, access, isAdmin)
	}
	if !rt.Insert.Block {
		rt.Insert.Presets = modePresets(writeMode, access, isAdmin)
	}
	if !rt.Update.Block {
		rt.Update.Filters = modeFilters(writeMode, access, isAdmin)
		rt.Update.Presets = modePresets(writeMode, access, isAdmin)
	}
	if !rt.Upsert.Block {
		rt.Upsert.Filters = modeFilters(writeMode, access, isAdmin)
		rt.Upsert.Presets = modePresets(writeMode, access, isAdmin)
	}
	if !rt.Delete.Block {
		rt.Delete.Filters = modeFilters(deleteMode, access, isAdmin)
	}
	return rt, nil
}

func modeBlocksRole(mode, role string, isAdmin bool) bool {
	mode = normalizeAccessMode(mode)
	switch mode {
	case AccessModePublic:
		return false
	case AccessModeAuthenticated:
		return strings.EqualFold(role, "anon")
	case AccessModeAccount, AccessModeOwner:
		return strings.EqualFold(role, "anon")
	case AccessModeAdmin:
		return !isAdmin
	case AccessModeBlocked, "":
		return true
	default:
		return true
	}
}

func modeFilters(mode string, access SourceAccessConfig, isAdmin bool) []string {
	if isAdmin {
		return nil
	}
	switch normalizeAccessMode(mode) {
	case AccessModeAccount:
		return []string{fmt.Sprintf("{ %s: { eq: $account_id } }", access.NamespaceColumn)}
	case AccessModeOwner:
		return []string{fmt.Sprintf("{ %s: { eq: $user_id } }", access.OwnerColumn)}
	default:
		return nil
	}
}

func modePresets(mode string, access SourceAccessConfig, isAdmin bool) map[string]string {
	if isAdmin {
		return nil
	}
	switch normalizeAccessMode(mode) {
	case AccessModeAccount:
		return map[string]string{access.NamespaceColumn: "$account_id"}
	case AccessModeOwner:
		return map[string]string{access.OwnerColumn: "$user_id"}
	default:
		return nil
	}
}

func sourceAccessTableListed(list []string, table *sdata.DBTable) bool {
	for _, item := range list {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if item == strings.ToLower(table.Name) ||
			item == strings.ToLower(table.Schema+"."+table.Name) ||
			item == strings.ToLower(table.Database+":"+table.Name) ||
			item == strings.ToLower(table.Database+":"+table.Schema+"."+table.Name) {
			return true
		}
	}
	return false
}

func tableHasColumn(table *sdata.DBTable, column string) bool {
	column = strings.ToLower(strings.TrimSpace(column))
	if column == "" || table == nil {
		return false
	}
	for _, col := range table.Columns {
		if strings.ToLower(col.Name) == column {
			return true
		}
	}
	return false
}

func stringSet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func roleInSet(role string, set map[string]struct{}) bool {
	_, ok := set[strings.ToLower(strings.TrimSpace(role))]
	return ok
}

func appendRuntimeRoleTableCore(conf *Config, role string, table RoleTable) {
	if conf == nil {
		return
	}
	for i := range conf.Roles {
		if strings.EqualFold(conf.Roles[i].Name, role) {
			if !roleTableExistsCore(conf.Roles[i], table.Name, table.Schema, table.Database) {
				conf.Roles[i].Tables = append(conf.Roles[i].Tables, table)
			}
			return
		}
	}
	conf.Roles = append(conf.Roles, Role{Name: role, Tables: []RoleTable{table}})
}

func roleTableExistsCore(role Role, table, schema, database string) bool {
	for _, rt := range role.Tables {
		if rt.Database != "" && database != "" && !strings.EqualFold(rt.Database, database) {
			continue
		}
		if rt.Schema != "" && schema != "" && !strings.EqualFold(rt.Schema, schema) {
			continue
		}
		if strings.EqualFold(rt.Name, table) {
			return true
		}
	}
	return false
}
