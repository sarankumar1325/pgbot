package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLocalEnvLoadsOnlyModelConfigurationWithoutOverridingShell(t *testing.T) {
	for key := range localEnvAllowlist {
		t.Setenv(key, "")
	}
	t.Setenv("DATABASE_URL", "")
	t.Setenv("UNRELATED", "")
	t.Setenv("AWS_REGION", "ap-south-1")
	path := filepath.Join(t.TempDir(), ".env")
	content := "AWS_REGION=us-east-1\nQUERYSHIFT_AI_PROVIDER=bedrock\nQUERYSHIFT_AGENT_MODEL='deepseek.v3.2'\nDATABASE_URL=postgresql://secret\nUNRELATED=value\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := LoadLocalEnv(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("AWS_REGION"); got != "ap-south-1" {
		t.Fatalf("shell environment was overwritten: %q", got)
	}
	if got := os.Getenv("QUERYSHIFT_AI_PROVIDER"); got != "bedrock" {
		t.Fatalf("provider not loaded: %q", got)
	}
	if got := os.Getenv("QUERYSHIFT_AGENT_MODEL"); got != "deepseek.v3.2" {
		t.Fatalf("quoted model not loaded: %q", got)
	}
	if got := os.Getenv("DATABASE_URL"); got != "" {
		t.Fatalf("database URL must not be loaded by the TUI env reader: %q", got)
	}
	if got := os.Getenv("UNRELATED"); got != "" {
		t.Fatalf("unrelated variable must not be loaded: %q", got)
	}
}
