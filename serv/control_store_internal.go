package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

const graphjinInternalStoreRole = "__graphjin_internal_store"

const internalStorePageSize = 500

type internalStoreContextKey struct{}

func (s *graphjinService) internalStoreRoot(table string) (string, string, bool) {
	if s == nil || s.conf == nil {
		return "", "", false
	}
	_, dbType, _, ok := s.artifactDB()
	if !ok {
		return "", "", false
	}
	cfg := s.conf.Core.EffectiveArtifactsConfig()
	if dbType == "postgres" || dbType == "" {
		return table, cfg.Schema, true
	}
	prefix := strings.TrimSpace(cfg.Schema)
	if prefix == "" {
		prefix = "_graphjin"
	}
	return prefix + "_" + table, "", true
}

func internalStoreQueryField(root, schema, args string) string {
	var b strings.Builder
	b.WriteString(root)
	if args != "" {
		b.WriteString("(")
		b.WriteString(args)
		b.WriteString(")")
	}
	if schema != "" {
		b.WriteString(` @schema(name: `)
		b.WriteString(strconvQuote(schema))
		b.WriteString(`)`)
	}
	return b.String()
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (s *graphjinService) injectInternalStoreRole() {
	if s == nil || s.conf == nil || !s.conf.Core.Artifacts.Enabled {
		return
	}
	coreConf := &s.conf.Core
	if s.runtimeCore != nil {
		coreConf = s.runtimeCore
	}
	dbType := ""
	if _, resolvedType, _, ok := s.artifactDB(); ok {
		dbType = resolvedType
	}
	cfg := s.conf.Core.EffectiveArtifactsConfig()
	dbName := cfg.Source
	if s.managedArtifactDB != "" {
		dbName = s.managedArtifactDB
	}
	if dbName == "" {
		dbName = core.DefaultDBName
	}
	// Raw store tables are reachable ONLY through the internal role. They are
	// discovered like any table (schema _graphjin on postgres, _graphjin_*
	// prefix elsewhere), so every other role gets an explicit fail-closed
	// block — otherwise a public/authenticated source access mode would let
	// callers read the raw rows and bypass owner scoping.
	blockRoles := []string{"anon", "user"}
	if s.conf.Core.IsSourcesUsed() {
		blockRoles = sourceModeSystemRoleNames(s.conf)
	}
	for _, role := range coreConf.Roles {
		blockRoles = append(blockRoles, role.Name)
	}
	blockRoles = append(blockRoles, s.conf.Core.EffectiveIdentityConfig().AdminRoles...)
	blockRoles = uniqueInternalStoreRoles(blockRoles)
	add := func(table, schema string) {
		rt := core.RoleTable{
			Name:     table,
			Schema:   schema,
			Database: dbName,
			Query:    &core.Query{Block: false},
			Insert:   &core.Insert{Block: false},
			Update:   &core.Update{Block: false},
			Upsert:   &core.Upsert{Block: false},
			Delete:   &core.Delete{Block: false},
		}
		appendRuntimeRoleTable(coreConf, graphjinInternalStoreRole, rt)
		for _, role := range blockRoles {
			if role == graphjinInternalStoreRole {
				continue
			}
			appendRuntimeRoleTable(coreConf, role, core.RoleTable{
				Name:     table,
				Schema:   schema,
				Database: dbName,
				Query:    &core.Query{Block: true},
				Insert:   &core.Insert{Block: true},
				Update:   &core.Update{Block: true},
				Upsert:   &core.Upsert{Block: true},
				Delete:   &core.Delete{Block: true},
			})
		}
	}
	addRevisionSignal := func() {
		database := s.metadataDB
		if database == "" {
			database = core.DefaultDBName
		}
		rt := core.RoleTable{
			Name:     revisionSignalRootTable,
			Database: database,
			Query:    &core.Query{Block: false},
		}
		setPrivateRuntimeRoleTable(coreConf, graphjinInternalStoreRole, rt)
		for _, role := range blockRoles {
			if role == graphjinInternalStoreRole {
				continue
			}
			setPrivateRuntimeRoleTable(coreConf, role, core.RoleTable{
				Name:     revisionSignalRootTable,
				Database: database,
				Query:    &core.Query{Block: true},
			})
		}
	}
	addRevisionSignal()
	if dbType == "postgres" || dbType == "" {
		add("artifacts", cfg.Schema)
		add("revisions", cfg.Schema)
		if s.watchesEnabled() {
			add("watches", cfg.Schema)
			add("watch_events", cfg.Schema)
		}
		return
	}
	prefix := strings.TrimSpace(cfg.Schema)
	if prefix == "" {
		prefix = "_graphjin"
	}
	add(prefix+"_artifacts", "")
	add(prefix+"_revisions", "")
	if s.watchesEnabled() {
		add(prefix+"_watches", "")
		add(prefix+"_watch_events", "")
	}
}

func uniqueInternalStoreRoles(roles []string) []string {
	seen := make(map[string]struct{}, len(roles))
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		key := strings.ToLower(role)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, role)
	}
	return out
}

func setPrivateRuntimeRoleTable(conf *core.Config, role string, table core.RoleTable) {
	if conf == nil || strings.TrimSpace(role) == "" {
		return
	}
	table.Generated = true
	for index := range conf.Roles {
		if !strings.EqualFold(conf.Roles[index].Name, role) {
			continue
		}
		for tableIndex, existing := range conf.Roles[index].Tables {
			if existing.Database != "" && table.Database != "" &&
				!strings.EqualFold(existing.Database, table.Database) {
				continue
			}
			if strings.EqualFold(existing.Name, table.Name) {
				conf.Roles[index].Tables[tableIndex] = table
				return
			}
		}
		conf.Roles[index].Tables = append(conf.Roles[index].Tables, table)
		return
	}
	conf.Roles = append(conf.Roles, core.Role{Name: role, Tables: []core.RoleTable{table}})
}

func (s *graphjinService) internalStoreContext(parent context.Context) context.Context {
	ctx := parent
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, internalStoreContextKey{}, true)
	ctx = context.WithValue(ctx, core.UserRoleKey, graphjinInternalStoreRole)
	ctx = context.WithValue(ctx, core.IdentityRolesKey, []string{graphjinInternalStoreRole})
	return ctx
}

func (s *graphjinService) authorizeReservedRole(ctx context.Context, role string) bool {
	return role == graphjinInternalStoreRole && ctx != nil && ctx.Value(internalStoreContextKey{}) == true
}

func (s *graphjinService) internalStoreGraphQL(ctx context.Context, query string, vars any) (map[string]json.RawMessage, error) {
	if s == nil || s.gj == nil {
		return nil, fmt.Errorf("GraphJin core is not initialized")
	}
	var raw json.RawMessage
	if vars != nil {
		data, err := json.Marshal(vars)
		if err != nil {
			return nil, err
		}
		raw = data
	}
	res, err := s.gj.GraphQL(s.internalStoreContext(ctx), query, raw, nil)
	if err != nil {
		return nil, err
	}
	if len(res.Errors) != 0 {
		return nil, fmt.Errorf("%s", res.Errors[0].Message)
	}
	var out map[string]json.RawMessage
	if len(res.Data) != 0 {
		if err := json.Unmarshal(res.Data, &out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// internalStoreRows returns a single GraphJin result page and therefore
// inherits core default_limit when args do not specify a smaller limit. Use it
// only for exact or intentionally bounded reads; completeness-dependent reads
// must use internalStoreAllRows.
func (s *graphjinService) internalStoreRows(ctx context.Context, table, args, fields string, vars any) ([]map[string]any, error) {
	root, schema, ok := s.internalStoreRoot(table)
	if !ok {
		return nil, nil
	}
	field := internalStoreQueryField(root, schema, args)
	query := fmt.Sprintf("query { %s { %s } }", field, fields)
	out, err := s.internalStoreGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}
	return decodeInternalStoreRows(out[root])
}

// internalStoreAllRows reads every matching physical store row in deterministic
// pages. args may contain filters, but must not contain order_by, limit, or
// offset because this helper owns pagination.
func (s *graphjinService) internalStoreAllRows(ctx context.Context, table, args, fields string, vars any) ([]map[string]any, error) {
	return s.internalStoreAllRowsOrdered(ctx, table, args, fields, "id", vars)
}

// internalStoreAllRowsOrdered supports the internal tables whose stable key is
// not id. orderField must be a trusted schema field supplied by GraphJin.
func (s *graphjinService) internalStoreAllRowsOrdered(ctx context.Context, table, args, fields, orderField string, vars any) ([]map[string]any, error) {
	args = strings.TrimSpace(args)
	orderField = strings.TrimSpace(orderField)
	if orderField == "" || strings.ContainsAny(orderField, " \t\r\n,{}()") {
		return nil, fmt.Errorf("invalid internal store order field %q", orderField)
	}
	var out []map[string]any
	for offset := 0; ; offset += internalStorePageSize {
		pageArgs := fmt.Sprintf(
			"order_by: { %s: asc }, limit: %d, offset: %d",
			orderField,
			internalStorePageSize,
			offset,
		)
		if args != "" {
			pageArgs = args + ", " + pageArgs
		}
		rows, err := s.internalStoreRows(ctx, table, pageArgs, fields, vars)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
		if len(rows) < internalStorePageSize {
			return out, nil
		}
	}
}

func (s *graphjinService) internalStoreMutationRows(ctx context.Context, table, opArgs, fields string, vars any) ([]map[string]any, error) {
	root, schema, ok := s.internalStoreRoot(table)
	if !ok {
		return nil, nil
	}
	field := internalStoreQueryField(root, schema, opArgs)
	query := fmt.Sprintf("mutation { %s { %s } }", field, fields)
	out, err := s.internalStoreGraphQL(ctx, query, vars)
	if err != nil {
		return nil, err
	}
	return decodeInternalStoreRows(out[root])
}

func decodeInternalStoreRows(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}
