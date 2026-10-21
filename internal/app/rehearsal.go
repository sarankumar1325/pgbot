package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type VerificationResult struct {
	ProbeID  string `json:"probe_id"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
}

type RehearsalReceipt struct {
	Version               string               `json:"version"`
	Status                string               `json:"status"`
	ContractID            string               `json:"contract_id"`
	RehearsalTarget       ChangeTarget         `json:"rehearsal_target"`
	StartedAt             time.Time            `json:"started_at"`
	CompletedAt           time.Time            `json:"completed_at"`
	Execution             *ChangeResult        `json:"execution,omitempty"`
	Verification          []VerificationResult `json:"verification"`
	BeforeSchemaSHA256    string               `json:"before_schema_sha256"`
	AfterSchemaSHA256     string               `json:"after_schema_sha256,omitempty"`
	ProductionApplyLocked bool                 `json:"production_apply_locked"`
}

// RehearseChangeContract applies a reviewed contract only to a distinct target
// with the same starting schema and returns a machine-readable verification
// receipt. The rehearsal confirmation is bound to the contract ID.
func RehearseChangeContract(ctx context.Context, connectionString string, contract ChangeContract, confirmation string) (*RehearsalReceipt, error) {
	expected := "REHEARSE " + contract.ID
	if strings.TrimSpace(confirmation) != expected {
		return nil, fmt.Errorf("rehearsal confirmation must exactly match %q", expected)
	}
	before, err := LoadDatabase(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("capture rehearsal schema: %w", err)
	}
	if err := ValidateChangeContractForRehearsal(contract, before); err != nil {
		return nil, err
	}
	started := time.Now().UTC()
	result, err := executeChangeContract(ctx, connectionString, contract, before, "rehearsal")
	if err != nil {
		return nil, err
	}
	after, afterErr := LoadDatabase(ctx, connectionString)
	receipt := &RehearsalReceipt{
		Version: "1.0", ContractID: contract.ID, RehearsalTarget: changeTarget(before),
		StartedAt: started, CompletedAt: time.Now().UTC(), Execution: result,
		BeforeSchemaSHA256: result.BeforeSchemaSHA256, AfterSchemaSHA256: result.AfterSchemaSHA256,
		ProductionApplyLocked: true,
	}
	receipt.Verification = verifyRehearsal(contract, result, after, afterErr)
	receipt.Status = rehearsalStatus(receipt.Verification)
	return receipt, nil
}

func verifyRehearsal(contract ChangeContract, result *ChangeResult, after *DatabaseWorkspace, afterErr error) []VerificationResult {
	results := make([]VerificationResult, 0, len(contract.Verification))
	for _, probe := range contract.Verification {
		verification := VerificationResult{ProbeID: probe.ID, Kind: probe.Kind, Status: "passed"}
		switch probe.Kind {
		case "execution_receipt":
			verification.Evidence = fmt.Sprintf("%s; rows=%d; duration=%s", result.Command, result.RowsAffected, result.Duration.Round(time.Millisecond))
		case "schema_diff":
			if afterErr != nil || !result.SnapshotAfter {
				verification.Status = "failed"
				verification.Evidence = "post-change schema snapshot unavailable"
			} else {
				verification.Evidence = "before=" + shortHash(result.BeforeSchemaSHA256) + " after=" + shortHash(result.AfterSchemaSHA256)
			}
		case "index_validity":
			if afterErr != nil || after == nil || !rehearsedIndexExists(contract, after) {
				verification.Status = "failed"
				verification.Evidence = "reviewed index was not found in the post-change catalog"
			} else {
				verification.Evidence = "reviewed index is present in the post-change catalog"
			}
		case "application_invariant":
			verification.Status = "needs_review"
			verification.Evidence = "provide a domain-specific read assertion before production approval"
		default:
			verification.Status = "needs_review"
			verification.Evidence = "no deterministic verifier is registered for this probe"
		}
		results = append(results, verification)
	}
	return results
}

func rehearsalStatus(results []VerificationResult) string {
	status := "verified"
	for _, result := range results {
		if result.Status == "failed" {
			return "failed"
		}
		if result.Status == "needs_review" {
			status = "needs_review"
		}
	}
	return status
}

func rehearsedIndexExists(contract ChangeContract, workspace *DatabaseWorkspace) bool {
	if len(contract.AffectedObjects) == 0 {
		return false
	}
	name := unqualifiedObjectName(contract.AffectedObjects[0].Name)
	for _, table := range workspace.Schema.Tables {
		for _, index := range table.Indexes {
			if index.Name == name {
				return true
			}
		}
	}
	return false
}

func shortHash(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}
