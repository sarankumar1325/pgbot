package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/sarankumar1325/queryshift/internal/erd"
)

func changeTestWorkspace() *app.DatabaseWorkspace {
	return &app.DatabaseWorkspace{
		Host: "private-db.example.test", Database: "app", Provider: "neon",
		Schema: erd.Schema{Tables: []erd.Table{{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}}}}},
	}
}

func TestChangePlanAndVerifyCommandsProduceCISafeProof(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://owner:secret@private-db.example.test/app")
	sqlPath := filepath.Join(t.TempDir(), "migration.sql")
	if err := os.WriteFile(sqlPath, []byte("CREATE INDEX users_id_idx ON public.users(id)"), 0o600); err != nil {
		t.Fatal(err)
	}
	loader := func(context.Context, string) (*app.DatabaseWorkspace, error) { return changeTestWorkspace(), nil }
	plan := newChangePlanCmd(loader)
	var planOutput bytes.Buffer
	plan.SetOut(&planOutput)
	plan.SetArgs([]string{sqlPath})
	if err := plan.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(planOutput.String(), "secret") || strings.Contains(planOutput.String(), "private-db.example.test") {
		t.Fatalf("contract leaked connection data: %s", planOutput.String())
	}
	var contract app.ChangeContract
	if err := json.Unmarshal(planOutput.Bytes(), &contract); err != nil {
		t.Fatal(err)
	}
	if contract.ID == "" || contract.Risk.Level != "elevated" {
		t.Fatalf("planned contract is incomplete: %+v", contract)
	}

	contractPath := filepath.Join(t.TempDir(), "change.contract.json")
	if err := os.WriteFile(contractPath, planOutput.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	verify := newChangeVerifyCmd(loader)
	var verifyOutput bytes.Buffer
	verify.SetOut(&verifyOutput)
	verify.SetArgs([]string{contractPath})
	if err := verify.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(verifyOutput.String(), `"status":"verified"`) || !strings.Contains(verifyOutput.String(), contract.ID) {
		t.Fatalf("verification receipt is incomplete: %s", verifyOutput.String())
	}
}

func TestChangeVerifyFailsOnSchemaDrift(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://owner:secret@example.test/app")
	workspace := changeTestWorkspace()
	contract, err := app.BuildChangeContract("CREATE TABLE public.events(id bigint)", workspace)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(contract)
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loader := func(context.Context, string) (*app.DatabaseWorkspace, error) {
		drifted := *workspace
		drifted.Schema.Tables = append(append([]erd.Table(nil), workspace.Schema.Tables...), erd.Table{Schema: "public", Name: "drift"})
		return &drifted, nil
	}
	verify := newChangeVerifyCmd(loader)
	verify.SetArgs([]string{path})
	err = verify.Execute()
	if err == nil || !strings.Contains(err.Error(), "schema changed") {
		t.Fatalf("schema drift should fail CI verification, got %v", err)
	}
}

func TestChangeRehearseUsesDedicatedTargetAndBoundConfirmation(t *testing.T) {
	workspace := changeTestWorkspace()
	contract, err := app.BuildChangeContract("CREATE TABLE public.events(id bigint)", workspace)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(contract)
	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	rehearser := func(_ context.Context, dsn string, got app.ChangeContract, confirmation string) (*app.RehearsalReceipt, error) {
		called = true
		if dsn != "postgresql://rehearsal" || got.ID != contract.ID || confirmation != "REHEARSE "+contract.ID {
			t.Fatalf("rehearsal arguments were not bound: dsn=%q id=%q confirmation=%q", dsn, got.ID, confirmation)
		}
		return &app.RehearsalReceipt{Version: "1.0", Status: "verified", ContractID: got.ID, ProductionApplyLocked: true}, nil
	}
	cmd := newChangeRehearseCmd(rehearser)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{path, "--rehearsal-url", "postgresql://rehearsal", "--confirm", "REHEARSE " + contract.ID})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !called || !strings.Contains(output.String(), `"production_apply_locked": true`) {
		t.Fatalf("rehearsal receipt missing: %s", output.String())
	}
}
