package serv

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
)

func TestWorkflowRegistryReusesSnapshotWhenFilesAreUnchanged(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"report.js": `// @graphjin-workflow {"description":"Build report","tags":["ops"]}
function main(input) { return {}; }
`,
	})
	s := ms.service

	first := s.workflowSnapshot(defaultWorkflowScriptTimeout)
	s.workflowMu.Lock()
	cached := s.workflowCache
	s.workflowMu.Unlock()
	if cached == nil {
		t.Fatal("expected workflow snapshot to be cached")
	}

	second := s.workflowSnapshot(defaultWorkflowScriptTimeout)
	s.workflowMu.Lock()
	reused := s.workflowCache
	s.workflowMu.Unlock()
	if reused != cached {
		t.Fatal("expected unchanged workflow files to reuse cached registry snapshot")
	}
	if first.revision == "" || second.revision == "" || first.revision != second.revision {
		t.Fatalf("expected stable workflow revision, got first=%q second=%q", first.revision, second.revision)
	}
	if len(second.workflows) != 1 || second.workflows[0].Name != "report" {
		t.Fatalf("unexpected workflow registry contents: %+v", second.workflows)
	}
}

func TestWorkflowRegistryDetectsExternalFileEdits(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{}, map[string]string{
		"report.js": `// @graphjin-workflow {"description":"Build report"}
function main(input) { return { version: 1 }; }
`,
	})
	s := ms.service

	first := s.workflowSnapshot(defaultWorkflowScriptTimeout)
	if len(first.workflows) != 1 {
		t.Fatalf("expected one workflow before edit, got %+v", first.workflows)
	}

	time.Sleep(2 * time.Millisecond)
	if err := s.fs.Put("workflows/report.js", []byte(`// @graphjin-workflow {"description":"Build updated report"}
function main(input) { return { version: 2 }; }
`)); err != nil {
		t.Fatalf("write external workflow edit: %v", err)
	}

	second := s.workflowSnapshot(defaultWorkflowScriptTimeout)
	if second.revision == first.revision {
		t.Fatalf("expected external edit to change workflow revision %q", first.revision)
	}
	if len(second.workflows) != 1 || second.workflows[0].Description != "Build updated report" {
		t.Fatalf("expected updated workflow metadata, got %+v", second.workflows)
	}

	snap, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("catalog snapshot after external edit: %v", err)
	}
	result, err := snap.QueryResult(coreCatalogWorkflowQuery())
	if err != nil {
		t.Fatalf("query workflow catalog after external edit: %v", err)
	}
	if len(result.Cards) != 1 || result.Cards[0].Summary != "Build updated report" {
		t.Fatalf("expected catalog projection to reflect registry edit, got %+v", result.Cards)
	}
}

func TestWorkflowRegistryUsesConfiguredTimeoutAcrossReaders(t *testing.T) {
	ms := workflowCatalogTestServer(t, MCPConfig{WorkflowTimeout: 120}, map[string]string{
		"report.js": `// @graphjin-workflow {"description":"Build report"}
function main(input) { return {}; }
`,
	})
	s := ms.service

	snap := s.workflowSnapshot(s.workflowTimeoutSeconds())
	if len(snap.workflows) != 1 || snap.workflows[0].TimeoutSeconds != 120 {
		t.Fatalf("expected registry timeout_seconds=120, got %+v", snap.workflows)
	}

	rows, err := newControlPlaneGraphQL(s).workflowRows(context.Background(), false, core.ManagedQueryRoot{})
	if err != nil {
		t.Fatalf("workflowRows: %v", err)
	}
	if len(rows) != 1 || rows[0]["timeout_seconds"] != 120 || rows[0]["workflow_revision"] != snap.revision {
		t.Fatalf("expected control-plane workflow row to share registry timeout/revision, got rows=%+v snap=%+v", rows, snap)
	}

	catalogSnap, err := s.catalogSnapshot()
	if err != nil {
		t.Fatalf("catalog snapshot: %v", err)
	}
	details := catalogSnap.CardDetails("workflow:report")
	if len(details) != 1 || !strings.Contains(details[0].DataJSON, `"timeout_seconds":120`) {
		t.Fatalf("expected catalog workflow projection to use configured timeout, got %+v", details)
	}
}
