package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRunnerRequestIsShortLivedSignedAndCredentialFree(t *testing.T) {
	now := time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)
	contract, err := BuildChangeContract("CREATE TABLE public.events(id bigint)", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewVerifiedChangeRun(contract, DefaultChangePolicy(), now)
	if err != nil {
		t.Fatal(err)
	}
	principal := Principal{Subject: "alice@example.com", Issuer: "https://id.example", Roles: []string{"dba"}, AuthenticatedAt: now, ExpiresAt: now.Add(time.Hour)}
	key := []byte("0123456789abcdef0123456789abcdef")
	request, err := NewRunnerRequest(*run, "production", principal, "APPLY", "nonce-1", 5*time.Minute, key, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRunnerRequest(request, key, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(request)
	if strings.Contains(string(encoded), "postgresql://") || strings.Contains(string(encoded), "password") {
		t.Fatal("runner request contains credentials")
	}
	tampered := request
	tampered.Environment = "staging"
	if err := VerifyRunnerRequest(tampered, key, now.Add(time.Minute)); err == nil {
		t.Fatal("tampered request should fail")
	}
	if err := VerifyRunnerRequest(request, key, now.Add(6*time.Minute)); err == nil {
		t.Fatal("expired request should fail")
	}
}

func TestEnterpriseCapabilityNonceRateAndAudit(t *testing.T) {
	now := time.Now().UTC()
	principal := Principal{Subject: "dev", Issuer: "issuer", Roles: []string{"developer"}, ExpiresAt: now.Add(time.Hour)}
	policy := DefaultCapabilityPolicy()
	if err := AuthorizeCapability(principal, policy, "change.propose", now); err != nil {
		t.Fatal(err)
	}
	if err := AuthorizeCapability(principal, policy, "change.execute", now); err == nil {
		t.Fatal("developer should not execute production")
	}
	nonces := NewMemoryNonceStore()
	if err := nonces.Use("one", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := nonces.Use("one", now.Add(time.Minute)); err == nil {
		t.Fatal("replay should fail")
	}
	limiter := NewPrincipalRateLimiter()
	if err := limiter.Allow("dev", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := limiter.Allow("dev", 1, now); err == nil {
		t.Fatal("second request should be limited")
	}

	contract, _ := BuildChangeContract("CREATE TABLE public.events(id bigint)", contractWorkspace())
	run, _ := NewVerifiedChangeRun(contract, DefaultChangePolicy(), now)
	var output bytes.Buffer
	if err := ExportAuditJSONL(&output, []VerifiedChangeRun{*run}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), run.ID) || !strings.Contains(output.String(), "contract_created") {
		t.Fatalf("unexpected audit: %s", output.String())
	}
}
