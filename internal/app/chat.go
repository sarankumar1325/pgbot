package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/sarankumar1325/queryshift/internal/ai"
)

// ChatTurn is one compact conversation turn supplied to the database assistant.
// Connection strings and table values are never part of this history.
type ChatTurn struct {
	Role string
	Text string
}

type ChatRequest struct {
	Question  string
	Workspace *DatabaseWorkspace
	History   []ChatTurn
}

type ChatResponse struct {
	Text     string
	Model    string
	Provider string
}

// Chat answers a conversational TUI prompt using the configured model. The
// model sees schema structure only; it cannot query or mutate the database.
func Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error) {
	model, err := ai.Resolve()
	if err != nil {
		return nil, err
	}
	return chatWithModel(ctx, model, request)
}

func chatWithModel(ctx context.Context, model ai.LanguageModel, request ChatRequest) (*ChatResponse, error) {
	question := strings.TrimSpace(request.Question)
	if question == "" {
		return nil, fmt.Errorf("question is required")
	}
	if request.Workspace == nil {
		return nil, fmt.Errorf("connect to a database before starting the agent")
	}

	system := `You are QueryShift, an agentic PostgreSQL database workbench.
Be concise, practical, and honest about what has or has not executed.
You receive schema structure, never credentials or table values.
Do not invent tables, columns, query results, performance findings, or completed actions.
Never claim a database change was applied. QueryShift changes require the guarded SQL workspace,
a versioned migration, WRITE unlock, and explicit confirmation.

Available UI commands:
/schema — browse live schema
/erd — open relationships
/inspect — collect deterministic health evidence
/query — open the guarded SQL workspace
/modernize — translate and optimize legacy SQL against the live schema
/status — show connection and safety state

When the user asks what you can do, explain these capabilities naturally.
When they ask for a database action, recommend the exact slash command and explain the next safe step.`

	var prompt strings.Builder
	prompt.WriteString("CONNECTED DATABASE\n")
	prompt.WriteString(schemaContext(request.Workspace))
	if len(request.History) > 0 {
		prompt.WriteString("\n\nRECENT CONVERSATION\n")
		history := request.History
		if len(history) > 8 {
			history = history[len(history)-8:]
		}
		for _, turn := range history {
			role := strings.ToUpper(strings.TrimSpace(turn.Role))
			if role != "YOU" && role != "QUERYSHIFT" {
				continue
			}
			text := strings.TrimSpace(turn.Text)
			if text == "" {
				continue
			}
			if len([]rune(text)) > 1600 {
				text = string([]rune(text)[:1600]) + "…"
			}
			fmt.Fprintf(&prompt, "%s: %s\n", role, text)
		}
	}
	prompt.WriteString("\nUSER MESSAGE\n")
	prompt.WriteString(question)

	temperature := 0.2
	maxOutputTokens := int64(1200)
	result, err := model.Generate(ctx, ai.Call{
		System: system, Prompt: prompt.String(),
		Temperature: &temperature, MaxOutputTokens: &maxOutputTokens,
	})
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(result.Text)
	if text == "" {
		return nil, fmt.Errorf("model returned an empty response")
	}
	return &ChatResponse{Text: text, Model: model.Model(), Provider: model.Provider()}, nil
}

func schemaContext(workspace *DatabaseWorkspace) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Database: %s\nPostgreSQL: %s\nProvider: %s\n", workspace.Database, workspace.Version, workspace.Provider)
	b.WriteString("Schema (structure only):\n")
	tables := workspace.Schema.Tables
	if len(tables) > 60 {
		tables = tables[:60]
	}
	for _, table := range tables {
		fmt.Fprintf(&b, "- %s.%s(", table.Schema, table.Name)
		columns := table.Columns
		if len(columns) > 20 {
			columns = columns[:20]
		}
		for index, column := range columns {
			if index > 0 {
				b.WriteString(", ")
			}
			b.WriteString(column.Name + " " + column.Type)
			if column.PK {
				b.WriteString(" PK")
			}
			if column.FKTarget != "" {
				b.WriteString(" FK→" + column.FKTarget)
			}
		}
		if len(table.Columns) > len(columns) {
			fmt.Fprintf(&b, ", … %d more", len(table.Columns)-len(columns))
		}
		b.WriteString(")\n")
		if b.Len() > 14_000 {
			b.WriteString("… schema summary truncated\n")
			break
		}
	}
	if len(workspace.Schema.Tables) > len(tables) {
		fmt.Fprintf(&b, "… %d more tables\n", len(workspace.Schema.Tables)-len(tables))
	}
	return b.String()
}
