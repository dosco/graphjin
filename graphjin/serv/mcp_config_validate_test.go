package serv

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

// validate_config runs the full update pipeline as a dry run: it validates,
// stages a runtime, classifies the reload impact, then discards everything.
// These tests pin the two guarantees callers rely on — an accurate verdict and
// zero mutation of the live service.

func TestHandleValidateConfig_ValidChangeReportsImpactWithoutMutating(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	replacementPath := createSQLiteDBFile(t, "replacement.sqlite3", true)
	ms := newTransactionalConfigMCPServerWithOptions(t, livePath, false, nil)

	oldGJ := ms.service.gj
	oldDB := ms.service.dbs["main"]
	oldPath := ms.service.conf.Core.Databases["main"].Path
	oldRev := ms.currentConfigCatalogRevision(context.Background())

	res, err := ms.handleValidateConfig(context.Background(), newToolRequest(map[string]any{
		"databases": map[string]any{
			"main": map[string]any{"type": "sqlite", "path": replacementPath},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !out.Valid || out.Applied {
		t.Fatalf("expected valid=true applied=false, got valid=%v applied=%v", out.Valid, out.Applied)
	}
	if out.Mode != "validate" {
		t.Fatalf("expected mode=validate, got %q", out.Mode)
	}
	if len(out.Changes) == 0 {
		t.Fatal("expected a change summary describing the proposed update")
	}

	// Nothing about the running service may have moved.
	if ms.service.gj != oldGJ {
		t.Fatal("validate must not swap the live GraphJin instance")
	}
	if ms.service.dbs["main"] != oldDB {
		t.Fatal("validate must not swap the live database handle")
	}
	if got := ms.service.conf.Core.Databases["main"].Path; got != oldPath {
		t.Fatalf("validate must not change live config path, got %q want %q", got, oldPath)
	}
	if got := ms.currentConfigCatalogRevision(context.Background()); got != oldRev {
		t.Fatalf("validate must not bump catalog revision, got %q want %q", got, oldRev)
	}
	if err := oldDB.Ping(); err != nil {
		t.Fatalf("original database handle should stay open, ping failed: %v", err)
	}
}

func TestHandleValidateConfig_InvalidChangeReportsErrorsWithoutMutating(t *testing.T) {
	livePath := createSQLiteDBFile(t, "live.sqlite3", true)
	emptyPath := createSQLiteDBFile(t, "empty.sqlite3", false)
	ms := newTransactionalConfigMCPServerWithOptions(t, livePath, false, nil)

	oldGJ := ms.service.gj
	oldPath := ms.service.conf.Core.Databases["main"].Path

	res, err := ms.handleValidateConfig(context.Background(), newToolRequest(map[string]any{
		"databases": map[string]any{
			"main": map[string]any{"type": "sqlite", "path": emptyPath},
		},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out ConfigUpdateResult
	if err := json.Unmarshal([]byte(assertToolSuccess(t, res)), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Valid || out.Success {
		t.Fatalf("expected an invalid verdict for a schema-less database, got %+v", out)
	}
	if len(out.Errors) == 0 {
		t.Fatal("expected validation errors to be reported")
	}
	if ms.service.gj != oldGJ {
		t.Fatal("validate must not swap the live GraphJin instance on failure")
	}
	if got := ms.service.conf.Core.Databases["main"].Path; got != oldPath {
		t.Fatalf("validate must not change live config path on failure, got %q want %q", got, oldPath)
	}
}

func TestRegisterConfigTools_DevOnly(t *testing.T) {
	has := func(prod bool) map[string]bool {
		ms := newTransactionalConfigMCPServerWithOptions(t, createSQLiteDBFile(t, "reg.sqlite3", true), prod, nil)
		ms.service.conf.MCP.AllowConfigUpdates = true
		ms.srv = server.NewMCPServer("test", "0.0.0")
		ms.registerConfigTools()
		out := map[string]bool{}
		for name := range ms.srv.ListTools() {
			out[name] = true
		}
		return out
	}

	dev := has(false)
	for _, name := range []string{"get_current_config", "validate_config", "update_current_config"} {
		if !dev[name] {
			t.Fatalf("dev mode should register %q", name)
		}
	}

	prod := has(true)
	for _, name := range []string{"get_current_config", "validate_config", "update_current_config"} {
		if prod[name] {
			t.Fatalf("production mode must not register %q", name)
		}
	}
}
