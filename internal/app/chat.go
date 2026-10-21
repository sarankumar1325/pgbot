package app

import (
	"context"
	"fmt"
	"strings"
	"time"

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
	Evidence  []ChatEvidence
}

type ChatResponse struct {
	Text     string
	Model    string
	Provider string
	Evidence []ChatEvidence
}

// ChatEvidence is a bounded, credential-free observation supplied to the
// assistant. Evidence IDs are stable within one answer and are the only source
// the model may cite for claims about a connected database.
type ChatEvidence struct {
	ID         string
	Kind       string
	Source     string
	ObservedAt time.Time
	Summary    string
	Content    string
}

// Chat answers a conversational TUI prompt using the configured model. The
// model reasons about evidence and changes; deterministic application tools
// perform database reads and guarded writes on its behalf.
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
	system := `You are QueryShift, an AI assistant for databases and enterprise data work.
Lead with the direct answer. Be concise, practical, and honest about what has or has not executed.
For ordinary questions, answer in 2–6 short lines. Do not turn a simple answer into documentation.
Do not repeat connection metadata, database versions, table counts, model details, or safety policy unless
they directly affect the answer. Prefer one next step over a menu of every possible action.
Chat works with or without a connected database. When no database is connected, help with concepts,
architecture, SQL discussion, troubleshooting, and planning, but never imply access to a live schema.
When a database is connected, you receive schema structure, never credentials or table values.
QueryShift can both read and modify connected databases. It can draft SQL, create an immutable change
proposal and versioned migration, rehearse it, collect approval when policy requires it, and execute it
after the user confirms the exact guarded change. Never describe QueryShift itself as read-only or claim
that it cannot edit data or schema. Read-only is the default access state, not the product's capability.
Do not invent tables, columns, query results, performance findings, or completed actions.
Every factual claim about the connected database must be supported by the supplied EVIDENCE and cite
one or more evidence IDs exactly as [E1], [E2], and so on. Never cite an ID that was not supplied.
Treat collection errors as unavailable evidence, not proof that the database is healthy or unhealthy.
QueryShift automatically selects and invokes eligible read-only tools for natural-language requests.
Slash commands are optional shortcuts, not a permission boundary. Never tell a connected user to type or
click /inspect, /schema, or /erd when their request can be handled by the corresponding read-only tool.
Never claim those tools are user-initiated only or that you cannot invoke them. If their evidence is supplied,
answer from it directly. If collection failed, explain the failure and one actionable recovery step without
repeating the same request. If evidence is insufficient or stale, say exactly what remains unknown.
For requested writes, help define the exact change and guide it into the guarded change workflow. State
confidently that QueryShift can execute it after the required review and confirmation. Never claim a change
has already been applied unless an execution receipt is supplied. Writes require an immutable change
contract, versioned migration, WRITE unlock, policy checks, and explicit confirmation.

Optional command shortcuts:
/schema — analyze live schema in chat
/erd — analyze relationships in chat
/inspect — collect health evidence in chat
/query — run read-only SQL or create a guarded write proposal
/modernize — translate and optimize legacy SQL
/rehearse — test the current change against a disposable environment
/approve — record policy-required approval for the current change
/apply — execute the exact reviewed and confirmed change
/status — show connection and safety state

When the user asks what you can do, name at most three outcome categories and give at most two example prompts.
Explain the write boundary in one sentence. Do not reproduce the command list unless explicitly asked for it.
When they ask for a database action without live context, explain that connection is needed for that action,
recommend the exact slash command, and continue helping with anything that can be answered generally.`

	var prompt strings.Builder
	if request.Workspace == nil {
		prompt.WriteString("DATABASE CONTEXT\nNo database is connected. General chat is available; live schema inspection and execution are unavailable.\n")
	} else {
		prompt.WriteString("CONNECTED DATABASE\n")
		fmt.Fprintf(&prompt, "Database: %s\nPostgreSQL: %s\nProvider: %s\n", request.Workspace.Database, request.Workspace.Version, request.Workspace.Provider)
	}
	evidence := normalizeChatEvidence(request.Workspace, request.Evidence)
	if len(request.Evidence) > 0 {
		prompt.WriteString("\nTOOL EXECUTION\n")
		prompt.WriteString("QueryShift has already executed the selected read-only tools for this turn. Use the resulting evidence below to answer now. Do not ask the user to run a slash command.\n")
	}
	if len(evidence) > 0 {
		prompt.WriteString("\nEVIDENCE\n")
		for _, item := range evidence {
			fmt.Fprintf(&prompt, "[%s] kind=%s source=%s", item.ID, item.Kind, item.Source)
			if !item.ObservedAt.IsZero() {
				fmt.Fprintf(&prompt, " observed_at=%s", item.ObservedAt.UTC().Format(time.RFC3339))
			}
			prompt.WriteString("\n")
			if item.Summary != "" {
				prompt.WriteString("Summary: " + item.Summary + "\n")
			}
			prompt.WriteString(item.Content + "\n\n")
		}
	}
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
	maxOutputTokens := int64(600)
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
	return &ChatResponse{Text: text, Model: model.Model(), Provider: model.Provider(), Evidence: evidence}, nil
}

func normalizeChatEvidence(workspace *DatabaseWorkspace, supplied []ChatEvidence) []ChatEvidence {
	evidence := make([]ChatEvidence, 0, len(supplied)+1)
	if workspace != nil {
		evidence = append(evidence, ChatEvidence{
			Kind:       "schema_catalog",
			Source:     "queryshift.workspace",
			ObservedAt: time.Now(),
			Summary:    fmt.Sprintf("Live catalog snapshot for %s with %d tables", workspace.Database, len(workspace.Schema.Tables)),
			Content:    schemaContext(workspace),
		})
	}
	for _, item := range supplied {
		item.Kind = strings.TrimSpace(item.Kind)
		item.Source = strings.TrimSpace(item.Source)
		item.Summary = strings.TrimSpace(item.Summary)
		item.Content = strings.TrimSpace(item.Content)
		if item.Kind == "" || item.Source == "" || item.Content == "" {
			continue
		}
		if len([]rune(item.Content)) > 16_000 {
			item.Content = string([]rune(item.Content)[:16_000]) + "\n… evidence truncated"
		}
		evidence = append(evidence, item)
	}
	for index := range evidence {
		evidence[index].ID = fmt.Sprintf("E%d", index+1)
	}
	return evidence
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
