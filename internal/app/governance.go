package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const GovernanceVersion = "1.0"

type ChangePolicy struct {
	Version                   string              `json:"version"`
	Name                      string              `json:"name"`
	MaximumRisk               string              `json:"maximum_risk"`
	DeniedKinds               []string            `json:"denied_kinds,omitempty"`
	ApprovalRequiredAtOrAbove string              `json:"approval_required_at_or_above"`
	RequiredRolesByRisk       map[string][]string `json:"required_roles_by_risk,omitempty"`
	SeparationOfDuties        bool                `json:"separation_of_duties"`
	ApprovalTTLMinutes        int                 `json:"approval_ttl_minutes"`
}

type PolicyDecision struct {
	Version       string   `json:"version"`
	Outcome       string   `json:"outcome"`
	PolicyName    string   `json:"policy_name"`
	PolicySHA256  string   `json:"policy_sha256"`
	ContractID    string   `json:"contract_id"`
	Reasons       []string `json:"reasons"`
	RequiredRoles []string `json:"required_roles,omitempty"`
}

type ChangeApproval struct {
	Version        string    `json:"version"`
	ID             string    `json:"id"`
	ContractID     string    `json:"contract_id"`
	ContractSHA256 string    `json:"contract_sha256"`
	PolicySHA256   string    `json:"policy_sha256"`
	Approver       string    `json:"approver"`
	Roles          []string  `json:"roles"`
	Decision       string    `json:"decision"`
	ApprovedAt     time.Time `json:"approved_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	Signature      string    `json:"signature"`
}

func DefaultChangePolicy() ChangePolicy {
	return ChangePolicy{
		Version: GovernanceVersion, Name: "queryshift-default", MaximumRisk: "critical",
		ApprovalRequiredAtOrAbove: "high", SeparationOfDuties: true, ApprovalTTLMinutes: 60,
		RequiredRolesByRisk: map[string][]string{"high": {"db_owner"}, "critical": {"dba"}},
	}
}

func EvaluateChangePolicy(contract ChangeContract, policy ChangePolicy) (PolicyDecision, error) {
	if err := validateChangeContractIntegrity(contract); err != nil {
		return PolicyDecision{}, err
	}
	if policy.Version != GovernanceVersion || strings.TrimSpace(policy.Name) == "" {
		return PolicyDecision{}, fmt.Errorf("invalid governance policy version or name")
	}
	if riskRank(policy.MaximumRisk) == 99 || riskRank(policy.ApprovalRequiredAtOrAbove) == 99 {
		return PolicyDecision{}, fmt.Errorf("policy risk thresholds must be controlled, elevated, high, or critical")
	}
	policyHash, err := policyDigest(policy)
	if err != nil {
		return PolicyDecision{}, err
	}
	decision := PolicyDecision{Version: GovernanceVersion, Outcome: "allow", PolicyName: policy.Name, PolicySHA256: policyHash, ContractID: contract.ID}
	if riskRank(contract.Risk.Level) > riskRank(policy.MaximumRisk) {
		decision.Outcome = "deny"
		decision.Reasons = append(decision.Reasons, "contract risk exceeds policy maximum")
	}
	for _, denied := range policy.DeniedKinds {
		if strings.EqualFold(denied, contract.Plan.Kind) {
			decision.Outcome = "deny"
			decision.Reasons = append(decision.Reasons, "statement kind is denied by policy")
		}
	}
	if decision.Outcome != "deny" && riskRank(contract.Risk.Level) >= riskRank(policy.ApprovalRequiredAtOrAbove) {
		decision.Outcome = "approval_required"
		decision.Reasons = append(decision.Reasons, "risk requires an independent approval")
		decision.RequiredRoles = append([]string(nil), policy.RequiredRolesByRisk[contract.Risk.Level]...)
		sort.Strings(decision.RequiredRoles)
	}
	if len(decision.Reasons) == 0 {
		decision.Reasons = []string{"contract satisfies policy"}
	}
	return decision, nil
}

func ApproveChange(contract ChangeContract, policy ChangePolicy, approver string, roles []string, key []byte, now time.Time) (ChangeApproval, error) {
	decision, err := EvaluateChangePolicy(contract, policy)
	if err != nil {
		return ChangeApproval{}, err
	}
	if decision.Outcome == "deny" {
		return ChangeApproval{}, fmt.Errorf("policy denied this contract: %s", strings.Join(decision.Reasons, "; "))
	}
	approver = strings.TrimSpace(approver)
	if approver == "" || len(key) < 32 {
		return ChangeApproval{}, fmt.Errorf("approver identity and a signing key of at least 32 bytes are required")
	}
	if policy.SeparationOfDuties && contract.Proposer != "" && strings.EqualFold(contract.Proposer, approver) {
		return ChangeApproval{}, fmt.Errorf("separation of duties requires an approver other than the proposer")
	}
	if !containsAllRoles(roles, decision.RequiredRoles) {
		return ChangeApproval{}, fmt.Errorf("approver is missing required roles: %s", strings.Join(decision.RequiredRoles, ", "))
	}
	ttl := time.Duration(policy.ApprovalTTLMinutes) * time.Minute
	if ttl <= 0 {
		ttl = time.Hour
	}
	approval := ChangeApproval{
		Version: GovernanceVersion, ContractID: contract.ID, ContractSHA256: contract.ContractSHA256,
		PolicySHA256: decision.PolicySHA256, Approver: approver, Roles: append([]string(nil), roles...),
		Decision: "approved", ApprovedAt: now.UTC(), ExpiresAt: now.UTC().Add(ttl),
	}
	sort.Strings(approval.Roles)
	digest, _ := approvalDigest(approval)
	approval.ID = "qsa_" + digest[:16]
	approval.Signature = signApproval(approval, key)
	return approval, nil
}

func VerifyChangeApproval(contract ChangeContract, policy ChangePolicy, approval ChangeApproval, key []byte, now time.Time) error {
	if len(key) < 32 {
		return fmt.Errorf("a signing key of at least 32 bytes is required")
	}
	if approval.Version != GovernanceVersion || approval.Decision != "approved" || approval.ID == "" {
		return fmt.Errorf("approval identity or decision is invalid")
	}
	decision, err := EvaluateChangePolicy(contract, policy)
	if err != nil {
		return err
	}
	if approval.ContractID != contract.ID || approval.ContractSHA256 != contract.ContractSHA256 || approval.PolicySHA256 != decision.PolicySHA256 {
		return fmt.Errorf("approval is not bound to this contract and policy")
	}
	if now.After(approval.ExpiresAt) {
		return fmt.Errorf("approval expired at %s", approval.ExpiresAt.Format(time.RFC3339))
	}
	if !approval.ExpiresAt.After(approval.ApprovedAt) {
		return fmt.Errorf("approval expiry is invalid")
	}
	expected := signApproval(approval, key)
	if !hmac.Equal([]byte(expected), []byte(approval.Signature)) {
		return fmt.Errorf("approval signature is invalid")
	}
	if !containsAllRoles(approval.Roles, decision.RequiredRoles) {
		return fmt.Errorf("approval no longer satisfies required roles")
	}
	return nil
}

// AuthorizeChangeExecution is the host-side policy gate used before opening a
// production write connection. Model output cannot bypass a deny or fabricate
// the independently signed approval required by policy.
func AuthorizeChangeExecution(contract ChangeContract, policy ChangePolicy, approval *ChangeApproval, key []byte, now time.Time) error {
	decision, err := EvaluateChangePolicy(contract, policy)
	if err != nil {
		return err
	}
	switch decision.Outcome {
	case "deny":
		return fmt.Errorf("change policy denied execution: %s", strings.Join(decision.Reasons, "; "))
	case "approval_required":
		if approval == nil {
			return fmt.Errorf("change policy requires an independent signed approval")
		}
		return VerifyChangeApproval(contract, policy, *approval, key, now)
	case "allow":
		return nil
	default:
		return fmt.Errorf("unknown policy outcome %q", decision.Outcome)
	}
}

func policyDigest(policy ChangePolicy) (string, error) {
	b, err := json.Marshal(policy)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func approvalDigest(a ChangeApproval) (string, error) {
	a.ID, a.Signature = "", ""
	b, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func signApproval(a ChangeApproval, key []byte) string {
	digest, _ := approvalDigest(a)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(digest))
	return hex.EncodeToString(mac.Sum(nil))
}
func riskRank(level string) int {
	for i, v := range []string{"controlled", "elevated", "high", "critical"} {
		if level == v {
			return i
		}
	}
	return 99
}
func containsAllRoles(actual, required []string) bool {
	set := map[string]bool{}
	for _, role := range actual {
		set[strings.ToLower(role)] = true
	}
	for _, role := range required {
		if !set[strings.ToLower(role)] {
			return false
		}
	}
	return true
}
