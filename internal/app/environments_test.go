package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sarankumar1325/queryshift/internal/erd"
)

func TestEnvironmentConfigAllowsOnlySecretReferences(t *testing.T) {
	config := EnvironmentConfig{Version: EnvironmentConfigVersion, Environments: []EnvironmentProfile{{Name: "production", Tier: "production", SecretRef: "env://PRODUCTION_DATABASE_URL"}}}
	if err := ValidateEnvironmentConfig(config); err != nil {
		t.Fatal(err)
	}
	bad := config
	bad.Environments = append([]EnvironmentProfile(nil), config.Environments...)
	bad.Environments[0].SecretRef = "postgresql://user:password@example/db"
	if err := ValidateEnvironmentConfig(bad); err == nil {
		t.Fatal("raw DSN should be rejected")
	}
	t.Setenv("PRODUCTION_DATABASE_URL", "postgresql://example/db")
	if got, err := ResolveEnvironmentSecret(config.Environments[0]); err != nil || got != "postgresql://example/db" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestCompareDatabaseSchemasReportsStructuralDrift(t *testing.T) {
	from := &DatabaseWorkspace{Host: "dev", Database: "app", Schema: erd.Schema{Tables: []erd.Table{{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}}}}}}
	to := &DatabaseWorkspace{Host: "prod", Database: "app", Schema: erd.Schema{Tables: []erd.Table{{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", Type: "uuid", PK: true}, {Name: "email", Type: "text"}}}}}}
	drift, err := CompareDatabaseSchemas(from, to, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if drift.InSync || drift.Risk != "elevated" || len(drift.Changed) != 1 || len(drift.Added) != 1 {
		t.Fatalf("unexpected drift: %+v", drift)
	}
}

func TestChangeBundleDetectsTampering(t *testing.T) {
	contract, err := BuildChangeContract("CREATE TABLE public.events(id bigint)", contractWorkspace())
	if err != nil {
		t.Fatal(err)
	}
	run, err := NewVerifiedChangeRun(contract, DefaultChangePolicy(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "bundle")
	if _, err := ExportChangeRunBundle(*run, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyChangeRunBundle(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "change.sql"), []byte("DROP TABLE users;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyChangeRunBundle(directory); err == nil {
		t.Fatal("tampered bundle should fail")
	}
}
