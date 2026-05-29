package snowflake

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dosco/graphjin/core/v3"
	"github.com/dosco/graphjin/serv/v3"
	"github.com/dosco/graphjin/tests/v3/hostedemu"
)

func TestDiscoveryUsesBatchMetadataForLargeCatalog(t *testing.T) {
	const tableCount = 5000

	dir := t.TempDir()
	seedPath := filepath.Join(dir, "snowflake.sql")
	if err := os.WriteFile(seedPath, []byte(scaleSeed(tableCount)), 0644); err != nil {
		t.Fatal(err)
	}
	captureDir := filepath.Join(dir, "capture")

	db := sql.OpenDB(hostedemu.NewConnector(hostedemu.Config{
		SeedPath:   seedPath,
		CaptureDir: captureDir,
		TestName:   "snowflake-discovery-scale",
		Backend:    hostedemu.BackendDuckDB,
		Fallback:   hostedemu.FallbackStrict,
	}, NewAdapter()))
	defer db.Close()

	start := time.Now()
	gj, err := core.NewGraphJin(&core.Config{
		DBType:               "snowflake",
		DisableAllowList:     true,
		DBSchemaPollDuration: -1,
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	defer gj.Close()

	dm := serv.NewDiscoveryManager(gj)
	index := dm.TableIndex(gj.DefaultDatabase())
	elapsed := time.Since(start)
	if len(index) != tableCount {
		t.Fatalf("table index length = %d, want %d", len(index), tableCount)
	}
	t.Logf("loaded %d Snowflake tables in %s", tableCount, elapsed)

	events, err := readCaptureEvents(hostedemu.CapturePath(captureDir, "snowflake-discovery-scale"))
	if err != nil {
		t.Fatal(err)
	}
	var discoveryQueries int
	var tableSpecificMetadata int
	var showMetadata int
	for _, ev := range events {
		if ev.Phase != "discovery" {
			continue
		}
		norm := strings.ToUpper(ev.NormalizedSQL)
		if ev.Op == "query" {
			discoveryQueries++
			if strings.Contains(norm, "SCALE_TABLE_") && !strings.Contains(norm, "INFORMATION_SCHEMA") {
				tableSpecificMetadata++
			}
		}
		// Count actual SHOW executions (not RESULT_SCAN read-backs whose SQL
		// comments mention SHOW), so the metric reflects real discovery cost.
		if ev.Op == "exec" && strings.HasPrefix(norm, "SHOW ") {
			showMetadata++
		}
	}
	t.Logf("Snowflake discovery query count=%d table-specific metadata queries=%d SHOW metadata queries=%d", discoveryQueries, tableSpecificMetadata, showMetadata)
	if discoveryQueries > 12 {
		t.Fatalf("discovery query count = %d, want <= 12", discoveryQueries)
	}
	if tableSpecificMetadata != 0 {
		t.Fatalf("table-specific metadata queries = %d, want 0", tableSpecificMetadata)
	}
	// All metadata comes from SHOW (Snowflake restricts INFORMATION_SCHEMA). The
	// SHOW count is fixed — SHOW COLUMNS + PK/UK/FK + SHOW TABLES (clustering),
	// plus at most one composite-FK SHOW — and must not grow with the 5000-table
	// catalog.
	if showMetadata > 6 {
		t.Fatalf("SHOW metadata queries = %d, want <= 6 (must not scale with catalog size)", showMetadata)
	}
}

func scaleSeed(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "CREATE TABLE scale_table_%04d (id NUMBER NOT NULL PRIMARY KEY, name VARCHAR);\n", i)
	}
	return b.String()
}

type captureLine struct {
	Op            string `json:"op"`
	Phase         string `json:"phase"`
	NormalizedSQL string `json:"normalized_sql"`
}

func readCaptureEvents(path string) ([]captureLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []captureLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var ev captureLine
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, sc.Err()
}
