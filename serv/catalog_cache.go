package serv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	gjagent "github.com/dosco/graphjin/agent/v3"
	"github.com/dosco/graphjin/core/v3"
)

type workflowStatFS interface {
	Stat(path string) (os.FileInfo, error)
}

const workflowTimestampFormat = "2006-01-02T15:04:05.000000000Z07:00"

type catalogCacheEntry struct {
	revision  string
	snapshot  *core.CatalogSnapshot
	discovery *gjagent.CatalogDiscoveryIndex
}

func (s *graphjinService) catalogSnapshot() (*core.CatalogSnapshot, error) {
	if s == nil {
		return core.BuildCatalogSnapshot(&core.MetadataSnapshot{}, nil), nil
	}
	if s.conf != nil && !s.conf.Core.CatalogEnabled() {
		return core.BuildCatalogSnapshot(&core.MetadataSnapshot{}, nil), nil
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

	opts := s.catalogBuildOptions()
	var conf *core.Config
	if s.conf != nil {
		conf = &s.conf.Core
	}
	sourceRevisions := core.CatalogSourceRevisions(md, conf, opts)
	revision := core.CatalogRevisionFromSourceRevisions(sourceRevisions)

	s.catalogMu.Lock()
	if s.catalogCache != nil && s.catalogCache.revision == revision {
		snapshot := s.catalogCache.snapshot
		s.catalogMu.Unlock()
		return snapshot, nil
	}
	s.catalogMu.Unlock()

	snapshot := core.BuildCatalogSnapshotWithOptions(md, conf, opts)

	s.catalogMu.Lock()
	entry := &catalogCacheEntry{revision: snapshot.Revision, snapshot: snapshot}
	if s.conf != nil && s.conf.Agent.Selection.Enabled {
		entry.discovery = gjagent.NewCatalogDiscoveryIndex(snapshot)
	}
	s.catalogCache = entry
	s.catalogMu.Unlock()
	return snapshot, nil
}

// maxContextCatalogSnapshots bounds the per-caller snapshots kept in memory.
// Callers with the same visible metadata and options share one entry.
const maxContextCatalogSnapshots = 16

type contextCatalogEntry struct {
	metadata *core.MetadataSnapshot
	snapshot *core.CatalogSnapshot
}

// buildCatalogSnapshotCached returns the snapshot for md, the caller's view of
// base. A build indexes every card for search, which takes seconds on a large
// schema, so the snapshot is reused until an input changes. GraphJin keeps
// one base snapshot per schema, so its identity stands for the tables and
// columns, and only the small inputs are hashed on each call.
func (s *graphjinService) buildCatalogSnapshotCached(base, md *core.MetadataSnapshot, conf *core.Config, opts core.CatalogBuildOptions) *core.CatalogSnapshot {
	if s == nil || base == nil || md == nil {
		return core.BuildCatalogSnapshotWithOptions(md, conf, opts)
	}
	key := catalogSnapshotKey(base, md, conf, opts)
	s.catalogMu.Lock()
	if entry, ok := s.contextCatalogCache[key]; ok && entry.metadata == base {
		s.catalogMu.Unlock()
		return entry.snapshot
	}
	s.catalogMu.Unlock()

	snapshot := core.BuildCatalogSnapshotWithOptions(md, conf, opts)

	s.catalogMu.Lock()
	if s.contextCatalogCache == nil || len(s.contextCatalogCache) >= maxContextCatalogSnapshots {
		s.contextCatalogCache = make(map[string]contextCatalogEntry)
	}
	s.contextCatalogCache[key] = contextCatalogEntry{metadata: base, snapshot: snapshot}
	s.catalogMu.Unlock()
	return snapshot
}

// catalogSnapshotKey hashes every build input except the base tables and
// columns, which the cache entry matches by identity.
func catalogSnapshotKey(base, md *core.MetadataSnapshot, conf *core.Config, opts core.CatalogBuildOptions) string {
	small := &core.MetadataSnapshot{Databases: md.Databases, APIOperations: md.APIOperations}
	revision := core.CatalogRevisionFromSourceRevisions(core.CatalogSourceRevisions(small, conf, opts))
	data, _ := json.Marshal(struct {
		Databases     []core.MetadataDatabase
		APIOperations []core.MetadataAPIOperation
		Options       core.CatalogBuildOptions
	}{md.Databases, md.APIOperations, opts})
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("%p\x00%s\x00", base, revision)), data...))
	return hex.EncodeToString(sum[:])
}

func (s *graphjinService) invalidateCatalogCache() {
	if s == nil {
		return
	}
	s.catalogMu.Lock()
	s.catalogCache = nil
	s.contextCatalogCache = nil
	s.catalogMu.Unlock()
}

func (s *graphjinService) markWorkflowChanged(reason string) {
	if s == nil {
		return
	}
	s.invalidateWorkflowRegistry()
	s.markCatalogChanged(reason)
}

func (s *graphjinService) markCatalogChanged(reason string) {
	if s == nil {
		return
	}
	s.invalidateCatalogCache()
	if semantic := s.semanticCatalog(); semantic != nil {
		semantic.CatalogChanged()
	}
	if s.systemNanoDB != nil {
		s.markSystemNanoChanged(reason)
		return
	}
	if err := s.refreshMetadataCatalogMirror(); err != nil && s.log != nil {
		if strings.TrimSpace(reason) == "" {
			reason = "catalog change"
		}
		s.log.Warnf("metadata catalog refresh after %s failed: %v", reason, err)
		s.recordRuntimeEvent(context.Background(), runtimeEvent{
			Phase:      "catalog",
			Status:     runtimeStatusDegraded,
			Severity:   "warn",
			Summary:    "Metadata catalog refresh failed after a runtime change.",
			NextAction: "Check recent config/schema events, then retry catalog discovery or reload schema.",
			ErrorCode:  "catalog_refresh_failed",
			Details:    map[string]any{"reason": reason, "error": err.Error()},
		})
	}
}

func (s *graphjinService) catalogBuildOptions() core.CatalogBuildOptions {
	opts := core.CatalogBuildOptions{
		WorkflowRuntime:        "goja",
		WorkflowTimeoutSeconds: s.workflowTimeoutSeconds(),
	}
	if s == nil {
		return opts
	}
	// Enum values belong here rather than at one build site: gj_catalog is served
	// from more than one snapshot path (the cache and the per-request artifact
	// overlay), and a card that shows real values on one path and a placeholder on
	// another is worse than neither.
	opts.ObservedColumnValues = s.observedColumnValues()
	if s.conf != nil {
		opts.EnabledTools = mcpToolList(s.conf)
		opts.EnabledToolsKnown = true
	}
	if s.conf != nil && s.conf.workflowsEnabled() {
		workflowSnap := s.workflowSnapshot(opts.WorkflowTimeoutSeconds)
		opts.Workflows = workflowSnap.workflows
	}
	if s.gj != nil {
		opts.Fragments = catalogFragmentsFromGraphJin(s.gj)
		opts.SavedQueries = catalogSavedQueriesFromGraphJin(s.gj)
	}
	return opts
}

func catalogFragmentsFromGraphJin(gj *core.GraphJin) []core.CatalogFragment {
	fragments, err := gj.ListFragments()
	if err != nil {
		return nil
	}
	out := make([]core.CatalogFragment, 0, len(fragments))
	for _, fragment := range fragments {
		details, err := gj.GetFragment(qualifyAllowListName(fragment.Namespace, fragment.Name))
		if err != nil {
			continue
		}
		out = append(out, core.CatalogFragment{
			Name:       details.Name,
			Namespace:  details.Namespace,
			Definition: details.Definition,
			On:         details.On,
		})
	}
	return out
}

func catalogSavedQueriesFromGraphJin(gj *core.GraphJin) []core.CatalogSavedQuery {
	queries, err := gj.ListSavedQueries()
	if err != nil {
		return nil
	}
	out := make([]core.CatalogSavedQuery, 0, len(queries))
	for _, query := range queries {
		details, err := gj.GetSavedQuery(qualifyAllowListName(query.Namespace, query.Name))
		if err != nil {
			continue
		}
		out = append(out, core.CatalogSavedQuery{
			Name:      details.Name,
			Namespace: details.Namespace,
			Operation: details.Operation,
			Query:     details.Query,
			Variables: details.Variables,
		})
	}
	return out
}

func (s *graphjinService) workflowTimeoutSeconds() int {
	if s != nil && s.conf != nil && s.conf.MCP.WorkflowTimeout > 0 {
		return s.conf.MCP.WorkflowTimeout
	}
	return defaultWorkflowScriptTimeout
}

func (s *graphjinService) workflowTimestamps(path string, meta WorkflowMeta, now time.Time) (string, string) {
	modifiedAt, hasModifiedAt := s.workflowModifiedAt(path)
	createdAt := normalizeWorkflowTimestamp(meta.CreatedAt)
	if createdAt == "" {
		if hasModifiedAt {
			createdAt = formatWorkflowTime(modifiedAt)
		} else {
			createdAt = formatWorkflowTime(now)
		}
	}

	updatedAt := ""
	if hasModifiedAt {
		updatedAt = formatWorkflowTime(modifiedAt)
	} else {
		updatedAt = normalizeWorkflowTimestamp(meta.UpdatedAt)
		if updatedAt == "" {
			updatedAt = createdAt
		}
	}
	return createdAt, updatedAt
}

func (s *graphjinService) workflowModifiedAt(path string) (time.Time, bool) {
	if s == nil || s.fs == nil {
		return time.Time{}, false
	}
	fs, ok := s.fs.(workflowStatFS)
	if !ok {
		return time.Time{}, false
	}
	info, err := fs.Stat(path)
	if err != nil || info == nil || info.ModTime().IsZero() {
		return time.Time{}, false
	}
	return info.ModTime().UTC(), true
}

func normalizeWorkflowTimestamp(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return ""
	}
	return formatWorkflowTime(t)
}

func formatWorkflowTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(workflowTimestampFormat)
}

func catalogWorkflowMeta(meta WorkflowMeta) WorkflowMeta {
	meta.Description = strings.TrimSpace(meta.Description)
	meta.Tags = sanitizeWorkflowTags(meta.Tags)
	meta.Variables = sanitizeWorkflowVariables(meta.Variables)
	meta.CreatedAt = normalizeWorkflowTimestamp(meta.CreatedAt)
	meta.UpdatedAt = normalizeWorkflowTimestamp(meta.UpdatedAt)
	return meta
}

func sanitizeWorkflowTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func sanitizeWorkflowVariables(vars []WorkflowVariable) []WorkflowVariable {
	out := make([]WorkflowVariable, 0, len(vars))
	seen := make(map[string]struct{}, len(vars))
	for _, v := range vars {
		v.Name = strings.TrimSpace(v.Name)
		if v.Name == "" || !workflowVariableNameRe.MatchString(v.Name) {
			continue
		}
		if _, ok := seen[v.Name]; ok {
			continue
		}
		seen[v.Name] = struct{}{}
		v.Type = strings.TrimSpace(v.Type)
		v.Description = strings.TrimSpace(v.Description)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func catalogWorkflowVariables(vars []WorkflowVariable) []core.CatalogWorkflowVariable {
	out := make([]core.CatalogWorkflowVariable, len(vars))
	for i, v := range vars {
		out[i] = core.CatalogWorkflowVariable{
			Name:        v.Name,
			Type:        v.Type,
			Description: v.Description,
			Required:    v.Required,
		}
	}
	return out
}
