package bigquery

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
	seedPath := filepath.Join(dir, "bigquery.sql")
	if err := os.WriteFile(seedPath, []byte(scaleSeed(tableCount)), 0644); err != nil {
		t.Fatal(err)
	}
	captureDir := filepath.Join(dir, "capture")

	db := sql.OpenDB(hostedemu.NewConnector(hostedemu.Config{
		SeedPath:   seedPath,
		CaptureDir: captureDir,
		TestName:   "bigquery-discovery-scale",
		Backend:    hostedemu.BackendCapture,
		Fallback:   hostedemu.FallbackStrict,
	}, NewAdapter()))
	defer db.Close()

	start := time.Now()
	gj, err := core.NewGraphJin(&core.Config{
		DBType:               "bigquery",
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
	t.Logf("loaded %d BigQuery tables in %s", tableCount, elapsed)

	events, err := readCaptureEvents(hostedemu.CapturePath(captureDir, "bigquery-discovery-scale"))
	if err != nil {
		t.Fatal(err)
	}
	var discoveryQueries int
	var tableSpecificMetadata int
	for _, ev := range events {
		if ev.Phase != "discovery" || ev.Op != "query" {
			continue
		}
		discoveryQueries++
		norm := strings.ToUpper(ev.NormalizedSQL)
		if strings.Contains(norm, "SCALE_TABLE_") && !strings.Contains(norm, "INFORMATION_SCHEMA") {
			tableSpecificMetadata++
		}
	}
	if discoveryQueries > 8 {
		t.Fatalf("discovery query count = %d, want <= 8", discoveryQueries)
	}
	if tableSpecificMetadata != 0 {
		t.Fatalf("table-specific metadata queries = %d, want 0", tableSpecificMetadata)
	}
}

func scaleSeed(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "CREATE TABLE scale_table_%04d (id INT64 NOT NULL PRIMARY KEY NOT ENFORCED, name STRING);\n", i)
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
