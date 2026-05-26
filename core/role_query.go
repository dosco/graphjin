package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

type roleQueryMode uint8

const (
	roleQuerySQL roleQueryMode = iota
	roleQueryGraphQL

	internalRoleQueryRole = "__gj_roles_query"
)

func isGraphQLRoleQuery(query string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return false
	}
	q = strings.ToLower(q)
	return strings.HasPrefix(q, "{") ||
		strings.HasPrefix(q, "query") ||
		strings.HasPrefix(q, "fragment")
}

func (gj *graphjinEngine) prepareGraphQLRoleStmt() error {
	pdb := gj.primaryDB()
	if pdb == nil || pdb.qcodeCompiler == nil || pdb.psqlCompiler == nil {
		return fmt.Errorf("roles_query: primary database not initialized")
	}

	qc, err := pdb.qcodeCompiler.Compile(
		[]byte(gj.conf.RolesQuery),
		nil,
		internalRoleQueryRole,
		gj.namespace)
	if err != nil {
		return fmt.Errorf("roles_query: graphql compile: %w", err)
	}
	if qc.Type != qcode.QTQuery {
		return fmt.Errorf("roles_query: graphql roles_query must be a query")
	}

	var w bytes.Buffer
	md, err := pdb.psqlCompiler.Compile(&w, qc)
	if err != nil {
		return fmt.Errorf("roles_query: graphql SQL compile: %w", err)
	}

	matches, err := compileRoleMatches(gj.conf.Roles)
	if err != nil {
		return err
	}

	gj.roleGraphQLStmt = stmt{
		role: internalRoleQueryRole,
		qc:   qc,
		md:   md,
		sql:  w.String(),
	}
	gj.roleGraphQLMatches = matches
	return nil
}

func (gj *graphjinEngine) executeGraphQLRoleQuery(
	c context.Context,
	conn *sql.Conn,
	vmap map[string]json.RawMessage,
	rc *RequestConfig,
) (role string, err error) {
	role = "user"

	pdb := gj.primaryDB()
	if pdb == nil || pdb.psqlCompiler == nil {
		return "", fmt.Errorf("roles_query: primary database not initialized")
	}
	if gj.roleGraphQLStmt.qc == nil {
		return "", fmt.Errorf("roles_query: graphql roles_query not initialized")
	}

	vars := roleQueryVars(vmap, gj.roleGraphQLStmt.qc.Vars)
	ar, err := gj.argList(c,
		gj.roleGraphQLStmt.md,
		vars,
		rc,
		false,
		pdb.psqlCompiler)
	if err != nil {
		return "", err
	}

	query, queryArgs, err := prepareQueryArgsForDB(pdb.dbtype, gj.roleGraphQLStmt.sql, ar.values)
	if err != nil {
		return "", err
	}

	raw, err := scanJSONRow(c, pdb.dbtype, conn, requestTx(rc), query, queryArgs)
	if err == sql.ErrNoRows {
		return role, nil
	}
	if err != nil {
		return "", err
	}

	attrs, empty, err := extractRoleQueryAttrs(raw)
	if err != nil {
		return "", err
	}
	if empty {
		return role, nil
	}

	for _, rm := range gj.roleGraphQLMatches {
		ok, err := rm.expr.eval(attrs)
		if err != nil {
			return "", fmt.Errorf("roles_query: role %q match failed: %w", rm.role, err)
		}
		if ok {
			return rm.role, nil
		}
	}
	return role, nil
}

func requestTx(rc *RequestConfig) *sql.Tx {
	if rc == nil {
		return nil
	}
	return rc.Tx
}

func roleQueryVars(vmap map[string]json.RawMessage, vars []qcode.Var) map[string]json.RawMessage {
	if len(vars) == 0 {
		return cloneRawMessageMap(vmap)
	}

	out := cloneRawMessageMap(vmap)
	if out == nil {
		out = make(map[string]json.RawMessage, len(vars))
	}
	for _, v := range vars {
		if _, ok := out[v.Name]; ok {
			continue
		}
		if len(v.Val) != 0 {
			out[v.Name] = json.RawMessage(cloneBytes(v.Val))
		}
	}
	return out
}

func extractRoleQueryAttrs(raw []byte) (map[string]any, bool, error) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, false, fmt.Errorf("roles_query: graphql result decode: %w", err)
	}

	keys := make([]string, 0, len(top))
	for k := range top {
		if k == "__typename" || strings.HasSuffix(k, "_cursor") {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil, true, nil
	}
	if len(keys) > 1 {
		return nil, false, fmt.Errorf("roles_query: graphql roles_query must return exactly one root field")
	}

	switch v := top[keys[0]].(type) {
	case nil:
		return nil, true, nil
	case map[string]any:
		return v, false, nil
	case []any:
		if len(v) == 0 {
			return nil, true, nil
		}
		if len(v) > 1 {
			return nil, false, fmt.Errorf("roles_query: graphql roles_query returned multiple rows")
		}
		obj, ok := v[0].(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("roles_query: graphql roles_query row must be an object")
		}
		return obj, false, nil
	default:
		return nil, false, fmt.Errorf("roles_query: graphql roles_query root field must be an object or list")
	}
}
