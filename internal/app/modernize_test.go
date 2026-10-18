package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sarankumar1325/queryshift/internal/store"
)

func TestModernizeStreamDeliversEventsAndResult(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test helper uses a POSIX shell script")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	binDir := t.TempDir()
	command := filepath.Join(binDir, "queryshift-modernize")
	script := `#!/bin/sh
printf '%s\n' '{"type":"event","event":{"run_id":"run-1","sequence":1,"kind":"stage_started","stage":"mapper","logical_attempt":1,"transport_attempt":1,"timestamp":"2026-10-18T00:00:00Z","data":{"agent":"mapper"}}}'
printf '%s\n' '{"type":"result","result":{"run_id":"run-1","source_dialect":"oracle","target_dialect":"postgres","confidence_score":90,"risk_level":"low"}}'
`
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var events []AgentRunEvent
	result, err := ModernizeStream(
		context.Background(),
		ModernizeRequest{SQL: "SELECT 1", SourceDialect: "oracle"},
		func(event AgentRunEvent) { events = append(events, event) },
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.RunID != "run-1" || len(events) != 1 || events[0].Stage != "mapper" {
		t.Fatalf("unexpected stream result=%+v events=%+v", result, events)
	}
}

func TestPersistAgentRunDropsSensitiveAndUnapprovedFields(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	now := time.Now().UTC()
	secret := "postgresql://owner:password@example.test/db"
	events := []AgentRunEvent{{
		RunID: "safe-run", Sequence: 1, Kind: "run_started", Timestamp: now,
		Data: map[string]any{
			"model_id": "deepseek.v3.2",
			"agent":    "mapper",
			"sql":      "SELECT secret FROM credentials",
			"dsn":      secret,
			"message":  "provider echoed " + secret,
		},
	}}
	result := &ModernizationResult{RunID: "safe-run", ConfidenceScore: 88, RiskLevel: "medium"}
	persistAgentRun(
		ModernizeRequest{SQL: "SELECT secret", SourceDialect: "oracle", ConnectionString: secret},
		result,
		events,
		nil,
	)

	st, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	runs, err := st.RecentAgentRuns(1)
	if err != nil || len(runs) != 1 {
		t.Fatalf("load run history: runs=%+v err=%v", runs, err)
	}
	for _, forbidden := range []string{secret, "SELECT secret", "credentials", "message", "dsn"} {
		if strings.Contains(runs[0].EventsJSON, forbidden) {
			t.Fatalf("persisted agent history leaked %q: %s", forbidden, runs[0].EventsJSON)
		}
	}
	if !strings.Contains(runs[0].EventsJSON, "deepseek.v3.2") {
		t.Fatal("allowlisted model metadata was not retained")
	}
}
