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

const artifactStoreFields = `id name kind path source visibility read_only account_id owner_id content content_json metadata_json content_hash status target_ref tier catalog_revision task_id approved_by approved_at revision created_at updated_at`

const revisionStoreFields = `domain revision updated_at`

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
		cpCol("content_hash", "text", false),
		cpCol("status", "text", false),
		cpCol("target_ref", "text", false),
		cpCol("tier", "text", false),
		cpCol("catalog_revision", "text", false),
		cpCol("task_id", "text", false),
		cpCol("approved_by", "text", false),
		cpCol("approved_at", "text", false),
		cpCol("revision", "integer", false),
		cpCol("account_ref", "text", false),
		cpCol("owner_ref", "text", false),
		cpCol("author_ref", "text", false),
		cpCol("approved_ref", "text", false),
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
			kind, _ := row["kind"].(string)
			seen[artifactOverlayKey(kind, name)] = struct{}{}
		}
	}
	globalRows, err := h.globalArtifactRows()
	if err != nil {
		return nil, err
	}
	for _, row := range globalRows {
		name, _ := row["name"].(string)
		kind, _ := row["kind"].(string)
		if _, ok := seen[artifactOverlayKey(kind, name)]; ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (h artifactControlPlane) dbArtifactRows(ctx context.Context) ([]map[string]any, error) {
	s := h.service
	if _, _, _, ok := s.artifactDB(); !ok {
		return nil, nil
	}
	userID, hasUser := artifactUserID(ctx)
	if !hasUser {
		return nil, nil
	}
	admin := s.identityRoleIsAdmin(ctx)
	where := `where: { owner_id: { eq: $owner_id } }`
	vars := map[string]any{"owner_id": userID}
	if admin {
		where = ""
		vars = nil
	} else if accountID, ok := identityVarString(ctx, "account_id"); ok {
		where = `where: { or: [{ owner_id: { eq: $owner_id } }, { and: [{ visibility: { eq: "account" } }, { account_id: { eq: $account_id } }] }, { and: [{ visibility: { eq: "account" } }, { account_id: { eq: "" } }] }] }`
		vars["account_id"] = accountID
	} else {
		where = `where: { or: [{ owner_id: { eq: $owner_id } }, { and: [{ visibility: { eq: "account" } }, { account_id: { eq: "" } }] }] }`
	}
	rows, err := s.internalStoreAllRows(ctx, "artifacts", where, artifactStoreFields, vars)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, row := range rows {
		visibility := stringMapValue(row, "visibility")
		if visibility != "user" && visibility != "account" {
			continue
		}
		ownerID := stringMapValue(row, "owner_id")
		accountID := stringMapValue(row, "account_id")
		callerAccount, _ := identityVarString(ctx, "account_id")
		visible := ownerID == userID || admin || (visibility == "account" && (accountID == "" || accountID == callerAccount))
		if !visible {
			continue
		}
		out = append(out, artifactProjectionRow(row, admin))
	}
	return out, nil
}

func (h artifactControlPlane) allArtifactRowsForProjection(ctx context.Context) ([]map[string]any, error) {
	dbRows, err := h.dbArtifactRowsForProjection(ctx)
	if err != nil {
		return nil, err
	}
	globalRows, err := h.globalArtifactRows()
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0, len(dbRows)+len(globalRows))
	rows = append(rows, dbRows...)
	rows = append(rows, globalRows...)
	return rows, nil
}

func (h artifactControlPlane) dbArtifactRowsForProjection(ctx context.Context) ([]map[string]any, error) {
	s := h.service
	if _, _, _, ok := s.artifactDB(); !ok {
		return nil, nil
	}
	rows, err := s.internalStoreAllRows(ctx, "artifacts", "", artifactStoreFields, nil)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, artifactProjectionRow(row, true))
	}
	return out, nil
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
	var out []map[string]any
	appendRow := func(rel string, b []byte) {
		rel = filepath.ToSlash(strings.TrimPrefix(rel, "/"))
		kind := artifactKindFromPath(rel)
		content := string(b)
		now := time.Now().UTC().Format(time.RFC3339)
		out = append(out, map[string]any{
			"id":               "config:" + rel,
			"name":             strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)),
			"kind":             kind,
			"path":             rel,
			"source":           "config",
			"visibility":       "global",
			"read_only":        true,
			"account_id":       "",
			"owner_id":         "",
			"content":          content,
			"content_json":     nil,
			"metadata_json":    map[string]any{"path": rel},
			"content_hash":     hashString(content),
			"status":           "approved",
			"target_ref":       "",
			"tier":             "",
			"catalog_revision": "",
			"task_id":          "",
			"approved_by":      "",
			"approved_at":      "",
			"revision":         int64(1),
			"account_ref":      "",
			"owner_ref":        "",
			"created_at":       now,
			"updated_at":       now,
		})
	}
	if s.fs != nil {
		for _, dir := range artifactGlobalFSSearchDirs(root) {
			names, err := s.fs.List(dir)
			if err != nil {
				continue
			}
			for _, name := range names {
				path := artifactFSJoin(dir, name)
				if !artifactGlobalFile(path) {
					continue
				}
				b, err := s.fs.Get(path)
				if err != nil {
					return nil, err
				}
				appendRow(artifactFSRel(root, path), b)
			}
		}
		if len(out) != 0 {
			return out, nil
		}
	}
	if !filepath.IsAbs(root) {
		base, err := s.basePath()
		if err != nil {
			return nil, err
		}
		root = filepath.Join(base, root)
	}
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
		appendRow(rel, b)
		return nil
	})
	return out, err
}

func artifactGlobalFSSearchDirs(root string) []string {
	root = filepath.ToSlash(strings.TrimSpace(root))
	root = strings.TrimSuffix(root, "/")
	if root == "" || root == "." {
		root = "."
	}
	return []string{
		root,
		artifactFSJoin(root, "queries"),
		artifactFSJoin(root, "queries/fragments"),
		artifactFSJoin(root, "workflows"),
	}
}

func artifactFSJoin(base, elem string) string {
	base = filepath.ToSlash(strings.TrimSpace(base))
	elem = filepath.ToSlash(strings.TrimSpace(elem))
	switch {
	case base == "" || base == ".":
		return elem
	case base == "/":
		return "/" + strings.TrimPrefix(elem, "/")
	default:
		return strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(elem, "/")
	}
}

func artifactFSRel(root, path string) string {
	root = filepath.ToSlash(strings.TrimSpace(root))
	path = filepath.ToSlash(strings.TrimSpace(path))
	if root == "" || root == "." || root == "/" {
		return strings.TrimPrefix(path, "/")
	}
	rel, err := filepath.Rel(filepath.FromSlash(root), filepath.FromSlash(path))
	if err != nil {
		return strings.TrimPrefix(path, "/")
	}
	return filepath.ToSlash(rel)
}

func (h artifactControlPlane) upsertArtifact(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if _, _, _, ok := s.artifactDB(); !ok {
		return nil, fmt.Errorf("artifact store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "access",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Artifact write denied because user identity is missing.",
			NextAction: "Add user_id to the verified identity context before writing gj_artifacts.",
			ErrorCode:  "artifact_user_missing",
			Details:    map[string]any{"root": artifactsRootTable},
		})
		return nil, fmt.Errorf("gj_artifacts write requires user identity")
	}
	requestedKind := normalizeArtifactKind(stringInput(root.Input, "kind", "artifact"))
	requestedID := strings.TrimSpace(stringWhere(root.Where, "id"))
	if requestedID == "" {
		requestedID = strings.TrimSpace(stringInput(root.Input, "id", ""))
	}
	var requestedExisting map[string]any
	var err error
	if requestedID != "" {
		requestedExisting, err = s.internalArtifactStoreRow(ctx, requestedID)
		if err != nil {
			return nil, err
		}
	}
	if requestedKind == artifactKindAnnotation || normalizeArtifactKind(stringMapValue(requestedExisting, "kind")) == artifactKindAnnotation {
		return h.upsertAnnotation(ctx, root, requestedExisting)
	}
	visibility := stringInput(root.Input, "visibility", "user")
	if visibility == "global" {
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "access",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Global artifact write denied.",
			NextAction: "Edit config-folder artifacts through configuration review; db-backed gj_artifacts are user-scoped.",
			ErrorCode:  "artifact_global_write_denied",
			Details:    map[string]any{"root": artifactsRootTable},
		})
		return nil, fmt.Errorf("global gj_artifacts are read-only")
	}
	if visibility != "user" && visibility != "account" {
		visibility = "user"
	}
	name := stringInput(root.Input, "name", "")
	if name == "" {
		return nil, fmt.Errorf("gj_artifacts mutation requires name")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	admin := s.identityRoleIsAdmin(ctx)
	kind := normalizeArtifactKind(stringInput(root.Input, "kind", "artifact"))
	if err := s.checkArtifactKindWritable(kind); err != nil {
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "access",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Artifact write denied because the artifact kind is locked.",
			NextAction: "Remove the kind from core.artifacts.locked or choose a writable artifact kind.",
			ErrorCode:  "artifact_kind_locked",
			Details:    map[string]any{"root": artifactsRootTable, "kind": kind},
		})
		return nil, err
	}
	id := artifactID(ownerID, kind, name)
	if admin {
		id = stringInput(root.Input, "id", id)
	}
	content := stringInput(root.Input, "content", "")
	contentJSON := jsonStringInput(root.Input, "content_json")
	metadataJSON := jsonStringInput(root.Input, "metadata_json")
	contentHash := artifactContentHash(content, contentJSON)
	status := "approved"
	accountID, _ := identityVarString(ctx, "account_id")
	existing, err := s.internalArtifactStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing != nil && !admin && stringMapValue(existing, "owner_id") != ownerID {
		return nil, fmt.Errorf("gj_artifacts write denied")
	}
	revision := int64(1)
	createdAt := now
	if existing != nil {
		revision = int64MapValue(existing, "revision") + 1
		createdAt = stringMapValue(existing, "created_at")
		if createdAt == "" {
			createdAt = now
		}
	}
	input := map[string]any{
		"id": id, "name": name, "kind": kind, "path": "", "source": "database", "visibility": "user", "read_only": false,
		"account_id": accountID, "owner_id": ownerID, "content": content, "content_json": nullableJSONString(contentJSON),
		"metadata_json": nullableJSONString(metadataJSON), "content_hash": contentHash, "status": status,
		"revision": revision, "created_at": createdAt, "updated_at": now,
	}
	var rows []map[string]any
	if existing == nil {
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `insert: $input`, artifactStoreFields, map[string]any{"input": input})
	} else {
		update := cloneMap(input)
		delete(update, "id")
		delete(update, "created_at")
		rows, err = s.internalStoreMutationRows(ctx, "artifacts", `where: { id: { eq: $id } }, update: $input`, artifactStoreFields, map[string]any{"id": id, "input": update})
	}
	if err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "artifacts"); err != nil {
		return nil, err
	}
	s.markArtifactChanged("artifact mutation")
	if len(rows) != 0 {
		return artifactProjectionRow(rows[0], admin), nil
	}
	return artifactProjectionRow(input, admin), nil
}

func (h artifactControlPlane) deleteArtifact(ctx context.Context, root core.ManagedMutationRoot) (map[string]any, error) {
	s := h.service
	if _, _, _, ok := s.artifactDB(); !ok {
		return nil, fmt.Errorf("artifact store database is not configured")
	}
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return nil, fmt.Errorf("gj_artifacts delete requires user identity")
	}
	id := stringInput(root.Input, "id", "")
	if id == "" {
		id = stringWhere(root.Where, "id")
	}
	if id == "" {
		return nil, fmt.Errorf("gj_artifacts delete requires id")
	}
	admin := s.identityRoleIsAdmin(ctx)
	existing, err := s.internalArtifactStoreRow(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing == nil || (!admin && stringMapValue(existing, "owner_id") != ownerID) {
		return map[string]any{"id": id, "deleted": true}, nil
	}
	kind := normalizeArtifactKind(stringInput(root.Input, "kind", stringWhere(root.Where, "kind")))
	if kind == "artifact" {
		kind = normalizeArtifactKind(stringMapValue(existing, "kind"))
	}
	if err := s.checkArtifactKindWritable(kind); err != nil {
		s.recordRuntimeEvent(ctx, runtimeEvent{
			Phase:      "access",
			Status:     runtimeStatusFailed,
			Severity:   "warn",
			Summary:    "Artifact delete denied because the artifact kind is locked.",
			NextAction: "Remove the kind from core.artifacts.locked before deleting artifacts of this kind.",
			ErrorCode:  "artifact_kind_locked",
			Details:    map[string]any{"root": artifactsRootTable, "kind": kind},
		})
		return nil, err
	}
	if _, err := s.internalStoreMutationRows(ctx, "artifacts", `where: { id: { eq: $id } }, delete: true`, `id`, map[string]any{"id": id}); err != nil {
		return nil, err
	}
	if err := s.bumpArtifactRevision(ctx, "artifacts"); err != nil {
		return nil, err
	}
	s.markArtifactChanged("artifact delete")
	return map[string]any{"id": id, "deleted": true}, nil
}

func (s *graphjinService) internalArtifactStoreRow(ctx context.Context, id string) (map[string]any, error) {
	rows, err := s.internalStoreRows(ctx, "artifacts", `where: { id: { eq: $id } }`, artifactStoreFields, map[string]any{"id": id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// EnsureArtifactStore creates the artifact (and watch) store tables for the
// configured artifact source. Demo tooling calls this after seeding and before
// generating its schema-DDL cache, so the cached schema, which core loads in
// place of live discovery in demo mode — includes the store tables that
// gj_artifacts/gj_watch role rules and internal-store queries compile against.
func EnsureArtifactStore(conf *Config, dbs map[string]*sql.DB) error {
	if conf == nil || !conf.Core.Artifacts.Enabled {
		return nil
	}
	// The managed store is created by graphjinService after application
	// databases are initialized. Demo bootstrap only owns application sources;
	// it must not mistake the private store for a missing application database.
	if conf.managedArtifactStore {
		return nil
	}
	s := &graphjinService{conf: conf, dbs: dbs}
	return s.initArtifactsBeforeCore()
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
	if s.watchesEnabled() {
		for _, stmt := range watchDDL(dbType, s.conf.Core.EffectiveArtifactsConfig().Schema) {
			if _, err := db.ExecContext(context.Background(), stmt); err != nil {
				return err
			}
		}
		if err := s.migrateWatchSchema(context.Background(), db, dbType, s.conf.Core.EffectiveArtifactsConfig().Schema); err != nil {
			return err
		}
	}
	if s.tasksEnabled() {
		for _, stmt := range taskDDL(dbType, s.conf.Core.EffectiveArtifactsConfig().Schema) {
			if _, err := db.ExecContext(context.Background(), stmt); err != nil {
				return err
			}
		}
		if err := s.migrateTaskSchema(context.Background(), db, dbType, s.conf.Core.EffectiveArtifactsConfig().Schema); err != nil {
			return err
		}
	}
	return s.migrateArtifactSchema(context.Background(), db, dbType, s.conf.Core.EffectiveArtifactsConfig().Schema)
}

func (s *graphjinService) artifactDB() (*sql.DB, string, string, bool) {
	if s == nil || s.conf == nil || !s.conf.Core.Artifacts.Enabled {
		return nil, "", "", false
	}
	cfg := s.conf.Core.EffectiveArtifactsConfig()
	source := cfg.Source
	if s.conf.managedArtifactStore && s.managedArtifactDB != "" {
		source = s.managedArtifactDB
	}
	db := s.dbs[source]
	if db == nil {
		return nil, "", "", false
	}
	coreConf := &s.conf.Core
	if s.runtimeCore != nil {
		coreConf = s.runtimeCore
	}
	dbConf := coreConf.Databases[source]
	dbType := strings.ToLower(dbConf.Type)
	if dbType == "" {
		dbType = "postgres"
	}
	return db, dbType, artifactTableName(dbType, cfg.Schema, "artifacts"), true
}

func artifactDDL(dbType, schema string) []string {
	table := artifactTableName(dbType, schema, "artifacts")
	revisions := artifactTableName(dbType, schema, "revisions")
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
visibility TEXT NOT NULL DEFAULT 'user',
read_only BOOLEAN NOT NULL DEFAULT FALSE,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
content TEXT NOT NULL DEFAULT '',
content_json JSONB,
metadata_json JSONB,
content_hash TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT 'approved',
target_ref TEXT NOT NULL DEFAULT '',
tier TEXT NOT NULL DEFAULT '',
catalog_revision TEXT NOT NULL DEFAULT '',
task_id TEXT NOT NULL DEFAULT '',
approved_by TEXT NOT NULL DEFAULT '',
approved_at TEXT NOT NULL DEFAULT '',
revision BIGINT NOT NULL DEFAULT 1,
created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, table),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
domain TEXT PRIMARY KEY,
revision BIGINT NOT NULL DEFAULT 0,
updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`, revisions),
		}
	case "mysql", "mariadb":
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id VARCHAR(191) PRIMARY KEY,
name VARCHAR(255) NOT NULL,
kind VARCHAR(64) NOT NULL,
path VARCHAR(1024) NOT NULL DEFAULT '',
source VARCHAR(64) NOT NULL DEFAULT 'database',
visibility VARCHAR(32) NOT NULL DEFAULT 'user',
read_only BOOLEAN NOT NULL DEFAULT FALSE,
account_id VARCHAR(191) NOT NULL DEFAULT '',
owner_id VARCHAR(191) NOT NULL DEFAULT '',
content LONGTEXT NOT NULL,
content_json LONGTEXT,
metadata_json LONGTEXT,
content_hash VARCHAR(128) NOT NULL DEFAULT '',
status VARCHAR(32) NOT NULL DEFAULT 'approved',
target_ref VARCHAR(1024) NOT NULL DEFAULT '',
tier VARCHAR(32) NOT NULL DEFAULT '',
catalog_revision VARCHAR(191) NOT NULL DEFAULT '',
task_id VARCHAR(191) NOT NULL DEFAULT '',
approved_by VARCHAR(191) NOT NULL DEFAULT '',
approved_at VARCHAR(64) NOT NULL DEFAULT '',
revision BIGINT NOT NULL DEFAULT 1,
created_at VARCHAR(64) NOT NULL DEFAULT '',
updated_at VARCHAR(64) NOT NULL DEFAULT '',
KEY idx_graphjin_artifacts_annotation_target (kind, target_ref(191), tier),
KEY idx_graphjin_artifacts_annotation_owner (owner_id, kind, tier)
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, table),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
domain VARCHAR(64) PRIMARY KEY,
revision BIGINT NOT NULL DEFAULT 0,
updated_at VARCHAR(64) NOT NULL DEFAULT ''
) DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci`, revisions),
		}
	default:
		return []string{
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
id TEXT PRIMARY KEY,
name TEXT NOT NULL,
kind TEXT NOT NULL,
path TEXT NOT NULL DEFAULT '',
source TEXT NOT NULL DEFAULT 'database',
visibility TEXT NOT NULL DEFAULT 'user',
read_only BOOLEAN NOT NULL DEFAULT 0,
account_id TEXT NOT NULL DEFAULT '',
owner_id TEXT NOT NULL DEFAULT '',
content TEXT NOT NULL DEFAULT '',
content_json TEXT,
metadata_json TEXT,
content_hash TEXT NOT NULL DEFAULT '',
status TEXT NOT NULL DEFAULT 'approved',
target_ref TEXT NOT NULL DEFAULT '',
tier TEXT NOT NULL DEFAULT '',
catalog_revision TEXT NOT NULL DEFAULT '',
task_id TEXT NOT NULL DEFAULT '',
approved_by TEXT NOT NULL DEFAULT '',
approved_at TEXT NOT NULL DEFAULT '',
revision INTEGER NOT NULL DEFAULT 1,
created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, table),
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
domain TEXT PRIMARY KEY,
revision INTEGER NOT NULL DEFAULT 0,
updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, revisions),
		}
	}
}

func artifactMigrationDDL(dbType, schema string) []string {
	if dbType != "postgres" && dbType != "" {
		return nil
	}
	table := artifactTableName(dbType, schema, "artifacts")
	return []string{
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'approved'`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS target_ref TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS tier TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS catalog_revision TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS task_id TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS approved_by TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS approved_at TEXT NOT NULL DEFAULT ''`, table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (kind, target_ref, tier)`, quotePGIdent("idx_graphjin_artifacts_annotation_target"), table),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s (owner_id, kind, tier)`, quotePGIdent("idx_graphjin_artifacts_annotation_owner"), table),
	}
}

func (s *graphjinService) migrateArtifactSchema(ctx context.Context, db *sql.DB, dbType, schema string) error {
	for _, stmt := range artifactMigrationDDL(dbType, schema) {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if dbType == "postgres" || dbType == "" {
		return nil
	}
	table := artifactTableName(dbType, schema, "artifacts")
	columns := []struct{ name, definition string }{
		{"content_hash", "TEXT NOT NULL DEFAULT ''"},
		{"status", "TEXT NOT NULL DEFAULT 'approved'"},
		{"target_ref", "TEXT NOT NULL DEFAULT ''"},
		{"tier", "TEXT NOT NULL DEFAULT ''"},
		{"catalog_revision", "TEXT NOT NULL DEFAULT ''"},
		{"task_id", "TEXT NOT NULL DEFAULT ''"},
		{"approved_by", "TEXT NOT NULL DEFAULT ''"},
		{"approved_at", "TEXT NOT NULL DEFAULT ''"},
	}
	if dbType == "mysql" || dbType == "mariadb" {
		columns = []struct{ name, definition string }{
			{"content_hash", "VARCHAR(128) NOT NULL DEFAULT ''"},
			{"status", "VARCHAR(32) NOT NULL DEFAULT 'approved'"},
			{"target_ref", "VARCHAR(1024) NOT NULL DEFAULT ''"},
			{"tier", "VARCHAR(32) NOT NULL DEFAULT ''"},
			{"catalog_revision", "VARCHAR(191) NOT NULL DEFAULT ''"},
			{"task_id", "VARCHAR(191) NOT NULL DEFAULT ''"},
			{"approved_by", "VARCHAR(191) NOT NULL DEFAULT ''"},
			{"approved_at", "VARCHAR(64) NOT NULL DEFAULT ''"},
		}
	}
	for _, column := range columns {
		if err := ensureSQLColumn(ctx, db, dbType, table, column.name, column.definition); err != nil {
			return err
		}
	}
	return ensureArtifactAnnotationIndexes(ctx, db, dbType, table)
}

func ensureArtifactAnnotationIndexes(ctx context.Context, db *sql.DB, dbType, table string) error {
	indexes := []struct {
		name    string
		columns string
	}{
		{"idx_graphjin_artifacts_annotation_target", "kind, target_ref, tier"},
		{"idx_graphjin_artifacts_annotation_owner", "owner_id, kind, tier"},
	}
	for _, index := range indexes {
		if dbType == "mysql" || dbType == "mariadb" {
			rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW INDEX FROM %s WHERE Key_name = '%s'", table, index.name))
			if err == nil {
				exists := rows.Next()
				_ = rows.Close()
				if exists {
					continue
				}
			}
			columns := index.columns
			if strings.Contains(columns, "target_ref") {
				columns = "kind, target_ref(191), tier"
			}
			if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX %s ON %s (%s)", quoteStoreIdent(dbType, index.name), table, columns)); err != nil {
				return err
			}
			continue
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE INDEX IF NOT EXISTS %s ON %s (%s)", quoteStoreIdent(dbType, index.name), table, index.columns)); err != nil {
			return err
		}
	}
	return nil
}

func ensureSQLColumn(ctx context.Context, db *sql.DB, dbType, table, column, definition string) error {
	if dbType == "sqlite" {
		exists, err := sqliteColumnExists(ctx, db, table, column)
		if err != nil {
			return err
		}
		if exists {
			return nil
		}
	} else {
		q := fmt.Sprintf("SELECT %s FROM %s WHERE 1 = 0", quoteStoreIdent(dbType, column), table)
		rows, err := db.QueryContext(ctx, q)
		if err == nil {
			rows.Close() // nolint:errcheck
			return nil
		}
	}
	_, err := db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, quoteStoreIdent(dbType, column), definition))
	return err
}

func sqliteColumnExists(ctx context.Context, db *sql.DB, table, column string) (bool, error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func artifactTableName(dbType, schema, table string) string {
	if dbType == "postgres" || dbType == "" {
		return quotePGIdent(schema) + "." + quotePGIdent(table)
	}
	return quoteStoreIdent(dbType, schema+"_"+table)
}

func quotePGIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteSQLIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteStoreIdent(dbType, s string) string {
	switch dbType {
	case "mysql", "mariadb":
		return "`" + strings.ReplaceAll(s, "`", "``") + "`"
	default:
		return quoteSQLIdent(s)
	}
}

func (s *graphjinService) checkArtifactKindWritable(kind string) error {
	if strings.TrimSpace(kind) == "" {
		return nil
	}
	kind = normalizeArtifactKind(kind)
	if s == nil || s.conf == nil {
		return nil
	}
	cfg := s.conf.Core.EffectiveArtifactsConfig()
	for _, locked := range cfg.Locked {
		if normalizeArtifactKind(locked) == kind {
			return artifactPolicyError{
				Code:        "artifact_kind_locked",
				Kind:        kind,
				PolicyFinal: true,
			}
		}
	}
	return nil
}

type artifactPolicyError struct {
	Code        string
	Kind        string
	PolicyFinal bool
}

func (e artifactPolicyError) Error() string {
	if e.Code == "" {
		e.Code = "artifact_policy_error"
	}
	if e.Kind == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: artifact kind %q is locked by policy", e.Code, e.Kind)
}

func artifactContentHash(content, contentJSON string) string {
	if strings.TrimSpace(content) != "" || strings.TrimSpace(contentJSON) == "" {
		return hashString(content)
	}
	return hashString(contentJSON)
}

func artifactStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return "approved"
	}
	return status
}

func (s *graphjinService) bumpArtifactRevision(ctx context.Context, domain string) error {
	if s == nil {
		return nil
	}
	if _, _, _, ok := s.artifactDB(); !ok {
		return nil
	}
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = "artifacts"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	current, err := s.artifactRevision(ctx, domain)
	if err != nil {
		return err
	}
	input := map[string]any{"domain": domain, "revision": current + 1, "updated_at": now}
	if current == 0 {
		_, err = s.internalStoreMutationRows(ctx, "revisions", `insert: $input`, revisionStoreFields, map[string]any{"input": input})
		return err
	}
	_, err = s.internalStoreMutationRows(ctx, "revisions", `where: { domain: { eq: $domain } }, update: $input`, revisionStoreFields, map[string]any{"domain": domain, "input": map[string]any{"revision": current + 1, "updated_at": now}})
	return err
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

func artifactID(ownerID, kind, name string) string {
	sum := sha256.Sum256([]byte(ownerID + ":" + normalizeArtifactKind(kind) + ":" + name))
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
	case strings.Contains(p, "query"), strings.Contains(p, "queries/"):
		return "saved_query"
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
	// GraphQL where clauses arrive as operator maps ({ id: { eq: "..." } });
	// only equality can identify a single row here.
	if m, ok := v.(map[string]interface{}); ok {
		if ev, ok := m["eq"]; ok && ev != nil {
			return fmt.Sprint(ev)
		}
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

func artifactProjectionRow(row map[string]any, rawIDs bool) map[string]any {
	accountID := stringMapValue(row, "account_id")
	ownerID := stringMapValue(row, "owner_id")
	approvedBy := stringMapValue(row, "approved_by")
	return map[string]any{
		"id":               stringMapValue(row, "id"),
		"name":             stringMapValue(row, "name"),
		"kind":             stringMapValue(row, "kind"),
		"path":             stringMapValue(row, "path"),
		"source":           stringMapValue(row, "source"),
		"visibility":       stringMapValue(row, "visibility"),
		"read_only":        boolMapValue(row, "read_only"),
		"account_id":       safeArtifactIdentity(accountID, rawIDs),
		"owner_id":         safeArtifactIdentity(ownerID, rawIDs),
		"content":          stringMapValue(row, "content"),
		"content_json":     row["content_json"],
		"metadata_json":    row["metadata_json"],
		"content_hash":     stringMapValue(row, "content_hash"),
		"status":           artifactStatus(stringMapValue(row, "status")),
		"target_ref":       stringMapValue(row, "target_ref"),
		"tier":             stringMapValue(row, "tier"),
		"catalog_revision": stringMapValue(row, "catalog_revision"),
		"task_id":          stringMapValue(row, "task_id"),
		"approved_by":      safeArtifactIdentity(approvedBy, rawIDs),
		"approved_at":      stringMapValue(row, "approved_at"),
		"revision":         int64MapValue(row, "revision"),
		"account_ref":      safeArtifactIdentity(accountID, false),
		"owner_ref":        safeArtifactIdentity(ownerID, false),
		"author_ref":       safeArtifactIdentity(ownerID, false),
		"approved_ref":     safeArtifactIdentity(approvedBy, false),
		"created_at":       stringMapValue(row, "created_at"),
		"updated_at":       stringMapValue(row, "updated_at"),
	}
}

func stringMapValue(row map[string]any, key string) string {
	if row == nil {
		return ""
	}
	v := row[key]
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	default:
		return fmt.Sprint(t)
	}
}

func boolMapValue(row map[string]any, key string) bool {
	if row == nil {
		return false
	}
	switch t := row[key].(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(t, "true") || t == "1"
	case float64:
		return t != 0
	case int:
		return t != 0
	case int64:
		return t != 0
	default:
		return false
	}
}

func int64MapValue(row map[string]any, key string) int64 {
	if row == nil {
		return 0
	}
	switch t := row[key].(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		v, _ := t.Int64()
		return v
	case string:
		var n int64
		_, _ = fmt.Sscan(t, &n)
		return n
	default:
		return 0
	}
}

func jsonMapString(row map[string]any, key string) string {
	if row == nil || row[key] == nil {
		return ""
	}
	switch v := row[key].(type) {
	case string:
		return v
	case json.RawMessage:
		return string(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
