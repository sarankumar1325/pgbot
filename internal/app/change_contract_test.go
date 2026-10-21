package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sarankumar1325/queryshift/internal/erd"
)

func contractWorkspace() *DatabaseWorkspace {
	return &DatabaseWorkspace{
		Host: "db.example.test", Database: "app", Version: "PostgreSQL 18.1", Provider: "neon",
		Schema: erd.Schema{
			Tables: []erd.Table{
				{Schema: "public", Name: "accounts", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}}},
				{Schema: "public", Name: "orders", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}, {Name: "account_id", Type: "bigint", FKTarget: "accounts.id"}}},
			},
			Edges: []erd.Edge{{FromTable: "orders", FromColumn: "account_id", ToTable: "accounts", ToColumn: "id"}},
		},
	}
}

func TestChangeContractJSONUsesVersionedExternalShape(t *testing.T) {
	contract, err := BuildChangeContract("CREATE TABLE public.events(id bigint)", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"contract_sha256"`, `"statement_sha256"`, `"required_confirmation"`, `"write_unlock_required"`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("contract JSON missing %s: %s", want, encoded)
		}
	}
	schemaBytes, err := os.ReadFile(filepath.Join("..", "..", "schema", "queryshift-change-contract-1.0.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("change contract JSON Schema is invalid: %v", err)
	}
}

func TestBuildChangeContractBindsSQLTargetSchemaRiskAndVerification(t *testing.T) {
	workspace := contractWorkspace()
	contract, err := BuildChangeContract("ALTER TABLE public.accounts DROP COLUMN legacy", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Version != ChangeContractVersion || !strings.HasPrefix(contract.ID, "qsc_") || len(contract.ContractSHA256) != 64 {
		t.Fatalf("contract identity is incomplete: %+v", contract)
	}
	if contract.Target.Database != "app" || contract.Target.Fingerprint == "" || contract.SchemaSHA256 == "" {
		t.Fatalf("contract target is incomplete: %+v", contract.Target)
	}
	if contract.Risk.Level != "critical" || !contract.Risk.BackupProof || contract.Plan.RequiredConfirmation != "BACKUP AND APPLY DESTRUCTIVE" {
		t.Fatalf("contract risk is not fail-safe: risk=%+v plan=%+v", contract.Risk, contract.Plan)
	}
	if len(contract.AffectedObjects) != 1 || contract.AffectedObjects[0].Name != "public.accounts" {
		t.Fatalf("affected object missing: %+v", contract.AffectedObjects)
	}
	if len(contract.Dependencies) != 1 || !strings.Contains(contract.Dependencies[0], "orders") {
		t.Fatalf("foreign-key dependency missing: %+v", contract.Dependencies)
	}
	if len(contract.Verification) < 2 || len(contract.Evidence) != 2 || !contract.WriteUnlockRequired {
		t.Fatalf("verification/evidence boundary incomplete: %+v", contract)
	}
	if err := ValidateChangeContract(contract, workspace); err != nil {
		t.Fatalf("fresh contract should validate: %v", err)
	}
}

func TestValidateChangeContractRejectsTamperingTargetChangesAndSchemaDrift(t *testing.T) {
	workspace := contractWorkspace()
	contract, err := BuildChangeContract("CREATE INDEX accounts_id_idx ON public.accounts(id)", workspace)
	if err != nil {
		t.Fatal(err)
	}

	tampered := contract
	tampered.Statement = "DROP TABLE public.accounts"
	if err := ValidateChangeContract(tampered, workspace); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("tampered SQL should fail contract integrity, got %v", err)
	}

	otherTarget := *workspace
	otherTarget.Database = "production"
	if err := ValidateChangeContract(contract, &otherTarget); err == nil || !strings.Contains(err.Error(), "target changed") {
		t.Fatalf("target mismatch should fail closed, got %v", err)
	}

	drifted := *workspace
	drifted.Schema.Tables = append([]erd.Table(nil), workspace.Schema.Tables...)
	drifted.Schema.Tables = append(drifted.Schema.Tables, erd.Table{Schema: "public", Name: "new_table"})
	if err := ValidateChangeContract(contract, &drifted); err == nil || !strings.Contains(err.Error(), "schema changed") {
		t.Fatalf("schema drift should fail closed, got %v", err)
	}
}

func TestRehearsalValidationRequiresDistinctTargetWithMatchingSchema(t *testing.T) {
	production := contractWorkspace()
	contract, err := BuildChangeContract("CREATE TABLE public.events(id bigint)", production)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateChangeContractForRehearsal(contract, production); err == nil || !strings.Contains(err.Error(), "matches the reviewed target") {
		t.Fatalf("production target must never pass rehearsal validation: %v", err)
	}
	clone := *production
	clone.Host = "disposable-branch.example.test"
	if err := ValidateChangeContractForRehearsal(contract, &clone); err != nil {
		t.Fatalf("matching disposable clone should pass rehearsal validation: %v", err)
	}
	clone.Schema.Tables = append(clone.Schema.Tables, erd.Table{Schema: "public", Name: "drift"})
	if err := ValidateChangeContractForRehearsal(contract, &clone); err == nil || !strings.Contains(err.Error(), "schema changed") {
		t.Fatalf("drifted clone should fail rehearsal validation: %v", err)
	}
}

func TestCreateIndexContractIncludesTableLockAndValidityProbe(t *testing.T) {
	contract, err := BuildChangeContract("CREATE INDEX accounts_id_idx ON public.accounts(id)", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	if contract.Risk.Level != "elevated" || len(contract.AffectedObjects) != 2 {
		t.Fatalf("index operational risk is incomplete: risk=%+v objects=%+v", contract.Risk, contract.AffectedObjects)
	}
	found := false
	for _, probe := range contract.Verification {
		if probe.Kind == "index_validity" {
			found = true
		}
	}
	if !found {
		t.Fatalf("index validity probe missing: %+v", contract.Verification)
	}
}

func TestSchemaFingerprintIgnoresTelemetryAndOrderingButDetectsStructure(t *testing.T) {
	workspace := contractWorkspace()
	first, err := workspaceSchemaSHA256(workspace)
	if err != nil {
		t.Fatal(err)
	}
	reordered := *workspace
	reordered.Schema = workspace.Schema
	reordered.Schema.Info.SizeBytes = 999_999
	reordered.Schema.Tables = []erd.Table{workspace.Schema.Tables[1], workspace.Schema.Tables[0]}
	second, err := workspaceSchemaSHA256(&reordered)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("non-structural telemetry/order changed schema fingerprint: %s != %s", first, second)
	}
}
