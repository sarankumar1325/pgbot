package store

import (
	"strings"
	"testing"
	"time"
)

func TestAgentRunHistoryStoresOnlySanitizedMetadata(t *testing.T) {
	st := tempStore(t)
	score := 91
	now := time.Now().UTC().Truncate(time.Second)
	run := AgentRun{
		RunID: "run-1", StartedAt: now.Add(-time.Second), CompletedAt: now,
		SourceDialect: "oracle", TargetDialect: "postgres", ModelID: "deepseek.v3.2",
		LiveSchema: true, Status: "completed", ConfidenceScore: &score,
		RiskLevel: "low", EventsJSON: `[{"kind":"stage_completed","stage":"mapper"}]`,
	}
	if err := st.SaveAgentRun(run); err != nil {
		t.Fatal(err)
	}

	runs, err := st.RecentAgentRuns(5)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].RunID != "run-1" || *runs[0].ConfidenceScore != 91 {
		t.Fatalf("unexpected run history: %+v", runs)
	}
	for _, forbidden := range []string{"postgresql://", "SELECT ", "password", "prompt"} {
		if strings.Contains(runs[0].EventsJSON, forbidden) {
			t.Fatalf("agent history contains forbidden content %q", forbidden)
		}
	}
}
