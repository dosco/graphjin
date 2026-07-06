package serv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

const graphjinInternalStoreRole = "__graphjin_internal_store"

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
	if dbName == "" {
		dbName = core.DefaultDBName
	}
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
	}
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
