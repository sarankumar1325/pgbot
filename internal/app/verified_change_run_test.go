package app

import (
	"strings"
	"testing"
	"time"
)

func TestVerifiedChangeRunRequiresRehearsalAndApprovalInOrder(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	contract, err := BuildChangeContractForActor("DROP TABLE public.accounts", contractWorkspace(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewVerifiedChangeRun(contract, DefaultChangePolicy(), now)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "rehearsal_required" {
		t.Fatalf("status=%q", run.Status)
	}
	if err := run.Authorize(nil, now); err == nil || !strings.Contains(err.Error(), "rehearsal") {
		t.Fatalf("expected rehearsal gate, got %v", err)
	}
	receipt := RehearsalReceipt{
		Version: "1.0", Status: "verified", ContractID: contract.ID,
		RehearsalTarget:    ChangeTarget{Database: "clone", Fingerprint: "different"},
		BeforeSchemaSHA256: contract.SchemaSHA256, ProductionApplyLocked: true,
	}
	if err := run.RecordRehearsal(receipt, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if run.Status != "approval_required" {
		t.Fatalf("status=%q", run.Status)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	approval, err := ApproveChange(contract, run.Policy, "bob", []string{"dba"}, key, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := run.RecordApproval(approval, key, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if run.Status != "ready" {
		t.Fatalf("status=%q", run.Status)
	}
	if err := run.Authorize(key, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("ready run was blocked: %v", err)
	}
	if err := VerifyChangeRunAudit(*run); err != nil {
		t.Fatalf("valid audit failed: %v", err)
	}
}

func TestVerifiedChangeRunRejectsWrongRehearsalAndTamperedAudit(t *testing.T) {
	contract, err := BuildChangeContract("CREATE INDEX accounts_id_idx ON public.accounts(id)", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewVerifiedChangeRun(contract, DefaultChangePolicy(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	receipt := RehearsalReceipt{Status: "verified", ContractID: "wrong", RehearsalTarget: ChangeTarget{Fingerprint: "clone"}, BeforeSchemaSHA256: contract.SchemaSHA256}
	if err := run.RecordRehearsal(receipt, time.Now()); err == nil {
		t.Fatal("wrong contract rehearsal should fail")
	}
	tampered := *run
	tampered.Audit = append([]ChangeRunEvent(nil), run.Audit...)
	tampered.Audit[0].Summary = "edited"
	if err := VerifyChangeRunAudit(tampered); err == nil {
		t.Fatal("tampered audit should fail")
	}
}
