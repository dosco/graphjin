package codesql

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	changeStatusPreviewed = "previewed"
	changeStatusApplied   = "applied"
	changeStatusFailed    = "failed"
	changeStatusConflict  = "conflict"

	lockStatusActive   = "active"
	lockStatusReleased = "released"
	lockStatusExpired  = "expired"

	defaultLockTTL = 120 * time.Second
	maxLockTTL     = 10 * time.Minute
)

const (
	editOpReplace = "replace"
	editOpCreate  = "create"
	editOpDelete  = "delete"
	editOpRename  = "rename"
)

// MutationRequest describes GraphQL roots routed to the CodeSQL mutation bridge.
type MutationRequest struct {
	Database  string
	Operation string
	Roots     []MutationRoot
}

// MutationRoot describes one managed mutation root.
type MutationRoot struct {
	FieldName string
	Table     string
	Operation string
	Input     map[string]interface{}
	Fields    []MutationField
}

// MutationField describes a selected return field.
type MutationField struct {
	Name   string
	Column string
}

type byteRange struct {
	StartByte int64 `json:"start_byte"`
	EndByte   int64 `json:"end_byte"`
}

type replacementSpec struct {
	StartByte int64  `json:"start_byte"`
	EndByte   int64  `json:"end_byte"`
	OldText   string `json:"old_text"`
	NewText   string `json:"new_text"`
	WholeFile bool   `json:"whole_file,omitempty"`
}

type editSpec struct {
	Op           string            `json:"op,omitempty"`
	Path         string            `json:"path"`
	NewPath      string            `json:"new_path,omitempty"`
	ExpectedHash string            `json:"expected_hash,omitempty"`
	Content      *string           `json:"content,omitempty"`
	Mkdirs       bool              `json:"mkdirs,omitempty"`
	Replacements []replacementSpec `json:"replacements,omitempty"`
	LockTokens   []string          `json:"lock_tokens,omitempty"`
	WholeFile    bool              `json:"whole_file,omitempty"`
}

type sourceLock struct {
	ID        int64
	Path      string
	Ranges    []byteRange
	Owner     string
	Token     string
	Status    string
	ExpiresAt time.Time
	WholeFile bool
}

type changePlan struct {
	replacements map[string]*plannedReplacement
	lifecycle    []plannedLifecycle
	changed      map[string]struct{}
	diff         strings.Builder
	audit        []plannedAudit
}

type plannedReplacement struct {
	Rel   string
	Full  string
	Data  []byte
	Mode  fs.FileMode
	Repls []replacementSpec
	Next  []byte
}

type plannedLifecycle struct {
	Op      string
	Rel     string
	Full    string
	NewRel  string
	NewFull string
	Content []byte
	Mkdirs  bool
	Data    []byte
	Mode    fs.FileMode
}

type plannedAudit struct {
	Op      string
	Path    string
	NewPath string
}

type filesystemAction struct {
	apply    func() error
	rollback func() error
	finalize func() error
}

// ManagedMutationTables returns CodeSQL's service-managed mutation tables.
func ManagedMutationTables() []string {
	return []string{"gj_code"}
}

// ExecuteManagedMutation handles CodeSQL preview/apply/lock GraphQL mutations.
func (m *Managed) ExecuteManagedMutation(ctx context.Context, req MutationRequest) (json.RawMessage, error) {
	if m == nil || m.DB == nil {
		return nil, errors.New("codesql managed database is not open")
	}
	out := make(map[string]interface{}, len(req.Roots))
	needsRefresh := false
	for _, root := range req.Roots {
		var row map[string]interface{}
		var err error
		switch root.Table {
		case "gj_code":
			row, err = m.handleGJCodeMutation(ctx, root)
			needsRefresh = true
		case "code_change_sets":
			row, err = m.handleChangeSetMutation(ctx, root)
			needsRefresh = true
		case "code_locks":
			row, err = m.handleLockMutation(ctx, root)
			needsRefresh = true
		default:
			err = fmt.Errorf("table %q is not a managed CodeSQL mutation table", root.Table)
		}
		if err != nil {
			return nil, err
		}
		out[root.FieldName] = filterMutationFields(row, root.Fields)
	}
	if needsRefresh {
		if err := RefreshPublicGraph(ctx, m.DB); err != nil {
			if !strings.Contains(err.Error(), "SQLITE_BUSY") && !strings.Contains(err.Error(), "database is locked") {
				return nil, err
			}
		}
	}
	return json.Marshal(out)
}

func (m *Managed) handleGJCodeMutation(ctx context.Context, root MutationRoot) (map[string]interface{}, error) {
	kind := strings.ToLower(stringInput(root.Input, "kind"))
	switch kind {
	case "change_set", "changeset":
		row, err := m.handleChangeSetMutation(ctx, root)
		if err != nil {
			return nil, err
		}
		row["kind"] = "change_set"
		row["errors_json"] = row["errors"]
		return row, nil
	case "lock":
		row, err := m.handleLockMutation(ctx, root)
		if err != nil {
			return nil, err
		}
		row["kind"] = "lock"
		row["errors_json"] = row["errors"]
		return row, nil
	default:
		return nil, fmt.Errorf("gj_code kind must be change_set or lock for mutations")
	}
}

func (m *Managed) handleChangeSetMutation(ctx context.Context, root MutationRoot) (map[string]interface{}, error) {
	action := strings.ToLower(stringInput(root.Input, "action"))
	if action == "" && root.Operation == "insert" {
		action = "preview"
	}
	switch action {
	case "preview":
		return m.previewChangeSet(ctx, root.Input)
	case "apply":
		return m.applyChangeSet(ctx, root.Input)
	default:
		return nil, fmt.Errorf("code_change_sets action must be preview or apply")
	}
}

func (m *Managed) previewChangeSet(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	edits, err := parseEditSpecs(input)
	if err != nil {
		return nil, err
	}
	title := stringInput(input, "title")
	owner := stringInput(input, "owner")
	rootTokens := stringListInput(input, "lock_tokens")
	if token := stringInput(input, "lease_token"); token != "" {
		rootTokens = append(rootTokens, token)
	}
	for i := range edits {
		edits[i].LockTokens = append(edits[i].LockTokens, rootTokens...)
	}

	status, errs, diff := m.validateAndDiff(ctx, edits, nil)
	if len(errs) == 0 {
		status = changeStatusPreviewed
	}

	editsJSON := mustJSON(edits)
	errorsJSON := mustJSON(errs)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := m.DB.ExecContext(ctx, `INSERT INTO code_change_sets(action, title, owner, edits, lock_tokens, status, diff, errors, files_changed, files_reindexed, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, '[]', '[]', ?, ?)`,
		"preview", title, owner, editsJSON, mustJSON(rootTokens), status, diff, errorsJSON, now, now)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	_ = m.audit(ctx, id, 0, "preview", "", status, map[string]interface{}{"errors": errs})

	return map[string]interface{}{
		"id":              id,
		"action":          "preview",
		"title":           title,
		"owner":           owner,
		"edits":           edits,
		"lock_tokens":     rootTokens,
		"status":          status,
		"diff":            diff,
		"errors":          errs,
		"files_changed":   []string{},
		"files_reindexed": []string{},
		"created_at":      now,
		"updated_at":      now,
		"applied_at":      nil,
	}, nil
}

func (m *Managed) applyChangeSet(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	id, ok := int64Input(input, "id")
	if !ok || id <= 0 {
		return nil, fmt.Errorf("code_change_sets apply requires id in the mutation input")
	}
	rootTokens := stringListInput(input, "lock_tokens")
	if token := stringInput(input, "lease_token"); token != "" {
		rootTokens = append(rootTokens, token)
	}

	var title, owner, editsRaw string
	err := m.DB.QueryRowContext(ctx, `SELECT title, owner, edits FROM code_change_sets WHERE id = ?`, id).Scan(&title, &owner, &editsRaw)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("code_change_sets id %d not found", id)
	}
	if err != nil {
		return nil, err
	}
	var edits []editSpec
	if err := json.Unmarshal([]byte(editsRaw), &edits); err != nil {
		return nil, err
	}
	for i := range edits {
		edits[i].LockTokens = append(edits[i].LockTokens, rootTokens...)
	}

	m.lockMu.Lock()
	defer m.lockMu.Unlock()
	if err := m.ensureLocksLoadedLocked(ctx); err != nil {
		return nil, err
	}
	m.cleanupExpiredLocksLocked(ctx, time.Now().UTC())

	plan, errs := m.buildChangePlanLocked(edits, tokenSet(rootTokens))
	if len(errs) != 0 {
		status := conflictStatus(errs)
		_ = m.updateChangeSetFailure(ctx, id, status, errs)
		_ = m.audit(ctx, id, 0, "apply_failed", "", status, map[string]interface{}{"errors": errs})
		return changeSetRow(id, "apply", title, owner, edits, status, "", errs, nil, nil, "", time.Now().UTC()), nil
	}

	changed, err := m.applyChangePlanLocked(plan)
	if err != nil {
		errs = []string{err.Error()}
		_ = m.updateChangeSetFailure(ctx, id, changeStatusFailed, errs)
		_ = m.audit(ctx, id, 0, "apply_failed", "", err.Error(), nil)
		return changeSetRow(id, "apply", title, owner, edits, changeStatusFailed, "", errs, changed, nil, "", time.Now().UTC()), nil
	}

	reindexed := append([]string(nil), changed...)
	if m.reconciler != nil {
		if _, err := m.reconciler.Reconcile(ctx); err != nil {
			errs = []string{err.Error()}
			_ = m.updateChangeSetFailure(ctx, id, changeStatusFailed, errs)
			_ = m.audit(ctx, id, 0, "reindex_failed", "", err.Error(), nil)
			return changeSetRow(id, "apply", title, owner, edits, changeStatusFailed, "", errs, changed, nil, "", time.Now().UTC()), nil
		}
	}

	now := time.Now().UTC()
	_, err = m.DB.ExecContext(ctx, `UPDATE code_change_sets
		SET action = 'apply', status = ?, errors = '[]', files_changed = ?, files_reindexed = ?, updated_at = ?, applied_at = ?
		WHERE id = ?`,
		changeStatusApplied, mustJSON(changed), mustJSON(reindexed), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	for _, item := range plan.audit {
		_ = m.audit(ctx, id, 0, "apply", item.Path, changeStatusApplied, map[string]interface{}{
			"op":       item.Op,
			"path":     item.Path,
			"new_path": item.NewPath,
		})
	}
	return changeSetRow(id, "apply", title, owner, edits, changeStatusApplied, "", nil, changed, reindexed, now.Format(time.RFC3339Nano), now), nil
}

func (m *Managed) handleLockMutation(ctx context.Context, root MutationRoot) (map[string]interface{}, error) {
	action := strings.ToLower(stringInput(root.Input, "action"))
	if action == "" && root.Operation == "insert" {
		action = "acquire"
	}
	if action == "" && root.Operation != "insert" {
		if strings.EqualFold(stringInput(root.Input, "status"), lockStatusReleased) {
			action = "release"
		}
	}
	switch action {
	case "acquire", "lock":
		return m.acquireExplicitLock(ctx, root.Input)
	case "release", "unlock":
		return m.releaseExplicitLock(ctx, root.Input)
	default:
		return nil, fmt.Errorf("code_locks action must be acquire or release")
	}
}

func (m *Managed) acquireExplicitLock(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	path := stringInput(input, "path")
	rel, _, err := m.resolveMutationPath(path)
	if err != nil {
		return nil, err
	}
	wholeFile := boolInput(input, "whole_file")
	ranges, err := parseRanges(input["ranges"])
	if err != nil {
		return nil, err
	}
	if !wholeFile && len(ranges) == 0 {
		return nil, fmt.Errorf("code_locks requires ranges unless whole_file is true")
	}
	if err := validateRanges(ranges); err != nil {
		return nil, err
	}

	ttl := defaultLockTTL
	if n, ok := int64Input(input, "ttl_seconds"); ok && n > 0 {
		ttl = time.Duration(n) * time.Second
	}
	if ttl > maxLockTTL {
		ttl = maxLockTTL
	}
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	owner := stringInput(input, "owner")
	token, err := newLeaseToken()
	if err != nil {
		return nil, err
	}

	m.lockMu.Lock()
	defer m.lockMu.Unlock()
	if err := m.ensureLocksLoadedLocked(ctx); err != nil {
		return nil, err
	}
	m.cleanupExpiredLocksLocked(ctx, now)
	if err := m.checkConflictingLocksLocked(rel, ranges, wholeFile, nil, 0); err != nil {
		return nil, err
	}

	res, err := m.DB.ExecContext(ctx, `INSERT INTO code_locks(path, ranges, owner, lease_token, ttl_seconds, whole_file, status, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rel, mustJSON(ranges), owner, token, int(ttl.Seconds()), boolToInt(wholeFile), lockStatusActive,
		expiresAt.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	lock := &sourceLock{
		ID:        id,
		Path:      rel,
		Ranges:    ranges,
		Owner:     owner,
		Token:     token,
		Status:    lockStatusActive,
		ExpiresAt: expiresAt,
		WholeFile: wholeFile,
	}
	m.locks[id] = lock
	_ = m.audit(ctx, 0, id, "lock_acquired", rel, "", nil)
	return lockRow(lock, int(ttl.Seconds()), "acquire"), nil
}

func (m *Managed) releaseExplicitLock(ctx context.Context, input map[string]interface{}) (map[string]interface{}, error) {
	id, ok := int64Input(input, "id")
	if !ok || id <= 0 {
		return nil, fmt.Errorf("code_locks release requires id in the mutation input")
	}
	token := stringInput(input, "lease_token")
	if token == "" {
		return nil, fmt.Errorf("code_locks release requires lease_token")
	}

	m.lockMu.Lock()
	defer m.lockMu.Unlock()
	if err := m.ensureLocksLoadedLocked(ctx); err != nil {
		return nil, err
	}
	lock, ok := m.locks[id]
	if !ok || lock.Status != lockStatusActive || time.Now().UTC().After(lock.ExpiresAt) {
		return nil, fmt.Errorf("code_locks id %d is not active", id)
	}
	if lock.Token != token {
		return nil, fmt.Errorf("code_locks lease_token does not match")
	}
	now := time.Now().UTC()
	_, err := m.DB.ExecContext(ctx, `UPDATE code_locks SET action = 'release', status = ?, updated_at = ? WHERE id = ?`,
		lockStatusReleased, now.Format(time.RFC3339Nano), id)
	if err != nil {
		return nil, err
	}
	lock.Status = lockStatusReleased
	delete(m.locks, id)
	_ = m.audit(ctx, 0, id, "lock_released", lock.Path, "", nil)
	return lockRow(lock, int(time.Until(lock.ExpiresAt).Seconds()), "release"), nil
}

func (m *Managed) validateAndDiff(ctx context.Context, edits []editSpec, tokens map[string]struct{}) (string, []string, string) {
	m.lockMu.Lock()
	defer m.lockMu.Unlock()
	if err := m.ensureLocksLoadedLocked(ctx); err != nil {
		return changeStatusFailed, []string{err.Error()}, ""
	}
	m.cleanupExpiredLocksLocked(ctx, time.Now().UTC())
	return m.validateAndDiffLocked(ctx, edits, tokens)
}

func (m *Managed) validateAndDiffLocked(_ context.Context, edits []editSpec, tokens map[string]struct{}) (string, []string, string) {
	plan, errs := m.buildChangePlanLocked(edits, tokens)
	diff := ""
	if plan != nil {
		diff = plan.diff.String()
	}
	if len(errs) != 0 {
		return conflictStatus(errs), errs, diff
	}
	return changeStatusPreviewed, nil, diff
}

func (m *Managed) buildChangePlanLocked(edits []editSpec, tokens map[string]struct{}) (*changePlan, []string) {
	var errs []string
	plan := &changePlan{
		replacements: make(map[string]*plannedReplacement),
		changed:      make(map[string]struct{}),
	}
	replacePaths := make(map[string]struct{})
	lifecyclePaths := make(map[string]string)
	lifecycleTargets := make(map[string]string)

	for _, edit := range edits {
		op := normalizedEditOp(edit)
		allTokens := tokenSet(edit.LockTokens)
		for token := range tokens {
			allTokens[token] = struct{}{}
		}

		switch op {
		case editOpReplace:
			rel, full, err := m.resolveMutationPath(edit.Path)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			data, mode, err := readRegularFile(rel, full)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if edit.ExpectedHash == "" {
				errs = append(errs, fmt.Sprintf("%s: expected_hash is required", rel))
				continue
			}
			if hashBytes(data) != strings.ToLower(edit.ExpectedHash) {
				errs = append(errs, fmt.Sprintf("%s: stale expected_hash", rel))
				continue
			}

			repls := normalizeReplacements(edit, int64(len(data)))
			if err := validateReplacements(rel, data, repls); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			ranges := rangesForReplacements(repls)
			if err := m.checkConflictingLocksLocked(rel, ranges, edit.WholeFile, allTokens, 0); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			pf := plan.replacements[rel]
			if pf == nil {
				pf = &plannedReplacement{Rel: rel, Full: full, Data: data, Mode: mode}
				plan.replacements[rel] = pf
			}
			pf.Repls = append(pf.Repls, repls...)
			replacePaths[rel] = struct{}{}

		case editOpCreate:
			rel, full, err := m.resolveMutationPath(edit.Path)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := validateTargetAvailable(rel, full); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := validateParentDir(rel, full, edit.Mkdirs); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := m.checkConflictingLocksLocked(rel, nil, true, allTokens, 0); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			content := editContent(edit)
			plan.lifecycle = append(plan.lifecycle, plannedLifecycle{
				Op:      editOpCreate,
				Rel:     rel,
				Full:    full,
				Content: []byte(content),
				Mkdirs:  edit.Mkdirs,
				Mode:    0644,
			})
			plan.addChanged(rel)
			plan.audit = append(plan.audit, plannedAudit{Op: editOpCreate, Path: rel})
			plan.diff.WriteString(simplePathDiff("/dev/null", "b/"+rel, "", content))
			markLifecyclePath(lifecyclePaths, rel, editOpCreate, &errs)
			markLifecycleTarget(lifecycleTargets, rel, editOpCreate, &errs)

		case editOpDelete:
			rel, full, err := m.resolveMutationPath(edit.Path)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			data, mode, err := readRegularFile(rel, full)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if edit.ExpectedHash == "" {
				errs = append(errs, fmt.Sprintf("%s: expected_hash is required", rel))
				continue
			}
			if hashBytes(data) != strings.ToLower(edit.ExpectedHash) {
				errs = append(errs, fmt.Sprintf("%s: stale expected_hash", rel))
				continue
			}
			if err := m.checkConflictingLocksLocked(rel, nil, true, allTokens, 0); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			plan.lifecycle = append(plan.lifecycle, plannedLifecycle{
				Op:   editOpDelete,
				Rel:  rel,
				Full: full,
				Data: data,
				Mode: mode,
			})
			plan.addChanged(rel)
			plan.audit = append(plan.audit, plannedAudit{Op: editOpDelete, Path: rel})
			plan.diff.WriteString(simplePathDiff("a/"+rel, "/dev/null", string(data), ""))
			markLifecyclePath(lifecyclePaths, rel, editOpDelete, &errs)

		case editOpRename:
			rel, full, err := m.resolveMutationPath(edit.Path)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			newRel, newFull, err := m.resolveMutationPath(edit.NewPath)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if rel == newRel {
				errs = append(errs, fmt.Sprintf("%s: rename target is the same path", rel))
				continue
			}
			data, mode, err := readRegularFile(rel, full)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if edit.ExpectedHash == "" {
				errs = append(errs, fmt.Sprintf("%s: expected_hash is required", rel))
				continue
			}
			if hashBytes(data) != strings.ToLower(edit.ExpectedHash) {
				errs = append(errs, fmt.Sprintf("%s: stale expected_hash", rel))
				continue
			}
			if err := validateTargetAvailable(newRel, newFull); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := validateParentDir(newRel, newFull, edit.Mkdirs); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := m.checkConflictingLocksLocked(rel, nil, true, allTokens, 0); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := m.checkConflictingLocksLocked(newRel, nil, true, allTokens, 0); err != nil {
				errs = append(errs, err.Error())
				continue
			}
			plan.lifecycle = append(plan.lifecycle, plannedLifecycle{
				Op:      editOpRename,
				Rel:     rel,
				Full:    full,
				NewRel:  newRel,
				NewFull: newFull,
				Mkdirs:  edit.Mkdirs,
				Data:    data,
				Mode:    mode,
			})
			plan.addChanged(rel)
			plan.addChanged(newRel)
			plan.audit = append(plan.audit, plannedAudit{Op: editOpRename, Path: rel, NewPath: newRel})
			plan.diff.WriteString(simplePathDiff("a/"+rel, "b/"+newRel, string(data), string(data)))
			markLifecyclePath(lifecyclePaths, rel, editOpRename, &errs)
			markLifecyclePath(lifecyclePaths, newRel, editOpRename, &errs)
			markLifecycleTarget(lifecycleTargets, newRel, editOpRename, &errs)

		default:
			errs = append(errs, fmt.Sprintf("%s: unsupported edit op %q", edit.Path, op))
		}
	}

	for path, op := range lifecyclePaths {
		if _, ok := replacePaths[path]; ok {
			errs = append(errs, fmt.Sprintf("%s: internal path conflict between replace and %s", path, op))
		}
	}

	replaceKeys := sortedReplacementKeys(plan.replacements)
	for _, path := range replaceKeys {
		pf := plan.replacements[path]
		if err := validateReplacementOverlap(path, pf.Repls); err != nil {
			errs = append(errs, err.Error())
			continue
		}
		next, err := applyReplacements(pf.Data, pf.Repls)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		pf.Next = next
		plan.addChanged(path)
		plan.audit = append(plan.audit, plannedAudit{Op: editOpReplace, Path: path})
		plan.diff.WriteString(simpleUnifiedDiff(path, string(pf.Data), string(next)))
	}

	return plan, errs
}

func (m *Managed) applyChangePlanLocked(plan *changePlan) ([]string, error) {
	if plan == nil {
		return nil, fmt.Errorf("change plan is required")
	}
	var applied []filesystemAction
	run := func(action filesystemAction) error {
		if err := action.apply(); err != nil {
			rollbackErr := action.rollback()
			if rbErr := rollbackActions(applied); rbErr != nil {
				rollbackErr = joinErrors(rollbackErr, rbErr)
			}
			if rollbackErr != nil {
				return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
			}
			return err
		}
		applied = append(applied, action)
		return nil
	}

	for _, path := range sortedReplacementKeys(plan.replacements) {
		if err := run(replaceFileAction(plan.replacements[path])); err != nil {
			if strings.Contains(err.Error(), "rollback failed") {
				return sortedStringSet(plan.changed), err
			}
			return nil, err
		}
	}
	for _, item := range plan.lifecycle {
		if err := run(lifecycleFileAction(item)); err != nil {
			if strings.Contains(err.Error(), "rollback failed") {
				return sortedStringSet(plan.changed), err
			}
			return nil, err
		}
	}
	for _, action := range applied {
		if action.finalize != nil {
			if err := action.finalize(); err != nil {
				return sortedStringSet(plan.changed), err
			}
		}
	}
	return sortedStringSet(plan.changed), nil
}

func replaceFileAction(pf *plannedReplacement) filesystemAction {
	var tmp, backup string
	var backupMoved, installed bool
	return filesystemAction{
		apply: func() error {
			var err error
			data, _, err := readRegularFile(pf.Rel, pf.Full)
			if err != nil {
				return err
			}
			if hashBytes(data) != hashBytes(pf.Data) {
				return fmt.Errorf("%s: stale expected_hash", pf.Rel)
			}
			tmp, err = writeTempFile(filepath.Dir(pf.Full), ".codesql-replace-*", pf.Next, pf.Mode)
			if err != nil {
				return err
			}
			backup, err = reserveTempPath(filepath.Dir(pf.Full), ".codesql-backup-*")
			if err != nil {
				return err
			}
			if err := os.Rename(pf.Full, backup); err != nil {
				return err
			}
			backupMoved = true
			if err := os.Rename(tmp, pf.Full); err != nil {
				return err
			}
			tmp = ""
			installed = true
			return nil
		},
		rollback: func() error {
			var err error
			if installed {
				_ = os.Remove(pf.Full)
				installed = false
			}
			if backupMoved {
				if rbErr := os.Rename(backup, pf.Full); rbErr != nil && !errors.Is(rbErr, os.ErrNotExist) {
					err = joinErrors(err, rbErr)
				} else {
					backupMoved = false
				}
			}
			if tmp != "" {
				if rbErr := os.Remove(tmp); rbErr != nil && !errors.Is(rbErr, os.ErrNotExist) {
					err = joinErrors(err, rbErr)
				}
			}
			return err
		},
		finalize: func() error {
			if !backupMoved {
				return nil
			}
			if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			backupMoved = false
			return nil
		},
	}
}

func lifecycleFileAction(item plannedLifecycle) filesystemAction {
	switch item.Op {
	case editOpCreate:
		return createFileAction(item)
	case editOpDelete:
		return deleteFileAction(item)
	case editOpRename:
		return renameFileAction(item)
	default:
		return filesystemAction{apply: func() error {
			return fmt.Errorf("%s: unsupported edit op %q", item.Rel, item.Op)
		}}
	}
}

func createFileAction(item plannedLifecycle) filesystemAction {
	var createdDirs []string
	var created bool
	return filesystemAction{
		apply: func() error {
			var err error
			if err := validateTargetAvailable(item.Rel, item.Full); err != nil {
				return err
			}
			createdDirs, err = ensureParentDirForApply(item.Rel, item.Full, item.Mkdirs)
			if err != nil {
				return err
			}
			if err := writeExclusiveFile(item.Full, item.Content, item.Mode); err != nil {
				return err
			}
			created = true
			return nil
		},
		rollback: func() error {
			var err error
			if created {
				if rbErr := os.Remove(item.Full); rbErr != nil && !errors.Is(rbErr, os.ErrNotExist) {
					err = joinErrors(err, rbErr)
				}
			}
			return joinErrors(err, removeCreatedDirs(createdDirs))
		},
	}
}

func deleteFileAction(item plannedLifecycle) filesystemAction {
	var backup string
	var moved bool
	return filesystemAction{
		apply: func() error {
			var err error
			data, _, err := readRegularFile(item.Rel, item.Full)
			if err != nil {
				return err
			}
			if hashBytes(data) != hashBytes(item.Data) {
				return fmt.Errorf("%s: stale expected_hash", item.Rel)
			}
			backup, err = reserveTempPath(filepath.Dir(item.Full), ".codesql-delete-*")
			if err != nil {
				return err
			}
			if err := os.Rename(item.Full, backup); err != nil {
				return err
			}
			moved = true
			return nil
		},
		rollback: func() error {
			if !moved {
				return nil
			}
			if err := os.Rename(backup, item.Full); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			moved = false
			return nil
		},
		finalize: func() error {
			if !moved {
				return nil
			}
			if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			moved = false
			return nil
		},
	}
}

func renameFileAction(item plannedLifecycle) filesystemAction {
	var createdDirs []string
	var moved bool
	return filesystemAction{
		apply: func() error {
			var err error
			data, _, err := readRegularFile(item.Rel, item.Full)
			if err != nil {
				return err
			}
			if hashBytes(data) != hashBytes(item.Data) {
				return fmt.Errorf("%s: stale expected_hash", item.Rel)
			}
			if err := validateTargetAvailable(item.NewRel, item.NewFull); err != nil {
				return err
			}
			createdDirs, err = ensureParentDirForApply(item.NewRel, item.NewFull, item.Mkdirs)
			if err != nil {
				return err
			}
			if err := os.Rename(item.Full, item.NewFull); err != nil {
				return err
			}
			moved = true
			return nil
		},
		rollback: func() error {
			var err error
			if moved {
				if rbErr := os.Rename(item.NewFull, item.Full); rbErr != nil && !errors.Is(rbErr, os.ErrNotExist) {
					err = joinErrors(err, rbErr)
				} else {
					moved = false
				}
			}
			return joinErrors(err, removeCreatedDirs(createdDirs))
		},
	}
}

func (p *changePlan) addChanged(path string) {
	if path != "" {
		p.changed[path] = struct{}{}
	}
}

func sortedReplacementKeys(items map[string]*plannedReplacement) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedStringSet(items map[string]struct{}) []string {
	out := make([]string, 0, len(items))
	for item := range items {
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func rollbackActions(applied []filesystemAction) error {
	var err error
	for i := len(applied) - 1; i >= 0; i-- {
		if applied[i].rollback == nil {
			continue
		}
		err = joinErrors(err, applied[i].rollback())
	}
	return err
}

func writeTempFile(dir, pattern string, data []byte, mode fs.FileMode) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()       //nolint:errcheck
		os.Remove(name) //nolint:errcheck
		return "", err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()       //nolint:errcheck
		os.Remove(name) //nolint:errcheck
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(name) //nolint:errcheck
		return "", err
	}
	return name, nil
}

func writeExclusiveFile(path string, data []byte, mode fs.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()       //nolint:errcheck
		os.Remove(path) //nolint:errcheck
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()       //nolint:errcheck
		os.Remove(path) //nolint:errcheck
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(path) //nolint:errcheck
		return err
	}
	return nil
}

func reserveTempPath(dir, pattern string) (string, error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		os.Remove(name) //nolint:errcheck
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

func ensureParentDirForApply(rel, full string, mkdirs bool) ([]string, error) {
	if err := validateParentDir(rel, full, mkdirs); err != nil {
		return nil, err
	}
	if !mkdirs {
		return nil, nil
	}
	created := missingParentDirs(full)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		return nil, err
	}
	return created, nil
}

func missingParentDirs(full string) []string {
	var dirs []string
	for dir := filepath.Dir(full); dir != "." && dir != string(filepath.Separator); dir = filepath.Dir(dir) {
		info, err := os.Stat(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				dirs = append(dirs, dir)
				continue
			}
			break
		}
		if info.IsDir() {
			break
		}
		break
	}
	return dirs
}

func removeCreatedDirs(dirs []string) error {
	var err error
	for _, dir := range dirs {
		if rbErr := os.Remove(dir); rbErr != nil && !errors.Is(rbErr, os.ErrNotExist) {
			err = joinErrors(err, rbErr)
		}
	}
	return err
}

func validateTargetAvailable(rel, full string) error {
	_, err := os.Lstat(full)
	if err == nil {
		return fmt.Errorf("%s: target already exists", rel)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return fmt.Errorf("%s: %w", rel, err)
}

func validateParentDir(rel, full string, mkdirs bool) error {
	parent := filepath.Dir(full)
	info, err := os.Stat(parent)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s: parent path is not a directory", rel)
		}
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if mkdirs {
			return nil
		}
		return fmt.Errorf("%s: parent directory does not exist", rel)
	}
	return fmt.Errorf("%s: %w", rel, err)
}

func readRegularFile(rel, full string) ([]byte, fs.FileMode, error) {
	info, err := os.Lstat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, 0, fmt.Errorf("%s: source file does not exist", rel)
		}
		return nil, 0, fmt.Errorf("%s: %w", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("%s: symlink source files are not supported", rel)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("%s: source path is not a regular file", rel)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", rel, err)
	}
	return data, info.Mode().Perm(), nil
}

func markLifecyclePath(paths map[string]string, path, op string, errs *[]string) {
	if prev, ok := paths[path]; ok {
		*errs = append(*errs, fmt.Sprintf("%s: internal path conflict between %s and %s", path, prev, op))
		return
	}
	paths[path] = op
}

func markLifecycleTarget(targets map[string]string, path, op string, errs *[]string) {
	if prev, ok := targets[path]; ok {
		*errs = append(*errs, fmt.Sprintf("%s: duplicate create/rename target for %s and %s", path, prev, op))
		return
	}
	targets[path] = op
}

func joinErrors(a, b error) error {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return fmt.Errorf("%v; %w", a, b)
}

func (m *Managed) resolveSourcePath(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return "", "", fmt.Errorf("path must be relative to the CodeSQL root: %s", path)
	}
	root, err := filepath.Abs(m.Root)
	if err != nil {
		return "", "", err
	}
	full := filepath.Join(root, filepath.Clean(path))
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return "", "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", "", fmt.Errorf("path escapes CodeSQL root: %s", path)
	}
	if rootEval, err := filepath.EvalSymlinks(root); err == nil {
		checkPath := full
		for {
			if fileEval, err := filepath.EvalSymlinks(checkPath); err == nil {
				relEval, err := filepath.Rel(rootEval, fileEval)
				if err != nil {
					return "", "", err
				}
				if relEval == ".." || strings.HasPrefix(relEval, ".."+string(filepath.Separator)) {
					return "", "", fmt.Errorf("path escapes CodeSQL root: %s", path)
				}
				break
			}
			parent := filepath.Dir(checkPath)
			if parent == checkPath {
				break
			}
			checkPath = parent
		}
	}
	return filepath.ToSlash(rel), full, nil
}

func (m *Managed) resolveMutationPath(path string) (string, string, error) {
	rel, full, err := m.resolveSourcePath(path)
	if err != nil {
		return "", "", err
	}
	if err := validateMutationPath(rel); err != nil {
		return "", "", err
	}
	return rel, full, nil
}

func validateMutationPath(rel string) error {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) >= 2 && parts[0] == "config" && parts[1] == "codesql" {
		return fmt.Errorf("%s: path is inside the managed CodeSQL cache", rel)
	}
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("%s: invalid path component", rel)
		}
		if i < len(parts)-1 && shouldSkipDir(part) {
			return fmt.Errorf("%s: path is inside skipped directory %q", rel, part)
		}
	}
	return nil
}

func normalizedEditOp(edit editSpec) string {
	op := strings.ToLower(strings.TrimSpace(edit.Op))
	if op == "" && len(edit.Replacements) != 0 {
		return editOpReplace
	}
	return op
}

func editContent(edit editSpec) string {
	if edit.Content == nil {
		return ""
	}
	return *edit.Content
}

func simplePathDiff(oldPath, newPath, oldText, newText string) string {
	if oldPath == newPath && oldText == newText {
		return ""
	}
	var b strings.Builder
	b.WriteString("--- ")
	b.WriteString(oldPath)
	b.WriteByte('\n')
	b.WriteString("+++ ")
	b.WriteString(newPath)
	b.WriteByte('\n')
	b.WriteString("@@\n")
	for _, line := range splitLines(oldText) {
		b.WriteByte('-')
		b.WriteString(line)
	}
	for _, line := range splitLines(newText) {
		b.WriteByte('+')
		b.WriteString(line)
	}
	return b.String()
}

func (m *Managed) ensureLocksLoadedLocked(ctx context.Context) error {
	if m.locksLoaded {
		return nil
	}
	return m.loadActiveLocksLocked(ctx)
}

func (m *Managed) loadActiveLocks(ctx context.Context) error {
	m.lockMu.Lock()
	defer m.lockMu.Unlock()
	return m.loadActiveLocksLocked(ctx)
}

func (m *Managed) loadActiveLocksLocked(ctx context.Context) error {
	rows, err := m.DB.QueryContext(ctx, `SELECT id, path, ranges, owner, lease_token, status, expires_at, whole_file FROM code_locks WHERE status = 'active'`)
	if err != nil {
		return err
	}
	defer rows.Close() //nolint:errcheck
	m.locks = make(map[int64]*sourceLock)
	now := time.Now().UTC()
	for rows.Next() {
		var lock sourceLock
		var rangesRaw, expiresRaw string
		var wholeFile int
		if err := rows.Scan(&lock.ID, &lock.Path, &rangesRaw, &lock.Owner, &lock.Token, &lock.Status, &expiresRaw, &wholeFile); err != nil {
			return err
		}
		lock.WholeFile = wholeFile != 0
		lock.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresRaw)
		if lock.ExpiresAt.IsZero() || now.After(lock.ExpiresAt) {
			continue
		}
		_ = json.Unmarshal([]byte(rangesRaw), &lock.Ranges)
		m.locks[lock.ID] = &lock
	}
	m.locksLoaded = true
	return rows.Err()
}

func (m *Managed) cleanupExpiredLocksLocked(ctx context.Context, now time.Time) {
	for id, lock := range m.locks {
		if now.Before(lock.ExpiresAt) {
			continue
		}
		_, _ = m.DB.ExecContext(ctx, `UPDATE code_locks SET status = ?, updated_at = ? WHERE id = ? AND status = 'active'`,
			lockStatusExpired, now.Format(time.RFC3339Nano), id)
		delete(m.locks, id)
	}
}

func (m *Managed) checkConflictingLocksLocked(path string, ranges []byteRange, wholeFile bool, tokens map[string]struct{}, ignoreID int64) error {
	for _, lock := range m.locks {
		if lock.ID == ignoreID || lock.Path != path || lock.Status != lockStatusActive {
			continue
		}
		if _, ok := tokens[lock.Token]; ok {
			continue
		}
		if lock.WholeFile || wholeFile {
			return fmt.Errorf("%s: active lock %d overlaps", path, lock.ID)
		}
		for _, lr := range lock.Ranges {
			for _, r := range ranges {
				if rangesOverlap(lr, r) {
					return fmt.Errorf("%s: active lock %d overlaps", path, lock.ID)
				}
			}
		}
	}
	return nil
}

func parseEditSpecs(input map[string]interface{}) ([]editSpec, error) {
	items, ok := listInput(input, "edits")
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("code_change_sets preview requires at least one edit")
	}
	edits := make([]editSpec, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("edit must be an object")
		}
		op := strings.ToLower(strings.TrimSpace(stringInput(obj, "op")))
		edit := editSpec{
			Op:           op,
			Path:         stringInput(obj, "path"),
			NewPath:      stringInput(obj, "new_path"),
			ExpectedHash: stringInput(obj, "expected_hash"),
			Mkdirs:       boolInput(obj, "mkdirs"),
			LockTokens:   stringListInput(obj, "lock_tokens"),
			WholeFile:    boolInput(obj, "whole_file"),
		}
		if raw, ok := obj["content"]; ok {
			content, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("%s: content must be a string", edit.Path)
			}
			edit.Content = &content
		}
		if token := stringInput(obj, "lease_token"); token != "" {
			edit.LockTokens = append(edit.LockTokens, token)
		}
		replItems, ok := listInput(obj, "replacements")
		if edit.Op == "" && ok && len(replItems) != 0 {
			edit.Op = editOpReplace
		}
		if edit.Op == "" {
			return nil, fmt.Errorf("%s: replacements are required", edit.Path)
		}
		switch edit.Op {
		case editOpReplace:
			if !ok || len(replItems) == 0 {
				return nil, fmt.Errorf("%s: replacements are required", edit.Path)
			}
			for _, replItem := range replItems {
				replObj, ok := replItem.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("%s: replacement must be an object", edit.Path)
				}
				start, _ := int64Input(replObj, "start_byte")
				end, _ := int64Input(replObj, "end_byte")
				edit.Replacements = append(edit.Replacements, replacementSpec{
					StartByte: start,
					EndByte:   end,
					OldText:   stringInput(replObj, "old_text"),
					NewText:   stringInput(replObj, "new_text"),
					WholeFile: boolInput(replObj, "whole_file"),
				})
			}
		case editOpCreate:
			if edit.Content == nil {
				return nil, fmt.Errorf("%s: content is required for create", edit.Path)
			}
		case editOpDelete, editOpRename:
		default:
			return nil, fmt.Errorf("%s: unsupported edit op %q", edit.Path, edit.Op)
		}
		edits = append(edits, edit)
	}
	return edits, nil
}

func parseRanges(v interface{}) ([]byteRange, error) {
	items, ok := toList(v)
	if !ok {
		return nil, nil
	}
	ranges := make([]byteRange, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("lock range must be an object")
		}
		start, _ := int64Input(obj, "start_byte")
		end, _ := int64Input(obj, "end_byte")
		ranges = append(ranges, byteRange{StartByte: start, EndByte: end})
	}
	return ranges, nil
}

func validateRanges(ranges []byteRange) error {
	for _, r := range ranges {
		if r.StartByte < 0 || r.EndByte < r.StartByte {
			return fmt.Errorf("invalid byte range %d:%d", r.StartByte, r.EndByte)
		}
	}
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			if rangesOverlap(ranges[i], ranges[j]) {
				return fmt.Errorf("overlapping lock ranges")
			}
		}
	}
	return nil
}

func normalizeReplacements(edit editSpec, fileSize int64) []replacementSpec {
	repls := append([]replacementSpec(nil), edit.Replacements...)
	for i := range repls {
		if edit.WholeFile || repls[i].WholeFile {
			repls[i].StartByte = 0
			repls[i].EndByte = fileSize
			repls[i].WholeFile = true
		}
	}
	return repls
}

func validateReplacements(path string, data []byte, repls []replacementSpec) error {
	if err := validateReplacementOverlap(path, repls); err != nil {
		return err
	}
	for _, repl := range repls {
		if repl.StartByte < 0 || repl.EndByte < repl.StartByte || repl.EndByte > int64(len(data)) {
			return fmt.Errorf("%s: replacement range %d:%d outside file", path, repl.StartByte, repl.EndByte)
		}
		if string(data[repl.StartByte:repl.EndByte]) != repl.OldText {
			return fmt.Errorf("%s: old_text mismatch at %d:%d", path, repl.StartByte, repl.EndByte)
		}
	}
	return nil
}

func validateReplacementOverlap(path string, repls []replacementSpec) error {
	ranges := rangesForReplacements(repls)
	for i := 0; i < len(ranges); i++ {
		for j := i + 1; j < len(ranges); j++ {
			if rangesOverlap(ranges[i], ranges[j]) {
				return fmt.Errorf("%s: overlapping replacements", path)
			}
		}
	}
	return nil
}

func rangesForReplacements(repls []replacementSpec) []byteRange {
	ranges := make([]byteRange, len(repls))
	for i, repl := range repls {
		ranges[i] = byteRange{StartByte: repl.StartByte, EndByte: repl.EndByte}
	}
	return ranges
}

func applyReplacements(data []byte, repls []replacementSpec) ([]byte, error) {
	next := append([]byte(nil), data...)
	sort.Slice(repls, func(i, j int) bool {
		return repls[i].StartByte > repls[j].StartByte
	})
	for _, repl := range repls {
		if repl.StartByte < 0 || repl.EndByte < repl.StartByte || repl.EndByte > int64(len(next)) {
			return nil, fmt.Errorf("replacement range %d:%d outside file", repl.StartByte, repl.EndByte)
		}
		if string(next[repl.StartByte:repl.EndByte]) != repl.OldText {
			return nil, fmt.Errorf("old_text mismatch at %d:%d", repl.StartByte, repl.EndByte)
		}
		buf := make([]byte, 0, len(next)-int(repl.EndByte-repl.StartByte)+len(repl.NewText))
		buf = append(buf, next[:repl.StartByte]...)
		buf = append(buf, repl.NewText...)
		buf = append(buf, next[repl.EndByte:]...)
		next = buf
	}
	return next, nil
}

func rangesOverlap(a, b byteRange) bool {
	if a.StartByte == a.EndByte || b.StartByte == b.EndByte {
		return a.StartByte >= b.StartByte && a.StartByte <= b.EndByte ||
			b.StartByte >= a.StartByte && b.StartByte <= a.EndByte
	}
	return a.StartByte < b.EndByte && b.StartByte < a.EndByte
}

func simpleUnifiedDiff(path, oldText, newText string) string {
	if oldText == newText {
		return ""
	}
	var b strings.Builder
	b.WriteString("--- a/")
	b.WriteString(path)
	b.WriteByte('\n')
	b.WriteString("+++ b/")
	b.WriteString(path)
	b.WriteByte('\n')
	b.WriteString("@@\n")
	for _, line := range splitLines(oldText) {
		b.WriteByte('-')
		b.WriteString(line)
	}
	for _, line := range splitLines(newText) {
		b.WriteByte('+')
		b.WriteString(line)
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.SplitAfter(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func conflictStatus(errs []string) string {
	for _, err := range errs {
		if strings.Contains(err, "stale expected_hash") ||
			strings.Contains(err, "old_text mismatch") ||
			strings.Contains(err, "active lock") ||
			strings.Contains(err, "overlapping") ||
			strings.Contains(err, "already exists") ||
			strings.Contains(err, "source file does not exist") ||
			strings.Contains(err, "internal path conflict") ||
			strings.Contains(err, "duplicate create/rename target") {
			return changeStatusConflict
		}
	}
	return changeStatusFailed
}

func (m *Managed) updateChangeSetFailure(ctx context.Context, id int64, status string, errs []string) error {
	_, err := m.DB.ExecContext(ctx, `UPDATE code_change_sets SET action = 'apply', status = ?, errors = ?, updated_at = ? WHERE id = ?`,
		status, mustJSON(errs), time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func (m *Managed) audit(ctx context.Context, changeSetID, lockID int64, event, path, message string, details interface{}) error {
	detailsJSON := "{}"
	if details != nil {
		detailsJSON = mustJSON(details)
	}
	var csID interface{}
	if changeSetID != 0 {
		csID = changeSetID
	}
	var lID interface{}
	if lockID != 0 {
		lID = lockID
	}
	_, err := m.DB.ExecContext(ctx, `INSERT INTO code_change_audit(change_set_id, lock_id, event, path, message, details, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		csID, lID, event, path, message, detailsJSON, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func changeSetRow(id int64, action, title, owner string, edits []editSpec, status, diff string, errs []string, changed, reindexed []string, appliedAt string, now time.Time) map[string]interface{} {
	if errs == nil {
		errs = []string{}
	}
	if changed == nil {
		changed = []string{}
	}
	if reindexed == nil {
		reindexed = []string{}
	}
	var applied interface{}
	if appliedAt != "" {
		applied = appliedAt
	}
	return map[string]interface{}{
		"id":              id,
		"action":          action,
		"title":           title,
		"owner":           owner,
		"edits":           edits,
		"status":          status,
		"diff":            diff,
		"errors":          errs,
		"files_changed":   changed,
		"files_reindexed": reindexed,
		"updated_at":      now.Format(time.RFC3339Nano),
		"applied_at":      applied,
	}
}

func lockRow(lock *sourceLock, ttlSeconds int, action string) map[string]interface{} {
	return map[string]interface{}{
		"id":          lock.ID,
		"action":      action,
		"path":        lock.Path,
		"ranges":      lock.Ranges,
		"owner":       lock.Owner,
		"lease_token": lock.Token,
		"ttl_seconds": ttlSeconds,
		"whole_file":  lock.WholeFile,
		"status":      lock.Status,
		"expires_at":  lock.ExpiresAt.Format(time.RFC3339Nano),
	}
}

func filterMutationFields(row map[string]interface{}, fields []MutationField) map[string]interface{} {
	if len(fields) == 0 {
		return row
	}
	out := make(map[string]interface{}, len(fields))
	for _, field := range fields {
		if v, ok := row[field.Column]; ok {
			out[field.Name] = v
		}
	}
	return out
}

func newLeaseToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func tokenSet(tokens []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		if token != "" {
			set[token] = struct{}{}
		}
	}
	return set
}

func listInput(input map[string]interface{}, key string) ([]interface{}, bool) {
	if input == nil {
		return nil, false
	}
	return toList(input[key])
}

func toList(v interface{}) ([]interface{}, bool) {
	switch items := v.(type) {
	case []interface{}:
		return items, true
	case []map[string]interface{}:
		out := make([]interface{}, len(items))
		for i := range items {
			out[i] = items[i]
		}
		return out, true
	default:
		return nil, false
	}
}

func stringInput(input map[string]interface{}, key string) string {
	if input == nil {
		return ""
	}
	switch v := input[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	default:
		return ""
	}
}

func stringListInput(input map[string]interface{}, key string) []string {
	items, ok := listInput(input, key)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func int64Input(input map[string]interface{}, key string) (int64, bool) {
	if input == nil {
		return 0, false
	}
	switch v := input[key].(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case float64:
		return int64(v), true
	case json.Number:
		i, err := v.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}

func boolInput(input map[string]interface{}, key string) bool {
	if input == nil {
		return false
	}
	switch v := input[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	default:
		return false
	}
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
