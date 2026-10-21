package app

import (
	"strings"
	"testing"

	"github.com/sarankumar1325/queryshift/internal/erd"
	"github.com/sarankumar1325/queryshift/internal/model"
)

func TestPlanChatToolsOnlyCollectsHealthEvidenceWhenRelevantAndConnected(t *testing.T) {
	workspace := &DatabaseWorkspace{Database: "app"}
	for _, question := range []string{
		"Why is the database slow?",
		"Run a database health check",
		"Tell me about the health of the database",
		"Are there any missing indexes?",
		"Do we have replication lag?",
	} {
		tools := PlanChatTools(question, workspace)
		if len(tools) != 1 || tools[0] != ChatToolHealthInspection {
			t.Fatalf("question %q planned tools %v", question, tools)
		}
	}
	if tools := PlanChatTools("Explain B-tree indexes", workspace); len(tools) != 0 {
		t.Fatalf("general discussion should not collect live evidence: %v", tools)
	}
	if tools := PlanChatTools("Why is the database slow?", nil); len(tools) != 0 {
		t.Fatalf("disconnected chat should not plan database tools: %v", tools)
	}
}

func TestPlanChatToolsWithHistoryCarriesHealthIntentAcrossConfirmation(t *testing.T) {
	workspace := &DatabaseWorkspace{Database: "app"}
	history := []ChatTurn{
		{Role: "you", Text: "tell me about the health of the database"},
		{Role: "queryshift", Text: "I need to collect health evidence."},
	}
	for _, confirmation := range []string{"do it yourself", "do it yourseld", "yess", "go ahead"} {
		tools := PlanChatToolsWithHistory(confirmation, history, workspace)
		if len(tools) != 1 || tools[0] != ChatToolHealthInspection {
			t.Fatalf("confirmation %q planned tools %v", confirmation, tools)
		}
	}
	if tools := PlanChatToolsWithHistory("yes", nil, workspace); len(tools) != 0 {
		t.Fatalf("an ungrounded confirmation should not invoke a tool: %v", tools)
	}
}

func TestInspectionChatEvidenceSummarizesDeterministicFindings(t *testing.T) {
	inspection := &Inspection{
		Context: &model.Context{Findings: []model.Finding{
			{ID: "blocking", Suppressed: false},
			{ID: "ignored", Suppressed: true},
		}},
		Report: "HIGH  blocking sessions observed",
	}
	evidence := InspectionChatEvidence(inspection)
	if evidence.Kind != string(ChatToolHealthInspection) || evidence.Source != "queryshift.inspect" {
		t.Fatalf("unexpected evidence identity: %+v", evidence)
	}
	if evidence.Summary != "Deterministic read-only database health inspection with 1 active findings" {
		t.Fatalf("unexpected evidence summary %q", evidence.Summary)
	}
	if evidence.Content != inspection.Report || evidence.ObservedAt.IsZero() {
		t.Fatalf("inspection was not converted completely: %+v", evidence)
	}
}

func TestPlanChatToolsSelectsSchemaEvidenceWithoutWakingDatabaseHealth(t *testing.T) {
	workspace := chatToolWorkspace()
	tests := []struct {
		question string
		want     ChatToolName
	}{
		{"Describe table users", ChatToolSchemaSearch},
		{"What depends on customers?", ChatToolSchemaDependencies},
		{"Show me the columns in @orders", ChatToolSchemaSearch},
	}
	for _, test := range tests {
		tools := PlanChatTools(test.question, workspace)
		if len(tools) != 1 || tools[0] != test.want {
			t.Fatalf("question %q planned tools %v, want %s", test.question, tools, test.want)
		}
	}
}

func TestWorkspaceChatEvidenceSearchesSchemaAndDependencies(t *testing.T) {
	workspace := chatToolWorkspace()
	search, err := WorkspaceChatEvidence(ChatToolSchemaSearch, "describe table users", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if search.Kind != string(ChatToolSchemaSearch) || !strings.Contains(search.Content, "public.users") || strings.Contains(search.Content, "public.orders") {
		t.Fatalf("unexpected focused schema evidence: %+v", search)
	}
	dependencies, err := WorkspaceChatEvidence(ChatToolSchemaDependencies, "what depends on users?", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if dependencies.Kind != string(ChatToolSchemaDependencies) || !strings.Contains(dependencies.Content, "orders.user_id → users.id") {
		t.Fatalf("unexpected dependency evidence: %+v", dependencies)
	}
}

func TestChatToolRegistryDeclaresPermissionAndActivity(t *testing.T) {
	for _, name := range []ChatToolName{ChatToolHealthInspection, ChatToolSchemaSearch, ChatToolSchemaDependencies} {
		spec, ok := ChatToolDefinition(name)
		if !ok || spec.Source == "" || spec.Activity == "" || spec.Permission == "" {
			t.Fatalf("incomplete tool definition for %q: %+v", name, spec)
		}
	}
}

func chatToolWorkspace() *DatabaseWorkspace {
	return &DatabaseWorkspace{Database: "app", Schema: erd.Schema{
		Tables: []erd.Table{
			{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}, {Name: "email", Type: "text"}}},
			{Schema: "public", Name: "orders", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}, {Name: "user_id", Type: "bigint", FKTarget: "users.id"}}},
		},
		Edges: []erd.Edge{{FromTable: "orders", FromColumn: "user_id", ToTable: "users", ToColumn: "id"}},
	}}
}
