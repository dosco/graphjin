package serv

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

const artifactsRootTable = "gj_artifacts"

type artifactControlPlane struct {
	service *graphjinService
}

func newArtifactControlPlane(s *graphjinService) artifactControlPlane {
	return artifactControlPlane{service: s}
}

func (h artifactControlPlane) ManagedQueryTables() []core.ManagedTable {
	if h.service == nil || h.service.conf == nil || !h.service.conf.Core.Artifacts.Enabled {
		return nil
	}
	return []core.ManagedTable{managedTable(artifactsRootTable, artifactColumns())}
}

func (h artifactControlPlane) ManagedMutationTables() []string {
	if h.service == nil || h.service.conf == nil || !h.service.conf.Core.Artifacts.Enabled {
		return nil
	}
	return []string{artifactsRootTable}
}

func artifactColumns() []core.ManagedColumn {
	return []core.ManagedColumn{
		cpCol("id", "text", true),
		cpCol("name", "text", false),
		cpCol("kind", "text", false),
		cpCol("path", "text", false),
		cpCol("source", "text", false),
		cpCol("visibility", "text", false),
		cpCol("read_only", "boolean", false),
		cpCol("account_id", "text", false),
		cpCol("owner_id", "text", false),
		cpCol("content", "text", false),
		cpCol("content_json", "json", false),
		cpCol("metadata_json", "json", false),
		cpCol("revision", "integer", false),
		cpCol("created_at", "text", false),
		cpCol("updated_at", "text", false),
	}
}

func (h artifactControlPlane) ExecuteManagedQuery(ctx context.Context, req core.ManagedQueryRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		rows, err := h.queryRows(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRows(rows, root.Fields)
	}
	return json.Marshal(out)
}

func (h artifactControlPlane) queryRows(ctx context.Context, root core.ManagedQueryRoot) ([]map[string]any, error) {
	if root.Table != artifactsRootTable {
		return nil, fmt.Errorf("unsupported GraphJin artifact root: %s", root.Table)
	}
	rows, err := h.artifactRows(ctx)
	if err != nil {
		return nil, err
	}
	return applyManagedQuery(rows, root), nil
}

func (h artifactControlPlane) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	out := make(map[string]any, len(req.Roots))
	for _, root := range req.Roots {
		row, err := h.mutateRow(ctx, root)
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterRow(row, root.Fields)
	}
	return json.Marshal(out)
}

func (h artifactControlPlane) mutateRow(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	if root.Table != artifactsRootTable {
		return nil, fmt.Errorf("unsupported GraphJin artifact mutation root: %s", root.Table)
	}
	switch root.Operation {
	case "insert", "upsert", "update":
		return h.upsertArtifact(ctx, root)
	case "delete":
		return h.deleteArtifact(ctx, root)
	default:
		return nil, fmt.Errorf("gj_artifacts supports insert, update, upsert, and delete mutations")
	}
}

func (h artifactControlPlane) artifactRows(ctx context.Context) ([]map[string]any, error) {
	var rows []map[string]any
	dbRows, err := h.dbArtifactRows(ctx)
	if err != nil {
		return nil, err
	}
	rows = append(rows, dbRows...)

	seen := make(map[string]struct{}, len(dbRows))
	for _, row := range dbRows {
		if name, _ := row["name"].(string); name != "" {
			seen[strings.ToLower(name)] = struct{}{}
		}
	}
	globalRows, err := h.globalArtifactRows()
	if err != nil {
		return nil, err
	}
	for _, row := range globalRows {
		name, _ := row["name"].(string)
		if _, ok := seen[strings.ToLower(name)]; ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (h artifactControlPlane) dbArtifactRows(ctx context.Context) ([]map[string]any, error) {
	s := h.service
	db, dbType, table, ok := s.artifactDB()
	if !ok {
		return nil, nil
	}
	q := fmt.Sprintf("SELECT id, name, kind, path, source, visibility, read_only, account_id, owner_id, content, content_json, metadata_json, revision, created_at, updated_at FROM %s", table)
	r, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	accountID, _ := identityVarString(ctx, "account_id")
	admin := s.identityRoleIsAdmin(ctx)
	var out []map[string]any
	for r.Next() {
		var id, name, kind, path, source, visibility, accountIDRow, ownerID, content, contentJSON, metadataJSON, createdAt, updatedAt sql.NullString
		var readOnly sql.NullBool
		var revision sql.NullInt64
		if err := r.Scan(&id, &name, &kind, &path, &source, &visibility, &readOnly, &accountIDRow, &ownerID, &content, &contentJSON, &metadataJSON, &revision, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if !admin && visibility.String == "account" && accountIDRow.String != accountID {
			continue
		}
		out = append(out, map[string]any{
			"id":            id.String,
			"name":          name.String,
			"kind":          kind.String,
			"path":          path.String,
			"source":        source.String,
			"visibility":    visibility.String,
			"read_only":     readOnly.Bool,
			"account_id":    safeArtifactIdentity(accountIDRow.String, admin),
			"owner_id":      safeArtifactIdentity(ownerID.String, admin),
			"content":       content.String,
			"content_json":  jsonValue(contentJSON.String, dbType),
			"metadata_json": jsonValue(metadataJSON.String, dbType),
			"revision":      revision.Int64,
			"created_at":    createdAt.String,
			"updated_at":    updatedAt.String,
		})
	}
	return out, r.Err()
}

func (h artifactControlPlane) globalArtifactRows() ([]map[string]any, error) {
	s := h.service
	if s == nil || s.conf == nil {
		return nil, nil
	}
	cfg := s.conf.Core.EffectiveArtifactsConfig()
	root := cfg.GlobalsPath
	if root == "" {
		return nil, nil
	}
	if !filepath.IsAbs(root) {
		base, err := s.basePath()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(base, root)
	}
	var out []map[string]any
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	err := filepath.WalkDir(root, func(path string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() || !artifactGlobalFile(path) {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		kind := artifactKindFromPath(rel)
		now := time.Now().UTC().Format(time.RFC3339)
		out = append(out, map[string]any{
			"id":            "config:" + rel,
			"name":          strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)),
			"kind":          kind,
			"path":          rel,
			"source":        "config",
			"visibility":    "global",
			"read_only":     true,
			"account_id":    "",
			"owner_id":      "",
			"content":       string(b),
			"content_json":  nil,
			"metadata_json": map[string]any{"path": rel},
			"revision":      int64(1),
			"created_at":    now,
			"updated_at":    now,
		})
		return nil
	})
	return out, err
}

func (h artifactControlPlane) upsertArtifact(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	db, dbType, table, ok := s.artifactDB()
	if !ok {
		return nil, fmt.Errorf("artifact store database is not configured")
	}
	accountID, ok := identityVarString(ctx, "account_id")
	if !ok && !s.identityRoleIsAdmin(ctx) {
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "access",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Artifact write denied because account identity is missing.",
			NextAction: "Add account_id to the verified identity context before writing gj_artifacts.",
			ErrorCode:  "artifact_account_missing",
			Details:    map[string]any{"root": artifactsRootTable},
		})
		return nil, fmt.Errorf("gj_artifacts write requires account identity")
	}
	visibility := stringInput(root.Input, "visibility", "account")
	if visibility == "global" {
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "access",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Global artifact write denied.",
			NextAction: "Edit config-folder artifacts through configuration review; db-backed gj_artifacts are account-scoped.",
			ErrorCode:  "artifact_global_write_denied",
			Details:    map[string]any{"root": artifactsRootTable},
		})
		return nil, fmt.Errorf("global gj_artifacts are read-only")
	}
	name := stringInput(root.Input, "name", "")
	if name == "" {
		return nil, fmt.Errorf("gj_artifacts mutation requires name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	admin := s.identityRoleIsAdmin(ctx)
	id := artifactID(accountID, name)
	if admin {
		id = stringInput(root.Input, "id", id)
	}
	kind := stringInput(root.Input, "kind", "artifact")
	content := stringInput(root.Input, "content", "")
	contentJSON := jsonStringInput(root.Input, "content_json")
	metadataJSON := jsonStringInput(root.Input, "metadata_json")
	ownerID, _ := identityVarString(ctx, "user_id")
	args := []any{id, name, kind, "", "database", "account", false, accountID, ownerID, content, contentJSON, metadataJSON, now, now}
	q := artifactUpsertSQL(dbType, table)
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		return nil, err
	}
	return map[string]any{
		"id": id, "name": name, "kind": kind, "path": "", "source": "database", "visibility": "account",
		"read_only": false, "account_id": safeArtifactIdentity(accountID, admin), "owner_id": safeArtifactIdentity(ownerID, admin),
		"content": content, "content_json": jsonValue(contentJSON, dbType), "metadata_json": jsonValue(metadataJSON, dbType), "revision": int64(1), "created_at": now, "updated_at": now,
	}, nil
}

func (h artifactControlPlane) deleteArtifact(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	db, dbType, table, ok := s.artifactDB()
	if !ok {
		return nil, fmt.Errorf("artifact store database is not configured")
	}
	accountID, ok := identityVarString(ctx, "account_id")
	if !ok && !s.identityRoleIsAdmin(ctx) {
		return nil, fmt.Errorf("gj_artifacts delete requires account identity")
	}
	id := stringInput(root.Input, "id", "")
	if id == "" {
		id = stringWhere(root.Where, "id")
	}
	if id == "" {
		return nil, fmt.Errorf("gj_artifacts delete requires id")
	}
	q, args := artifactDeleteSQL(dbType, table, id, accountID, s.identityRoleIsAdmin(ctx))
	if _, err := db.ExecContext(ctx, q, args...); err != nil {
		return nil, err
	}
	return map[string]any{"id": id, "deleted": true}, nil
}

func (s *graphjinService) initArtifactsBeforeCore() error {
	if s == nil || s.conf == nil || !s.conf.Core.Artifacts.Enabled || !s.conf.Core.Artifacts.AutoInitEnabled() {
		return nil
	}
	db, dbType, _, ok := s.artifactDB()
	if !ok {
		return fmt.Errorf("artifact store database is not configured")
	}
	for _, stmt := range artifactDDL(dbType, s.conf.Core.EffectiveArtifactsConfig().Schema) {
		if _, err := db.ExecContext(context.Background(), stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *graphjinService) artifactDB() (*sql.DB, string, string, bool) {
	if s == nil || s.conf == nil || !s.conf.Core.Artifacts.Enabled {
		return nil, "", "", false
	}
	cfg := s.conf.Core.EffectiveArtifactsConfig()
	db := s.dbs[cfg.Source]
	if db == nil {
		return nil, "", "", false
	}
	dbConf := s.conf.Core.Databases[cfg.Source]
	dbType := strings.ToLower(dbConf.Type)
	if dbType == "" {
		dbType = "postgres"
	}
	return db, dbType, artifactTableName(dbType, cfg.Schema, "artifacts"), true
}

func artifactDDL(dbType, schema string) []string {
	table := artifactTableName(dbType, schema, "artifacts")
	revisions := artifactTableName(dbType, schema, "artifact_revisions")
	switch dbType {
	case "postgres", "":
		return []string{
			fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, quotePGIdent(schema)),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
kind TEXT NOT NULL,
path TEXT NOT NULL DEFAULT '',
source TEXT NOT NULL DEFAULT 'database',
visibility TEXT NOT NULL DEFAULT 'account',
read_only BOOLEAN NOT NULL DEFAULT FALSE,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
content TEXT NOT NULL DEFAULT '',
content_json JSONB,
metadata_json JSONB,
revision BIGINT NOT NULL DEFAULT 1,
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, table),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
artifact_id TEXT NOT NULL,
revision BIGINT NOT NULL,
content TEXT NOT NULL DEFAULT '',
content_json JSONB,
metadata_json JSONB,
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, revisions),
		}
	default:
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
kind TEXT NOT NULL,
path TEXT NOT NULL DEFAULT '',
source TEXT NOT NULL DEFAULT 'database',
visibility TEXT NOT NULL DEFAULT 'account',
read_only BOOLEAN NOT NULL DEFAULT 0,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
content TEXT NOT NULL DEFAULT '',
content_json TEXT,
metadata_json TEXT,
revision INTEGER NOT NULL DEFAULT 1,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, table),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
artifact_id TEXT NOT NULL,
revision INTEGER NOT NULL,
content TEXT NOT NULL DEFAULT '',
content_json TEXT,
metadata_json TEXT,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, revisions),
		}
	}
}

func artifactTableName(dbType, schema, table string) string {
	if dbType == "postgres" || dbType == "" {
		return quotePGIdent(schema) + "." + quotePGIdent(table)
	}
	return quoteSQLIdent(schema + "_" + table)
}

func quotePGIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteSQLIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func artifactUpsertSQL(dbType, table string) string {
	if dbType == "postgres" || dbType == "" {
		return fmt.Sprintf(`INSERT INTO %s (id, name, kind, path, source, visibility, read_only, account_id, owner_id, content, content_json, metadata_json, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
ON CONFLICT (id) DO UPDATE SET name=EXCLUDED.name, kind=EXCLUDED.kind, content=EXCLUDED.content, content_json=EXCLUDED.content_json, metadata_json=EXCLUDED.metadata_json, revision=%s.revision+1, updated_at=EXCLUDED.updated_at`, table, quotePGIdent("artifacts"))
	}
	return fmt.Sprintf(`INSERT INTO %s (id, name, kind, path, source, visibility, read_only, account_id, owner_id, content, content_json, metadata_json, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, kind=excluded.kind, content=excluded.content, content_json=excluded.content_json, metadata_json=excluded.metadata_json, revision=revision+1, updated_at=excluded.updated_at`, table)
}

func artifactDeleteSQL(dbType, table, id, accountID string, admin bool) (string, []any) {
	if dbType == "postgres" || dbType == "" {
		if admin {
			return fmt.Sprintf("DELETE FROM %s WHERE id = $1", table), []any{id}
		}
		return fmt.Sprintf("DELETE FROM %s WHERE id = $1 AND account_id = $2", table), []any{id, accountID}
	}
	if admin {
		return fmt.Sprintf("DELETE FROM %s WHERE id = ?", table), []any{id}
	}
	return fmt.Sprintf("DELETE FROM %s WHERE id = ? AND account_id = ?", table), []any{id, accountID}
}

func identityVarString(ctx context.Context, name string) (string, bool) {
	vars, ok := ctx.Value(core.IdentityVarsKey).(map[string]interface{})
	if !ok {
		return "", false
	}
	v, ok := vars[name]
	if !ok || v == nil {
		return "", false
	}
	s := fmt.Sprint(v)
	return s, s != ""
}

func (s *graphjinService) identityRoleIsAdmin(ctx context.Context) bool {
	if s == nil || s.conf == nil {
		return false
	}
	var roles []string
	if role, ok := ctx.Value(core.UserRoleKey).(string); ok && role != "" {
		roles = append(roles, role)
	}
	switch rs := ctx.Value(core.IdentityRolesKey).(type) {
	case []string:
		roles = append(roles, rs...)
	case string:
		roles = append(roles, rs)
	}
	admins := s.conf.Core.EffectiveIdentityConfig().AdminRoles
	for _, role := range roles {
		for _, admin := range admins {
			if strings.EqualFold(strings.TrimSpace(role), strings.TrimSpace(admin)) {
				return true
			}
		}
	}
	return false
}

func safeArtifactIdentity(value string, admin bool) string {
	if value == "" || admin {
		return value
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:8])
}

func artifactID(accountID, name string) string {
	sum := sha256.Sum256([]byte(accountID + ":" + name))
	return "artifact:" + hex.EncodeToString(sum[:16])
}

func artifactGlobalFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".graphql", ".gql", ".js", ".json", ".yaml", ".yml", ".sql":
		return true
	default:
		return false
	}
}

func artifactKindFromPath(path string) string {
	p := strings.ToLower(filepath.ToSlash(path))
	switch {
	case strings.Contains(p, "fragment"):
		return "fragment"
	case strings.Contains(p, "workflow"):
		return "workflow"
	case strings.Contains(p, "query"):
		return "query"
	default:
		return "artifact"
	}
}

func stringInput(input map[string]interface{}, key, def string) string {
	if input == nil {
		return def
	}
	if v, ok := input[key]; ok && v != nil {
		s := fmt.Sprint(v)
		if s != "" {
			return s
		}
	}
	return def
}

func jsonStringInput(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	v, ok := input[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case json.RawMessage:
		return string(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func stringWhere(where map[string]interface{}, key string) string {
	if where == nil {
		return ""
	}
	v, ok := where[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func jsonValue(s string, _ string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return s
	}
	return out
}
