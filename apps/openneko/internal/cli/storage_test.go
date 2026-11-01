package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/open-neko/neko/apps/openneko/internal/db"
)

func TestStorageContractCommandReportsCompiledContract(t *testing.T) {
	cmd := newStorageCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs([]string{"contract"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(output.String()), "1"; got != want {
		t.Fatalf("storage contract = %q, want %q", got, want)
	}
	if db.StorageContractVersion != 1 {
		t.Fatalf("update the database image label and this test for storage contract %d", db.StorageContractVersion)
	}
}
