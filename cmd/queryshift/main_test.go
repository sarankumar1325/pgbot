package main

import (
	"encoding/json"
	"testing"

	"github.com/sarankumar1325/queryshift/internal/model"
)

// TestExitCode pins the CI exit-code contract (0 clean / 1 warn / 2 critical).
// Scripts gate on these, so the mapping must not drift.
func TestExitCode(t *testing.T) {
	crit := model.Finding{Severity: model.SeverityCritical}
	warn := model.Finding{Severity: model.SeverityWarn}
	info := model.Finding{Severity: model.SeverityInfo}
	cases := []struct {
		name string
		fs   []model.Finding
		want int
	}{
		{"clean", nil, exitClean},
		{"info only is still clean", []model.Finding{info}, exitClean},
		{"a warning", []model.Finding{info, warn}, exitWarn},
		{"a critical", []model.Finding{warn, crit}, exitCritical},
		{"critical outranks warning regardless of order", []model.Finding{crit, warn}, exitCritical},
	}
	for _, c := range cases {
		if got := exitCode(c.fs, "warn"); got != c.want {
			t.Errorf("%s: exitCode = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDsnFromArgs(t *testing.T) {
	t.Setenv(mcpHideConnectionStringEnv, "")
	// Explicit argument wins.
	if dsn, err := dsnFromArgs(json.RawMessage(`{"connection_string":"postgres://arg"}`)); err != nil || dsn != "postgres://arg" {
		t.Errorf("arg DSN not honored: %q, %v", dsn, err)
	}

	// Falls back to $DATABASE_URL when no argument is given.
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("QUERYSHIFT_DATABASE_URL", "")
	t.Setenv("PGSERVICE", "")
	if dsn, err := dsnFromArgs(json.RawMessage(`{}`)); err != nil || dsn != "postgres://env" {
		t.Errorf("env fallback not honored: %q, %v", dsn, err)
	}

	// No argument and no env is a clear error, not an empty string.
	t.Setenv("DATABASE_URL", "")
	t.Setenv("QUERYSHIFT_DATABASE_URL", "")
	t.Setenv("PGSERVICE", "")
	if _, err := dsnFromArgs(json.RawMessage(`{}`)); err == nil {
		t.Error("missing DSN everywhere should be an error")
	}
}

func TestDsnFromArgs_agentModeIgnoresModelSuppliedDSN(t *testing.T) {
	t.Setenv(mcpHideConnectionStringEnv, "1")
	t.Setenv("DATABASE_URL", "postgres://trusted-env")
	t.Setenv("QUERYSHIFT_DATABASE_URL", "")
	t.Setenv("PGSERVICE", "")

	dsn, err := dsnFromArgs(json.RawMessage(`{"connection_string":"postgres://model-injected"}`))
	if err != nil {
		t.Fatal(err)
	}
	if dsn != "postgres://trusted-env" {
		t.Fatalf("agent mode used %q; want trusted environment DSN", dsn)
	}

	for _, tool := range queryshiftTools() {
		props, _ := tool.InputSchema["properties"].(map[string]any)
		if _, ok := props["connection_string"]; ok {
			t.Fatalf("agent mode exposed connection_string on tool %q", tool.Name)
		}
	}
}

func TestPgServiceFallback(t *testing.T) {
	t.Setenv("PGSERVICE", "")
	if got := pgServiceFallback(); got != "" {
		t.Errorf("no $PGSERVICE should fall back to empty, got %q", got)
	}

	t.Setenv("PGSERVICE", "mydb")
	if got := pgServiceFallback(); got != "service=mydb" {
		t.Errorf("pgServiceFallback = %q, want %q", got, "service=mydb")
	}

	// A bare $PGSERVICE resolves a connection when nothing else is set —
	// pggo.ParseConfig reads PGSERVICE(FILE) itself once it sees "service=...".
	t.Setenv("DATABASE_URL", "")
	t.Setenv("QUERYSHIFT_DATABASE_URL", "")
	if dsn, err := dsnFromArgs(json.RawMessage(`{}`)); err != nil || dsn != "service=mydb" {
		t.Errorf("PGSERVICE fallback not honored: %q, %v", dsn, err)
	}

	// An explicit argument still wins over $PGSERVICE.
	if dsn, err := dsnFromArgs(json.RawMessage(`{"connection_string":"postgres://arg"}`)); err != nil || dsn != "postgres://arg" {
		t.Errorf("arg should outrank $PGSERVICE: %q, %v", dsn, err)
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", "third"); got != "third" {
		t.Errorf("firstNonEmpty picked %q, want third", got)
	}
	if got := firstNonEmpty("first", "second"); got != "first" {
		t.Errorf("firstNonEmpty picked %q, want first", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("all-empty should be empty, got %q", got)
	}
}
