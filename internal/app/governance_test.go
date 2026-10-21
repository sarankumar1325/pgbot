package app

import (
	"strings"
	"testing"
	"time"
)

func TestGovernanceRequiresIndependentRoleBoundApproval(t *testing.T) {
	contract, err := BuildChangeContractForActor("DROP TABLE public.accounts", contractWorkspace(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultChangePolicy()
	decision, err := EvaluateChangePolicy(contract, policy)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != "approval_required" || len(decision.RequiredRoles) != 1 || decision.RequiredRoles[0] != "dba" {
		t.Fatalf("critical contract did not require DBA approval: %+v", decision)
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	if _, err := ApproveChange(contract, policy, "alice", []string{"dba"}, key, time.Now()); err == nil || !strings.Contains(err.Error(), "separation") {
		t.Fatalf("self approval should fail, got %v", err)
	}
	now := time.Now().UTC()
	approval, err := ApproveChange(contract, policy, "bob", []string{"dba"}, key, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyChangeApproval(contract, policy, approval, key, now.Add(time.Minute)); err != nil {
		t.Fatalf("fresh signed approval should verify: %v", err)
	}
	if err := AuthorizeChangeExecution(contract, policy, nil, key, now); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("execution without required approval should fail: %v", err)
	}
	if err := AuthorizeChangeExecution(contract, policy, &approval, key, now); err != nil {
		t.Fatalf("signed independent approval should authorize execution: %v", err)
	}
	tampered := approval
	tampered.Approver = "mallory"
	if err := VerifyChangeApproval(contract, policy, tampered, key, now.Add(time.Minute)); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered approval should fail signature verification: %v", err)
	}
	if err := VerifyChangeApproval(contract, policy, approval, key, approval.ExpiresAt.Add(time.Second)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired approval should fail: %v", err)
	}
}

func TestGovernanceCanDenyStatementKinds(t *testing.T) {
	contract, err := BuildChangeContract("DROP TABLE public.accounts", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	policy := DefaultChangePolicy()
	policy.DeniedKinds = []string{"DROP"}
	decision, err := EvaluateChangePolicy(contract, policy)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != "deny" {
		t.Fatalf("DROP should be denied: %+v", decision)
	}
}
