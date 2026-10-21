package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const VerifiedChangeRunVersion = "1.0"

// VerifiedChangeRun is the durable proof chain for one database change. It
// joins artifacts that previously existed as disconnected commands and is safe
// to serialize because it never contains a connection string or signing key.
type VerifiedChangeRun struct {
	Version      string               `json:"version"`
	ID           string               `json:"id"`
	Status       string               `json:"status"`
	CreatedAt    time.Time            `json:"created_at"`
	UpdatedAt    time.Time            `json:"updated_at"`
	Contract     ChangeContract       `json:"contract"`
	Policy       ChangePolicy         `json:"policy"`
	Decision     PolicyDecision       `json:"policy_decision"`
	Rehearsal    *RehearsalReceipt    `json:"rehearsal,omitempty"`
	Approval     *ChangeApproval      `json:"approval,omitempty"`
	Execution    *ChangeResult        `json:"execution,omitempty"`
	Verification []VerificationResult `json:"production_verification,omitempty"`
	Audit        []ChangeRunEvent     `json:"audit"`
}

type ChangeRunEvent struct {
	Sequence    int       `json:"sequence"`
	At          time.Time `json:"at"`
	Kind        string    `json:"kind"`
	Actor       string    `json:"actor,omitempty"`
	Summary     string    `json:"summary"`
	Previous    string    `json:"previous_sha256,omitempty"`
	EventSHA256 string    `json:"event_sha256"`
}

func NewVerifiedChangeRun(contract ChangeContract, policy ChangePolicy, now time.Time) (*VerifiedChangeRun, error) {
	if err := ValidateChangeContract(contract, nil); err != nil {
		return nil, err
	}
	decision, err := EvaluateChangePolicy(contract, policy)
	if err != nil {
		return nil, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	run := &VerifiedChangeRun{
		Version: VerifiedChangeRunVersion, ID: "qsr_" + contract.ContractSHA256[:16],
		Status: "proposed", CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
		Contract: contract, Policy: policy, Decision: decision,
	}
	run.appendEvent(now, "contract_created", contract.Proposer, "Change Contract bound to target and schema")
	if decision.Outcome == "deny" {
		run.Status = "denied"
		run.appendEvent(now, "policy_denied", "policy:"+policy.Name, strings.Join(decision.Reasons, "; "))
	} else {
		run.refreshStatus(now)
	}
	return run, nil
}

// RecordRehearsal attaches only a receipt for this exact contract. A failed or
// incomplete rehearsal remains visible but never unlocks production.
func (r *VerifiedChangeRun) RecordRehearsal(receipt RehearsalReceipt, now time.Time) error {
	if err := r.validateIdentity(); err != nil {
		return err
	}
	if receipt.ContractID != r.Contract.ID {
		return fmt.Errorf("rehearsal receipt is not bound to change contract %s", r.Contract.ID)
	}
	if receipt.RehearsalTarget.Fingerprint == r.Contract.Target.Fingerprint {
		return fmt.Errorf("rehearsal receipt targets the reviewed production database")
	}
	if receipt.BeforeSchemaSHA256 != r.Contract.SchemaSHA256 {
		return fmt.Errorf("rehearsal did not start from the reviewed schema")
	}
	r.Rehearsal = &receipt
	r.appendEvent(now, "rehearsal_recorded", "runner", "Rehearsal status: "+receipt.Status)
	r.refreshStatus(now)
	return nil
}

func (r *VerifiedChangeRun) RecordApproval(approval ChangeApproval, key []byte, now time.Time) error {
	if err := r.validateIdentity(); err != nil {
		return err
	}
	if err := VerifyChangeApproval(r.Contract, r.Policy, approval, key, now); err != nil {
		return err
	}
	r.Approval = &approval
	r.appendEvent(now, "approval_recorded", approval.Approver, "Signed approval "+approval.ID)
	r.refreshStatus(now)
	return nil
}

func (r *VerifiedChangeRun) Authorize(key []byte, now time.Time) error {
	if err := r.validateIdentity(); err != nil {
		return err
	}
	if r.Decision.Outcome == "deny" {
		return fmt.Errorf("change run was denied by policy")
	}
	if riskRank(r.Contract.Risk.Level) >= riskRank("elevated") {
		if r.Rehearsal == nil {
			return fmt.Errorf("%s-risk change requires a rehearsal receipt", r.Contract.Risk.Level)
		}
		if r.Rehearsal.Status != "verified" {
			return fmt.Errorf("rehearsal status %q does not authorize production", r.Rehearsal.Status)
		}
	}
	return AuthorizeChangeExecution(r.Contract, r.Policy, r.Approval, key, now)
}

// ExecuteVerifiedChangeRun is the only unified production path. Authorization
// is completed before ApplyChangeContract opens its write connection.
func ExecuteVerifiedChangeRun(ctx context.Context, connectionString string, run *VerifiedChangeRun, confirmation string, approvalKey []byte, now time.Time) (*ChangeResult, error) {
	if run == nil {
		return nil, fmt.Errorf("verified change run is required")
	}
	if err := run.Authorize(approvalKey, now); err != nil {
		run.appendEvent(now, "execution_blocked", "policy", err.Error())
		return nil, err
	}
	result, err := ApplyChangeContract(ctx, connectionString, run.Contract, confirmation)
	if err != nil {
		run.Status = "failed"
		run.appendEvent(now, "execution_failed", "runner", err.Error())
		return nil, err
	}
	run.Execution = result
	run.Verification = productionVerification(run.Contract, result)
	run.Status = "verified"
	for _, verification := range run.Verification {
		if verification.Status == "failed" {
			run.Status = "verification_failed"
			break
		}
		if verification.Status == "needs_review" && run.Status == "verified" {
			run.Status = "applied_needs_review"
		}
	}
	run.appendEvent(time.Now(), "production_executed", "runner", fmt.Sprintf("%s; rows=%d; verification=%s", result.Command, result.RowsAffected, run.Status))
	return result, nil
}

func productionVerification(contract ChangeContract, result *ChangeResult) []VerificationResult {
	results := make([]VerificationResult, 0, len(contract.Verification))
	for _, probe := range contract.Verification {
		verification := VerificationResult{ProbeID: probe.ID, Kind: probe.Kind, Status: "passed"}
		switch probe.Kind {
		case "execution_receipt":
			verification.Evidence = fmt.Sprintf("%s; rows=%d; duration=%s", result.Command, result.RowsAffected, result.Duration.Round(time.Millisecond))
		case "schema_diff", "index_validity":
			if !result.SnapshotAfter || result.AfterSchemaSHA256 == "" {
				verification.Status = "failed"
				verification.Evidence = "post-change schema snapshot unavailable"
			} else {
				verification.Evidence = "before=" + shortHash(result.BeforeSchemaSHA256) + " after=" + shortHash(result.AfterSchemaSHA256)
			}
		default:
			verification.Status = "needs_review"
			verification.Evidence = "domain-specific production assertion is still required"
		}
		results = append(results, verification)
	}
	return results
}

func (r *VerifiedChangeRun) refreshStatus(now time.Time) {
	if r.Decision.Outcome == "deny" {
		r.Status = "denied"
		return
	}
	if riskRank(r.Contract.Risk.Level) >= riskRank("elevated") && (r.Rehearsal == nil || r.Rehearsal.Status != "verified") {
		r.Status = "rehearsal_required"
	} else if r.Decision.Outcome == "approval_required" && r.Approval == nil {
		r.Status = "approval_required"
	} else {
		r.Status = "ready"
	}
	if !now.IsZero() {
		r.UpdatedAt = now.UTC()
	}
}

func (r *VerifiedChangeRun) validateIdentity() error {
	if r == nil || r.Version != VerifiedChangeRunVersion || r.ID == "" {
		return fmt.Errorf("invalid verified change run")
	}
	if err := ValidateChangeContract(r.Contract, nil); err != nil {
		return err
	}
	decision, err := EvaluateChangePolicy(r.Contract, r.Policy)
	if err != nil {
		return err
	}
	if decision.PolicySHA256 != r.Decision.PolicySHA256 || decision.Outcome != r.Decision.Outcome {
		return fmt.Errorf("change run policy decision was modified")
	}
	return nil
}

func (r *VerifiedChangeRun) appendEvent(at time.Time, kind, actor, summary string) {
	if at.IsZero() {
		at = time.Now()
	}
	event := ChangeRunEvent{Sequence: len(r.Audit) + 1, At: at.UTC(), Kind: kind, Actor: strings.TrimSpace(actor), Summary: summary}
	if len(r.Audit) > 0 {
		event.Previous = r.Audit[len(r.Audit)-1].EventSHA256
	}
	encoded, _ := json.Marshal(event)
	digest := sha256.Sum256(encoded)
	event.EventSHA256 = hex.EncodeToString(digest[:])
	r.Audit = append(r.Audit, event)
	r.UpdatedAt = event.At
}

// VerifyChangeRunAudit detects editing, removal, insertion, or reordering of
// events in an exported run.
func VerifyChangeRunAudit(run VerifiedChangeRun) error {
	previous := ""
	for index, event := range run.Audit {
		if event.Sequence != index+1 || event.Previous != previous {
			return fmt.Errorf("audit chain is broken at event %d", index+1)
		}
		actual := event.EventSHA256
		event.EventSHA256 = ""
		encoded, _ := json.Marshal(event)
		digest := sha256.Sum256(encoded)
		if actual != hex.EncodeToString(digest[:]) {
			return fmt.Errorf("audit event %d integrity check failed", index+1)
		}
		previous = actual
	}
	return nil
}
