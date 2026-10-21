package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sarankumar1325/queryshift/internal/erd"
)

func TestMigrationJournalVersionsSQLAndSchemaWithIntegrityManifest(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	plan, err := AnalyzeChange("CREATE INDEX users_email_idx ON public.users(email)")
	if err != nil {
		t.Fatal(err)
	}
	workspace := &DatabaseWorkspace{
		Host: "db.example.test", Database: "app",
		Schema: erd.Schema{Tables: []erd.Table{{
			Schema: "public", Name: "users",
			Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}},
		}}},
	}
	contract, err := BuildChangeContract("CREATE INDEX users_email_idx ON public.users(email)", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Plan != plan {
		t.Fatalf("contract plan changed: got %+v want %+v", contract.Plan, plan)
	}
	journal, err := startMigrationJournal(contract, workspace, "production")
	if err != nil {
		t.Fatal(err)
	}

	sqlBytes, err := os.ReadFile(journal.sqlPath)
	if err != nil {
		t.Fatal(err)
	}
	content := string(sqlBytes)
	for _, want := range []string{"change contract " + contract.ID, "queryshift:up", "CREATE INDEX", "queryshift:down", "DROP INDEX IF EXISTS users_email_idx"} {
		if !strings.Contains(content, want) {
			t.Fatalf("migration is missing %q:\n%s", want, content)
		}
	}
	if mode := fileMode(t, journal.sqlPath); mode != 0o600 {
		t.Fatalf("migration mode = %o, want 600", mode)
	}
	contractBytes, err := os.ReadFile(journal.contractPath)
	if err != nil {
		t.Fatalf("change contract artifact missing: %v", err)
	}
	if !strings.Contains(string(contractBytes), contract.ContractSHA256) || fileMode(t, journal.contractPath) != 0o600 {
		t.Fatalf("change contract artifact is incomplete or insecure: %s", contractBytes)
	}
	manifest := filepath.Join(filepath.Dir(journal.sqlPath), "queryshift.sum")
	manifestBytes, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatalf("integrity manifest missing: %v", err)
	}
	if !strings.Contains(string(manifestBytes), filepath.Base(journal.contractPath)) {
		t.Fatalf("integrity manifest does not cover the contract:\n%s", manifestBytes)
	}

	after := *workspace
	after.Schema.Tables[0].Indexes = []erd.Index{{Name: "users_email_idx", Def: "btree (email)"}}
	if err := journal.finish("applied", &after, nil); err != nil {
		t.Fatal(err)
	}
	metaBytes, err := os.ReadFile(journal.metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(metaBytes), `"status": "applied"`) || !strings.Contains(string(metaBytes), "after_schema_sha256") || !strings.Contains(string(metaBytes), contract.ID) {
		t.Fatalf("metadata was not finalized:\n%s", metaBytes)
	}
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
