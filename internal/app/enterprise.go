package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const EnterpriseProtocolVersion = "1.0"

type Principal struct {
	Subject         string    `json:"subject"`
	Issuer          string    `json:"issuer"`
	Roles           []string  `json:"roles"`
	AuthenticatedAt time.Time `json:"authenticated_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

type CapabilityPolicy struct {
	Version            string              `json:"version"`
	Name               string              `json:"name"`
	CapabilitiesByRole map[string][]string `json:"capabilities_by_role"`
	RequestsPerMinute  int                 `json:"requests_per_minute"`
}

func DefaultCapabilityPolicy() CapabilityPolicy {
	return CapabilityPolicy{
		Version: EnterpriseProtocolVersion, Name: "queryshift-enterprise-default", RequestsPerMinute: 30,
		CapabilitiesByRole: map[string][]string{
			"developer": {"database.read", "change.propose", "change.rehearse"},
			"dba":       {"database.read", "change.propose", "change.rehearse", "change.approve", "change.execute"},
			"auditor":   {"database.read", "audit.read"},
		},
	}
}

func AuthorizeCapability(principal Principal, policy CapabilityPolicy, capability string, now time.Time) error {
	if strings.TrimSpace(principal.Subject) == "" || strings.TrimSpace(principal.Issuer) == "" {
		return fmt.Errorf("authenticated principal subject and issuer are required")
	}
	if principal.ExpiresAt.IsZero() || !now.Before(principal.ExpiresAt) {
		return fmt.Errorf("principal authentication is expired")
	}
	for _, role := range principal.Roles {
		for _, allowed := range policy.CapabilitiesByRole[strings.ToLower(role)] {
			if allowed == capability {
				return nil
			}
		}
	}
	return fmt.Errorf("principal %s lacks capability %s", principal.Subject, capability)
}

// RunnerRequest is a short-lived, signed instruction for an agent running
// inside the customer's network. It contains no DSN or approval signing key.
type RunnerRequest struct {
	Version      string            `json:"version"`
	ID           string            `json:"id"`
	IssuedAt     time.Time         `json:"issued_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Nonce        string            `json:"nonce"`
	Environment  string            `json:"environment"`
	Principal    Principal         `json:"principal"`
	Run          VerifiedChangeRun `json:"run"`
	Confirmation string            `json:"confirmation"`
	Signature    string            `json:"signature"`
}

func NewRunnerRequest(run VerifiedChangeRun, environment string, principal Principal, confirmation, nonce string, ttl time.Duration, signingKey []byte, now time.Time) (RunnerRequest, error) {
	if len(signingKey) < 32 {
		return RunnerRequest{}, fmt.Errorf("runner signing key must contain at least 32 bytes")
	}
	if err := run.validateIdentity(); err != nil {
		return RunnerRequest{}, err
	}
	if err := VerifyChangeRunAudit(run); err != nil {
		return RunnerRequest{}, err
	}
	if strings.TrimSpace(environment) == "" || strings.TrimSpace(nonce) == "" {
		return RunnerRequest{}, fmt.Errorf("environment and unique nonce are required")
	}
	if ttl <= 0 || ttl > 15*time.Minute {
		return RunnerRequest{}, fmt.Errorf("runner request TTL must be between 1ns and 15m")
	}
	request := RunnerRequest{Version: EnterpriseProtocolVersion, IssuedAt: now.UTC(), ExpiresAt: now.UTC().Add(ttl), Nonce: nonce, Environment: environment, Principal: principal, Run: run, Confirmation: confirmation}
	digest, _ := runnerRequestDigest(request)
	request.ID = "qrr_" + digest[:16]
	request.Signature = signRunnerRequest(request, signingKey)
	return request, nil
}

func VerifyRunnerRequest(request RunnerRequest, signingKey []byte, now time.Time) error {
	if request.Version != EnterpriseProtocolVersion || request.ID == "" || len(signingKey) < 32 {
		return fmt.Errorf("invalid runner request identity or signing key")
	}
	if now.Before(request.IssuedAt.Add(-time.Minute)) || !now.Before(request.ExpiresAt) {
		return fmt.Errorf("runner request is not active")
	}
	if request.ExpiresAt.Sub(request.IssuedAt) > 15*time.Minute {
		return fmt.Errorf("runner request lifetime exceeds 15m")
	}
	digest, err := runnerRequestDigest(request)
	if err != nil {
		return err
	}
	if request.ID != "qrr_"+digest[:16] {
		return fmt.Errorf("runner request identity was modified")
	}
	expected := signRunnerRequest(request, signingKey)
	if !hmac.Equal([]byte(expected), []byte(request.Signature)) {
		return fmt.Errorf("runner request signature is invalid")
	}
	if err := request.Run.validateIdentity(); err != nil {
		return err
	}
	return VerifyChangeRunAudit(request.Run)
}

type NonceStore interface {
	Use(nonce string, expiresAt time.Time) error
}

type MemoryNonceStore struct {
	mu   sync.Mutex
	used map[string]time.Time
}

func NewMemoryNonceStore() *MemoryNonceStore { return &MemoryNonceStore{used: map[string]time.Time{}} }
func (s *MemoryNonceStore) Use(nonce string, expiresAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for value, expiry := range s.used {
		if !now.Before(expiry) {
			delete(s.used, value)
		}
	}
	if _, exists := s.used[nonce]; exists {
		return fmt.Errorf("runner request nonce was already used")
	}
	s.used[nonce] = expiresAt
	return nil
}

// FileNonceStore persists consumed request nonces across runner processes. The
// advisory lock makes two concurrent executions of the same request serialize.
type FileNonceStore struct{ Path string }

func DefaultRunnerNonceStore() (*FileNonceStore, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return &FileNonceStore{Path: filepath.Join(base, "queryshift", "runner", "used-nonces.json")}, nil
}

func (s *FileNonceStore) Use(nonce string, expiresAt time.Time) error {
	if s == nil || strings.TrimSpace(s.Path) == "" {
		return fmt.Errorf("runner nonce store path is required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.Path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN) //nolint:errcheck
	used := map[string]time.Time{}
	if info, statErr := file.Stat(); statErr == nil && info.Size() > 0 {
		if err := json.NewDecoder(file).Decode(&used); err != nil {
			return fmt.Errorf("decode runner nonce store: %w", err)
		}
	}
	now := time.Now()
	for value, expiry := range used {
		if !now.Before(expiry) {
			delete(used, value)
		}
	}
	if _, exists := used[nonce]; exists {
		return fmt.Errorf("runner request nonce was already used")
	}
	used[nonce] = expiresAt.UTC()
	if _, err := file.Seek(0, 0); err != nil {
		return err
	}
	if err := file.Truncate(0); err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(used); err != nil {
		return err
	}
	return file.Sync()
}

// ExecuteRunnerRequest resolves the target secret only inside the customer
// runner, authorizes RBAC, consumes the anti-replay nonce, and then invokes the
// same Verified Change Run gate as the TUI.
func ExecuteRunnerRequest(ctx context.Context, request RunnerRequest, environments EnvironmentConfig, policy CapabilityPolicy, runnerKey, approvalKey []byte, nonces NonceStore, now time.Time) (*ChangeResult, error) {
	if err := VerifyRunnerRequest(request, runnerKey, now); err != nil {
		return nil, err
	}
	if err := AuthorizeCapability(request.Principal, policy, "change.execute", now); err != nil {
		return nil, err
	}
	if err := ValidateEnvironmentConfig(environments); err != nil {
		return nil, err
	}
	if nonces == nil {
		return nil, fmt.Errorf("runner nonce store is required")
	}
	if err := nonces.Use(request.Nonce, request.ExpiresAt); err != nil {
		return nil, err
	}
	var selected *EnvironmentProfile
	for index := range environments.Environments {
		if environments.Environments[index].Name == request.Environment {
			selected = &environments.Environments[index]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("environment %q is not configured on this runner", request.Environment)
	}
	dsn, err := ResolveEnvironmentSecret(*selected)
	if err != nil {
		return nil, err
	}
	run := request.Run
	return ExecuteVerifiedChangeRun(ctx, dsn, &run, request.Confirmation, approvalKey, now)
}

func runnerRequestDigest(request RunnerRequest) (string, error) {
	request.ID, request.Signature = "", ""
	encoded, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
func signRunnerRequest(request RunnerRequest, key []byte) string {
	digest, _ := runnerRequestDigest(request)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(digest))
	return hex.EncodeToString(mac.Sum(nil))
}

// AuditRecord is a portable JSONL event. Its hash links every exported record
// so downstream SIEM ingestion can detect removal, editing, or reordering.
type AuditRecord struct {
	Sequence int            `json:"sequence"`
	RunID    string         `json:"run_id"`
	Event    ChangeRunEvent `json:"event"`
	Previous string         `json:"previous_sha256,omitempty"`
	SHA256   string         `json:"sha256"`
}

func ExportAuditJSONL(writer io.Writer, runs []VerifiedChangeRun) error {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].CreatedAt.Equal(runs[j].CreatedAt) {
			return runs[i].ID < runs[j].ID
		}
		return runs[i].CreatedAt.Before(runs[j].CreatedAt)
	})
	encoder := json.NewEncoder(writer)
	previous, sequence := "", 0
	for _, run := range runs {
		if err := VerifyChangeRunAudit(run); err != nil {
			return err
		}
		for _, event := range run.Audit {
			sequence++
			record := AuditRecord{Sequence: sequence, RunID: run.ID, Event: event, Previous: previous}
			encoded, _ := json.Marshal(record)
			digest := sha256.Sum256(encoded)
			record.SHA256 = hex.EncodeToString(digest[:])
			if err := encoder.Encode(record); err != nil {
				return err
			}
			previous = record.SHA256
		}
	}
	return nil
}

// PrincipalRateLimiter is an in-memory runner guard. A production control
// plane can replace it, while the local runner still has a fail-closed limit.
type PrincipalRateLimiter struct {
	mu      sync.Mutex
	windows map[string]rateWindow
}
type rateWindow struct {
	Started time.Time
	Count   int
}

func NewPrincipalRateLimiter() *PrincipalRateLimiter {
	return &PrincipalRateLimiter{windows: map[string]rateWindow{}}
}
func (r *PrincipalRateLimiter) Allow(subject string, limit int, now time.Time) error {
	if strings.TrimSpace(subject) == "" || limit <= 0 {
		return fmt.Errorf("subject and positive rate limit are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	window := r.windows[subject]
	if window.Started.IsZero() || now.Sub(window.Started) >= time.Minute {
		window = rateWindow{Started: now}
	}
	if window.Count >= limit {
		return fmt.Errorf("rate limit exceeded for principal %s", subject)
	}
	window.Count++
	r.windows[subject] = window
	return nil
}
