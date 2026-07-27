package serv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

const (
	artifactKindSavedQuery = "saved_query"
	artifactKindFragment   = "fragment"
	artifactKindWorkflow   = "workflow"
	artifactKindAnnotation = "annotation"
)

func normalizeArtifactKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "query", "saved-query", "saved_query":
		return artifactKindSavedQuery
	case "fragment", "fragments":
		return artifactKindFragment
	case "workflow", "workflows":
		return artifactKindWorkflow
	case "annotation", "annotations":
		return artifactKindAnnotation
	default:
		if strings.TrimSpace(kind) == "" {
			return "artifact"
		}
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func artifactOverlayKey(kind, name string) string {
	return normalizeArtifactKind(kind) + "\x00" + strings.ToLower(strings.TrimSpace(name))
}

func artifactUserID(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	if v := ctx.Value(core.UserIDKey); v != nil {
		s := strings.TrimSpace(fmt.Sprint(v))
		if s != "" {
			return s, true
		}
	}
	return identityVarString(ctx, "user_id")
}

func (s *graphjinService) artifactStoreConfigured() bool {
	_, _, _, ok := s.artifactDB()
	return ok
}

func (s *graphjinService) artifactStoreForUser(ctx context.Context) bool {
	if !s.artifactStoreConfigured() {
		return false
	}
	_, ok := artifactUserID(ctx)
	return ok
}

func splitQualifiedArtifactName(name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}
	if i := strings.LastIndex(name, "."); i > 0 && i < len(name)-1 {
		return name[:i], name[i+1:]
	}
	return "", name
}

func (s *graphjinService) saveSavedQueryArtifactOrFallback(ctx context.Context, req core.SavedQuerySaveRequest) (bool, error) {
	if s == nil || !s.artifactStoreForUser(ctx) {
		return false, nil
	}
	name := qualifyAllowListName(req.Namespace, req.Name)
	if name == "" || len(bytes.TrimSpace(req.Query)) == 0 {
		return false, nil
	}

	content := savedQueryArtifactContent(req)
	meta := map[string]any{
		"namespace": req.Namespace,
		"operation": req.Operation,
	}
	if len(req.ActionJSON) != 0 {
		meta["variables"] = rawMessageMapToAny(req.ActionJSON)
	}
	if _, err := s.saveUserArtifact(ctx, artifactKindSavedQuery, name, string(content), meta); err != nil {
		return true, err
	}

	for _, fragment := range req.Fragments {
		fragName := qualifyAllowListName(req.Namespace, fragment.Name)
		if fragName == "" || len(bytes.TrimSpace(fragment.Value)) == 0 {
			continue
		}
		fragMeta := map[string]any{"namespace": req.Namespace}
		if _, err := s.saveUserArtifact(ctx, artifactKindFragment, fragName, string(bytes.TrimSpace(fragment.Value)), fragMeta); err != nil {
			return true, err
		}
	}
	s.markCatalogChanged("saved query artifact")
	return true, nil
}

func savedQueryArtifactContent(req core.SavedQuerySaveRequest) []byte {
	seen := make(map[string]struct{}, len(req.Fragments))
	var buf bytes.Buffer
	for _, fragment := range req.Fragments {
		fragName := qualifyAllowListName(req.Namespace, fragment.Name)
		if fragName == "" {
			continue
		}
		if _, ok := seen[fragName]; ok {
			continue
		}
		seen[fragName] = struct{}{}
		buf.WriteString(`#import "./fragments/`)
		buf.WriteString(fragName)
		buf.WriteString(`"`)
		buf.WriteByte('\n')
	}
	if buf.Len() != 0 {
		buf.WriteByte('\n')
	}
	buf.Write(bytes.TrimSpace(req.Query))
	return bytes.TrimSpace(buf.Bytes())
}

func rawMessageMapToAny(in map[string]json.RawMessage) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, raw := range in {
		var v any
		if err := json.Unmarshal(raw, &v); err == nil {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (s *graphjinService) saveUserArtifact(ctx context.Context, kind, name, content string, metadata map[string]any) (map[string]any, error) {
	if s == nil {
		return nil, fmt.Errorf("artifact service is not configured")
	}
	if _, ok := artifactUserID(ctx); !ok {
		return nil, fmt.Errorf("gj_artifacts write requires user identity")
	}
	kind = normalizeArtifactKind(kind)
	if err := s.checkArtifactKindWritable(kind); err != nil {
		return nil, err
	}
	meta := ""
	if len(metadata) != 0 {
		b, err := json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
		meta = string(b)
	}
	row, err := newArtifactControlPlane(s).upsertArtifact(ctx, core.ManagedMutationRoot{
		Operation: "upsert",
		Input: map[string]interface{}{
			"name":          name,
			"kind":          kind,
			"content":       content,
			"metadata_json": meta,
		},
	})
	if err != nil {
		return nil, err
	}
	s.markCatalogChanged("artifact mutation")
	return row, nil
}

func (s *graphjinService) deleteUserArtifact(ctx context.Context, kind, name string) error {
	ownerID, ok := artifactUserID(ctx)
	if !ok {
		return fmt.Errorf("gj_artifacts delete requires user identity")
	}
	row, ok, err := s.userArtifactRow(ctx, kind, name)
	if err != nil {
		return err
	}
	id, _ := row["id"].(string)
	if !ok || id == "" {
		id = artifactID(ownerID, kind, name)
	}
	_, err = newArtifactControlPlane(s).deleteArtifact(ctx, core.ManagedMutationRoot{
		Operation: "delete",
		Input:     map[string]interface{}{"id": id, "kind": normalizeArtifactKind(kind)},
	})
	if err == nil {
		s.markCatalogChanged("artifact delete")
	}
	return err
}

func (s *graphjinService) userArtifactRows(ctx context.Context) ([]map[string]any, error) {
	if s == nil || !s.artifactStoreForUser(ctx) {
		return nil, nil
	}
	return newArtifactControlPlane(s).dbArtifactRows(ctx)
}

func (s *graphjinService) userArtifactRow(ctx context.Context, kind, name string) (map[string]any, bool, error) {
	rows, err := s.userArtifactRows(ctx)
	if err != nil {
		return nil, false, err
	}
	key := artifactOverlayKey(kind, name)
	for _, row := range rows {
		rowName, _ := row["name"].(string)
		rowKind, _ := row["kind"].(string)
		if artifactOverlayKey(rowKind, rowName) == key {
			return row, true, nil
		}
	}
	return nil, false, nil
}

func (s *graphjinService) executeSavedQueryByName(ctx context.Context, name string, vars json.RawMessage, rc *core.RequestConfig) (*core.Result, error) {
	if s == nil || s.gj == nil {
		return nil, fmt.Errorf("GraphJin engine is not initialized")
	}
	ctx = s.applyIdentityContext(ctx)
	qualified := strings.TrimSpace(name)
	if rc != nil {
		if ns, ok := rc.GetNamespace(); ok {
			qualified = qualifyAllowListName(ns, qualified)
		}
	}
	details, fromArtifact, err := s.getSavedQueryForContext(ctx, qualified)
	if err != nil {
		return nil, err
	}
	if fromArtifact {
		return s.gj.GraphQLBySavedQuery(ctx, details, vars, rc)
	}
	return s.gj.GraphQLByName(ctx, qualified, vars, rc)
}

func (s *graphjinService) listSavedQueriesForContext(ctx context.Context) ([]core.SavedQueryInfo, error) {
	ctx = s.applyIdentityContext(ctx)
	global, err := s.gj.ListSavedQueries()
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]core.SavedQueryInfo, len(global))
	for _, q := range global {
		qualified := qualifyAllowListName(q.Namespace, q.Name)
		byKey[artifactOverlayKey(artifactKindSavedQuery, qualified)] = q
	}
	rows, err := s.userArtifactRows(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !artifactKindMatches(row["kind"], artifactKindSavedQuery) {
			continue
		}
		name, _ := row["name"].(string)
		ns, base := splitQualifiedArtifactName(name)
		byKey[artifactOverlayKey(artifactKindSavedQuery, name)] = core.SavedQueryInfo{
			Name:      base,
			Namespace: ns,
			Operation: savedQueryOperation(rowContent(row), metadataMap(row)),
		}
	}
	out := make([]core.SavedQueryInfo, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *graphjinService) getSavedQueryForContext(ctx context.Context, qualifiedName string) (*core.SavedQueryDetails, bool, error) {
	ctx = s.applyIdentityContext(ctx)
	qualifiedName = strings.TrimSpace(qualifiedName)
	if qualifiedName == "" {
		return nil, false, fmt.Errorf("query name is required")
	}
	if row, ok, err := s.userArtifactRow(ctx, artifactKindSavedQuery, qualifiedName); err != nil {
		return nil, false, err
	} else if ok {
		return s.savedQueryDetailsFromArtifact(ctx, row)
	}
	details, err := s.gj.GetSavedQuery(qualifiedName)
	return details, false, err
}

func (s *graphjinService) savedQueryDetailsFromArtifact(ctx context.Context, row map[string]any) (*core.SavedQueryDetails, bool, error) {
	name, _ := row["name"].(string)
	ns, base := splitQualifiedArtifactName(name)
	fs, path, err := s.savedQueryOverlayFS(ctx, row)
	if err != nil {
		return nil, true, err
	}
	query, err := core.ResolveGraphQLFile(fs, path)
	if err != nil {
		return nil, true, err
	}
	meta := metadataMap(row)
	return &core.SavedQueryDetails{
		Name:      base,
		Namespace: ns,
		Operation: metadataOperation(meta),
		Query:     string(query),
		Variables: metadataVariables(meta),
	}, true, nil
}

func (s *graphjinService) listFragmentsForContext(ctx context.Context) ([]core.FragmentInfo, error) {
	ctx = s.applyIdentityContext(ctx)
	global, err := s.gj.ListFragments()
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]core.FragmentInfo, len(global))
	for _, f := range global {
		qualified := qualifyAllowListName(f.Namespace, f.Name)
		byKey[artifactOverlayKey(artifactKindFragment, qualified)] = f
	}
	rows, err := s.userArtifactRows(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if !artifactKindMatches(row["kind"], artifactKindFragment) {
			continue
		}
		name, _ := row["name"].(string)
		ns, base := splitQualifiedArtifactName(name)
		byKey[artifactOverlayKey(artifactKindFragment, name)] = core.FragmentInfo{Name: base, Namespace: ns}
	}
	out := make([]core.FragmentInfo, 0, len(byKey))
	for _, item := range byKey {
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (s *graphjinService) getFragmentForContext(ctx context.Context, qualifiedName string) (*core.FragmentDetails, bool, error) {
	ctx = s.applyIdentityContext(ctx)
	qualifiedName = strings.TrimSpace(qualifiedName)
	if qualifiedName == "" {
		return nil, false, fmt.Errorf("fragment name is required")
	}
	if row, ok, err := s.userArtifactRow(ctx, artifactKindFragment, qualifiedName); err != nil {
		return nil, false, err
	} else if ok {
		name, _ := row["name"].(string)
		ns, base := splitQualifiedArtifactName(name)
		def := rowContent(row)
		return &core.FragmentDetails{
			Name:       base,
			Namespace:  ns,
			Definition: def,
			On:         fragmentOnType(def),
		}, true, nil
	}
	details, err := s.gj.GetFragment(qualifiedName)
	return details, false, err
}

func (s *graphjinService) catalogBuildOptionsForContext(ctx context.Context) core.CatalogBuildOptions {
	opts := s.catalogBuildOptions()
	ctx = s.applyIdentityContext(ctx)
	if !s.artifactStoreForUser(ctx) {
		return opts
	}
	if fragments, err := s.catalogFragmentsForContext(ctx); err == nil {
		opts.Fragments = fragments
	}
	if queries, err := s.catalogSavedQueriesForContext(ctx); err == nil {
		opts.SavedQueries = queries
	}
	if workflows, err := s.catalogWorkflowsForContext(ctx); err == nil {
		opts.Workflows = workflows
	}
	return opts
}

func (s *graphjinService) catalogSnapshotForContext(ctx context.Context) (*core.CatalogSnapshot, error) {
	ctx = s.applyIdentityContext(ctx)
	if !s.artifactStoreForUser(ctx) {
		return s.catalogSnapshot()
	}
	var md *core.MetadataSnapshot
	var err error
	if s.gj != nil {
		md, err = s.gj.MetadataSnapshot(s.metadataSnapshotExcludes()...)
		if err != nil {
			return nil, err
		}
	} else {
		md = &core.MetadataSnapshot{}
	}
	var conf *core.Config
	if s.conf != nil {
		conf = &s.conf.Core
	}
	return core.BuildCatalogSnapshotWithOptions(md, conf, s.catalogBuildOptionsForContext(ctx)), nil
}

func (s *graphjinService) catalogFragmentsForContext(ctx context.Context) ([]core.CatalogFragment, error) {
	fragments, err := s.listFragmentsForContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.CatalogFragment, 0, len(fragments))
	for _, fragment := range fragments {
		details, _, err := s.getFragmentForContext(ctx, qualifyAllowListName(fragment.Namespace, fragment.Name))
		if err != nil {
			continue
		}
		out = append(out, core.CatalogFragment{
			Name:       details.Name,
			Namespace:  details.Namespace,
			Definition: details.Definition,
			On:         details.On,
			SourceHash: hashString(details.Definition),
		})
	}
	return out, nil
}

func (s *graphjinService) catalogSavedQueriesForContext(ctx context.Context) ([]core.CatalogSavedQuery, error) {
	queries, err := s.listSavedQueriesForContext(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]core.CatalogSavedQuery, 0, len(queries))
	for _, query := range queries {
		details, _, err := s.getSavedQueryForContext(ctx, qualifyAllowListName(query.Namespace, query.Name))
		if err != nil {
			continue
		}
		out = append(out, core.CatalogSavedQuery{
			Name:       details.Name,
			Namespace:  details.Namespace,
			Operation:  details.Operation,
			Query:      details.Query,
			Variables:  details.Variables,
			SourceHash: hashString(details.Query),
		})
	}
	return out, nil
}

func (s *graphjinService) workflowSnapshotForContext(ctx context.Context, timeoutSeconds int) (workflowRegistrySnapshot, error) {
	snap := s.workflowSnapshot(timeoutSeconds)
	ctx = s.applyIdentityContext(ctx)
	if !s.artifactStoreForUser(ctx) {
		return snap, nil
	}
	rows, err := s.userArtifactRows(ctx)
	if err != nil {
		return snap, err
	}
	byName := make(map[string]core.CatalogWorkflow, len(snap.workflows))
	for _, wf := range snap.workflows {
		byName[wf.Name] = wf
	}
	for _, row := range rows {
		if !artifactKindMatches(row["kind"], artifactKindWorkflow) {
			continue
		}
		wf, err := s.workflowFromArtifactRow(row, timeoutSeconds)
		if err != nil {
			continue
		}
		byName[wf.Name] = wf
	}
	snap.workflows = make([]core.CatalogWorkflow, 0, len(byName))
	for _, wf := range byName {
		snap.workflows = append(snap.workflows, wf)
	}
	sort.Slice(snap.workflows, func(i, j int) bool { return snap.workflows[i].Name < snap.workflows[j].Name })
	snap.revision = workflowRegistryRevision(snap.workflows)
	return snap, nil
}

func (s *graphjinService) catalogWorkflowsForContext(ctx context.Context) ([]core.CatalogWorkflow, error) {
	snap, err := s.workflowSnapshotForContext(ctx, s.workflowTimeoutSeconds())
	if err != nil {
		return nil, err
	}
	return snap.workflows, nil
}

func (s *graphjinService) workflowFromArtifactRow(row map[string]any, timeoutSeconds int) (core.CatalogWorkflow, error) {
	name, _ := row["name"].(string)
	_, base := splitQualifiedArtifactName(name)
	if base == "" {
		return core.CatalogWorkflow{}, fmt.Errorf("artifact workflow name is empty")
	}
	src := rowContent(row)
	meta := workflowMetaFromArtifact(row)
	sum := sha256.Sum256([]byte(src))
	return core.CatalogWorkflow{
		Name:           base,
		Description:    meta.Description,
		Tags:           append([]string(nil), meta.Tags...),
		Variables:      catalogWorkflowVariables(meta.Variables),
		Path:           "artifact:" + name,
		SourceHash:     hex.EncodeToString(sum[:]),
		Runtime:        s.workflowRuntime(),
		TimeoutSeconds: timeoutSeconds,
		CreatedAt:      artifactTime(row, "created_at", meta.CreatedAt),
		UpdatedAt:      artifactTime(row, "updated_at", meta.UpdatedAt),
	}, nil
}

func (s *graphjinService) resolveWorkflowForContext(ctx context.Context, name string) (core.CatalogWorkflow, string, bool, error) {
	ctx = s.applyIdentityContext(ctx)
	if row, ok, err := s.userArtifactRow(ctx, artifactKindWorkflow, name); err != nil {
		return core.CatalogWorkflow{}, "", false, err
	} else if ok {
		wf, err := s.workflowFromArtifactRow(row, s.workflowTimeoutSeconds())
		return wf, rowContent(row), true, err
	}
	wf, ok := s.workflowByName(name, s.workflowTimeoutSeconds())
	if !ok {
		return core.CatalogWorkflow{}, "", false, fmt.Errorf("workflow not found: %s", name)
	}
	src, err := s.fs.Get(wf.Path)
	if err != nil {
		return core.CatalogWorkflow{}, "", false, fmt.Errorf("workflow not found: %s", name)
	}
	return wf, string(src), false, nil
}

func workflowMetaFromArtifact(row map[string]any) WorkflowMeta {
	meta := WorkflowMeta{}
	if err := decodeMetadataValue(row["metadata_json"], &meta); err == nil {
		return catalogWorkflowMeta(meta)
	}
	if parsed, ok := parseWorkflowMeta(rowContent(row)); ok {
		return catalogWorkflowMeta(parsed)
	}
	return meta
}

func workflowMetaMap(meta WorkflowMeta) map[string]any {
	return map[string]any{
		"description": meta.Description,
		"tags":        meta.Tags,
		"variables":   meta.Variables,
		"created_at":  meta.CreatedAt,
		"updated_at":  meta.UpdatedAt,
	}
}

func decodeMetadataValue(v any, out any) error {
	switch t := v.(type) {
	case nil:
		return fmt.Errorf("metadata is empty")
	case string:
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("metadata is empty")
		}
		return json.Unmarshal([]byte(t), out)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, out)
	}
}

func rowContent(row map[string]any) string {
	if row == nil {
		return ""
	}
	if s, _ := row["content"].(string); s != "" {
		return s
	}
	if v := row["content_json"]; v != nil {
		b, err := json.Marshal(v)
		if err == nil {
			return string(b)
		}
	}
	return ""
}

func metadataMap(row map[string]any) map[string]any {
	if row == nil {
		return nil
	}
	switch v := row["metadata_json"].(type) {
	case map[string]any:
		return v
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out
		}
	default:
		b, err := json.Marshal(v)
		if err == nil {
			var out map[string]any
			if err := json.Unmarshal(b, &out); err == nil {
				return out
			}
		}
	}
	return nil
}

func metadataVariables(meta map[string]any) map[string]interface{} {
	if len(meta) == 0 {
		return nil
	}
	vars, _ := meta["variables"].(map[string]interface{})
	if len(vars) != 0 {
		return vars
	}
	return nil
}

func savedQueryOperation(query string, meta map[string]any) string {
	if op := metadataOperation(meta); op != "" {
		return op
	}
	q := strings.TrimSpace(query)
	switch {
	case strings.HasPrefix(q, "mutation"):
		return "mutation"
	case strings.HasPrefix(q, "subscription"):
		return "subscription"
	default:
		return "query"
	}
}

func metadataOperation(meta map[string]any) string {
	if op, _ := meta["operation"].(string); strings.TrimSpace(op) != "" {
		return strings.ToLower(strings.TrimSpace(op))
	}
	return ""
}

func fragmentOnType(def string) string {
	if idx := strings.Index(def, " on "); idx != -1 {
		rest := def[idx+4:]
		if endIdx := strings.IndexAny(rest, " {"); endIdx != -1 {
			return strings.TrimSpace(rest[:endIdx])
		}
	}
	return ""
}

func artifactKindMatches(raw any, want string) bool {
	return normalizeArtifactKind(fmt.Sprint(raw)) == normalizeArtifactKind(want)
}

func artifactTime(row map[string]any, key, fallback string) string {
	if v, _ := row[key].(string); strings.TrimSpace(v) != "" {
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return formatWorkflowTime(t)
		}
		return v
	}
	return normalizeWorkflowTimestamp(fallback)
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type artifactGraphQLFS struct {
	base core.FS
	data map[string][]byte
}

func (fs artifactGraphQLFS) Get(path string) ([]byte, error) {
	key := artifactFSKey(path)
	if b, ok := fs.data[key]; ok {
		return append([]byte(nil), b...), nil
	}
	return fs.base.Get(path)
}

func (fs artifactGraphQLFS) Put(path string, data []byte) error {
	return fmt.Errorf("artifact overlay filesystem is read-only")
}

func (fs artifactGraphQLFS) Exists(path string) (bool, error) {
	key := artifactFSKey(path)
	if _, ok := fs.data[key]; ok {
		return true, nil
	}
	return fs.base.Exists(path)
}

func (fs artifactGraphQLFS) List(path string) ([]string, error) {
	entries := map[string]struct{}{}
	if fs.base != nil {
		base, _ := fs.base.List(path)
		for _, entry := range base {
			entries[entry] = struct{}{}
		}
	}
	prefix := strings.TrimSuffix(artifactFSKey(path), "/") + "/"
	for key := range fs.data {
		if strings.HasPrefix(key, prefix) {
			rest := strings.TrimPrefix(key, prefix)
			if rest != "" && !strings.Contains(rest, "/") {
				entries[rest] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(entries))
	for entry := range entries {
		out = append(out, entry)
	}
	sort.Strings(out)
	return out, nil
}

func (s *graphjinService) savedQueryOverlayFS(ctx context.Context, row map[string]any) (core.FS, string, error) {
	name, _ := row["name"].(string)
	if name == "" {
		return nil, "", fmt.Errorf("saved query artifact is missing name")
	}
	path := filepath.Join("/queries", name+".gql")
	data := map[string][]byte{artifactFSKey(path): []byte(rowContent(row))}
	rows, err := s.userArtifactRows(ctx)
	if err != nil {
		return nil, "", err
	}
	for _, r := range rows {
		rowName, _ := r["name"].(string)
		if rowName == "" {
			continue
		}
		switch {
		case artifactKindMatches(r["kind"], artifactKindSavedQuery):
			data[artifactFSKey(filepath.Join("/queries", rowName+".gql"))] = []byte(rowContent(r))
		case artifactKindMatches(r["kind"], artifactKindFragment):
			data[artifactFSKey(filepath.Join("/queries/fragments", rowName+".gql"))] = []byte(rowContent(r))
		}
	}
	return artifactGraphQLFS{base: s.fs, data: data}, path, nil
}

func artifactFSKey(path string) string {
	path = filepath.ToSlash(filepath.Clean(path))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}
