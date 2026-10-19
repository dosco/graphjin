package serv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

type workflowRegistrySnapshot struct {
	fingerprint string
	timeout     int
	revision    string
	workflows   []core.CatalogWorkflow
}

type workflowFileState struct {
	Name        string `json:"name"`
	Size        int64  `json:"size,omitempty"`
	ModUnixNano int64  `json:"mod_unix_nano,omitempty"`
	SourceHash  string `json:"source_hash,omitempty"`
	MissingStat bool   `json:"missing_stat,omitempty"`
}

func (s *graphjinService) workflowSnapshot(timeoutSeconds int) workflowRegistrySnapshot {
	if timeoutSeconds <= 0 {
		timeoutSeconds = defaultWorkflowScriptTimeout
	}
	states, fingerprint := s.workflowFilesystemState()

	s.workflowMu.Lock()
	if s.workflowCache != nil && s.workflowCache.timeout == timeoutSeconds && s.workflowCache.fingerprint == fingerprint {
		snap := cloneWorkflowRegistrySnapshot(*s.workflowCache)
		s.workflowMu.Unlock()
		return snap
	}
	s.workflowMu.Unlock()

	snap := workflowRegistrySnapshot{
		fingerprint: fingerprint,
		timeout:     timeoutSeconds,
		workflows:   s.scanWorkflowRegistry(states, timeoutSeconds),
	}
	snap.revision = workflowRegistryRevision(snap.workflows)

	s.workflowMu.Lock()
	s.workflowCache = &workflowRegistrySnapshot{
		fingerprint: snap.fingerprint,
		timeout:     snap.timeout,
		revision:    snap.revision,
		workflows:   cloneCatalogWorkflows(snap.workflows),
	}
	s.workflowMu.Unlock()
	return cloneWorkflowRegistrySnapshot(snap)
}

func (s *graphjinService) workflowByName(name string, timeoutSeconds int) (core.CatalogWorkflow, bool) {
	snap := s.workflowSnapshot(timeoutSeconds)
	for _, wf := range snap.workflows {
		if wf.Name == name {
			return wf, true
		}
	}
	return core.CatalogWorkflow{}, false
}

func (s *graphjinService) invalidateWorkflowRegistry() {
	if s == nil {
		return
	}
	s.workflowMu.Lock()
	s.workflowCache = nil
	s.workflowMu.Unlock()
}

func (s *graphjinService) workflowFilesystemState() ([]workflowFileState, string) {
	if s == nil || s.fs == nil {
		return nil, workflowRegistryRevision(nil)
	}

	basePath := s.workflowBasePath()
	files, err := s.fs.List(basePath)
	if err != nil {
		return nil, workflowHashJSON(map[string]string{"missing": basePath})
	}

	states := make([]workflowFileState, 0, len(files))
	statFS, hasStat := s.fs.(workflowStatFS)
	for _, name := range files {
		if !strings.HasSuffix(name, workflowExt) {
			continue
		}
		baseName := strings.TrimSuffix(name, workflowExt)
		if !workflowNameRe.MatchString(baseName) {
			continue
		}
		path := filepath.Join(basePath, name)
		state := workflowFileState{Name: name}
		if hasStat {
			if info, err := statFS.Stat(path); err == nil && info != nil {
				state.Size = info.Size()
				state.ModUnixNano = info.ModTime().UTC().UnixNano()
			} else {
				state.MissingStat = true
			}
		}
		if !hasStat || state.MissingStat {
			if src, err := s.fs.Get(path); err == nil {
				sum := sha256.Sum256(src)
				state.SourceHash = hex.EncodeToString(sum[:])
			}
		}
		states = append(states, state)
	}
	sort.Slice(states, func(i, j int) bool { return states[i].Name < states[j].Name })
	return states, workflowHashJSON(states)
}

func (s *graphjinService) scanWorkflowRegistry(states []workflowFileState, timeoutSeconds int) []core.CatalogWorkflow {
	if s == nil || s.fs == nil || len(states) == 0 {
		return nil
	}
	workflows := make([]core.CatalogWorkflow, 0, len(states))
	now := time.Now().UTC()
	basePath := s.workflowBasePath()
	runtime := s.workflowRuntime()
	for _, state := range states {
		name := state.Name
		path := filepath.Join(basePath, name)
		src, err := s.fs.Get(path)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(src)
		meta, _ := parseWorkflowMeta(string(src))
		meta = catalogWorkflowMeta(meta)
		createdAt, updatedAt := s.workflowTimestamps(path, meta, now)

		workflows = append(workflows, core.CatalogWorkflow{
			Name:           strings.TrimSuffix(name, workflowExt),
			Description:    meta.Description,
			Tags:           append([]string(nil), meta.Tags...),
			Variables:      catalogWorkflowVariables(meta.Variables),
			Path:           path,
			SourceHash:     hex.EncodeToString(sum[:]),
			Runtime:        runtime,
			TimeoutSeconds: timeoutSeconds,
			CreatedAt:      createdAt,
			UpdatedAt:      updatedAt,
		})
	}
	sort.Slice(workflows, func(i, j int) bool { return workflows[i].Name < workflows[j].Name })
	return workflows
}

func workflowRegistryRevision(workflows []core.CatalogWorkflow) string {
	return workflowHashJSON(workflows)
}

func workflowHashJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		data = []byte("{}")
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneWorkflowRegistrySnapshot(in workflowRegistrySnapshot) workflowRegistrySnapshot {
	in.workflows = cloneCatalogWorkflows(in.workflows)
	return in
}

func cloneCatalogWorkflows(in []core.CatalogWorkflow) []core.CatalogWorkflow {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.CatalogWorkflow, len(in))
	copy(out, in)
	for i := range out {
		out[i].Tags = append([]string(nil), in[i].Tags...)
		out[i].Variables = append([]core.CatalogWorkflowVariable(nil), in[i].Variables...)
	}
	return out
}
