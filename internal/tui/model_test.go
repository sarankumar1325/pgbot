package tui

import (
	"context"
	"fmt"
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
		Apply: func(context.Context, string, string, string) (*app.ChangeResult, error) {
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

func TestHomeOpensModernizer(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("/modernize")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)
	if got.screen != screenModernizer || !got.sql.Focused() {
		t.Fatalf("enter should open focused modernizer, screen=%d focused=%v", got.screen, got.sql.Focused())
	}
}

func TestHomeIsOneChatComposerWithoutWorkflowCards(t *testing.T) {
	m := testModel()
	view := m.homeView(100, 28)

	for _, want := range []string{"Database work, with a safety boundary.", "DATABASE NOT CONNECTED"} {
		if !strings.Contains(view, want) {
			t.Fatalf("chat home is missing %q:\n%s", want, view)
		}
	}
	if m.chat.Placeholder != "Ask QueryShift, paste SQL, or type /connect…" {
		t.Fatalf("unexpected chat placeholder %q", m.chat.Placeholder)
	}
	for _, unwanted := range []string{"SELECTED", "WORKFLOWS", "SESSION"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("chat home still contains dashboard UI %q:\n%s", unwanted, view)
		}
	}
}

func TestPastedSQLRoutesFromChatToModernizer(t *testing.T) {
	m := testModel()
	m.connection = "postgresql://owner:secret@example.test/db"
	m.workspace = &app.DatabaseWorkspace{Host: "example.test", Database: "app"}
	m.chat.SetValue("SELECT TOP 10 * FROM dbo.orders")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(Model)

	if got.screen != screenModernizer || !strings.Contains(got.sql.Value(), "SELECT TOP 10") {
		t.Fatalf("pasted SQL did not route to modernizer: screen=%d sql=%q", got.screen, got.sql.Value())
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
	if view := running.homeView(100, 28); !strings.Contains(view, "AGENT RUNNING") {
		t.Fatalf("running state is not visible:\n%s", view)
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
}

func TestNaturalLanguageWaitsForConnectionThenResumes(t *testing.T) {
	m := testModel()
	m.chat.SetValue("what can you do")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	connecting := updated.(Model)
	if connecting.screen != screenConnect || connecting.pendingAsk != "what can you do" {
		t.Fatalf("question should wait for database: screen=%d pending=%q", connecting.screen, connecting.pendingAsk)
	}

	connecting.connection = "postgresql://owner:secret@example.test/db"
	connecting.jobID = 9
	updated, cmd := connecting.Update(databaseLoadedMsg{id: 9, result: &app.DatabaseWorkspace{Host: "example.test", Database: "app"}})
	running := updated.(Model)
	if !running.chatBusy || running.pendingAsk != "" || cmd == nil {
		t.Fatalf("pending question did not resume: busy=%v pending=%q cmd=%v", running.chatBusy, running.pendingAsk, cmd)
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
	if !strings.Contains(view, "CONNECTED") || !strings.Contains(view, "READ MODE") {
		t.Fatalf("connected status missing:\n%s", view)
	}
	if got := lipgloss.Width(view); got != 146 {
		t.Fatalf("home width = %d, want full available width 146", got)
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
	updated, _ := m.Update(databaseLoadedMsg{id: 3, result: &app.DatabaseWorkspace{
		Host: "example.test", Database: "app", Version: "PostgreSQL 18.1",
	}})
	got := updated.(Model)
	if got.screen != screenDatabase || got.dbTab != 2 || got.pendingCmd != "" {
		t.Fatalf("pending command did not resume: screen=%d tab=%d pending=%q", got.screen, got.dbTab, got.pendingCmd)
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

func TestConnectionLoadsDatabaseWorkspace(t *testing.T) {
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
	if finished.screen != screenDatabase || finished.workspace == nil || finished.workspace.Database != "app" {
		t.Fatalf("database workspace did not load: screen=%d workspace=%+v", finished.screen, finished.workspace)
	}
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
