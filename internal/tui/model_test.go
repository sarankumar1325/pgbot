package tui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/sarankumar1325/queryshift/internal/erd"
)

func testModel() Model {
	m := NewModel(context.Background(), Dependencies{
		Inspect: func(context.Context, string, int) (*app.Inspection, error) {
			return &app.Inspection{Report: "connected · test\nHEALTH ok"}, nil
		},
		Modernize: func(context.Context, app.ModernizeRequest) (*app.ModernizationResult, error) {
			return &app.ModernizationResult{}, nil
		},
		LoadDatabase: func(context.Context, string) (*app.DatabaseWorkspace, error) {
			return &app.DatabaseWorkspace{Host: "example.test", Database: "app", Version: "PostgreSQL 18.1", Provider: "neon"}, nil
		},
		Query: func(context.Context, string, string) (*app.QueryResult, error) {
			return &app.QueryResult{Columns: []string{"id"}, Rows: [][]string{{"1"}}}, nil
		},
		Apply: func(context.Context, string, app.ChangeContract, string) (*app.ChangeResult, error) {
			return &app.ChangeResult{Command: "UPDATE 1", RowsAffected: 1}, nil
		},
		Chat: func(context.Context, app.ChatRequest) (*app.ChatResponse, error) {
			return &app.ChatResponse{Text: "I can inspect health, browse schema, and plan guarded changes.", Model: "deepseek.v3.2", Provider: "bedrock"}, nil
		},
	})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	ready := updated.(Model)
	ready.booting = false
	return ready
}

func TestModernizeSlashRunsInline(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("/modernize SELECT TOP 10 * FROM dbo.orders")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)
	if got.screen != screenHome || !got.modernizeInline || !got.chatBusy || cmd == nil {
		t.Fatalf("modernize should run in chat, screen=%d inline=%v busy=%v", got.screen, got.modernizeInline, got.chatBusy)
	}
}

func TestHomeIsOneChatComposerWithoutWorkflowCards(t *testing.T) {
	m := testModel()
	view := m.homeView(100, 28)

	for _, unwanted := range []string{"Database work, with a safety boundary.", "chat works without live context", "CHAT READY", "Ask anything. Connect only", "bedrock/", "READ MODE"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("empty home still contains noisy default copy %q:\n%s", unwanted, view)
		}
	}
	if m.chat.Placeholder != "Ask about your database, paste SQL, or type / for commands…" {
		t.Fatalf("unexpected chat placeholder %q", m.chat.Placeholder)
	}
	for _, unwanted := range []string{"SELECTED", "WORKFLOWS", "SESSION"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("chat home still contains dashboard UI %q:\n%s", unwanted, view)
		}
	}
}

func TestPastedSQLRunsModernizerInChat(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("SELECT TOP 10 * FROM dbo.orders")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)

	if got.screen != screenHome || !got.modernizeInline || !got.chatBusy || cmd == nil {
		t.Fatalf("pasted SQL did not start inline modernizer: screen=%d inline=%v busy=%v", got.screen, got.modernizeInline, got.chatBusy)
	}
}

func TestNaturalLanguageStartsAgentAndRendersResponse(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("hi there what can you do")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	running := updated.(Model)
	if !running.chatBusy || cmd == nil {
		t.Fatalf("natural language should start the agent immediately: busy=%v cmd=%v", running.chatBusy, cmd)
	}
	if view := running.homeView(100, 28); !strings.Contains(view, "Preparing answer") || !strings.Contains(view, "interrupt") {
		t.Fatalf("contextual running state is not visible:\n%s", view)
	} else {
		plain := plainTerminalText(view)
		queryAt := strings.Index(plain, "hi there what can you do")
		activityAt := strings.Index(plain, "Preparing answer")
		if queryAt < 0 || activityAt <= queryAt {
			t.Fatalf("loading activity must render directly after the submitted query:\n%s", plain)
		}
	}

	request := app.ChatRequest{Question: "hi there what can you do", Workspace: running.workspace}
	message := running.chatCmd(context.Background(), running.jobID, request)()
	updated, _ = running.Update(message)
	finished := updated.(Model)
	if finished.chatBusy || len(finished.chatLog) == 0 {
		t.Fatalf("agent did not complete: busy=%v log=%+v", finished.chatBusy, finished.chatLog)
	}
	last := finished.chatLog[len(finished.chatLog)-1]
	if !strings.Contains(last.text, "inspect health") || last.meta != "bedrock · deepseek.v3.2" {
		t.Fatalf("assistant response missing: %+v", last)
	}
	view := finished.homeView(100, 28)
	if strings.Contains(view, "bedrock") || strings.Contains(view, "deepseek") || strings.Contains(view, "YOU") {
		t.Fatalf("normal transcript leaked implementation metadata or heavy labels:\n%s", view)
	}
}

func TestNaturalLanguageStartsWithoutDatabaseConnection(t *testing.T) {
	m := testModel()
	m.chat.SetValue("what can you do")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	running := updated.(Model)
	if running.screen != screenHome || !running.chatBusy || cmd == nil {
		t.Fatalf("disconnected chat should start immediately: screen=%d busy=%v cmd=%v", running.screen, running.chatBusy, cmd)
	}
	if running.workspace != nil {
		t.Fatal("disconnected chat should not fabricate a database workspace")
	}
}

func TestHealthQuestionCollectsReadOnlyEvidenceBeforeChat(t *testing.T) {
	var received app.ChatRequest
	m := testModel()
	m.deps.Chat = func(_ context.Context, request app.ChatRequest) (*app.ChatResponse, error) {
		received = request
		return &app.ChatResponse{
			Text: "Blocking sessions are the likely cause [E2].", Model: "deepseek.v3.2", Provider: "bedrock",
			Evidence: []app.ChatEvidence{{ID: "E1", Kind: "schema_catalog"}, {ID: "E2", Kind: "health_inspection"}},
		}, nil
	}
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("Why is the database slow?")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	running := updated.(Model)
	request := app.ChatRequest{Question: "Why is the database slow?", Workspace: running.workspace}
	updates := running.chatAgentStream(context.Background(), request, app.PlanChatTools(request.Question, running.workspace))
	current := running
	for step := 0; step < 12 && current.chatBusy; step++ {
		message := current.chatProgressCmd(current.jobID, updates)()
		next, _ := current.Update(message)
		current = next.(Model)
	}
	finished := current
	if finished.chatBusy {
		t.Fatal("live agent event stream did not complete")
	}
	seen := map[string]bool{}
	for _, message := range finished.chatLog {
		seen[message.meta] = true
	}
	for _, meta := range []string{"tool result"} {
		if !seen[meta] {
			t.Fatalf("live agent log missing %q: %+v", meta, finished.chatLog)
		}
	}
	for _, meta := range []string{"plan", "context", "model", "tool call", "evidence collected"} {
		if seen[meta] {
			t.Fatalf("internal orchestration noise leaked into transcript as %q: %+v", meta, finished.chatLog)
		}
	}
	if len(received.Evidence) != 1 || received.Evidence[0].Kind != string(app.ChatToolHealthInspection) {
		t.Fatalf("chat did not receive collected health evidence: %+v", received.Evidence)
	}
	view := finished.homeView(120, 34)
	for _, want := range []string{"Inspected database health", "Blocking sessions are the likely cause [E2]."} {
		if !strings.Contains(view, want) {
			t.Fatalf("evidence-backed result missing %q:\n%s", want, view)
		}
	}
}

func TestHealthRequestAndConfirmationAutomaticallyInvokeInspector(t *testing.T) {
	for _, test := range []struct {
		name     string
		history  []chatMessage
		question string
	}{
		{
			name:     "direct health request",
			question: "tell me about the health of the database",
		},
		{
			name: "confirmation inherits request",
			history: []chatMessage{
				{role: "you", text: "tell me about the health of the database"},
				{role: "queryshift", text: "I need health evidence."},
			},
			question: "do it yourseld",
		},
		{
			name: "yes inherits request",
			history: []chatMessage{
				{role: "you", text: "tell me about the health of the database"},
				{role: "queryshift", text: "I need health evidence."},
				{role: "you", text: "do it yourseld"},
				{role: "queryshift", text: "Should I proceed?"},
			},
			question: "yess",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := testModel()
			m.connection = "postgresql://owner:secret@example.test/db"
			m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
			m.chatLog = append(m.chatLog, test.history...)
			m.chat.SetValue(test.question)

			updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			running := updated.(Model)
			if !running.chatBusy || !running.chatTooling || cmd == nil {
				t.Fatalf("health intent should invoke the inspector: busy=%v tooling=%v cmd=%v", running.chatBusy, running.chatTooling, cmd)
			}
		})
	}
}

func TestSchemaDependencyQuestionUsesSnapshotToolAndCompactReceipt(t *testing.T) {
	var received app.ChatRequest
	m := testModel()
	m.deps.Chat = func(_ context.Context, request app.ChatRequest) (*app.ChatResponse, error) {
		received = request
		return &app.ChatResponse{
			Text: "`orders.user_id` depends on `users.id` [E2].", Model: "deepseek.v3.2", Provider: "bedrock",
			Evidence: []app.ChatEvidence{{ID: "E1", Kind: "schema_catalog"}, {ID: "E2", Kind: string(app.ChatToolSchemaDependencies)}},
		}, nil
	}
	m.workspace = &app.DatabaseWorkspace{Database: "app", Schema: erd.Schema{
		Tables: []erd.Table{
			{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}}},
			{Schema: "public", Name: "orders", Columns: []erd.Column{{Name: "user_id", Type: "bigint", FKTarget: "users.id"}}},
		},
		Edges: []erd.Edge{{FromTable: "orders", FromColumn: "user_id", ToTable: "users", ToColumn: "id"}},
	}}
	m.chat.SetValue("What depends on users?")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	current := updated.(Model)
	updates := current.chatAgentStream(context.Background(), app.ChatRequest{Question: "What depends on users?", Workspace: current.workspace}, app.PlanChatTools("What depends on users?", current.workspace))
	for step := 0; step < 12 && current.chatBusy; step++ {
		message := current.chatProgressCmd(current.jobID, updates)()
		next, _ := current.Update(message)
		current = next.(Model)
	}
	if current.chatBusy {
		t.Fatal("schema dependency agent stream did not complete")
	}
	if len(received.Evidence) != 1 || received.Evidence[0].Kind != string(app.ChatToolSchemaDependencies) {
		t.Fatalf("dependency evidence was not supplied to chat: %+v", received.Evidence)
	}
	view := current.homeView(120, 32)
	for _, want := range []string{"Traced schema dependencies · 1 relationships", "orders.user_id", "users.id"} {
		if !strings.Contains(view, want) {
			t.Fatalf("dependency result missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Tracing schema dependencies") || strings.Contains(view, "tool call") {
		t.Fatalf("transcript leaked transient tool narration:\n%s", view)
	}
}

func TestDatabaseSlashCommandsStayInChat(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app", Schema: erd.Schema{Tables: []erd.Table{{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}}}}}}
	for _, command := range []string{"/schema users", "/erd users", "/inspect locks"} {
		candidate := m
		candidate.chat.SetValue(command)
		updated, cmd := candidate.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		got := updated.(Model)
		if got.screen != screenHome || !got.chatBusy || !got.chatTooling || cmd == nil {
			t.Fatalf("%s should run a tool in chat: screen=%d busy=%v tooling=%v", command, got.screen, got.chatBusy, got.chatTooling)
		}
	}
}

func TestQuerySlashReturnsReadsAndProposesWritesInChat(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("/query SELECT 1 AS id")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	read := updated.(Model)
	if read.screen != screenHome || !read.chatBusy || cmd == nil {
		t.Fatalf("read query should execute inline: screen=%d busy=%v", read.screen, read.chatBusy)
	}
	message := read.inlineQueryCmd(context.Background(), read.jobID, "SELECT 1 AS id")()
	updated, _ = read.Update(message)
	finished := updated.(Model)
	if finished.chatBusy || !strings.Contains(finished.chatLog[len(finished.chatLog)-1].text, "| id |") {
		t.Fatalf("read result was not rendered in chat: %+v", finished.chatLog)
	}

	m = testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("/query CREATE TABLE public.events(id bigint)")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	proposal := updated.(Model)
	if proposal.screen != screenHome || proposal.changeRun == nil || proposal.changeContract.ID == "" {
		t.Fatalf("write should create an inline proposal: screen=%d run=%+v", proposal.screen, proposal.changeRun)
	}
	if text := proposal.chatLog[len(proposal.chatLog)-1].text; !strings.Contains(text, "Created change proposal") || !strings.Contains(text, "/apply WRITE") {
		t.Fatalf("proposal guidance missing: %s", text)
	}
}

func TestEvidenceSlashRendersInline(t *testing.T) {
	m := testModel()
	m.chatEvidence = []app.ChatEvidence{{ID: "E1", Kind: "schema", Source: "queryshift.workspace", Summary: "users table", Content: "public.users(id bigint)"}}
	m.chat.SetValue("/evidence")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)
	if got.screen != screenHome || len(got.chatLog) < 2 || !strings.Contains(got.chatLog[len(got.chatLog)-1].text, "public.users") {
		t.Fatalf("evidence should render in chat: screen=%d log=%+v", got.screen, got.chatLog)
	}
}

func TestQuestionMarkTypesNormallyAndHelpUsesSlashCommand(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	typed := updated.(Model)
	if typed.helpOpen || typed.chat.Value() != "?" {
		t.Fatalf("question mark should type into chat, help=%v input=%q", typed.helpOpen, typed.chat.Value())
	}
	typed.chat.SetValue("/help")
	updated, _ = typed.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)
	if got.helpOpen || got.screen != screenHome || len(got.chatLog) < 2 || !strings.Contains(got.chatLog[len(got.chatLog)-1].text, "/schema") {
		t.Fatal("/help should render command guidance in chat")
	}
}

func TestPastedSQLCanBeDiscussedWithoutDatabaseConnection(t *testing.T) {
	m := testModel()
	m.chat.SetValue("SELECT id, email FROM users")

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	running := updated.(Model)
	if running.screen != screenHome || !running.chatBusy || cmd == nil {
		t.Fatalf("SQL should remain a chat prompt without live context: screen=%d busy=%v cmd=%v", running.screen, running.chatBusy, cmd)
	}
}

func TestBusyAgentAcceptsOneQueuedPrompt(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chatBusy = true
	m.chatSince = time.Now()
	m.chat.SetValue("show me the schema")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)
	if len(got.chatQueue) != 1 || got.chatQueue[0] != "show me the schema" || got.chat.Value() != "" {
		t.Fatalf("message was not queued cleanly: queue=%v input=%q", got.chatQueue, got.chat.Value())
	}
	if view := got.homeView(120, 32); !strings.Contains(view, "1 queued") {
		t.Fatalf("queue state is not visible:\n%s", view)
	}
}

func TestComposerSupportsOpenCodeStyleCommandPaletteAndMultilineDrafts(t *testing.T) {
	m := testModel()
	m.chat.SetValue("keep this draft")

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	palette := updated.(Model)
	if palette.chat.Value() != "/" {
		t.Fatalf("ctrl+p should open the command palette, input=%q", palette.chat.Value())
	}
	view := palette.homeView(120, 32)
	for _, want := range []string{"/connect", "/new", "Database", "Session"} {
		if !strings.Contains(view, want) {
			t.Fatalf("command palette missing %q:\n%s", want, view)
		}
	}
	updated, _ = palette.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	restored := updated.(Model)
	if restored.chat.Value() != "keep this draft" {
		t.Fatalf("closing the palette should restore the draft, input=%q", restored.chat.Value())
	}

	restored.chat.SetValue("first line")
	updated, _ = restored.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	multiline := updated.(Model)
	multiline.chat.InsertString("second line")
	if multiline.chat.Value() != "first line\nsecond line" {
		t.Fatalf("ctrl+j should insert a newline, input=%q", multiline.chat.Value())
	}
}

func TestNewCommandClearsConversationButKeepsDatabase(t *testing.T) {
	m := testModel()
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chatLog = []chatMessage{{role: "you", text: "old prompt"}, {role: "queryshift", text: "old answer"}}
	m.chatQueue = []string{"queued"}
	m.chatScroll = 8

	updated, _ := m.executeSlash("/new")
	got := updated.(Model)
	if len(got.chatLog) != 0 || len(got.chatQueue) != 0 || got.chatScroll != 0 {
		t.Fatalf("new session did not clear conversation state: log=%v queue=%v scroll=%d", got.chatLog, got.chatQueue, got.chatScroll)
	}
	if got.workspace == nil || got.workspace.Database != "app" {
		t.Fatal("new conversation should retain the verified database workspace")
	}
}

func TestTranscriptCanPageAwayFromLatest(t *testing.T) {
	m := testModel()
	for i := 0; i < 20; i++ {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: fmt.Sprintf("answer %02d\nline two", i)})
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	older := updated.(Model)
	if older.chatScroll == 0 || !strings.Contains(older.homeView(100, 28), "older messages") {
		t.Fatal("page up should move the transcript away from the latest response")
	}
	updated, _ = older.Update(tea.KeyPressMsg{Code: tea.KeyEnd})
	latest := updated.(Model)
	if latest.chatScroll != 0 {
		t.Fatalf("end should return to latest, scroll=%d", latest.chatScroll)
	}
}

func TestConnectedHomeUsesFullWidthWithoutRepeatingLongHostInComposer(t *testing.T) {
	m := testModel()
	m.workspace = &app.DatabaseWorkspace{
		Host:     "ep-delicate-unit-b447m4hv-pooler.c-6.us-east-2.aws.neon.tech",
		Database: "neondb",
	}
	view := m.homeView(146, 32)
	if strings.Contains(view, m.workspace.Host) {
		t.Fatalf("composer status should not repeat the long host:\n%s", view)
	}
	if strings.Contains(view, "CONNECTED") || strings.Contains(view, "READ MODE") || strings.Contains(view, "bedrock/") {
		t.Fatalf("composer repeats status or model metadata:\n%s", view)
	}
	if got := lipgloss.Width(view); got != 146 {
		t.Fatalf("home width = %d, want full available width 146", got)
	}
}

func TestComposerKeepsShortcutsInOneContextualFooter(t *testing.T) {
	m := testModel()
	view := m.homeView(120, 32)
	if got := strings.Count(view, "enter"); got != 1 {
		t.Fatalf("composer should show the send shortcut once, got %d occurrences:\n%s", got, view)
	}
	if !strings.Contains(view, "ctrl+p") || !strings.Contains(view, "palette") {
		t.Fatalf("composer should expose the command palette affordance:\n%s", view)
	}
}

func TestAnimatedScannerMovesBidirectionallyAtConstantWidth(t *testing.T) {
	forward := animatedScanner(1, 8)
	returning := animatedScanner(8, 8)
	if forward == returning {
		t.Fatal("scanner should move back after reaching the far edge")
	}
	if lipgloss.Width(forward) != 8 || lipgloss.Width(returning) != 8 {
		t.Fatalf("scanner width changed: forward=%d returning=%d", lipgloss.Width(forward), lipgloss.Width(returning))
	}
}

func TestAgentLoaderAnimatesAtConstantWidth(t *testing.T) {
	first := agentLoader(0)
	second := agentLoader(1)
	if first == second || lipgloss.Width(first) != lipgloss.Width(second) {
		t.Fatalf("agent loader should animate without changing width: %q %q", first, second)
	}
}

func TestSlashPaletteLocksDatabaseCommandsUntilConnected(t *testing.T) {
	m := testModel()
	m.chat.SetValue("/")
	view := m.homeView(100, 28)
	if !strings.Contains(view, "/schema") || !strings.Contains(view, "locked · connect first") {
		t.Fatalf("disconnected palette must expose locked database commands:\n%s", view)
	}

	m.chat.SetValue("/schema")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)
	if got.screen != screenConnect || got.pendingCmd != "/schema" {
		t.Fatalf("locked command should route through connection: screen=%d pending=%q", got.screen, got.pendingCmd)
	}
}

func TestPendingSlashCommandResumesAfterConnection(t *testing.T) {
	m := testModel()
	m.pendingCmd = "/erd"
	m.connection = "postgresql://owner:secret@example.test/db"
	m.jobID = 3
	updated, cmd := m.Update(databaseLoadedMsg{id: 3, result: &app.DatabaseWorkspace{
		Host: "example.test", Database: "app", Version: "PostgreSQL 18.1",
	}})
	got := updated.(Model)
	if got.screen != screenHome || !got.chatBusy || cmd == nil || got.pendingCmd != "" {
		t.Fatalf("pending command did not resume in chat: screen=%d busy=%v pending=%q", got.screen, got.chatBusy, got.pendingCmd)
	}
}

func TestBootAnimationIsBrandFirstNotAStatusTable(t *testing.T) {
	m := testModel()
	m.bootStep = 4
	view := m.bootView(80)

	if !strings.Contains(view, "QUERY") || !strings.Contains(view, "SHIFT") {
		t.Fatalf("boot animation is missing QueryShift wordmark:\n%s", view)
	}
	for _, unwanted := range []string{"Terminal workspace", "Live schema", "LOADING", "READY"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("boot animation still contains status-table row %q:\n%s", unwanted, view)
		}
	}
}

func TestConnectionSecretNeverAppearsInView(t *testing.T) {
	m := testModel()
	m.screen = screenConnect
	m.dsn.SetValue("postgresql://owner:super-secret@example.test/db")
	if view := m.View().Content; strings.Contains(view, "super-secret") || strings.Contains(view, "owner") {
		t.Fatalf("connection credential leaked into TUI view: %q", view)
	}
}

func TestBracketedPasteReachesConnectionInput(t *testing.T) {
	m := testModel()
	m.screen = screenConnect
	m.dsn.Focus()
	want := "postgresql://owner:secret@example.test/db?sslmode=require"
	updated, _ := m.Update(tea.PasteMsg{Content: want})
	got := updated.(Model)
	if got.dsn.Value() != want {
		t.Fatalf("paste was dropped: got %q", got.dsn.Value())
	}
	if !strings.Contains(got.connectView(100), "PASTE RECEIVED") {
		t.Fatal("connection view did not acknowledge the secure paste")
	}
}

func TestConnectionReturnsToChatWithDatabaseContext(t *testing.T) {
	m := testModel()
	m.screen = screenConnect
	m.dsn.SetValue("postgresql://owner:secret@example.test/db")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	running := updated.(Model)
	if running.screen != screenDatabaseLoading || cmd == nil {
		t.Fatalf("enter should load database, screen=%d cmd=%v", running.screen, cmd)
	}

	// The returned command is a batch. Drive the service command directly so
	// this unit test stays deterministic and does not depend on Bubble Tea's loop.
	message := running.loadDatabaseCmd(context.Background(), running.jobID, running.connection, false)()
	updated, _ = running.Update(message)
	finished := updated.(Model)
	if finished.screen != screenHome || finished.workspace == nil || finished.workspace.Database != "app" || !finished.chat.Focused() {
		t.Fatalf("database workspace did not load: screen=%d workspace=%+v", finished.screen, finished.workspace)
	}
	if len(finished.chatLog) != 0 {
		t.Fatalf("successful connection should update header without polluting conversation: %+v", finished.chatLog)
	}
	if view := finished.View().Content; !strings.Contains(view, "app") || !strings.Contains(view, "read") {
		t.Fatalf("header should show compact connected database context:\n%s", view)
	}
}

func TestDatabaseChangeTabFramesWritesAsReviewedProposals(t *testing.T) {
	m := testModel()
	m.screen = screenDatabase
	m.dbTab = 3
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app", Version: "PostgreSQL 18.1", Provider: "neon"}
	m.dbSQL.SetValue("ALTER TABLE users ADD COLUMN timezone text")
	view := m.databaseView(120, 32)
	for _, want := range []string{"CHANGE PROPOSAL", "REVIEW REQUIRED", "4  Change"} {
		if !strings.Contains(view, want) {
			t.Fatalf("change workspace missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "SQL console") {
		t.Fatalf("change workspace still uses generic SQL-console framing:\n%s", view)
	}
}

func TestChatMarkdownRendersWithoutSyntaxNoise(t *testing.T) {
	rendered := renderChatMarkdown("**Writing is controlled.**\n\n- Review `SQL`\n- Approve it", 60)
	for _, unwanted := range []string{"**", "`"} {
		if strings.Contains(rendered, unwanted) {
			t.Fatalf("rendered chat contains raw Markdown marker %q: %q", unwanted, rendered)
		}
	}
	if !strings.Contains(rendered, "Writing is controlled.") || !strings.Contains(rendered, "• ") || !strings.Contains(rendered, "SQL") {
		t.Fatalf("rendered chat lost content: %q", rendered)
	}
}

func TestChatTurnsUseOneBlankLineAndNoUserBubble(t *testing.T) {
	m := testModel()
	m.chatLog = []chatMessage{
		{role: "you", text: "hello"},
		{role: "queryshift", text: "Hi."},
		{role: "you", text: "Can you write?"},
		{role: "queryshift", text: "Yes, after review and approval."},
	}
	view := m.homeView(120, 32)
	plain := plainTerminalText(view)
	if strings.Contains(plain, "YOU") || strings.Contains(plain, "bedrock") || strings.Contains(plain, "deepseek") {
		t.Fatalf("compact transcript contains removed labels or model metadata:\n%s", view)
	}
	if !strings.Contains(plain, "› hello") || !strings.Contains(plain, "› Can you write?") {
		t.Fatalf("user turns do not use the compact prompt treatment:\n%s", view)
	}
	conversation := plain
	if composerAt := strings.LastIndex(conversation, "│ ›"); composerAt >= 0 {
		conversation = conversation[:composerAt]
	}
	conversation = strings.TrimSpace(conversation)
	if strings.Contains(conversation, "\n\n\n") {
		t.Fatalf("transcript contains more than one blank line between content:\n%s", view)
	}
}

func TestWideConversationKeepsOnlyUsefulInformation(t *testing.T) {
	m := testModel()
	m.width, m.height = 180, 48
	m.workspace = &app.DatabaseWorkspace{Database: "neondb", Host: "ep-delicate-unit-b447m4hv-pooler.c-6.us-east-2.aws.neon.tech"}
	m.chatLog = []chatMessage{
		{role: "you", text: "hello queryshift"},
		{role: "queryshift", meta: "bedrock · deepseek.v3.2", text: "Hello. I can inspect this database and help plan a safe change."},
		{role: "you", text: "can you write to the db?"},
		{role: "queryshift", meta: "bedrock · deepseek.v3.2", text: "**Yes.** Writes run only after you review the exact `SQL` and approve it."},
	}
	plain := plainTerminalText(m.View().Content)
	for _, unwanted := range []string{"bedrock", "deepseek", "YOU", "**", "`", m.workspace.Host, "Connected to"} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("wide conversation contains noisy detail %q:\n%s", unwanted, plain)
		}
	}
	for _, want := range []string{"neondb", "·  read", "› hello queryshift", "› can you write to the db?", "Writes run only after"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("wide conversation missing %q:\n%s", want, plain)
		}
	}
	nonEmpty := 0
	for _, line := range strings.Split(plain, "\n") {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	if nonEmpty > 16 {
		t.Fatalf("simple two-turn conversation uses %d non-empty rows, want <= 16:\n%s", nonEmpty, plain)
	}
}

var terminalEscapePattern = regexp.MustCompile(`\x1b\[[0-9;:]*[A-Za-z]`)

func plainTerminalText(value string) string {
	value = terminalEscapePattern.ReplaceAllString(value, "")
	lines := strings.Split(value, "\n")
	for index := range lines {
		lines[index] = strings.TrimRight(lines[index], " ")
	}
	return strings.Join(lines, "\n")
}

func TestWriteModeNeedsUnlockAndStatementConfirmation(t *testing.T) {
	m := testModel()
	m.screen = screenDatabase
	m.dbTab = 3
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.dbSQL.SetValue("UPDATE users SET active = false WHERE id = 1")

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	locked := updated.(Model)
	if locked.err == nil || locked.screen != screenDatabase {
		t.Fatal("write should stay locked before explicit unlock")
	}

	updated, _ = locked.Update(tea.KeyPressMsg{Code: 'w', Mod: tea.ModCtrl})
	unlock := updated.(Model)
	unlock.confirm.SetValue("WRITE")
	updated, _ = unlock.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	armed := updated.(Model)
	if !armed.writeMode || armed.screen != screenDatabase {
		t.Fatal("WRITE token should arm write mode")
	}

	updated, _ = armed.Update(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	preview := updated.(Model)
	if preview.screen != screenChangeConfirm || preview.changePlan.RequiredConfirmation != "BACKUP AND APPLY" {
		t.Fatalf("write should open a change preview: screen=%d plan=%+v", preview.screen, preview.changePlan)
	}
	if preview.changeContract.ID == "" || preview.changeContract.Statement != "UPDATE users SET active = false WHERE id = 1" {
		t.Fatalf("write preview did not bind an immutable contract: %+v", preview.changeContract)
	}
	view := preview.changeConfirmView(120)
	for _, want := range []string{"IMMUTABLE CHANGE CONTRACT", preview.changeContract.ID, "REQUIRED VERIFICATION"} {
		if !strings.Contains(view, want) {
			t.Fatalf("change preview missing %q:\n%s", want, view)
		}
	}
}

func TestCancelledJobCompletionIsIgnored(t *testing.T) {
	m := testModel()
	m.screen = screenInspecting
	m.jobID = 4
	m.jobID++
	updated, _ := m.Update(inspectFinishedMsg{id: 4, err: context.Canceled})
	got := updated.(Model)
	if got.screen != screenInspecting || got.err != nil {
		t.Fatalf("stale completion changed state: screen=%d err=%v", got.screen, got.err)
	}
}

func TestModernizerProgressUpdatesLiveTrace(t *testing.T) {
	m := testModel()
	m.screen = screenModernizing
	m.jobID = 7
	updates := make(chan app.ModernizationUpdate, 1)
	updates <- app.ModernizationUpdate{Result: &app.ModernizationResult{RunID: "run-1"}}

	updated, next := m.Update(modernizeProgressMsg{
		id: 7,
		event: app.AgentRunEvent{
			RunID: "run-1", Sequence: 2, Kind: "stage_started", Stage: "mapper",
		},
		updates: updates,
	})
	got := updated.(Model)

	if len(got.agentRun) != 1 || got.agentRun[0].Stage != "mapper" {
		t.Fatalf("progress event was not retained: %+v", got.agentRun)
	}
	if next == nil {
		t.Fatal("progress event must schedule the next stream read")
	}
	if view := got.modernizingView(100); !strings.Contains(view, "mapper") || !strings.Contains(view, "RUNNING") {
		t.Fatalf("live trace did not render current stage:\n%s", view)
	}
}

func TestCompactHomeFitsMinimumSupportedTerminal(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	compact := updated.(Model)
	compact.booting = false
	view := compact.View().Content
	if got := lipgloss.Width(view); got > 80 {
		t.Fatalf("compact home is %d columns wide, want <= 80", got)
	}
	if got := lipgloss.Height(view); got > 24 {
		t.Fatalf("compact home is %d rows tall, want <= 24", got)
	}
}

func TestCompactSlashPaletteFitsMinimumSupportedTerminal(t *testing.T) {
	m := testModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	compact := updated.(Model)
	compact.booting = false
	compact.chat.SetValue("/")
	view := compact.View().Content
	if got := lipgloss.Width(view); got > 80 {
		t.Fatalf("compact command palette is %d columns wide, want <= 80", got)
	}
	if got := lipgloss.Height(view); got > 24 {
		t.Fatalf("compact command palette is %d rows tall, want <= 24\n%s", got, view)
	}
}

func TestBootAnimationAdvancesToHome(t *testing.T) {
	m := NewModel(context.Background(), Dependencies{})
	for step := 0; step < 12; step++ {
		updated, _ := m.Update(bootTickMsg{})
		m = updated.(Model)
	}
	updated, _ := m.Update(bootDoneMsg{})
	m = updated.(Model)
	if m.booting {
		t.Fatal("boot animation did not hand off to the home screen")
	}
}

func TestDatabaseWorkspaceFitsMinimumTerminal(t *testing.T) {
	m := testModel()
	m.screen = screenDatabase
	m.workspace = &app.DatabaseWorkspace{
		Host: "example.test", Database: "app", Version: "PostgreSQL 18.1", Provider: "neon",
		Schema: erd.Schema{Tables: []erd.Table{{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", Type: "bigint", PK: true}}}}},
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	compact := updated.(Model)
	for tab := 0; tab < 4; tab++ {
		compact.dbTab = tab
		compact.refreshDatabaseViewport()
		view := compact.View().Content
		if got := lipgloss.Width(view); got > 80 {
			t.Fatalf("compact database tab %d is %d columns wide, want <= 80\n%s", tab, got, view)
		}
		if got := lipgloss.Height(view); got > 24 {
			t.Fatalf("compact database tab %d is %d rows tall, want <= 24", tab, got)
		}
	}
}

func TestDatabaseLoadingStreamsProgressAndLogs(t *testing.T) {
	m := testModel()
	m.screen = screenConnect
	neonConn := "postgresql://alex:pwd@ep-cool-unit.c-6.us-east-2.aws.neon.tech/neondb?sslmode=require"
	m.dsn.SetValue(neonConn)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	loading := updated.(Model)
	if loading.screen != screenDatabaseLoading || cmd == nil {
		t.Fatalf("expected screenDatabaseLoading, got %d", loading.screen)
	}

	// Send streaming progress message
	updatesCh := make(chan app.DatabaseLoadUpdate, 2)
	updated, _ = loading.Update(databaseProgressMsg{
		id:      loading.jobID,
		refresh: false,
		stage:   "PROBING ENGINE",
		log:     "Querying server version, settings, and provider capabilities",
		updates: updatesCh,
	})
	progressModel := updated.(Model)
	if progressModel.loadStage != "PROBING ENGINE" {
		t.Fatalf("expected stage PROBING ENGINE, got %q", progressModel.loadStage)
	}

	// Verify rendering contains logs and serverless hint
	view := progressModel.View().Content
	for _, want := range []string{"LIVE CONNECTION LOGS", "PROBING ENGINE", "Querying server version", "Serverless database detected"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q:\n%s", want, view)
		}
	}

	// Send finished message
	ws := &app.DatabaseWorkspace{Host: "ep-cool-unit.c-6.us-east-2.aws.neon.tech", Database: "neondb"}
	updated, _ = progressModel.Update(databaseLoadedMsg{
		id:      loading.jobID,
		refresh: false,
		result:  ws,
	})
	doneModel := updated.(Model)
	if doneModel.screen != screenHome || doneModel.workspace == nil {
		t.Fatalf("expected chat home after connection, got %d", doneModel.screen)
	}
}

func TestDatabaseLoadingHasHardDeadline(t *testing.T) {
	deadlineSeen := make(chan time.Duration, 1)
	m := NewModel(context.Background(), Dependencies{
		LoadDatabaseStream: func(ctx context.Context, _ string) <-chan app.DatabaseLoadUpdate {
			remaining := time.Duration(0)
			if deadline, ok := ctx.Deadline(); ok {
				remaining = time.Until(deadline)
			}
			deadlineSeen <- remaining
			updates := make(chan app.DatabaseLoadUpdate, 1)
			updates <- app.DatabaseLoadUpdate{Err: context.DeadlineExceeded}
			close(updates)
			return updates
		},
	})
	m.booting = false
	m.screen = screenConnect
	m.dsn.SetValue("postgresql://queryshift@example.test/app")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	loading := updated.(Model)
	if loading.screen != screenDatabaseLoading {
		t.Fatalf("expected loading screen, got %d", loading.screen)
	}
	remaining := <-deadlineSeen
	if remaining <= 0 || remaining > databaseWorkspaceLoadTimeout {
		t.Fatalf("database load deadline = %s, want 0 < deadline <= %s", remaining, databaseWorkspaceLoadTimeout)
	}
}

func TestDatabaseLoadingWarnsWhenServerlessWakeupIsAbnormallySlow(t *testing.T) {
	m := testModel()
	m.screen = screenDatabaseLoading
	m.connection = "postgresql://queryshift@ep-cool-unit-pooler.neon.tech/app"
	m.loadHostHint = "ep-cool-unit-pooler.neon.tech · app"
	m.loadSince = time.Now().Add(-13 * time.Second)

	view := m.View().Content
	if !strings.Contains(view, "No response after the normal serverless wake-up window") ||
		!strings.Contains(view, "stop automatically at") || !strings.Contains(view, "30s.") {
		t.Fatalf("slow serverless state should explain the hard deadline:\n%s", view)
	}
}

func TestDatabaseLoadingWatchdogStopsStuckDriver(t *testing.T) {
	canceled := false
	m := testModel()
	m.screen = screenDatabaseLoading
	m.connection = "postgresql://queryshift@ep-cool-unit-pooler.neon.tech/app"
	m.loadSince = time.Now().Add(-databaseWorkspaceLoadTimeout - time.Second)
	m.loadStage = "Connecting"
	m.loadLogs = []string{"Dialing database endpoint…"}
	m.cancel = func() { canceled = true }
	m.jobID = 7

	updated, cmd := m.Update(pulseMsg(time.Now()))
	stopped := updated.(Model)
	if cmd != nil {
		t.Fatal("watchdog should stop scheduling loading pulses")
	}
	if !canceled {
		t.Fatal("watchdog did not cancel the stuck database operation")
	}
	if stopped.screen != screenConnect {
		t.Fatalf("watchdog screen = %d, want screenConnect", stopped.screen)
	}
	if stopped.jobID != 8 {
		t.Fatalf("watchdog job ID = %d, want late results invalidated", stopped.jobID)
	}
	if stopped.err == nil || !strings.Contains(stopped.err.Error(), "timed out after 30s") {
		t.Fatalf("watchdog error = %v, want actionable timeout", stopped.err)
	}
}

func TestDatabaseLoadingEscCancelsSafely(t *testing.T) {
	m := testModel()
	m.screen = screenConnect
	neonConn := "postgresql://alex:pwd@ep-cool-unit.c-6.us-east-2.aws.neon.tech/neondb?sslmode=require"
	m.dsn.SetValue(neonConn)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	loading := updated.(Model)
	if loading.screen != screenDatabaseLoading {
		t.Fatalf("expected screenDatabaseLoading, got %d", loading.screen)
	}

	// Press Esc while loading
	updated, _ = loading.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	canceled := updated.(Model)
	if canceled.screen != screenConnect {
		t.Fatalf("expected screenConnect after Esc, got %d", canceled.screen)
	}
	if canceled.dsn.Value() != neonConn {
		t.Fatalf("expected connection string preserved in dsn, got %q", canceled.dsn.Value())
	}
}

func TestChatTableMentionAutocomplete(t *testing.T) {
	m := testModel()
	m.workspace = &app.DatabaseWorkspace{
		Host:     "example.test",
		Database: "app",
		Schema: erd.Schema{
			Tables: []erd.Table{
				{Schema: "public", Name: "users", Columns: []erd.Column{{Name: "id", PK: true}, {Name: "email"}}},
				{Schema: "public", Name: "orders", Columns: []erd.Column{{Name: "id", PK: true}, {Name: "total"}}},
			},
		},
	}

	// Type @u in the chat composer
	m.chat.SetValue("tell me about @u")
	matches := m.atMatches()
	if len(matches) != 1 || matches[0].Name != "users" {
		t.Fatalf("expected 1 match for @u, got %v", matches)
	}

	view := m.homeView(120, 32)
	for _, want := range []string{"TABLE CONTEXT", "@users"} {
		if !strings.Contains(view, want) {
			t.Fatalf("atPalette view missing %q:\n%s", want, view)
		}
	}

	// Press Tab to complete @users
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	completed := updated.(Model)
	if completed.chat.Value() != "tell me about @users " {
		t.Fatalf("expected completed table reference, got %q", completed.chat.Value())
	}
}

func TestChatPromptHistoryNavigation(t *testing.T) {
	m := testModel()
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}

	// Submit prompt 1
	m.chat.SetValue("show database slow queries")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	// Submit prompt 2
	m.chat.SetValue("explain users table indexes")
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)

	// Currently empty input. Press Up to recall latest prompt
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(Model)
	if m.chat.Value() != "explain users table indexes" {
		t.Fatalf("expected prompt 2 on Up arrow, got %q", m.chat.Value())
	}

	// Press Up again to recall older prompt
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m = updated.(Model)
	if m.chat.Value() != "show database slow queries" {
		t.Fatalf("expected prompt 1 on second Up arrow, got %q", m.chat.Value())
	}

	// Press Down to return to prompt 2
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.chat.Value() != "explain users table indexes" {
		t.Fatalf("expected prompt 2 on Down arrow, got %q", m.chat.Value())
	}

	// Press Down again to return to empty draft
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(Model)
	if m.chat.Value() != "" {
		t.Fatalf("expected empty draft on Down past newest, got %q", m.chat.Value())
	}
}

func TestChatInlineQueryMode(t *testing.T) {
	m := testModel()
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.connection = "postgresql://owner:secret@example.test/db"

	// Type inline query with !
	m.chat.SetValue("!SELECT 1 AS num")
	view := m.homeView(120, 32)
	if !strings.Contains(view, "SQL RUNNER") {
		t.Fatalf("expected SQL RUNNER badge for ! prefix, view:\n%s", view)
	}

	// Submit inline query
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(Model)
	if cmd == nil || !m.chatBusy {
		t.Fatal("expected query execution cmd to be batched")
	}
}

func TestChatCtrlUAndCtrlCClearing(t *testing.T) {
	m := testModel()
	m.chat.SetValue("draft that should be cleared")

	// Ctrl+U clears draft
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	cleared := updated.(Model)
	if cleared.chat.Value() != "" {
		t.Fatalf("ctrl+u should clear draft, got %q", cleared.chat.Value())
	}

	// Ctrl+C with non-empty input clears draft without quitting
	cleared.chat.SetValue("another draft")
	updated, cmd := cleared.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	clearedAgain := updated.(Model)
	if cmd != nil || clearedAgain.chat.Value() != "" {
		t.Fatalf("ctrl+c on non-empty input should clear draft, cmd=%v value=%q", cmd, clearedAgain.chat.Value())
	}
}
