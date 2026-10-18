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
	for _, want := range []string{"public.users", "id bigint PK", "USER MESSAGE", "What can you do?"} {
		if !strings.Contains(model.call.Prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, model.call.Prompt)
		}
	}
	if strings.Contains(model.call.Prompt, workspace.Host) {
		t.Fatal("database host leaked into model prompt")
	}
	if model.call.MaxOutputTokens == nil || *model.call.MaxOutputTokens != 1200 {
		t.Fatalf("max output tokens = %v, want 1200", model.call.MaxOutputTokens)
	}
}

func TestChatRequiresConnectedWorkspace(t *testing.T) {
	_, err := chatWithModel(context.Background(), &recordingChatModel{}, ChatRequest{Question: "hello"})
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("expected connection error, got %v", err)
	}
}
