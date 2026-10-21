package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sarankumar1325/queryshift/internal/ai"
	"github.com/sarankumar1325/queryshift/internal/erd"
)

type recordingChatModel struct {
	call ai.Call
}

func (m *recordingChatModel) Generate(_ context.Context, call ai.Call) (*ai.Response, error) {
	m.call = call
	return &ai.Response{Text: "I can inspect health, browse the schema, and help with guarded changes."}, nil
}
func (*recordingChatModel) Provider() string { return "bedrock" }
func (*recordingChatModel) Model() string    { return "deepseek.v3.2" }
func (*recordingChatModel) Endpoint() string { return "https://bedrock.example.test" }

func TestChatUsesConnectedSchemaAndBoundedOutput(t *testing.T) {
	model := &recordingChatModel{}
	workspace := &DatabaseWorkspace{
		Host: "secret-host.example.test", Database: "app", Version: "PostgreSQL 18.1", Provider: "neon",
		Schema: erd.Schema{Tables: []erd.Table{{
			Schema: "public", Name: "users",
			Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}},
		}}},
	}
	response, err := chatWithModel(context.Background(), model, ChatRequest{
		Question: "What can you do?", Workspace: workspace,
		History: []ChatTurn{{Role: "you", Text: "hello"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Provider != "bedrock" || response.Model != "deepseek.v3.2" {
		t.Fatalf("unexpected response metadata: %+v", response)
	}
	for _, want := range []string{"[E1] kind=schema_catalog", "public.users", "id bigint PK", "USER MESSAGE", "What can you do?"} {
		if !strings.Contains(model.call.Prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, model.call.Prompt)
		}
	}
	if len(response.Evidence) != 1 || response.Evidence[0].ID != "E1" || response.Evidence[0].Kind != "schema_catalog" {
		t.Fatalf("unexpected response evidence: %+v", response.Evidence)
	}
	if !strings.Contains(model.call.System, "must be supported by the supplied EVIDENCE") {
		t.Fatalf("system prompt does not require evidence citations:\n%s", model.call.System)
	}
	if strings.Contains(model.call.Prompt, workspace.Host) {
		t.Fatal("database host leaked into model prompt")
	}
	if model.call.MaxOutputTokens == nil || *model.call.MaxOutputTokens != 600 {
		t.Fatalf("max output tokens = %v, want 600", model.call.MaxOutputTokens)
	}
	for _, want := range []string{"answer in 2–6 short lines", "at most three outcome categories", "Do not repeat connection metadata"} {
		if !strings.Contains(model.call.System, want) {
			t.Fatalf("concise response contract missing %q:\n%s", want, model.call.System)
		}
	}
	for _, want := range []string{
		"automatically selects and invokes eligible read-only tools",
		"Slash commands are optional shortcuts",
		"Never tell a connected user to type or",
		"Never claim those tools are user-initiated only",
	} {
		if !strings.Contains(model.call.System, want) {
			t.Fatalf("automatic tool contract missing %q:\n%s", want, model.call.System)
		}
	}
	for _, want := range []string{
		"QueryShift can both read and modify connected databases",
		"Never describe QueryShift itself as read-only",
		"QueryShift can execute it after the required review and confirmation",
		"unless an execution receipt is supplied",
		"/apply — execute the exact reviewed and confirmed change",
	} {
		if !strings.Contains(model.call.System, want) {
			t.Fatalf("write capability contract missing %q:\n%s", want, model.call.System)
		}
	}
}

func TestChatIncludesSuppliedOperationalEvidence(t *testing.T) {
	model := &recordingChatModel{}
	workspace := &DatabaseWorkspace{Database: "app", Version: "PostgreSQL 18.1", Provider: "neon"}
	response, err := chatWithModel(context.Background(), model, ChatRequest{
		Question:  "Why is the database slow?",
		Workspace: workspace,
		Evidence: []ChatEvidence{{
			Kind: "health_inspection", Source: "queryshift.inspect",
			Summary: "One active finding", Content: "HIGH: blocking sessions observed",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"QueryShift has already executed the selected read-only tools for this turn",
		"Do not ask the user to run a slash command",
		"[E1] kind=schema_catalog",
		"[E2] kind=health_inspection",
		"blocking sessions observed",
	} {
		if !strings.Contains(model.call.Prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, model.call.Prompt)
		}
	}
	if len(response.Evidence) != 2 || response.Evidence[1].ID != "E2" {
		t.Fatalf("operational evidence was not preserved: %+v", response.Evidence)
	}
}

func TestChatWorksWithoutConnectedWorkspace(t *testing.T) {
	model := &recordingChatModel{}
	response, err := chatWithModel(context.Background(), model, ChatRequest{Question: "How should I index a multi-tenant table?"})
	if err != nil {
		t.Fatal(err)
	}
	if response.Text == "" {
		t.Fatal("expected a model response without a connected database")
	}
	for _, want := range []string{"No database is connected", "General chat is available", "How should I index a multi-tenant table?"} {
		if !strings.Contains(model.call.Prompt, want) {
			t.Fatalf("disconnected prompt missing %q:\n%s", want, model.call.Prompt)
		}
	}
	if !strings.Contains(model.call.System, "Chat works with or without a connected database") {
		t.Fatalf("system prompt does not define disconnected chat behavior:\n%s", model.call.System)
	}
}
