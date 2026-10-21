package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type DisposableRehearsalTarget struct {
	Provider         string    `json:"provider"`
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	CreatedAt        time.Time `json:"created_at"`
	ConnectionString string    `json:"-"`
}

type RehearsalTargetProvider interface {
	Create(context.Context, ChangeContract) (DisposableRehearsalTarget, error)
	Delete(context.Context, DisposableRehearsalTarget) error
}

// NeonRehearsalProvider creates a child branch with a read-write endpoint by
// using Neon's v2 API. The returned connection URI is memory-only and never
// appears in a receipt, run, bundle, or log.
type NeonRehearsalProvider struct {
	ProjectID string
	ParentID  string
	APIKey    string
	APIBase   string
	Client    *http.Client
}

var neonIdentifierPattern = regexp.MustCompile(`^[a-z0-9-]{1,60}$`)

func (p NeonRehearsalProvider) Create(ctx context.Context, contract ChangeContract) (DisposableRehearsalTarget, error) {
	if err := ValidateChangeContract(contract, nil); err != nil {
		return DisposableRehearsalTarget{}, err
	}
	if !neonIdentifierPattern.MatchString(p.ProjectID) {
		return DisposableRehearsalTarget{}, fmt.Errorf("invalid Neon project ID")
	}
	if p.ParentID != "" && !neonIdentifierPattern.MatchString(p.ParentID) {
		return DisposableRehearsalTarget{}, fmt.Errorf("invalid Neon parent branch ID")
	}
	if strings.TrimSpace(p.APIKey) == "" {
		return DisposableRehearsalTarget{}, fmt.Errorf("Neon API key is required")
	}
	name := "queryshift-" + strings.TrimPrefix(contract.ID, "qsc_")
	branch := map[string]any{"name": name}
	if p.ParentID != "" {
		branch["parent_id"] = p.ParentID
	}
	body, _ := json.Marshal(map[string]any{"branch": branch, "endpoints": []map[string]string{{"type": "read_write"}}})
	endpoint := strings.TrimRight(p.apiBase(), "/") + "/projects/" + url.PathEscape(p.ProjectID) + "/branches"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return DisposableRehearsalTarget{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	response, err := p.client().Do(req)
	if err != nil {
		return DisposableRehearsalTarget{}, fmt.Errorf("create Neon rehearsal branch: %w", err)
	}
	defer response.Body.Close()
	content, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return DisposableRehearsalTarget{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return DisposableRehearsalTarget{}, fmt.Errorf("create Neon rehearsal branch: HTTP %d: %s", response.StatusCode, safeProviderError(content))
	}
	var result struct {
		Branch struct {
			ID        string    `json:"id"`
			Name      string    `json:"name"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"branch"`
		ConnectionURIs []struct {
			ConnectionURI string `json:"connection_uri"`
		} `json:"connection_uris"`
	}
	if err := json.Unmarshal(content, &result); err != nil {
		return DisposableRehearsalTarget{}, fmt.Errorf("decode Neon branch response: %w", err)
	}
	if result.Branch.ID == "" || len(result.ConnectionURIs) == 0 || strings.TrimSpace(result.ConnectionURIs[0].ConnectionURI) == "" {
		return DisposableRehearsalTarget{}, fmt.Errorf("Neon branch response did not include a branch ID and connection URI")
	}
	return DisposableRehearsalTarget{Provider: "neon", ID: result.Branch.ID, Name: result.Branch.Name, CreatedAt: result.Branch.CreatedAt, ConnectionString: result.ConnectionURIs[0].ConnectionURI}, nil
}

func (p NeonRehearsalProvider) Delete(ctx context.Context, target DisposableRehearsalTarget) error {
	if !neonIdentifierPattern.MatchString(p.ProjectID) || !neonIdentifierPattern.MatchString(target.ID) {
		return fmt.Errorf("invalid Neon project or branch ID")
	}
	endpoint := strings.TrimRight(p.apiBase(), "/") + "/projects/" + url.PathEscape(p.ProjectID) + "/branches/" + url.PathEscape(target.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("Accept", "application/json")
	response, err := p.client().Do(req)
	if err != nil {
		return fmt.Errorf("delete Neon rehearsal branch: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		content, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		return fmt.Errorf("delete Neon rehearsal branch: HTTP %d: %s", response.StatusCode, safeProviderError(content))
	}
	return nil
}

func (p NeonRehearsalProvider) apiBase() string {
	if strings.TrimSpace(p.APIBase) != "" {
		return p.APIBase
	}
	return "https://console.neon.tech/api/v2"
}
func (p NeonRehearsalProvider) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}
func safeProviderError(content []byte) string {
	value := strings.TrimSpace(string(content))
	if len(value) > 500 {
		value = value[:500] + "…"
	}
	value = regexp.MustCompile(`(?i)postgres(?:ql)?://[^\s"']+`).ReplaceAllString(value, "[redacted connection URI]")
	return value
}

// RehearseWithProvider creates the disposable target, runs the exact contract,
// and deletes the target unless keep is explicitly true.
func RehearseWithProvider(ctx context.Context, provider RehearsalTargetProvider, contract ChangeContract, keep bool) (*RehearsalReceipt, DisposableRehearsalTarget, error) {
	if provider == nil {
		return nil, DisposableRehearsalTarget{}, fmt.Errorf("rehearsal target provider is required")
	}
	target, err := provider.Create(ctx, contract)
	if err != nil {
		return nil, DisposableRehearsalTarget{}, err
	}
	receipt, err := RehearseChangeContract(ctx, target.ConnectionString, contract, "REHEARSE "+contract.ID)
	if !keep {
		cleanupErr := provider.Delete(context.Background(), target)
		if err == nil && cleanupErr != nil {
			return receipt, target, fmt.Errorf("rehearsal succeeded but disposable target cleanup failed: %w", cleanupErr)
		}
	}
	return receipt, target, err
}
