// Package tui implements QueryShift's interactive full-screen terminal app.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/sarankumar1325/queryshift/internal/app"
)

type screen int

const (
	screenHome screen = iota
	screenConnect
	screenInspecting
	screenReport
	screenModernizer
	screenModernizing
	screenModernizerResult
	screenAbout
	screenDatabaseLoading
	screenDatabase
	screenWriteUnlock
	screenChangeConfirm
	screenApplying
	screenEvidence
	screenRehearsing
)

type Dependencies struct {
	Inspect            func(context.Context, string, int) (*app.Inspection, error)
	Modernize          func(context.Context, app.ModernizeRequest) (*app.ModernizationResult, error)
	ModernizeStream    func(context.Context, app.ModernizeRequest) <-chan app.ModernizationUpdate
	LoadDatabase       func(context.Context, string) (*app.DatabaseWorkspace, error)
	LoadDatabaseStream func(context.Context, string) <-chan app.DatabaseLoadUpdate
	Query              func(context.Context, string, string) (*app.QueryResult, error)
	Apply              func(context.Context, string, app.ChangeContract, string) (*app.ChangeResult, error)
	Rehearse           func(context.Context, string, app.ChangeContract, string) (*app.RehearsalReceipt, error)
	ApplyVerified      func(context.Context, string, *app.VerifiedChangeRun, string, []byte, time.Time) (*app.ChangeResult, error)
	Chat               func(context.Context, app.ChatRequest) (*app.ChatResponse, error)
}

type chatMessage struct {
	role string
	text string
	meta string
}

type slashCommand struct {
	Name        string
	Description string
	Category    string
	Shortcut    string
	RequiresDB  bool
}

var slashCommands = []slashCommand{
	{Name: "/connect", Description: "Connect or switch PostgreSQL database", Category: "Database"},
	{Name: "/schema", Description: "Explain live tables, columns, and indexes in chat", Category: "Database", RequiresDB: true},
	{Name: "/erd", Description: "Explain live relationships and dependencies in chat", Category: "Database", RequiresDB: true},
	{Name: "/inspect", Description: "Run the read-only health inspection", Category: "Database", RequiresDB: true},
	{Name: "/query", Description: "Review a change or run read-only SQL", Category: "Database", RequiresDB: true},
	{Name: "/modernize", Description: "Modernize SQL with optional live schema", Category: "Agent"},
	{Name: "/status", Description: "Show connection, model, and safety state", Category: "Session"},
	{Name: "/new", Description: "Start a clean conversation", Category: "Session", Shortcut: "ctrl+n"},
	{Name: "/clear", Description: "Clear the visible conversation", Category: "Session"},
	{Name: "/evidence", Description: "Show evidence from the latest answer in chat", Category: "Session"},
	{Name: "/about", Description: "Explain QueryShift's safety architecture", Category: "Help"},
	{Name: "/help", Description: "Show commands and keyboard shortcuts", Category: "Help", Shortcut: "f1"},
	{Name: "/rehearse", Description: "Rehearse the current change proposal", Category: "Change", RequiresDB: true},
	{Name: "/approve", Description: "Approve the current change from configured identity", Category: "Change", RequiresDB: true},
	{Name: "/apply", Description: "Apply the verified change run", Category: "Change", RequiresDB: true},
}

type Model struct {
	ctx    context.Context
	deps   Dependencies
	screen screen
	width  int
	height int

	chat            textarea.Model
	chatLog         []chatMessage
	slashIndex      int
	chatScroll      int
	paletteDraft    string
	pendingCmd      string
	chatBusy        bool
	chatTooling     bool
	chatStage       string
	chatSince       time.Time
	chatModel       string
	chatQueue       []string
	chatEvidence    []app.ChatEvidence
	modernizeInline bool
	rehearsalInline bool
	changeInline    bool
	refreshHome     bool
	dsn             textinput.Model
	sql             textarea.Model
	dbSQL           textarea.Model
	confirm         textinput.Model
	dialect         int
	liveSchema      bool
	connection      string
	dbTab           int
	writeMode       bool
	workspace       *app.DatabaseWorkspace
	changePlan      app.ChangePlan
	changeContract  app.ChangeContract
	changeRun       *app.VerifiedChangeRun
	query           *app.QueryResult
	change          *app.ChangeResult

	viewport      viewport.Model
	resultTab     int
	inspect       *app.Inspection
	modern        *app.ModernizationResult
	agentRun      []app.AgentRunEvent
	err           error
	cancel        context.CancelFunc
	pulse         int
	jobID         int
	booting       bool
	bootStep      int
	helpOpen      bool
	loadStage     string
	loadLogs      []string
	loadSince     time.Time
	loadHostHint  string
	promptHistory []string
	historyIndex  int
	historyDraft  string
	atIndex       int
}

type inspectFinishedMsg struct {
	id     int
	result *app.Inspection
	err    error
}

type modernizeFinishedMsg struct {
	id     int
	result *app.ModernizationResult
	err    error
}

type modernizeProgressMsg struct {
	id      int
	event   app.AgentRunEvent
	updates <-chan app.ModernizationUpdate
}

type databaseLoadedMsg struct {
	id      int
	refresh bool
	result  *app.DatabaseWorkspace
	err     error
}

type databaseProgressMsg struct {
	id      int
	refresh bool
	stage   string
	log     string
	updates <-chan app.DatabaseLoadUpdate
}

type queryFinishedMsg struct {
	id     int
	result *app.QueryResult
	err    error
}

type changeFinishedMsg struct {
	id     int
	result *app.ChangeResult
	err    error
}

type rehearsalFinishedMsg struct {
	id      int
	receipt *app.RehearsalReceipt
	err     error
}

type chatFinishedMsg struct {
	id           int
	response     *app.ChatResponse
	toolWarnings []string
	err          error
}

type chatAgentUpdate struct {
	kind     string
	detail   string
	visible  bool
	response *app.ChatResponse
	err      error
}

type chatProgressMsg struct {
	id      int
	update  chatAgentUpdate
	updates <-chan chatAgentUpdate
}

type pulseMsg time.Time
type bootTickMsg time.Time
type bootDoneMsg struct{}

var dialects = []string{"oracle", "tsql", "db2", "mysql", "sybase", "postgres"}

const databaseWorkspaceLoadTimeout = 30 * time.Second

func NewModel(ctx context.Context, deps Dependencies) Model {
	if deps.Inspect == nil {
		deps.Inspect = app.Inspect
	}
	customModernize := deps.Modernize
	if deps.Modernize == nil {
		deps.Modernize = app.Modernize
	}
	if deps.ModernizeStream == nil {
		if customModernize == nil {
			deps.ModernizeStream = app.StreamModernize
		} else {
			deps.ModernizeStream = func(ctx context.Context, request app.ModernizeRequest) <-chan app.ModernizationUpdate {
				updates := make(chan app.ModernizationUpdate, 1)
				go func() {
					defer close(updates)
					result, err := customModernize(ctx, request)
					updates <- app.ModernizationUpdate{Result: result, Err: err}
				}()
				return updates
			}
		}
	}
	customLoad := deps.LoadDatabase
	if deps.LoadDatabase == nil {
		deps.LoadDatabase = app.LoadDatabase
	}
	if deps.LoadDatabaseStream == nil {
		if customLoad == nil {
			deps.LoadDatabaseStream = app.StreamLoadDatabase
		} else {
			deps.LoadDatabaseStream = func(ctx context.Context, conn string) <-chan app.DatabaseLoadUpdate {
				ch := make(chan app.DatabaseLoadUpdate, 1)
				go func() {
					defer close(ch)
					res, err := customLoad(ctx, conn)
					ch <- app.DatabaseLoadUpdate{Result: res, Err: err}
				}()
				return ch
			}
		}
	}
	if deps.Query == nil {
		deps.Query = app.RunReadQuery
	}
	if deps.Apply == nil {
		deps.Apply = app.ApplyChangeContract
	}
	if deps.Rehearse == nil {
		deps.Rehearse = app.RehearseChangeContract
	}
	if deps.ApplyVerified == nil {
		deps.ApplyVerified = app.ExecuteVerifiedChangeRun
	}
	if deps.Chat == nil {
		deps.Chat = app.Chat
	}

	dsn := textinput.New()
	dsn.Placeholder = "postgresql://user:password@host/database?sslmode=require"
	dsn.Prompt = "› "
	dsn.EchoMode = textinput.EchoPassword
	dsn.EchoCharacter = '•'
	dsn.CharLimit = 4096
	dsn.SetWidth(72)

	chat := textarea.New()
	chat.Placeholder = "Ask about your database, paste SQL, or type / for commands…"
	chat.CharLimit = 100_000
	chat.ShowLineNumbers = false
	chat.SetWidth(80)
	chat.DynamicHeight = true
	chat.MinHeight = 1
	chat.MaxHeight = 5
	chat.MaxContentHeight = 100_000
	chat.SetHeight(1)
	chat.Prompt = "› "
	chatStyles := chat.Styles()
	chatStyles.Focused.Base = lipgloss.NewStyle()
	chatStyles.Focused.CursorLine = lipgloss.NewStyle()
	chatStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(colorMuted)
	chatStyles.Focused.Prompt = lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	chatStyles.Focused.Text = lipgloss.NewStyle().Foreground(colorInk)
	chatStyles.Blurred = chatStyles.Focused
	chat.SetStyles(chatStyles)
	chat.Focus()

	sql := textarea.New()
	sql.Placeholder = "Paste Oracle, T-SQL, DB2, MySQL, Sybase, or PostgreSQL here…"
	sql.ShowLineNumbers = true
	sql.SetWidth(80)
	sql.SetHeight(14)

	dbSQL := textarea.New()
	dbSQL.Placeholder = "SELECT * FROM public.users LIMIT 20;"
	dbSQL.ShowLineNumbers = true
	dbSQL.SetWidth(80)
	dbSQL.SetHeight(9)

	confirm := textinput.New()
	confirm.Prompt = "› "
	confirm.CharLimit = 64
	confirm.SetWidth(32)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true

	return Model{
		ctx: ctx, deps: deps, screen: screenHome,
		chat: chat, dsn: dsn, sql: sql, dbSQL: dbSQL, confirm: confirm, viewport: vp, booting: true,
		historyIndex: -1,
	}
}

func Run(ctx context.Context) error {
	if err := app.LoadLocalEnv(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}
	program := tea.NewProgram(NewModel(ctx, Dependencies{}), tea.WithContext(ctx))
	_, err := program.Run()
	return err
}

func (m Model) Init() tea.Cmd { return bootTick() }

func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		return m, nil
	case inspectFinishedMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.cancel = nil
		if msg.err != nil {
			m.err = msg.err
			if m.workspace != nil {
				m.screen = screenDatabase
				m.refreshDatabaseViewport()
			} else {
				m.screen = screenConnect
				m.dsn.Focus()
			}
			return m, nil
		}
		m.inspect = msg.result
		m.err = nil
		m.screen = screenReport
		m.viewport.SetContent(m.reportContent())
		m.viewport.GotoTop()
		return m, nil
	case modernizeProgressMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.agentRun = append(m.agentRun, msg.event)
		if m.modernizeInline {
			stage := strings.TrimSpace(msg.event.Stage)
			if stage == "" {
				stage = "Modernizing SQL"
			}
			m.chatStage = stage
		}
		return m, m.modernizeCmd(msg.id, msg.updates)
	case modernizeFinishedMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.cancel = nil
		if m.modernizeInline {
			m.modernizeInline, m.chatBusy, m.chatTooling, m.chatStage = false, false, false, ""
			if msg.err != nil {
				m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Modernization failed: " + msg.err.Error()})
				return m, m.chat.Focus()
			}
			m.modern = msg.result
			m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: modernizationChatText(msg.result)})
			return m, m.chat.Focus()
		}
		if msg.err != nil {
			m.err = msg.err
			m.screen = screenModernizer
			m.sql.Focus()
			return m, nil
		}
		m.modern = msg.result
		m.resultTab = 0
		m.err = nil
		m.screen = screenModernizerResult
		m.refreshResultViewport()
		return m, nil
	case databaseProgressMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		if msg.stage != "" {
			m.loadStage = msg.stage
		}
		if msg.log != "" {
			if len(m.loadLogs) == 0 || m.loadLogs[len(m.loadLogs)-1] != msg.log {
				m.loadLogs = append(m.loadLogs, msg.log)
			}
		}
		return m, m.databaseStreamCmd(msg.id, msg.refresh, msg.updates)
	case databaseLoadedMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.cancel = nil
		m.loadStage = ""
		m.loadLogs = nil
		m.loadHostHint = ""
		if m.refreshHome {
			m.refreshHome, m.chatBusy, m.chatTooling, m.chatStage = false, false, false, ""
			m.screen = screenHome
			if msg.err != nil {
				m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "The change was applied, but refreshing the schema failed: " + msg.err.Error()})
				return m, m.chat.Focus()
			}
			m.workspace = msg.result
			return m, m.chat.Focus()
		}
		if msg.err != nil {
			m.err = msg.err
			if msg.refresh && m.workspace != nil {
				m.screen = screenDatabase
				return m, nil
			}
			m.screen = screenConnect
			m.dsn.Focus()
			return m, nil
		}
		m.workspace = msg.result
		m.err = nil
		if m.pendingCmd != "" {
			pending := m.pendingCmd
			m.pendingCmd = ""
			return m.executeSlash(pending)
		}
		if msg.refresh {
			m.screen = screenDatabase
			m.refreshDatabaseViewport()
			return m, nil
		}
		m.screen = screenHome
		return m, m.chat.Focus()
	case queryFinishedMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.cancel = nil
		m.screen = screenDatabase
		m.dbSQL.Focus()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.query, m.change, m.err = msg.result, nil, nil
		m.refreshDatabaseViewport()
		return m, nil
	case changeFinishedMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.cancel = nil
		if m.changeInline {
			m.changeInline = false
			if msg.err != nil {
				m.chatBusy, m.chatTooling, m.chatStage = false, false, ""
				m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Change execution was blocked or failed: " + msg.err.Error()})
				return m, m.chat.Focus()
			}
			m.change = msg.result
			m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: fmt.Sprintf("Applied and recorded `%s` in %s · %d rows affected · run status **%s**.", msg.result.Command, msg.result.Duration.Round(time.Millisecond), msg.result.RowsAffected, strings.ReplaceAll(m.changeRun.Status, "_", " "))})
			m.chatStage = "Refreshing schema after change"
			m.refreshHome = true
			m.jobID++
			return m, tea.Batch(m.loadDatabaseCmd(m.ctx, m.jobID, m.connection, true), pulse())
		}
		m.screen = screenDatabase
		m.writeMode = false
		m.dbSQL.Focus()
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.change, m.query, m.err = msg.result, nil, nil
		m.refreshDatabaseViewport()
		m.jobID++
		return m, m.loadDatabaseCmd(m.ctx, m.jobID, m.connection, true)
	case rehearsalFinishedMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.cancel = nil
		if m.rehearsalInline {
			m.rehearsalInline, m.chatBusy, m.chatTooling, m.chatStage = false, false, false, ""
			m.screen = screenHome
			if msg.err != nil {
				m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Rehearsal failed: " + msg.err.Error()})
				return m, m.chat.Focus()
			}
			if m.changeRun == nil {
				m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "The change run was lost; create the proposal again."})
				return m, m.chat.Focus()
			}
			if err := m.changeRun.RecordRehearsal(*msg.receipt, time.Now()); err != nil {
				m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "The rehearsal receipt was rejected: " + err.Error()})
				return m, m.chat.Focus()
			}
			m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: fmt.Sprintf("Rehearsal **%s** for `%s`. Change run status: **%s**.", msg.receipt.Status, msg.receipt.ContractID, strings.ReplaceAll(m.changeRun.Status, "_", " "))})
			return m, m.chat.Focus()
		}
		m.screen = screenChangeConfirm
		if msg.err != nil {
			m.err = msg.err
			return m, m.confirm.Focus()
		}
		if m.changeRun == nil {
			m.err = fmt.Errorf("change run was lost; review the change again")
			return m, m.confirm.Focus()
		}
		if err := m.changeRun.RecordRehearsal(*msg.receipt, time.Now()); err != nil {
			m.err = err
			return m, m.confirm.Focus()
		}
		m.err = nil
		return m, m.confirm.Focus()
	case chatFinishedMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		m.cancel = nil
		m.chatBusy = false
		m.chatTooling = false
		m.chatStage = ""
		for _, warning := range msg.toolWarnings {
			m.chatLog = append(m.chatLog, chatMessage{role: "tool", text: warning, meta: "evidence unavailable"})
		}
		if msg.err != nil {
			m.chatLog = append(m.chatLog, chatMessage{
				role: "queryshift",
				text: "I couldn’t reach the configured model. " + msg.err.Error(),
				meta: "agent error",
			})
			m.chatScroll = 0
			if len(m.chatQueue) > 0 {
				next := m.chatQueue[0]
				m.chatQueue = m.chatQueue[1:]
				return m.startAgentChat(next)
			}
			return m, m.chat.Focus()
		}
		m.chatModel = msg.response.Provider + " · " + msg.response.Model
		m.chatEvidence = append([]app.ChatEvidence(nil), msg.response.Evidence...)
		m.chatLog = append(m.chatLog, chatMessage{
			role: "queryshift", text: msg.response.Text, meta: m.chatModel,
		})
		m.chatScroll = 0
		if len(m.chatQueue) > 0 {
			next := m.chatQueue[0]
			m.chatQueue = m.chatQueue[1:]
			return m.startAgentChat(next)
		}
		return m, m.chat.Focus()
	case chatProgressMsg:
		if msg.id != m.jobID {
			return m, nil
		}
		if msg.update.response != nil || msg.update.err != nil {
			return m.Update(chatFinishedMsg{id: msg.id, response: msg.update.response, err: msg.update.err})
		}
		m.chatStage = msg.update.detail
		if msg.update.visible {
			meta := msg.update.kind
			if meta == "" {
				meta = "agent"
			}
			m.chatLog = append(m.chatLog, chatMessage{role: "tool", text: msg.update.detail, meta: meta})
			m.chatScroll = 0
		}
		return m, m.chatProgressCmd(msg.id, msg.updates)
	case pulseMsg:
		if m.screen == screenDatabaseLoading && !m.loadSince.IsZero() && time.Since(m.loadSince) >= databaseWorkspaceLoadTimeout {
			if m.cancel != nil {
				m.cancel()
			}
			// Invalidate any late driver result. Some DNS resolvers and database
			// drivers do not return promptly when their context is canceled.
			m.jobID++
			m.cancel = nil
			m.loadStage = ""
			m.loadLogs = nil
			m.loadHostHint = ""
			m.err = fmt.Errorf("database connection timed out after %s; verify the Neon endpoint, network, and credentials, then retry", databaseWorkspaceLoadTimeout)
			if m.workspace != nil {
				m.screen = screenDatabase
				m.refreshDatabaseViewport()
			} else {
				m.screen = screenConnect
				m.dsn.Focus()
			}
			return m, nil
		}
		if m.chatBusy || m.screen == screenInspecting || m.screen == screenModernizing || m.screen == screenDatabaseLoading || m.screen == screenApplying || m.screen == screenRehearsing {
			m.pulse++
			return m, pulse()
		}
		return m, nil
	case bootTickMsg:
		if m.booting {
			m.bootStep++
			if m.bootStep >= 12 {
				return m, func() tea.Msg { return bootDoneMsg{} }
			}
			return m, bootTick()
		}
		return m, nil
	case bootDoneMsg:
		m.booting = false
		return m, m.chat.Focus()
	}

	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		// Bracketed terminal paste arrives as tea.PasteMsg rather than a key.
		// Forward all component messages or pasted connection strings disappear.
		var cmd tea.Cmd
		switch m.screen {
		case screenHome:
			m.chat, cmd = m.chat.Update(message)
		case screenConnect:
			m.dsn, cmd = m.dsn.Update(message)
		case screenModernizer:
			m.sql, cmd = m.sql.Update(message)
		case screenDatabase:
			if m.dbTab == 3 {
				m.dbSQL, cmd = m.dbSQL.Update(message)
			}
		case screenWriteUnlock, screenChangeConfirm:
			m.confirm, cmd = m.confirm.Update(message)
		}
		return m, cmd
	}
	if key.String() == "ctrl+c" {
		if m.screen == screenHome && strings.TrimSpace(m.chat.Value()) != "" {
			m.chat.Reset()
			m.chat.SetHeight(1)
			m.slashIndex = 0
			m.atIndex = 0
			m.historyIndex = -1
			m.paletteDraft = ""
			return m, nil
		}
		if m.cancel != nil {
			m.cancel()
		}
		return m, tea.Quit
	}
	if m.booting {
		return m, nil
	}
	if key.String() == "f1" || key.String() == "ctrl+h" {
		m.helpOpen = !m.helpOpen
		return m, nil
	}
	if m.helpOpen {
		if key.String() == "esc" || key.String() == "q" {
			m.helpOpen = false
		}
		return m, nil
	}

	switch m.screen {
	case screenHome:
		return m.updateHome(key)
	case screenConnect:
		return m.updateConnect(key)
	case screenInspecting, screenModernizing:
		if key.String() == "esc" {
			if m.cancel != nil {
				m.cancel()
			}
			m.jobID++ // invalidate the completion message from the cancelled job
			m.cancel = nil
			m.err = nil
			if m.screen == screenInspecting {
				m.screen = screenConnect
				m.dsn.Focus()
			} else {
				m.screen = screenModernizer
				m.sql.Focus()
			}
		}
		return m, nil
	case screenReport:
		if key.String() == "esc" {
			if m.workspace != nil {
				m.screen = screenDatabase
				m.refreshDatabaseViewport()
			} else {
				m.screen = screenHome
				m.chat.Focus()
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(message)
		return m, cmd
	case screenEvidence:
		if key.String() == "esc" || key.String() == "q" {
			m.screen = screenHome
			return m, m.chat.Focus()
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(message)
		return m, cmd
	case screenModernizer:
		return m.updateModernizer(key)
	case screenModernizerResult:
		return m.updateModernizerResult(key, message)
	case screenDatabase:
		return m.updateDatabase(key, message)
	case screenWriteUnlock:
		return m.updateWriteUnlock(key)
	case screenChangeConfirm:
		return m.updateChangeConfirm(key)
	case screenDatabaseLoading, screenApplying, screenRehearsing:
		if key.String() == "esc" {
			if m.cancel != nil {
				m.cancel()
			}
			m.jobID++
			m.cancel = nil
			m.loadStage = ""
			m.loadLogs = nil
			m.loadHostHint = ""
			if m.screen == screenRehearsing {
				m.screen = screenChangeConfirm
				m.confirm.Focus()
			} else if m.workspace != nil {
				m.screen = screenDatabase
			} else {
				m.screen = screenConnect
				m.dsn.Focus()
			}
		}
		return m, nil
	case screenAbout:
		if key.String() == "esc" || key.String() == "q" {
			m.screen = screenHome
			return m, m.chat.Focus()
		}
		return m, nil
	}
	return m, nil
}

func (m Model) updateHome(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	matches := m.slashMatches()
	atMatches := m.atMatches()
	switch key.String() {
	case "ctrl+p":
		if !strings.HasPrefix(strings.TrimSpace(m.chat.Value()), "/") {
			m.paletteDraft = m.chat.Value()
		}
		m.chat.SetValue("/")
		m.slashIndex = 0
		return m, m.chat.Focus()
	case "ctrl+n":
		if m.chatBusy {
			return m, nil
		}
		return m.executeSlash("/new")
	case "ctrl+u":
		m.chat.Reset()
		m.chat.SetHeight(1)
		m.slashIndex = 0
		m.atIndex = 0
		m.historyIndex = -1
		m.paletteDraft = ""
		return m, nil
	case "ctrl+l":
		m.chatLog = nil
		m.chatQueue = nil
		m.chatScroll = 0
		return m, nil
	case "ctrl+j", "alt+enter", "shift+enter", "ctrl+enter":
		m.chat.InsertString("\n")
		return m, nil
	case "pgup":
		if len(matches) == 0 && len(atMatches) == 0 {
			m.chatScroll += max(3, m.height/3)
			return m, nil
		}
	case "pgdown":
		if len(matches) == 0 && len(atMatches) == 0 {
			m.chatScroll = max(0, m.chatScroll-max(3, m.height/3))
			return m, nil
		}
	case "end":
		if m.chatScroll > 0 {
			m.chatScroll = 0
			return m, nil
		}
	case "up":
		if len(matches) > 0 {
			m.slashIndex = (m.slashIndex + len(matches) - 1) % len(matches)
			return m, nil
		}
		if len(atMatches) > 0 {
			m.atIndex = (m.atIndex + len(atMatches) - 1) % len(atMatches)
			return m, nil
		}
		// Prompt history navigation when single-line or empty
		if len(m.promptHistory) > 0 && !strings.Contains(m.chat.Value(), "\n") {
			if m.historyIndex == -1 {
				m.historyDraft = m.chat.Value()
				m.historyIndex = len(m.promptHistory) - 1
			} else if m.historyIndex > 0 {
				m.historyIndex--
			}
			m.chat.SetValue(m.promptHistory[m.historyIndex])
			return m, nil
		}
	case "down":
		if len(matches) > 0 {
			m.slashIndex = (m.slashIndex + 1) % len(matches)
			return m, nil
		}
		if len(atMatches) > 0 {
			m.atIndex = (m.atIndex + 1) % len(atMatches)
			return m, nil
		}
		// Prompt history navigation forward
		if m.historyIndex >= 0 {
			if m.historyIndex < len(m.promptHistory)-1 {
				m.historyIndex++
				m.chat.SetValue(m.promptHistory[m.historyIndex])
			} else {
				m.historyIndex = -1
				m.chat.SetValue(m.historyDraft)
				m.historyDraft = ""
			}
			return m, nil
		}
	case "tab":
		if len(atMatches) > 0 {
			if m.completeAtMatch() {
				return m, nil
			}
		}
		if len(matches) > 0 {
			m.slashIndex = (m.slashIndex + 1) % len(matches)
			return m, nil
		}
	case "shift+tab":
		if len(atMatches) > 0 {
			m.atIndex = (m.atIndex + len(atMatches) - 1) % len(atMatches)
			return m, nil
		}
		if len(matches) > 0 {
			m.slashIndex = (m.slashIndex + len(matches) - 1) % len(matches)
			return m, nil
		}
	case "esc":
		if m.chatBusy {
			if m.cancel != nil {
				m.cancel()
			}
			m.jobID++
			m.cancel = nil
			m.chatBusy = false
			m.chatTooling = false
			m.chatStage = ""
			m.chatQueue = nil
			m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Agent response cancelled.", meta: "cancelled"})
			return m, m.chat.Focus()
		}
		if len(matches) > 0 {
			m.chat.SetValue(m.paletteDraft)
			m.paletteDraft = ""
			m.slashIndex = 0
			return m, nil
		}
		if m.isAtTriggered() {
			m.atIndex = 0
			val := m.chat.Value()
			if atIdx := strings.LastIndex(val, "@"); atIdx >= 0 {
				m.chat.SetValue(strings.TrimRight(val[:atIdx], " "))
			}
			return m, nil
		}
		if strings.TrimSpace(m.chat.Value()) != "" {
			m.chat.Reset()
			m.chat.SetHeight(1)
			m.historyIndex = -1
			return m, nil
		}
	case "enter":
		if m.chatBusy {
			queued := strings.TrimSpace(m.chat.Value())
			if queued == "" || strings.HasPrefix(queued, "/") {
				return m, nil
			}
			if len(m.promptHistory) == 0 || m.promptHistory[len(m.promptHistory)-1] != queued {
				m.promptHistory = append(m.promptHistory, queued)
			}
			m.chatLog = append(m.chatLog, chatMessage{role: "you", text: queued, meta: "queued"})
			m.chatQueue = append(m.chatQueue, queued)
			m.chat.Reset()
			m.chat.SetHeight(1)
			return m, m.chat.Focus()
		}
		if len(atMatches) > 0 {
			if m.completeAtMatch() {
				return m, nil
			}
		}
		if len(matches) > 0 && !strings.Contains(strings.TrimPrefix(strings.TrimSpace(m.chat.Value()), "/"), " ") {
			m.chat.SetValue(matches[min(m.slashIndex, len(matches)-1)].Name)
		}
		return m.submitChat()
	}
	var cmd tea.Cmd
	m.chat, cmd = m.chat.Update(key)
	if m.slashIndex >= len(m.slashMatches()) {
		m.slashIndex = 0
	}
	if m.atIndex >= len(m.atMatches()) {
		m.atIndex = 0
	}
	return m, cmd
}

func (m Model) submitChat() (tea.Model, tea.Cmd) {
	input := strings.TrimSpace(m.chat.Value())
	if input == "" {
		return m, nil
	}
	if len(m.promptHistory) == 0 || m.promptHistory[len(m.promptHistory)-1] != input {
		m.promptHistory = append(m.promptHistory, input)
	}
	m.historyIndex = -1
	m.historyDraft = ""
	m.chatLog = append(m.chatLog, chatMessage{role: "you", text: input})
	m.chat.Reset()
	m.chat.SetHeight(1)
	m.slashIndex = 0
	m.atIndex = 0
	m.chatScroll = 0
	m.paletteDraft = ""
	m.err = nil
	lower := strings.ToLower(input)
	if strings.HasPrefix(lower, "/") {
		name := strings.Fields(lower)[0]
		for _, command := range slashCommands {
			if command.Name == name {
				return m.executeSlash(input)
			}
		}
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Unknown command. Type / to open the command palette."})
		return m, m.chat.Focus()
	}
	if strings.HasPrefix(input, "!") {
		querySQL := strings.TrimSpace(strings.TrimPrefix(input, "!"))
		if querySQL == "" {
			return m, nil
		}
		if m.workspace == nil {
			m.dbSQL.SetValue(querySQL)
			m.pendingCmd = "/query"
			m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "A database connection is required to execute SQL. Connect now; I’ll open this statement in the guarded SQL workspace afterward."})
			m.chat.Blur()
			m.screen = screenConnect
			return m, m.dsn.Focus()
		}
		if !app.IsReadQuery(querySQL) {
			m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Only read statements (SELECT, EXPLAIN, WITH, SHOW) can be executed inline with !. For modifications, switch to WRITE mode (/query)."})
			return m, m.chat.Focus()
		}
		workCtx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		m.cancel = cancel
		m.chatBusy = true
		m.chatSince = time.Now()
		m.jobID++
		return m, tea.Batch(func() tea.Msg {
			res, err := m.deps.Query(workCtx, m.connection, querySQL)
			if err != nil {
				return chatFinishedMsg{id: m.jobID, response: &app.ChatResponse{Text: "Query failed: " + err.Error(), Model: "sql-runner"}}
			}
			var b strings.Builder
			b.WriteString(fmt.Sprintf("Query executed in %s (returned %d rows", res.Duration.Round(time.Millisecond), len(res.Rows)))
			if res.Truncated {
				b.WriteString(", truncated at 100")
			}
			b.WriteString("):\n\n")
			if len(res.Columns) > 0 {
				b.WriteString("| " + strings.Join(res.Columns, " | ") + " |\n")
				b.WriteString("|" + strings.Repeat(" --- |", len(res.Columns)) + "\n")
				for _, row := range res.Rows {
					b.WriteString("| " + strings.Join(row, " | ") + " |\n")
				}
			} else {
				b.WriteString("No rows returned.")
			}
			return chatFinishedMsg{id: m.jobID, response: &app.ChatResponse{Text: b.String(), Model: "sql-runner"}}
		}, pulse())
	}
	switch {
	case looksLikeSQL(input) && m.workspace != nil:
		return m.runSlashModernize(input)
	case lower == "connect" || lower == "connect database":
		return m.executeSlash("/connect")
	default:
		return m.startAgentChat(input)
	}
}

func (m Model) startAgentChat(question string) (tea.Model, tea.Cmd) {
	turns := m.agentHistory(question)
	return m.startAgentChatWithHistory(question, turns, app.PlanChatToolsWithHistory(question, turns, m.workspace))
}

func (m Model) startAgentChatWithTools(question string, tools []app.ChatToolName) (tea.Model, tea.Cmd) {
	return m.startAgentChatWithHistory(question, m.agentHistory(question), tools)
}

func (m Model) agentHistory(question string) []app.ChatTurn {
	history := m.chatLog
	skip := -1
	for index := len(history) - 1; index >= 0; index-- {
		if history[index].role == "you" && history[index].text == question {
			skip = index
			break
		}
	}
	turns := make([]app.ChatTurn, 0, len(history))
	for index, message := range history {
		if index == skip {
			continue
		}
		turns = append(turns, app.ChatTurn{Role: message.role, Text: message.text})
	}
	return turns
}

func (m Model) startAgentChatWithHistory(question string, turns []app.ChatTurn, tools []app.ChatToolName) (tea.Model, tea.Cmd) {
	if m.chatBusy {
		return m, nil
	}
	request := app.ChatRequest{Question: question, Workspace: m.workspace, History: turns}
	m.chatTooling = len(tools) > 0
	m.chatStage = "Preparing answer"
	workCtx, cancel := context.WithTimeout(m.ctx, 3*time.Minute)
	m.cancel = cancel
	m.chatBusy = true
	m.chatSince = time.Now()
	m.jobID++
	updates := m.chatAgentStream(workCtx, request, tools)
	return m, tea.Batch(m.chatProgressCmd(m.jobID, updates), pulse())
}

type tableMatch struct {
	Name        string
	Schema      string
	Description string
	PK          string
	ColCount    int
}

func (m Model) isAtTriggered() bool {
	val := m.chat.Value()
	atIdx := strings.LastIndex(val, "@")
	if atIdx < 0 {
		return false
	}
	if atIdx > 0 {
		prev := val[atIdx-1]
		if !(prev == ' ' || prev == '\t' || prev == '\n' || prev == '(' || prev == '[' || prev == '{' || prev == ',') {
			return false
		}
	}
	token := val[atIdx+1:]
	return !strings.ContainsAny(token, " \t\n\r,;()")
}

func (m Model) atMatches() []tableMatch {
	if m.workspace == nil || !m.isAtTriggered() {
		return nil
	}
	val := m.chat.Value()
	atIdx := strings.LastIndex(val, "@")
	token := strings.ToLower(val[atIdx+1:])
	var matches []tableMatch
	for _, table := range m.workspace.Schema.Tables {
		tblLower := strings.ToLower(table.Name)
		if token == "" || strings.HasPrefix(tblLower, token) || strings.Contains(tblLower, token) {
			pk := ""
			var colNames []string
			for _, c := range table.Columns {
				if c.PK && pk == "" {
					pk = c.Name
				}
				colNames = append(colNames, c.Name)
			}
			desc := strings.Join(colNames, ", ")
			if len(desc) > 36 {
				desc = desc[:33] + "…"
			}
			matches = append(matches, tableMatch{
				Name:        table.Name,
				Schema:      table.Schema,
				Description: desc,
				PK:          pk,
				ColCount:    len(table.Columns),
			})
		}
	}
	return matches
}

func (m *Model) completeAtMatch() bool {
	matches := m.atMatches()
	if len(matches) == 0 {
		return false
	}
	selected := min(m.atIndex, len(matches)-1)
	match := matches[selected]
	val := m.chat.Value()
	atIdx := strings.LastIndex(val, "@")
	if atIdx >= 0 {
		newVal := val[:atIdx] + "@" + match.Name + " "
		m.chat.SetValue(newVal)
		m.atIndex = 0
		return true
	}
	return false
}

func (m Model) slashMatches() []slashCommand {
	value := strings.ToLower(strings.TrimSpace(m.chat.Value()))
	if !strings.HasPrefix(value, "/") || strings.Contains(strings.TrimPrefix(value, "/"), " ") {
		return nil
	}
	var matches []slashCommand
	for _, command := range slashCommands {
		if value == "/" || strings.HasPrefix(command.Name, value) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (m Model) executeSlash(input string) (tea.Model, tea.Cmd) {
	input = strings.TrimSpace(input)
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return m, m.chat.Focus()
	}
	name := strings.ToLower(fields[0])
	arguments := strings.TrimSpace(strings.TrimPrefix(input, fields[0]))
	var selected slashCommand
	found := false
	for _, command := range slashCommands {
		if command.Name == name {
			selected, found = command, true
			break
		}
	}
	if !found {
		return m, m.chat.Focus()
	}
	if selected.RequiresDB && m.workspace == nil {
		m.pendingCmd = input
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: strings.TrimPrefix(name, "/") + " needs a verified database connection. Connect now; I’ll continue automatically after the schema check."})
		m.chat.Blur()
		m.screen = screenConnect
		return m, m.dsn.Focus()
	}

	switch name {
	case "/connect":
		m.chat.Blur()
		m.screen = screenConnect
		return m, m.dsn.Focus()
	case "/help":
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Use `/connect` to attach PostgreSQL, `/schema [object]` to inspect structure, `/erd [object]` for dependencies, `/inspect [question]` for health, `/query <SQL>` for reads or proposals, and `/modernize <SQL>` for conversion. Change proposals continue with `/rehearse`, `/approve`, and `/apply WRITE <contract-id> <confirmation>`."})
		return m, m.chat.Focus()
	case "/status":
		status := "Chat is ready. No database is connected, so live schema, query, and write tools remain locked. Model: " + m.modelName() + "."
		if m.workspace != nil {
			status = fmt.Sprintf("Connected: %s @ %s (%s). Model: %s. Read mode is active; writes require WRITE plus a versioned migration confirmation.", m.workspace.Database, m.workspace.Host, m.workspace.Version, m.modelName())
		}
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: status})
		m.chatScroll = 0
		return m, m.chat.Focus()
	case "/evidence":
		if len(m.chatEvidence) == 0 {
			m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "No answer evidence is available yet. Ask a live-schema or workload question first."})
			return m, m.chat.Focus()
		}
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: m.evidenceChatText()})
		return m, m.chat.Focus()
	case "/new", "/clear":
		m.chatLog = nil
		m.chatQueue = nil
		m.chatScroll = 0
		m.chatModel = ""
		m.chatEvidence = nil
		m.chat.Reset()
		return m, m.chat.Focus()
	case "/about":
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "QueryShift is an evidence-backed PostgreSQL change agent. Models explain and propose; deterministic tools collect evidence, and policy-enforced code controls rehearsal, approval, execution, and verification."})
		return m, m.chat.Focus()
	case "/schema":
		question := "Summarize the live schema and its most important objects."
		if arguments != "" {
			question = "Describe this live schema object: " + arguments
		}
		return m.startAgentChatWithTools(question, []app.ChatToolName{app.ChatToolSchemaSearch})
	case "/erd":
		question := "Summarize the live foreign-key relationships and dependency structure."
		if arguments != "" {
			question = "Trace dependencies for this live schema object: " + arguments
		}
		return m.startAgentChatWithTools(question, []app.ChatToolName{app.ChatToolSchemaDependencies})
	case "/query":
		return m.runSlashQuery(arguments)
	case "/modernize":
		return m.runSlashModernize(arguments)
	case "/inspect":
		question := "Inspect the database health and summarize the highest-priority evidence."
		if arguments != "" {
			question = arguments
		}
		return m.startAgentChatWithTools(question, []app.ChatToolName{app.ChatToolHealthInspection})
	case "/rehearse":
		return m.runSlashRehearsal()
	case "/approve":
		return m.runSlashApproval()
	case "/apply":
		return m.runSlashApply(arguments)
	}
	return m, m.chat.Focus()
}

func (m Model) runSlashQuery(statement string) (tea.Model, tea.Cmd) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Add one statement after the command, for example `/query SELECT now()` or `/query CREATE INDEX ...`. Reads execute immediately; writes become a proposal first."})
		return m, m.chat.Focus()
	}
	if app.IsReadQuery(statement) {
		workCtx, cancel := context.WithTimeout(m.ctx, 30*time.Second)
		m.cancel = cancel
		m.chatBusy = true
		m.chatTooling = true
		m.chatStage = "Running guarded read query"
		m.chatSince = time.Now()
		m.jobID++
		return m, tea.Batch(m.inlineQueryCmd(workCtx, m.jobID, statement), pulse())
	}
	contract, err := app.BuildChangeContractForActor(statement, m.workspace, firstEnvironment("QUERYSHIFT_ACTOR"))
	if err != nil {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "I couldn’t create a change proposal: " + err.Error()})
		return m, m.chat.Focus()
	}
	run, err := app.NewVerifiedChangeRun(contract, app.DefaultChangePolicy(), time.Now())
	if err != nil {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "I couldn’t initialize the verified change run: " + err.Error()})
		return m, m.chat.Focus()
	}
	m.changeContract, m.changePlan, m.changeRun = contract, contract.Plan, run
	m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: m.changeProposalChatText()})
	return m, m.chat.Focus()
}

func (m Model) inlineQueryCmd(ctx context.Context, id int, statement string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.deps.Query(ctx, m.connection, statement)
		if err != nil {
			return chatFinishedMsg{id: id, response: &app.ChatResponse{Text: "Query failed: " + err.Error(), Model: "sql-runner", Provider: "queryshift"}}
		}
		return chatFinishedMsg{id: id, response: &app.ChatResponse{Text: queryResultChatText(result), Model: "sql-runner", Provider: "queryshift"}}
	}
}

func queryResultChatText(result *app.QueryResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Query completed in %s · %d rows", result.Duration.Round(time.Millisecond), len(result.Rows))
	if result.Truncated {
		b.WriteString(" · truncated at 100")
	}
	b.WriteString("\n\n")
	if len(result.Columns) == 0 {
		return b.String() + "No rows returned."
	}
	b.WriteString("| " + strings.Join(result.Columns, " | ") + " |\n")
	b.WriteString("|" + strings.Repeat(" --- |", len(result.Columns)) + "\n")
	for _, row := range result.Rows {
		b.WriteString("| " + strings.Join(row, " | ") + " |\n")
	}
	return b.String()
}

func modernizationChatText(result *app.ModernizationResult) string {
	if result == nil {
		return "The modernization run completed without a result."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Modernized SQL · **%s risk** · confidence %d/100 · validation %s\n\n```sql\n%s\n```", strings.ToUpper(result.RiskLevel), result.ConfidenceScore, passFail(result.Validation.Valid), strings.TrimSpace(result.Optimization.SQL))
	if len(result.Optimization.Changes) > 0 {
		b.WriteString("\n\nChanges:\n")
		for _, change := range result.Optimization.Changes {
			b.WriteString("- " + change + "\n")
		}
	}
	if len(result.Optimization.Cautions) > 0 {
		b.WriteString("\nCautions: " + strings.Join(result.Optimization.Cautions, "; "))
	}
	return b.String()
}

func (m Model) changeProposalChatText() string {
	runStatus := "proposal created"
	if m.changeRun != nil {
		runStatus = strings.ReplaceAll(m.changeRun.Status, "_", " ")
	}
	return fmt.Sprintf("Created change proposal `%s` · **%s risk** · %s.\n\n```sql\n%s\n```\n\n%s\n\nNext: `/rehearse`, then `/approve` when required, then `/apply WRITE %s %s`.", m.changeContract.ID, strings.ToUpper(m.changeContract.Risk.Level), runStatus, m.changeContract.Statement, m.changeContract.Risk.LockImpact, m.changeContract.ID, m.changePlan.RequiredConfirmation)
}

func (m Model) runSlashRehearsal() (tea.Model, tea.Cmd) {
	if m.changeRun == nil {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "There is no active change proposal. Create one with `/query <write SQL>`."})
		return m, m.chat.Focus()
	}
	dsn := firstEnvironment("QUERYSHIFT_REHEARSAL_DATABASE_URL")
	if dsn == "" {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Set `QUERYSHIFT_REHEARSAL_DATABASE_URL` to a disposable clone or branch, then run `/rehearse` again."})
		return m, m.chat.Focus()
	}
	workCtx, cancel := context.WithTimeout(m.ctx, 3*time.Minute)
	m.cancel, m.rehearsalInline, m.chatBusy, m.chatTooling = cancel, true, true, true
	m.chatStage, m.chatSince = "Rehearsing exact change contract", time.Now()
	m.jobID++
	return m, tea.Batch(m.rehearseCmd(workCtx, m.jobID, dsn, m.changeContract), pulse())
}

func (m Model) runSlashApproval() (tea.Model, tea.Cmd) {
	if m.changeRun == nil {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "There is no active change proposal to approve."})
		return m, m.chat.Focus()
	}
	key := []byte(firstEnvironment("QUERYSHIFT_APPROVAL_KEY"))
	approval, err := app.ApproveChange(m.changeContract, m.changeRun.Policy, firstEnvironment("QUERYSHIFT_APPROVER"), splitEnvironmentList(firstEnvironment("QUERYSHIFT_APPROVER_ROLES")), key, time.Now())
	if err == nil {
		err = m.changeRun.RecordApproval(approval, key, time.Now())
	}
	if err != nil {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Approval was not recorded: " + err.Error()})
		return m, m.chat.Focus()
	}
	m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: fmt.Sprintf("Recorded signed approval `%s` from `%s`. Change run status: **%s**.", approval.ID, approval.Approver, strings.ReplaceAll(m.changeRun.Status, "_", " "))})
	return m, m.chat.Focus()
}

func (m Model) runSlashApply(arguments string) (tea.Model, tea.Cmd) {
	if m.changeRun == nil {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "There is no active change proposal to apply."})
		return m, m.chat.Focus()
	}
	fields := strings.Fields(arguments)
	if len(fields) < 3 || !strings.EqualFold(fields[0], "WRITE") || fields[1] != m.changeContract.ID {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: fmt.Sprintf("Use `/apply WRITE %s %s` to bind execution to this exact proposal.", m.changeContract.ID, m.changePlan.RequiredConfirmation)})
		return m, m.chat.Focus()
	}
	confirmation := strings.Join(fields[2:], " ")
	if strings.ToUpper(confirmation) != m.changePlan.RequiredConfirmation {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "The confirmation phrase does not match the reviewed contract."})
		return m, m.chat.Focus()
	}
	workCtx, cancel := context.WithTimeout(m.ctx, 2*time.Minute)
	m.cancel, m.changeInline, m.chatBusy, m.chatTooling = cancel, true, true, true
	m.chatStage, m.chatSince = "Applying verified change run", time.Now()
	m.jobID++
	return m, tea.Batch(m.applyCmd(workCtx, m.jobID, m.changeContract, confirmation), pulse())
}

func (m Model) runSlashModernize(statement string) (tea.Model, tea.Cmd) {
	statement = strings.TrimSpace(statement)
	if statement == "" {
		m.chatLog = append(m.chatLog, chatMessage{role: "queryshift", text: "Add SQL after the command, for example `/modernize SELECT TOP 10 * FROM dbo.orders`."})
		return m, m.chat.Focus()
	}
	request := app.ModernizeRequest{SQL: statement, SourceDialect: dialects[m.dialect], LiveSchema: m.workspace != nil, ConnectionString: m.connection}
	workCtx, cancel := context.WithTimeout(m.ctx, 4*time.Minute)
	m.cancel, m.modernizeInline, m.chatBusy, m.chatTooling = cancel, true, true, true
	m.chatStage, m.chatSince, m.agentRun = "Modernizing SQL", time.Now(), nil
	m.jobID++
	updates := m.deps.ModernizeStream(workCtx, request)
	return m, tea.Batch(m.modernizeCmd(m.jobID, updates), pulse())
}

func (m Model) evidenceChatText() string {
	var sections []string
	for _, item := range m.chatEvidence {
		content := item.Content
		if len([]rune(content)) > 4000 {
			content = string([]rune(content)[:4000]) + "\n… truncated"
		}
		sections = append(sections, fmt.Sprintf("### %s · %s\n`%s`\n\n%s", item.ID, item.Summary, item.Source, content))
	}
	return strings.Join(sections, "\n\n")
}

func looksLikeSQL(value string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	for _, prefix := range []string{"select ", "with ", "insert ", "update ", "delete ", "merge ", "create ", "alter ", "drop "} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func (m Model) updateConnect(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.dsn.Blur()
		m.err = nil
		m.screen = screenHome
		return m, m.chat.Focus()
	case "ctrl+e":
		value := firstEnvironment("DATABASE_URL", "QUERYSHIFT_DATABASE_URL")
		if value == "" {
			m.err = fmt.Errorf("DATABASE_URL and QUERYSHIFT_DATABASE_URL are not set")
			return m, nil
		}
		m.dsn.SetValue(value)
		m.err = nil
		return m, nil
	case "enter":
		value := strings.TrimSpace(m.dsn.Value())
		if value == "" {
			m.err = fmt.Errorf("paste a PostgreSQL connection string first")
			return m, nil
		}
		m.connection = value
		m.err = nil
		m.dsn.Blur()
		m.screen = screenDatabaseLoading
		m.loadStage = "Connecting"
		m.loadLogs = []string{"Connecting to database endpoint…"}
		m.loadSince = time.Now()
		m.loadHostHint = app.FormatHostDisplay(value)
		workCtx, cancel := context.WithTimeout(m.ctx, databaseWorkspaceLoadTimeout)
		m.cancel = cancel
		m.jobID++
		updates := m.deps.LoadDatabaseStream(workCtx, value)
		return m, tea.Batch(m.databaseStreamCmd(m.jobID, false, updates), pulse())
	}
	var cmd tea.Cmd
	m.dsn, cmd = m.dsn.Update(key)
	return m, cmd
}

func (m Model) updateModernizer(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.sql.Blur()
		m.err = nil
		m.screen = screenHome
		return m, m.chat.Focus()
	case "ctrl+d":
		m.dialect = (m.dialect + 1) % len(dialects)
		return m, nil
	case "ctrl+l":
		if m.connection == "" {
			m.err = fmt.Errorf("connect to a database before enabling live schema")
			return m, nil
		}
		m.liveSchema = !m.liveSchema
		m.err = nil
		return m, nil
	case "ctrl+r":
		if strings.TrimSpace(m.sql.Value()) == "" {
			m.err = fmt.Errorf("paste a SQL query before starting the agent")
			return m, nil
		}
		m.err = nil
		m.sql.Blur()
		m.screen = screenModernizing
		m.agentRun = nil
		request := app.ModernizeRequest{
			SQL: m.sql.Value(), SourceDialect: dialects[m.dialect],
			LiveSchema: m.liveSchema, ConnectionString: m.connection,
		}
		workCtx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.jobID++
		updates := m.deps.ModernizeStream(workCtx, request)
		return m, tea.Batch(m.modernizeCmd(m.jobID, updates), pulse())
	}
	var cmd tea.Cmd
	m.sql, cmd = m.sql.Update(key)
	return m, cmd
}

func (m Model) updateModernizerResult(key tea.KeyPressMsg, message tea.Msg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.screen = screenModernizer
		return m, m.sql.Focus()
	case "tab", "right", "l":
		m.resultTab = (m.resultTab + 1) % 4
		m.refreshResultViewport()
		return m, nil
	case "shift+tab", "left", "h":
		m.resultTab = (m.resultTab + 3) % 4
		m.refreshResultViewport()
		return m, nil
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(message)
	return m, cmd
}

func (m Model) updateDatabase(key tea.KeyPressMsg, message tea.Msg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.dbSQL.Blur()
		m.writeMode = false
		m.err = nil
		m.screen = screenHome
		return m, m.chat.Focus()
	case "tab", "right":
		m.dbSQL.Blur()
		m.dbTab = (m.dbTab + 1) % 4
		m.refreshDatabaseViewport()
		if m.dbTab == 3 {
			return m, m.dbSQL.Focus()
		}
		return m, nil
	case "shift+tab", "left":
		m.dbSQL.Blur()
		m.dbTab = (m.dbTab + 3) % 4
		m.refreshDatabaseViewport()
		if m.dbTab == 3 {
			return m, m.dbSQL.Focus()
		}
		return m, nil
	case "1":
		m.dbTab = 0
		m.dbSQL.Blur()
		m.refreshDatabaseViewport()
		return m, nil
	case "2":
		m.dbTab = 1
		m.dbSQL.Blur()
		m.refreshDatabaseViewport()
		return m, nil
	case "3":
		m.dbTab = 2
		m.dbSQL.Blur()
		m.refreshDatabaseViewport()
		return m, nil
	case "4":
		m.dbTab = 3
		m.refreshDatabaseViewport()
		return m, m.dbSQL.Focus()
	case "ctrl+w":
		if m.dbTab != 3 {
			return m, nil
		}
		m.dbSQL.Blur()
		m.confirm.SetValue("")
		m.confirm.Placeholder = "WRITE"
		m.screen = screenWriteUnlock
		return m, m.confirm.Focus()
	case "ctrl+r":
		if m.dbTab != 3 {
			m.err = nil
			m.jobID++
			m.screen = screenDatabaseLoading
			m.loadStage = "Refreshing"
			m.loadLogs = []string{"Refreshing database schema…"}
			m.loadSince = time.Now()
			m.loadHostHint = app.FormatHostDisplay(m.connection)
			workCtx, cancel := context.WithTimeout(m.ctx, databaseWorkspaceLoadTimeout)
			m.cancel = cancel
			updates := m.deps.LoadDatabaseStream(workCtx, m.connection)
			return m, tea.Batch(m.databaseStreamCmd(m.jobID, true, updates), pulse())
		}
		statement := strings.TrimSpace(m.dbSQL.Value())
		if statement == "" {
			m.err = fmt.Errorf("enter one SQL statement first")
			return m, nil
		}
		if app.IsReadQuery(statement) {
			m.err = nil
			m.dbSQL.Blur()
			workCtx, cancel := context.WithCancel(m.ctx)
			m.cancel = cancel
			m.jobID++
			return m, tea.Batch(m.queryCmd(workCtx, m.jobID, statement), pulse())
		}
		if !m.writeMode {
			m.err = fmt.Errorf("write mode is locked; press Ctrl+W and type WRITE")
			return m, nil
		}
		contract, err := app.BuildChangeContractForActor(statement, m.workspace, firstEnvironment("QUERYSHIFT_ACTOR"))
		if err != nil {
			m.err = err
			return m, nil
		}
		m.changeContract = contract
		m.changePlan = contract.Plan
		run, err := app.NewVerifiedChangeRun(contract, app.DefaultChangePolicy(), time.Now())
		if err != nil {
			m.err = err
			return m, nil
		}
		m.changeRun = run
		m.err = nil
		m.dbSQL.Blur()
		m.confirm.SetValue("")
		m.confirm.Placeholder = contract.Plan.RequiredConfirmation
		m.screen = screenChangeConfirm
		return m, m.confirm.Focus()
	case "i":
		m.err = nil
		m.screen = screenInspecting
		workCtx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.jobID++
		return m, tea.Batch(m.inspectCmd(workCtx, m.jobID, m.connection), pulse())
	}

	if m.dbTab == 3 {
		var cmd tea.Cmd
		m.dbSQL, cmd = m.dbSQL.Update(key)
		return m, cmd
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(message)
	return m, cmd
}

func (m Model) updateWriteUnlock(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.confirm.Blur()
		m.confirm.SetValue("")
		m.screen = screenDatabase
		return m, m.dbSQL.Focus()
	case "enter":
		if strings.TrimSpace(strings.ToUpper(m.confirm.Value())) != "WRITE" {
			m.err = fmt.Errorf("type WRITE exactly to unlock this session")
			return m, nil
		}
		m.writeMode = true
		m.err = nil
		m.confirm.Blur()
		m.confirm.SetValue("")
		m.screen = screenDatabase
		return m, m.dbSQL.Focus()
	}
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(key)
	return m, cmd
}

func (m Model) updateChangeConfirm(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.confirm.Blur()
		m.confirm.SetValue("")
		m.screen = screenDatabase
		return m, m.dbSQL.Focus()
	case "ctrl+r":
		rehearsalDSN := firstEnvironment("QUERYSHIFT_REHEARSAL_DATABASE_URL")
		if rehearsalDSN == "" {
			m.err = fmt.Errorf("set QUERYSHIFT_REHEARSAL_DATABASE_URL to a disposable clone or branch")
			return m, nil
		}
		m.err = nil
		m.confirm.Blur()
		m.screen = screenRehearsing
		workCtx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.jobID++
		return m, tea.Batch(m.rehearseCmd(workCtx, m.jobID, rehearsalDSN, m.changeContract), pulse())
	case "ctrl+a":
		if m.changeRun == nil {
			m.err = fmt.Errorf("change run was lost; review the change again")
			return m, nil
		}
		key := []byte(firstEnvironment("QUERYSHIFT_APPROVAL_KEY"))
		approval, err := app.ApproveChange(
			m.changeContract,
			m.changeRun.Policy,
			firstEnvironment("QUERYSHIFT_APPROVER"),
			splitEnvironmentList(firstEnvironment("QUERYSHIFT_APPROVER_ROLES")),
			key,
			time.Now(),
		)
		if err == nil {
			err = m.changeRun.RecordApproval(approval, key, time.Now())
		}
		m.err = err
		return m, nil
	case "enter":
		confirmation := strings.TrimSpace(strings.ToUpper(m.confirm.Value()))
		if confirmation != m.changePlan.RequiredConfirmation {
			m.err = fmt.Errorf("type %s exactly to continue", m.changePlan.RequiredConfirmation)
			return m, nil
		}
		m.err = nil
		m.confirm.Blur()
		m.screen = screenApplying
		workCtx, cancel := context.WithCancel(m.ctx)
		m.cancel = cancel
		m.jobID++
		return m, tea.Batch(m.applyCmd(workCtx, m.jobID, m.changeContract, confirmation), pulse())
	}
	var cmd tea.Cmd
	m.confirm, cmd = m.confirm.Update(key)
	return m, cmd
}

func (m Model) inspectCmd(ctx context.Context, id int, connection string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.deps.Inspect(ctx, connection, max(80, m.width-6))
		return inspectFinishedMsg{id: id, result: result, err: err}
	}
}

func (m Model) modernizeCmd(id int, updates <-chan app.ModernizationUpdate) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-updates
		if !ok {
			return modernizeFinishedMsg{id: id, err: fmt.Errorf("agent event stream closed before a result")}
		}
		if update.Event != nil {
			return modernizeProgressMsg{id: id, event: *update.Event, updates: updates}
		}
		return modernizeFinishedMsg{id: id, result: update.Result, err: update.Err}
	}
}

func (m Model) databaseStreamCmd(id int, refresh bool, updates <-chan app.DatabaseLoadUpdate) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-updates
		if !ok {
			return databaseLoadedMsg{id: id, refresh: refresh, err: fmt.Errorf("connection stream closed unexpectedly")}
		}
		if update.Result != nil || update.Err != nil {
			return databaseLoadedMsg{id: id, refresh: refresh, result: update.Result, err: update.Err}
		}
		return databaseProgressMsg{id: id, refresh: refresh, stage: update.Stage, log: update.Log, updates: updates}
	}
}

func (m Model) loadDatabaseCmd(ctx context.Context, id int, connection string, refresh bool) tea.Cmd {
	return func() tea.Msg {
		result, err := m.deps.LoadDatabase(ctx, connection)
		return databaseLoadedMsg{id: id, refresh: refresh, result: result, err: err}
	}
}

func (m Model) queryCmd(ctx context.Context, id int, sql string) tea.Cmd {
	return func() tea.Msg {
		result, err := m.deps.Query(ctx, m.connection, sql)
		return queryFinishedMsg{id: id, result: result, err: err}
	}
}

func (m Model) applyCmd(ctx context.Context, id int, contract app.ChangeContract, confirmation string) tea.Cmd {
	return func() tea.Msg {
		var result *app.ChangeResult
		var err error
		if m.changeRun != nil {
			result, err = m.deps.ApplyVerified(ctx, m.connection, m.changeRun, confirmation, []byte(firstEnvironment("QUERYSHIFT_APPROVAL_KEY")), time.Now())
		} else {
			result, err = m.deps.Apply(ctx, m.connection, contract, confirmation)
		}
		return changeFinishedMsg{id: id, result: result, err: err}
	}
}

func (m Model) rehearseCmd(ctx context.Context, id int, dsn string, contract app.ChangeContract) tea.Cmd {
	return func() tea.Msg {
		receipt, err := m.deps.Rehearse(ctx, dsn, contract, "REHEARSE "+contract.ID)
		return rehearsalFinishedMsg{id: id, receipt: receipt, err: err}
	}
}

func (m Model) chatCmd(ctx context.Context, id int, request app.ChatRequest) tea.Cmd {
	return func() tea.Msg {
		response, err := m.deps.Chat(ctx, request)
		return chatFinishedMsg{id: id, response: response, err: err}
	}
}

func (m Model) chatWithEvidenceCmd(ctx context.Context, id int, request app.ChatRequest, tools []app.ChatToolName) tea.Cmd {
	return func() tea.Msg {
		warnings := make([]string, 0)
		var inspection *app.Inspection
		var inspectionErr error
		for _, tool := range tools {
			switch tool {
			case app.ChatToolHealthInspection, app.ChatToolWorkloadActivity, app.ChatToolQueryActivity, app.ChatToolChangeHistory:
				if inspection == nil && inspectionErr == nil {
					inspection, inspectionErr = m.deps.Inspect(ctx, m.connection, 120)
				}
				if inspectionErr != nil {
					warnings = append(warnings, string(tool)+" failed: "+inspectionErr.Error())
					request.Evidence = append(request.Evidence, app.ChatEvidence{
						Kind: "collection_error", Source: "queryshift.inspect", ObservedAt: time.Now(),
						Summary: string(tool) + " evidence could not be collected", Content: inspectionErr.Error(),
					})
					continue
				}
				request.Evidence = append(request.Evidence, app.InspectionChatEvidenceForTool(tool, inspection))
			default:
				evidence, err := app.WorkspaceChatEvidence(tool, request.Question, request.Workspace)
				if err != nil {
					warnings = append(warnings, string(tool)+" failed: "+err.Error())
					continue
				}
				request.Evidence = append(request.Evidence, evidence)
			}
		}
		response, err := m.deps.Chat(ctx, request)
		return chatFinishedMsg{id: id, response: response, toolWarnings: warnings, err: err}
	}
}

func (m Model) chatAgentStream(ctx context.Context, request app.ChatRequest, tools []app.ChatToolName) <-chan chatAgentUpdate {
	updates := make(chan chatAgentUpdate, 12)
	go func() {
		defer close(updates)
		send := func(update chatAgentUpdate) bool {
			select {
			case updates <- update:
				return true
			case <-ctx.Done():
				return false
			}
		}
		if !send(chatAgentUpdate{kind: "plan", detail: "Understanding request"}) {
			return
		}
		if request.Workspace != nil {
			if !send(chatAgentUpdate{kind: "context", detail: "Reading live schema"}) {
				return
			}
		}
		var inspection *app.Inspection
		var inspectionErr error
		for _, tool := range tools {
			spec, ok := app.ChatToolDefinition(tool)
			if !ok {
				continue
			}
			if !send(chatAgentUpdate{kind: "tool call", detail: spec.Activity}) {
				return
			}
			var evidence app.ChatEvidence
			var err error
			if app.IsInspectionChatTool(tool) {
				if inspection == nil && inspectionErr == nil {
					inspection, inspectionErr = m.deps.Inspect(ctx, m.connection, 120)
				}
				err = inspectionErr
				if err == nil {
					evidence = app.InspectionChatEvidenceForTool(tool, inspection)
					evidence.Summary = strings.Replace(evidence.Summary, "Deterministic read-only database health inspection", "Inspected database health", 1)
				}
			} else {
				evidence, err = app.WorkspaceChatEvidence(tool, request.Question, request.Workspace)
			}
			if err != nil {
				request.Evidence = append(request.Evidence, app.ChatEvidence{
					Kind: "collection_error", Source: spec.Source, ObservedAt: time.Now(),
					Summary: string(tool) + " evidence could not be collected", Content: err.Error(),
				})
				if !send(chatAgentUpdate{kind: "tool error", detail: spec.Activity + " failed · " + err.Error(), visible: true}) {
					return
				}
			} else {
				request.Evidence = append(request.Evidence, evidence)
				if !send(chatAgentUpdate{kind: "tool result", detail: evidence.Summary, visible: true}) {
					return
				}
			}
		}
		if !send(chatAgentUpdate{kind: "model", detail: "Writing evidence-backed answer"}) {
			return
		}
		response, err := m.deps.Chat(ctx, request)
		if err != nil {
			send(chatAgentUpdate{err: err})
			return
		}
		send(chatAgentUpdate{response: response})
	}()
	return updates
}

func (m Model) chatProgressCmd(id int, updates <-chan chatAgentUpdate) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-updates
		if !ok {
			return chatFinishedMsg{id: id, err: fmt.Errorf("agent event stream closed before a response")}
		}
		return chatProgressMsg{id: id, update: update, updates: updates}
	}
}

func pulse() tea.Cmd {
	return tea.Tick(110*time.Millisecond, func(t time.Time) tea.Msg { return pulseMsg(t) })
}

func bootTick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(t time.Time) tea.Msg { return bootTickMsg(t) })
}

func (m *Model) resize() {
	contentWidth := m.shellWidth()
	contentHeight := max(8, m.height-9)
	m.chat.SetWidth(max(36, contentWidth-chatComposerStyle.GetHorizontalFrameSize()-chatInputStyle.GetHorizontalFrameSize()))
	m.dsn.SetWidth(min(94, contentWidth-10))
	m.sql.SetWidth(contentWidth - panelStyle.GetHorizontalFrameSize())
	m.sql.SetHeight(max(8, contentHeight-12))
	m.dbSQL.SetWidth(max(40, contentWidth-34-panelStyle.GetHorizontalFrameSize()))
	m.dbSQL.SetHeight(max(6, min(10, contentHeight-13)))
	m.confirm.SetWidth(min(42, contentWidth-12))
	m.viewport.SetWidth(contentWidth)
	m.viewport.SetHeight(max(6, contentHeight-2))
}

func (m *Model) refreshResultViewport() {
	m.viewport.SetContent(m.resultContent())
	m.viewport.GotoTop()
}

func (m *Model) refreshDatabaseViewport() {
	m.viewport.SetContent(m.databaseContent())
	m.viewport.GotoTop()
}

func (m Model) databaseContent() string {
	if m.workspace == nil {
		return "No database workspace loaded."
	}
	switch m.dbTab {
	case 1:
		var b strings.Builder
		for _, table := range m.workspace.Schema.Tables {
			fmt.Fprintf(&b, "%s.%s  %d columns  %d indexes\n", table.Schema, table.Name, len(table.Columns), len(table.Indexes))
			for _, column := range table.Columns {
				markers := ""
				if column.PK {
					markers += " PK"
				}
				if column.FKTarget != "" {
					markers += " FK → " + column.FKTarget
				}
				fmt.Fprintf(&b, "  %-28s %-22s%s\n", column.Name, column.Type, markers)
			}
			if len(table.Indexes) > 0 {
				b.WriteString("  indexes\n")
				for _, index := range table.Indexes {
					fmt.Fprintf(&b, "    %s  %s\n", index.Name, index.Def)
				}
			}
			b.WriteString("\n")
		}
		return b.String()
	case 2:
		return m.workspace.ER
	case 3:
		if m.query != nil {
			return renderQueryResult(m.query)
		}
		if m.change != nil {
			return fmt.Sprintf(
				"Applied successfully\n%s\n%d row(s) affected · %s\n\nMigration %s\nRecovery: %s\n%s",
				m.change.Command, m.change.RowsAffected, m.change.Duration.Round(time.Millisecond),
				m.change.MigrationID, m.change.RecoveryLevel, m.change.MigrationPath,
			)
		}
		return "Run a SELECT to preview values, or unlock write mode to apply one schema/data statement."
	default:
		return ""
	}
}

func renderQueryResult(result *app.QueryResult) string {
	if result == nil {
		return ""
	}
	widths := make([]int, len(result.Columns))
	for i, name := range result.Columns {
		widths[i] = min(28, max(3, len([]rune(name))))
	}
	for _, row := range result.Rows {
		for i, cell := range row {
			if i < len(widths) {
				widths[i] = min(28, max(widths[i], len([]rune(cell))))
			}
		}
	}
	fit := func(value string, width int) string {
		runes := []rune(value)
		if len(runes) > width {
			runes = append(runes[:max(0, width-1)], '…')
		}
		return fmt.Sprintf("%-*s", width, string(runes))
	}
	var b strings.Builder
	for i, name := range result.Columns {
		if i > 0 {
			b.WriteString(" │ ")
		}
		b.WriteString(fit(name, widths[i]))
	}
	b.WriteString("\n")
	for i, width := range widths {
		if i > 0 {
			b.WriteString("─┼─")
		}
		b.WriteString(strings.Repeat("─", width))
	}
	b.WriteString("\n")
	for _, row := range result.Rows {
		for i, cell := range row {
			if i > 0 {
				b.WriteString(" │ ")
			}
			if i < len(widths) {
				b.WriteString(fit(cell, widths[i]))
			}
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "\n%d row(s) · %s", len(result.Rows), result.Duration.Round(time.Millisecond))
	if result.Truncated {
		b.WriteString(" · showing first 100")
	}
	return b.String()
}

func (m Model) reportContent() string {
	if m.inspect == nil {
		return "No report available."
	}
	if m.inspect.PoolerNote != "" {
		return m.inspect.PoolerNote + "\n\n" + m.inspect.Report
	}
	return m.inspect.Report
}

func (m Model) resultContent() string {
	if m.modern == nil {
		return "No modernization result available."
	}
	r := m.modern
	var b strings.Builder
	switch m.resultTab {
	case 0:
		fmt.Fprintf(&b, "%s risk · confidence %d/100 · validation %s\n%s\n\n", strings.ToUpper(r.RiskLevel), r.ConfidenceScore, passFail(r.Validation.Valid), r.ConfidenceBasis)
		b.WriteString(r.Optimization.SQL)
		if len(r.Optimization.Changes) > 0 {
			b.WriteString("\n\nCHANGES\n")
			for _, change := range r.Optimization.Changes {
				fmt.Fprintf(&b, "• %s\n", change)
			}
		}
	case 1:
		for index, event := range r.Trace {
			attempt := fmt.Sprintf("%d", event.Attempt)
			if event.TransportAttempt > 1 {
				attempt += fmt.Sprintf(".%d", event.TransportAttempt)
			}
			fmt.Fprintf(&b, "%02d  %-22s  %-10s  %dms  attempt %s\n", index+1, event.Stage, event.Status, event.DurationMS, attempt)
			if event.Agent != "" {
				fmt.Fprintf(&b, "    agent: %s", event.Agent)
				if event.TotalTokens > 0 || event.ToolCalls > 0 {
					fmt.Fprintf(&b, "  ·  tokens %d in / %d out  ·  tools %d", event.InputTokens, event.OutputTokens, event.ToolCalls)
				}
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "    %s\n", event.Summary)
			if event.ErrorCategory != "" {
				fmt.Fprintf(&b, "    category: %s\n", event.ErrorCategory)
			}
			b.WriteString("\n")
		}
		if len(r.Trace) == 0 {
			b.WriteString("No trace events were returned.")
		}
	case 2:
		fmt.Fprintf(&b, "+%d  -%d\n\n%s", r.Diff.Additions, r.Diff.Deletions, r.Diff.UnifiedDiff)
	case 3:
		for index, recommendation := range r.Optimization.IndexRecommendations {
			fmt.Fprintf(&b, "%02d  %s on %s (%s)\n%s\nImpact: %s · planner validated: %t\n%s\n\n", index+1, recommendation.Name, recommendation.Table, strings.Join(recommendation.Columns, ", "), recommendation.Rationale, recommendation.EstimatedImpact, recommendation.PlannerValidated, recommendation.DDL)
		}
		if len(r.Optimization.IndexRecommendations) == 0 {
			b.WriteString("No index recommendations were produced.")
		}
	}
	return b.String()
}

func passFail(valid bool) string {
	if valid {
		return "passed"
	}
	return "needs review"
}

func (m Model) View() tea.View {
	if m.width > 0 && (m.width < 70 || m.height < 20) {
		view := tea.NewView(panelStyle.Render("QueryShift needs at least 70 × 20 columns.\nResize this terminal to continue."))
		view.AltScreen = true
		view.WindowTitle = "QueryShift"
		return view
	}

	width := m.shellWidth()
	var content string
	if m.booting {
		content = lipgloss.Place(width, m.height, lipgloss.Center, lipgloss.Center, m.bootView(width))
	} else {
		header := m.header(width)
		footer := m.footer(width)
		bodyHeight := max(8, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
		body := m.body(width, bodyHeight)
		content = lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	}
	content = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, content)
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "QueryShift — database agent workbench"
	view.BackgroundColor = colorCanvas
	view.ForegroundColor = colorInk
	return view
}

func (m Model) shellWidth() int {
	return min(154, max(62, m.width-8))
}

func (m Model) header(width int) string {
	product := brandStyle.Render("queryshift") + mutedStyle.Render("  /  "+strings.ToLower(screenName(m.screen)))
	right := mutedStyle.Render("no database")
	if m.workspace != nil {
		right = goodStyle.Render(m.workspace.Database) + mutedStyle.Render("  ·  read")
	}
	if m.writeMode {
		right = warnStyle.Render(m.workspace.Database + "  ·  write armed")
	}
	return "\n" + spread(product, right, width) + "\n"
}

func (m Model) body(width, height int) string {
	var body string
	if m.helpOpen {
		body = m.helpView(width)
	} else {
		switch m.screen {
		case screenHome:
			body = m.homeView(width, height)
		case screenConnect:
			body = m.connectView(width)
		case screenInspecting:
			body = m.workingView("Inspecting database", "Collecting catalog, workload, index, vacuum, and configuration evidence")
		case screenReport:
			body = labelStyle.Render("DATABASE REPORT") + "\n" + m.viewport.View()
		case screenEvidence:
			body = labelStyle.Render("ANSWER EVIDENCE") + "\n" + m.viewport.View()
		case screenModernizer:
			body = m.modernizerView(width)
		case screenModernizing:
			body = m.modernizingView(width)
		case screenModernizerResult:
			body = m.modernizerResultView()
		case screenAbout:
			body = m.aboutView(width)
		case screenDatabaseLoading:
			body = m.databaseLoadingView(width)
		case screenDatabase:
			body = m.databaseView(width, height)
		case screenWriteUnlock:
			body = m.writeUnlockView(width)
		case screenChangeConfirm:
			body = m.changeConfirmView(width)
		case screenApplying:
			body = m.workingView("Versioning and applying change", "Snapshotting schema, writing the migration journal, then executing one short transaction")
		case screenRehearsing:
			body = m.workingView("Rehearsing change", "Applying the exact contract to the disposable target, then verifying its schema and probes")
		}
	}
	vertical := lipgloss.Top
	if m.helpOpen || m.screen == screenConnect || m.screen == screenInspecting || m.screen == screenModernizing || m.screen == screenAbout || m.screen == screenDatabaseLoading || m.screen == screenWriteUnlock || m.screen == screenChangeConfirm || m.screen == screenApplying || m.screen == screenRehearsing {
		vertical = lipgloss.Center
	}
	return lipgloss.Place(width, height, lipgloss.Left, vertical, body)
}

func (m Model) homeView(width, height int) string {
	columnWidth := width
	composerWidth := max(42, columnWidth)
	messageWidth := min(96, max(36, columnWidth-2))

	turns := make([]string, 0, len(m.chatLog))
	for _, message := range m.chatLog {
		switch message.role {
		case "you":
			turns = append(turns, renderUserTurn(message.text, messageWidth))
		case "status":
			// Status belongs in the header. Preserve older session entries only as
			// quiet receipts so they cannot dominate the conversation.
			turns = append(turns, chatReceiptStyle.Render("✓ "+message.text))
		case "tool":
			marker := goodStyle.Render("✓ ")
			if message.meta == "tool error" || message.meta == "evidence unavailable" {
				marker = warnStyle.Render("! ")
			}
			turns = append(turns, marker+chatReceiptStyle.Render(message.text))
		default:
			turns = append(turns, assistantLabelStyle.Render("queryshift")+"\n"+renderChatMarkdown(message.text, messageWidth))
		}
	}
	transcript := strings.Join(turns, "\n\n")
	if m.chatBusy {
		elapsed := time.Since(m.chatSince).Round(time.Second)
		stage := m.chatStage
		if stage == "" {
			stage = chatPhase(elapsed, m.chatTooling)
		}
		loader := agentLoader(m.pulse)
		stageWidth := max(12, messageWidth-lipgloss.Width(loader)-12)
		activity := "  " + loader + "  " + statusValueStyle.Render(strings.TrimSpace(fit(stage, stageWidth))) + mutedStyle.Render("  "+elapsed.String())
		if transcript == "" {
			transcript = activity
		} else {
			// Keep the active work attached to the submitted turn. The composer
			// stays visually quiet and contains controls only.
			transcript += "\n" + activity
		}
	}
	maxCommands := 8
	if height < 30 {
		maxCommands = 4
	}
	palette := m.slashPalette(composerWidth, maxCommands)
	atPalette := m.atPalette(composerWidth, maxCommands)

	inside := composerWidth - chatComposerStyle.GetHorizontalFrameSize()

	currentVal := strings.TrimSpace(m.chat.Value())

	// The composer footer contains controls only. Active work is rendered beside
	// the submitted query above, where its relationship is unambiguous.
	var bottomActionStrip string
	if m.chatBusy {
		bottomActionStrip = spread(helpKeys("enter", "queue"), helpKeys("esc", "interrupt"), inside)
	} else {
		leftAction := helpKeys("enter", "send", "ctrl+j", "newline", "/", "commands", "@", "tables")
		if strings.HasPrefix(currentVal, "/") {
			leftAction = badgeStyle.Render("COMMAND") + "  " + leftAction
		} else if strings.HasPrefix(currentVal, "@") || strings.Contains(currentVal, " @") {
			leftAction = keyStyle.Render("TABLE CONTEXT") + "  " + leftAction
		} else if strings.HasPrefix(currentVal, "!") {
			leftAction = dangerBadgeStyle.Render("SQL RUNNER") + "  " + leftAction
		}
		charCount := len([]rune(m.chat.Value()))
		rightAction := ""
		if charCount > 0 {
			rightAction = mutedStyle.Render(fmt.Sprintf("%d chars", charCount))
		} else {
			rightAction = keyStyle.Render("ctrl+p") + mutedStyle.Render(" palette")
		}
		bottomActionStrip = spread(leftAction, rightAction, inside)
	}

	inputSurface := chatInputStyle.Width(inside).Render(m.chat.View())
	composerContent := inputSurface + "\n" + bottomActionStrip
	composer := chatComposerStyle.Width(composerWidth).Render(composerContent)

	inputBlock := ""
	if palette != "" {
		inputBlock += palette + "\n"
	} else if atPalette != "" {
		inputBlock += atPalette + "\n"
	}
	if len(m.chatQueue) > 0 {
		next := strings.ReplaceAll(m.chatQueue[0], "\n", " ")
		queueText := fmt.Sprintf("%d queued  ·  %s", len(m.chatQueue), strings.TrimSpace(fit(next, max(12, composerWidth-18))))
		inputBlock += queuedPromptStyle.Width(composerWidth).Render(queueText) + "\n"
	}
	inputBlock += composer

	top := transcript
	availableTranscript := max(3, height-lipgloss.Height(inputBlock)-1)
	top = windowLines(top, availableTranscript, m.chatScroll)
	if m.chatScroll > 0 {
		top = mutedStyle.Render("↑ older messages  ·  End returns to latest") + "\n" + top
	}
	spacer := max(1, height-lipgloss.Height(top)-lipgloss.Height(inputBlock))
	return lipgloss.NewStyle().Width(columnWidth).Render(top + strings.Repeat("\n", spacer) + inputBlock)
}

func (m Model) atPalette(width, maxRows int) string {
	if !m.isAtTriggered() {
		return ""
	}
	inside := width - commandPaletteStyle.GetHorizontalFrameSize()
	if m.workspace == nil {
		msg := disconnectedStyle.Render("○ No database connected. ") + mutedStyle.Render("Type /connect to link a schema and enable @table context.")
		return commandPaletteStyle.Width(width).Render(overlineStyle.Render("TABLE CONTEXT (@)") + "\n" + fit(msg, inside))
	}
	matches := m.atMatches()
	if len(matches) == 0 {
		return ""
	}
	selected := min(m.atIndex, len(matches)-1)
	start := 0
	if len(matches) > maxRows && selected >= maxRows {
		start = selected - maxRows + 1
	}
	end := min(len(matches), start+maxRows)
	var rows []string
	rows = append(rows, overlineStyle.Render("TABLE CONTEXT (@ MENTIONS)"))
	for index := start; index < end; index++ {
		table := matches[index]
		nameStyle := commandStyle
		detailStyle := mutedStyle
		marker := "  "
		if index == selected {
			marker = "› "
			nameStyle = selectedCommandStyle
			detailStyle = selectedCommandDetailStyle
		}
		tableName := "@" + table.Name
		meta := fmt.Sprintf("%d cols", table.ColCount)
		if table.PK != "" {
			meta = fmt.Sprintf("PK: %s · %d cols", table.PK, table.ColCount)
		}
		metaFormatted := keyStyle.Render(meta)
		descWidth := max(8, inside-len(marker)-len(tableName)-lipgloss.Width(metaFormatted)-6)
		desc := strings.TrimSpace(fit(table.Description, descWidth))
		left := marker + nameStyle.Render(tableName) + "  " + detailStyle.Render(desc)
		rows = append(rows, spread(left, metaFormatted, inside))
	}
	if len(matches) > maxRows {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("  %d of %d tables · ↑/↓ to browse · tab to complete", selected+1, len(matches))))
	}
	return commandPaletteStyle.Width(width).Render(strings.Join(rows, "\n"))
}

func (m Model) slashPalette(width, maxRows int) string {
	matches := m.slashMatches()
	if len(matches) == 0 {
		return ""
	}
	selected := min(m.slashIndex, len(matches)-1)
	start := 0
	if len(matches) > maxRows && selected >= maxRows {
		start = selected - maxRows + 1
	}
	end := min(len(matches), start+maxRows)
	inside := width - commandPaletteStyle.GetHorizontalFrameSize()
	var rows []string
	for index := start; index < end; index++ {
		command := matches[index]
		nameStyle := commandStyle
		detailStyle := mutedStyle
		marker := "  "
		if index == selected {
			marker = "› "
			nameStyle = selectedCommandStyle
			detailStyle = selectedCommandDetailStyle
		}
		state := command.Shortcut
		if command.RequiresDB && m.workspace == nil {
			stateText := "locked · connect first"
			if width < 90 {
				stateText = "locked"
			}
			state = disconnectedStyle.Render(stateText)
		} else if state != "" {
			state = keyStyle.Render(state)
		}
		descriptionWidth := max(8, inside-len(marker)-len(command.Name)-len(command.Category)-7-lipgloss.Width(state))
		description := strings.TrimSpace(fit(command.Description, descriptionWidth))
		left := marker + nameStyle.Render(command.Name) + "  " + detailStyle.Render(description)
		right := mutedStyle.Render(command.Category)
		if state != "" {
			right += mutedStyle.Render("  ") + state
		}
		rows = append(rows, spread(left, right, inside))
	}
	if len(matches) > maxRows {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("  %d of %d commands · ↑/↓ to browse", selected+1, len(matches))))
	}
	return commandPaletteStyle.Width(width).Render(strings.Join(rows, "\n"))
}

func (m Model) connectView(width int) string {
	status := "\n\n" + mutedStyle.Render("PASTE STATUS  Waiting for a connection string")
	if count := len([]rune(m.dsn.Value())); count > 0 {
		status = "\n\n" + goodStyle.Render(fmt.Sprintf("PASTE RECEIVED  %d characters securely captured", count))
	}
	if m.connection != "" {
		status += "\n" + goodStyle.Render("SESSION READY  Live schema context will be added to chat")
	}
	errText := m.errorView()
	panelWidth := min(104, width)
	return renderPanel(panelStyle, panelWidth,
		overlineStyle.Render("DATABASE CONNECTION")+"\n"+
			titleStyle.Render("Add live database context")+"\n\n"+
			mutedStyle.Render("Paste with Cmd+V or Ctrl+V. QueryShift masks the value, keeps it only in memory, and never writes it to .env or history.")+"\n\n"+
			labelStyle.Render("CONNECTION STRING")+"\n"+m.dsn.View()+status+errText)
}

func (m Model) databaseView(width, height int) string {
	if m.workspace == nil {
		return m.workingView("Loading database workspace", "Reading the live catalog")
	}
	names := []string{"Overview", "Schema", "ER map", "Change"}
	mainWidth := width
	var nav string
	if width >= 112 {
		navWidth := 25
		mainWidth = width - navWidth - 3
		var rows []string
		for i, name := range names {
			prefix := "  "
			style := inactiveActionStyle
			if i == m.dbTab {
				prefix = "› "
				style = activeActionStyle
			}
			rows = append(rows, style.Render(fmt.Sprintf("%s%d  %s", prefix, i+1, name)))
		}
		access := goodStyle.Render("READ DEFAULT")
		if m.writeMode {
			access = warnStyle.Render("WRITE ARMED")
		}
		nav = renderPanel(panelStyle, navWidth,
			overlineStyle.Render("WORKSPACE")+"\n\n"+strings.Join(rows, "\n\n")+"\n\n"+rule(navWidth-panelStyle.GetHorizontalFrameSize())+"\n\n"+
				mutedStyle.Render("ACCESS")+"\n"+access+"\n\n"+mutedStyle.Render("Ctrl+W unlocks one\ncontrolled write session."))
	}

	heading := overlineStyle.Render(strings.ToUpper(names[m.dbTab])) + "\n" +
		titleStyle.Render(m.workspace.Database) + mutedStyle.Render("  @  ") + statusValueStyle.Render(m.workspace.Host) + "\n" +
		mutedStyle.Render(m.workspace.Version+"  /  "+strings.ToUpper(m.workspace.Provider))
	var content string
	switch m.dbTab {
	case 0:
		indexes := 0
		columns := 0
		for _, table := range m.workspace.Schema.Tables {
			indexes += len(table.Indexes)
			columns += len(table.Columns)
		}
		inside := mainWidth - panelStyle.GetHorizontalFrameSize()
		if height < 22 {
			stats := fmt.Sprintf("%d tables  ·  %d columns  ·  %d FKs  ·  %d indexes", len(m.workspace.Schema.Tables), columns, len(m.workspace.Schema.Edges), indexes)
			content = renderPanel(panelStyle, mainWidth, statusValueStyle.Render(stats)+"\n\n"+
				labelStyle.Render("i")+" inspect   "+labelStyle.Render("2")+" schema   "+labelStyle.Render("3")+" ER   "+labelStyle.Render("4")+" SQL")
		} else {
			stats := statusRow("TABLES", fmt.Sprint(len(m.workspace.Schema.Tables)), inside, statusValueStyle) + "\n" +
				statusRow("COLUMNS", fmt.Sprint(columns), inside, statusValueStyle) + "\n" +
				statusRow("FOREIGN KEYS", fmt.Sprint(len(m.workspace.Schema.Edges)), inside, statusValueStyle) + "\n" +
				statusRow("INDEXES", fmt.Sprint(indexes), inside, statusValueStyle)
			content = renderPanel(panelStyle, mainWidth, stats+"\n\n"+rule(inside)+"\n\n"+
				labelStyle.Render("i")+"  Run full health inspection\n"+
				labelStyle.Render("2")+"  Browse tables and columns\n"+
				labelStyle.Render("3")+"  Open relationship map\n"+
				labelStyle.Render("4")+"  Review a change or run a query")
		}
	case 1, 2:
		m.viewport.SetWidth(mainWidth - panelStyle.GetHorizontalFrameSize())
		m.viewport.SetHeight(max(4, height-11))
		content = renderPanel(panelStyle, mainWidth, m.viewport.View())
	case 3:
		access := goodStyle.Render("READ MODE")
		editorTitle := "READ QUERY"
		statement := strings.TrimSpace(m.dbSQL.Value())
		if statement != "" && !app.IsReadQuery(statement) {
			editorTitle = "CHANGE PROPOSAL"
			access = warnStyle.Render("REVIEW REQUIRED")
		}
		if m.writeMode {
			access = warnStyle.Render("WRITE MODE ARMED")
		}
		if height < 22 {
			m.dbSQL.SetHeight(4)
			last := strings.ReplaceAll(m.databaseContent(), "\n", "  ")
			lastRunes := []rune(last)
			if len(lastRunes) > max(12, mainWidth-16) {
				last = string(lastRunes[:max(12, mainWidth-17)]) + "…"
			}
			content = renderPanel(panelStyle, mainWidth,
				spread(overlineStyle.Render(editorTitle), access, mainWidth-panelStyle.GetHorizontalFrameSize())+"\n"+
					m.dbSQL.View()+m.errorView()+"\n"+mutedStyle.Render(last))
			break
		}
		outputHeight := max(5, height-lipgloss.Height(m.dbSQL.View())-15)
		m.viewport.SetWidth(mainWidth - panelStyle.GetHorizontalFrameSize())
		m.viewport.SetHeight(outputHeight)
		editor := renderPanel(panelStyle, mainWidth, spread(overlineStyle.Render(editorTitle), access, mainWidth-panelStyle.GetHorizontalFrameSize())+"\n\n"+m.dbSQL.View()+m.errorView())
		output := renderPanel(panelStyle, mainWidth, overlineStyle.Render("LAST RESULT")+"\n\n"+m.viewport.View())
		content = editor + "\n" + output
	}
	main := heading + "\n\n" + content
	if nav != "" {
		return lipgloss.JoinHorizontal(lipgloss.Top, nav, "   ", main)
	}
	var tabs []string
	for i, name := range names {
		if width < 90 {
			name = []string{"Home", "Schema", "ER", "Change"}[i]
		}
		if i == m.dbTab {
			tabs = append(tabs, activeTabStyle.Render(fmt.Sprintf("%d %s", i+1, name)))
		} else {
			tabs = append(tabs, tabStyle.Render(fmt.Sprintf("%d %s", i+1, name)))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Center, tabs...) + "\n\n" + main
}

func (m Model) writeUnlockView(width int) string {
	return renderPanel(dangerPanelStyle, min(78, width),
		overlineStyle.Render("WRITE MODE")+"\n"+
			titleStyle.Render("Unlock controlled database changes")+"\n\n"+
			mutedStyle.Render("This enables one-at-a-time DDL and table-value changes. Before execution, QueryShift captures the live schema and writes a checksummed migration artifact. Server, database, and role administration remain blocked.")+"\n\n"+
			warnStyle.Render("Type WRITE to unlock")+"\n"+m.confirm.View()+m.errorView())
}

func (m Model) changeConfirmView(width int) string {
	statement := strings.TrimSpace(m.changeContract.Statement)
	if len([]rune(statement)) > 900 {
		statement = string([]rune(statement)[:900]) + "…"
	}
	risk := goodStyle.Render(strings.ToUpper(m.changeContract.Risk.Level))
	if m.changeContract.Risk.Level == "elevated" || m.changeContract.Risk.Level == "high" {
		risk = warnStyle.Render(strings.ToUpper(m.changeContract.Risk.Level))
	} else if m.changeContract.Risk.Level == "critical" {
		risk = errorStyle.Render("CRITICAL")
	}
	objects := "none detected"
	if len(m.changeContract.AffectedObjects) > 0 {
		parts := make([]string, 0, len(m.changeContract.AffectedObjects))
		for _, object := range m.changeContract.AffectedObjects {
			parts = append(parts, object.Operation+" "+object.Name)
		}
		objects = strings.Join(parts, "  ·  ")
	}
	verification := make([]string, 0, len(m.changeContract.Verification))
	for _, probe := range m.changeContract.Verification {
		verification = append(verification, probe.ID+" "+probe.Description)
	}
	contractIdentity := m.changeContract.ID
	if len(m.changeContract.ContractSHA256) >= 12 {
		contractIdentity += "  ·  sha256:" + m.changeContract.ContractSHA256[:12]
	}
	runStatus := "NOT INITIALIZED"
	policyLine := "policy unavailable"
	if m.changeRun != nil {
		runStatus = strings.ToUpper(strings.ReplaceAll(m.changeRun.Status, "_", " "))
		policyLine = m.changeRun.Policy.Name + "  ·  " + m.changeRun.Decision.Outcome
	}
	return renderPanel(dangerPanelStyle, min(100, width),
		spread(overlineStyle.Render("CHANGE REVIEW"), risk, min(100, width)-dangerPanelStyle.GetHorizontalFrameSize())+"\n"+
			titleStyle.Render(m.changePlan.Summary)+"\n\n"+
			codeStyle.Render(statement)+"\n\n"+
			labelStyle.Render("IMMUTABLE CHANGE CONTRACT")+"\n"+
			statusValueStyle.Render(contractIdentity)+"\n"+
			mutedStyle.Render("Target "+m.changeContract.Target.Database+"  ·  schema "+shortDigest(m.changeContract.SchemaSHA256))+"\n"+
			mutedStyle.Render("Affected  "+objects)+"\n\n"+
			labelStyle.Render("VERIFIED CHANGE RUN")+"\n"+
			statusValueStyle.Render(runStatus)+"\n"+
			mutedStyle.Render(policyLine)+"\n"+
			mutedStyle.Render("ctrl+r rehearse on disposable target  ·  ctrl+a create signed approval from environment identity")+"\n\n"+
			labelStyle.Render("RISK & RECOVERY")+"\n"+
			mutedStyle.Render("Pre-change schema snapshot  ·  checksummed SQL migration  ·  one transaction")+"\n"+
			statusValueStyle.Render(strings.ToUpper(m.changePlan.RecoveryLevel))+"\n"+
			mutedStyle.Render(m.changePlan.RecoveryNote)+"\n"+
			mutedStyle.Render(m.changeContract.Risk.LockImpact+"  ·  "+m.changeContract.Risk.DataImpact)+"\n\n"+
			labelStyle.Render("REQUIRED VERIFICATION")+"\n"+
			mutedStyle.Render(strings.Join(verification, "\n"))+"\n\n"+
			mutedStyle.Render("30s statement timeout  ·  3s lock timeout  ·  automatic rollback on execution failure")+"\n\n"+
			warnStyle.Render("Type "+m.changePlan.RequiredConfirmation+" to commit")+"\n"+m.confirm.View()+m.errorView())
}

func shortDigest(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func (m Model) modernizerView(width int) string {
	live := "off"
	if m.liveSchema {
		live = "on"
	}
	connection := "not connected"
	if m.connection != "" {
		connection = "connected"
	}
	meta := statusValueStyle.Render(strings.ToUpper(dialects[m.dialect])) + mutedStyle.Render(" source  /  ") +
		statusValueStyle.Render("POSTGRES") + mutedStyle.Render(" target  /  live schema ") +
		statusValueStyle.Render(live) + mutedStyle.Render("  /  ") + statusValueStyle.Render(connection)
	editor := renderPanel(panelStyle, width, overlineStyle.Render("LEGACY SQL INPUT")+"\n\n"+m.sql.View()+m.errorView())
	return overlineStyle.Render("FIVE-AGENT WORKFLOW") + "\n" +
		titleStyle.Render("Enterprise Query Modernizer") + "\n" + meta + "\n\n" + editor
}

func (m Model) modernizerResultView() string {
	names := []string{"Modernized SQL", "Agent Trace", "Audit & Diff", "Index Advisory"}
	parts := make([]string, len(names))
	for index, name := range names {
		if index == m.resultTab {
			parts[index] = activeTabStyle.Render(name)
		} else {
			parts[index] = tabStyle.Render(name)
		}
	}
	summary := ""
	if m.modern != nil {
		summary = statusValueStyle.Render(fmt.Sprintf("%d/100 confidence", m.modern.ConfidenceScore)) +
			mutedStyle.Render("  /  ") + statusValueStyle.Render(strings.ToUpper(m.modern.RiskLevel)+" RISK") + "\n"
	}
	return overlineStyle.Render("MODERNIZATION RESULT") + "\n" + summary +
		lipgloss.JoinHorizontal(lipgloss.Center, parts...) + "\n\n" + m.viewport.View()
}

func (m Model) modernizingView(width int) string {
	panelWidth := min(108, max(58, width))
	innerWidth := panelWidth - panelStyle.GetHorizontalFrameSize()

	var b strings.Builder
	b.WriteString(animatedScanner(m.pulse, 8) + "  " + titleStyle.Render("Agent workflow running"))
	b.WriteString("\n" + mutedStyle.Render("Mapper → transpiler → validator → optimizer → equivalence judge"))
	b.WriteString("\n\n" + overlineStyle.Render("LIVE EXECUTION TRACE") + "\n")

	events := m.agentRun
	if len(events) > 9 {
		events = events[len(events)-9:]
	}
	if len(events) == 0 {
		b.WriteString(mutedStyle.Render("Waiting for the Bedrock agent runtime…"))
	} else {
		for _, event := range events {
			icon, status := agentEventStatus(event.Kind)
			stage := event.Stage
			if stage == "" {
				stage = "workflow"
			}
			line := fmt.Sprintf("%s  %-20s  %s", icon, stage, status)
			if duration := numberData(event.Data, "duration_ms"); duration > 0 {
				line += fmt.Sprintf("  %dms", int(duration))
			}
			if event.TransportAttempt > 1 {
				line += fmt.Sprintf("  retry %d", event.TransportAttempt-1)
			}
			b.WriteString("\n" + fit(line, max(24, innerWidth)))
			if category := stringData(event.Data, "error_category"); category != "" {
				b.WriteString("\n" + mutedStyle.Render("   "+fit(category, max(20, innerWidth-3))))
			}
		}
	}

	b.WriteString("\n\n" + warnStyle.Render("Esc cancels safely") + mutedStyle.Render("  ·  prompts and credentials are never shown in this trace"))
	return renderPanel(panelStyle, panelWidth, b.String())
}

func agentEventStatus(kind string) (string, string) {
	switch kind {
	case "stage_started":
		return "●", "RUNNING"
	case "stage_completed":
		return "✓", "DONE"
	case "retry_scheduled":
		return "↻", "RETRYING"
	case "stage_failed", "run_failed":
		return "×", "FAILED"
	case "fallback_applied":
		return "!", "FALLBACK"
	case "run_completed":
		return "✓", "COMPLETE"
	default:
		return "·", strings.ToUpper(strings.ReplaceAll(kind, "_", " "))
	}
}

func stringData(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}

func numberData(data map[string]any, key string) float64 {
	value, _ := data[key].(float64)
	return value
}

func (m Model) databaseLoadingView(width int) string {
	panelWidth := min(104, max(58, width))
	innerWidth := panelWidth - panelStyle.GetHorizontalFrameSize()

	elapsed := time.Duration(0)
	if !m.loadSince.IsZero() {
		elapsed = time.Since(m.loadSince)
	}
	timerStr := fmt.Sprintf("%02d:%02d", int(elapsed.Minutes()), int(elapsed.Seconds())%60)

	var b strings.Builder
	stageText := m.loadStage
	if stageText == "" {
		stageText = "CONNECTING"
	}
	title := "Connecting to database"
	if m.workspace != nil {
		title = "Refreshing database workspace"
	}

	headerLeft := animatedScanner(m.pulse, 8) + "  " + titleStyle.Render(title) + "  " + badgeStyle.Render(strings.ToUpper(stageText))
	headerRight := mutedStyle.Render(timerStr)
	b.WriteString(spread(headerLeft, headerRight, innerWidth))
	b.WriteString("\n" + mutedStyle.Render("Establishing read-only connection, probing capabilities, and introspecting schema"))

	if m.loadHostHint != "" {
		b.WriteString("\n" + subtitleStyle.Render("Target: "+fit(m.loadHostHint, max(24, innerWidth-8))))
	}

	b.WriteString("\n\n" + overlineStyle.Render("LIVE CONNECTION LOGS") + "\n")

	logs := m.loadLogs
	if len(logs) > 8 {
		logs = logs[len(logs)-8:]
	}
	if len(logs) == 0 {
		b.WriteString(mutedStyle.Render("Initializing connection…"))
	} else {
		for i, logLine := range logs {
			isLast := (i == len(logs)-1)
			var icon string
			if isLast {
				icon = goodStyle.Render("●")
			} else {
				icon = lipgloss.NewStyle().Foreground(colorCyan).Render("✓")
			}
			line := fmt.Sprintf("%s  %s", icon, logLine)
			b.WriteString("\n" + fit(line, max(24, innerWidth)))
		}
	}

	hostLower := strings.ToLower(m.loadHostHint + " " + m.connection)
	isServerless := strings.Contains(hostLower, "neon.tech") || strings.Contains(hostLower, "pooler") || strings.Contains(hostLower, "supabase") || strings.Contains(hostLower, "cockroach")
	if isServerless && elapsed >= 12*time.Second {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(colorAmber).Render("⚠ No response after the normal serverless wake-up window. QueryShift will stop automatically at 30s."))
	} else if elapsed >= 3*time.Second || isServerless {
		b.WriteString("\n\n" + lipgloss.NewStyle().Foreground(colorAmber).Render("⚡ Serverless database detected. If compute was suspended, wake-up takes 3–12s."))
	}

	b.WriteString("\n\n" + warnStyle.Render("Esc cancels safely") + mutedStyle.Render("  ·  connection string is preserved"))
	return renderPanel(panelStyle, panelWidth, b.String())
}

func (m Model) workingView(title, detail string) string {
	return renderPanel(panelStyle, min(88, max(50, m.shellWidth())),
		animatedScanner(m.pulse, 8)+"  "+titleStyle.Render(title)+"\n\n"+mutedStyle.Render(detail)+"\n\n"+warnStyle.Render("Esc cancels this run safely."))
}

func (m Model) aboutView(width int) string {
	copy := titleStyle.Render("A safe interface over real QueryShift workflows") + "\n\n" +
		"Schema discovery, ER generation, inspection, and SELECT queries use short READ ONLY transactions. Database changes use a separate connection, one statement, one transaction, strict timeouts, an explicit WRITE unlock, and a per-statement confirmation. Every approved change also creates a local checksummed migration plus before/after schema snapshots. The modernizer is a bounded five-stage workflow using Amazon Bedrock as its model provider.\n\n" +
		labelStyle.Render("Workflow") + "\nSchema mapper → dialect transpiler → AST validator + repair loop → optimizer → equivalence judge\n\n" +
		labelStyle.Render("Safety") + "\nRead access remains the default. Changes that overwrite or remove data require explicit backup acknowledgement. Multi-statement batches and server/role administration are rejected. Credentials remain in process memory."
	return renderPanel(panelStyle, min(104, width), copy)
}

func (m Model) errorView() string {
	if m.err == nil {
		return ""
	}
	return "\n\n" + errorStyle.Render("Error: "+m.err.Error())
}

func (m Model) footer(width int) string {
	var help string
	switch m.screen {
	case screenHome:
		if m.chatBusy {
			help = helpKeys("esc", "interrupt", "f1", "help", "ctrl+c", "quit")
		} else {
			help = helpKeys("pgup/pgdn", "history", "f1", "help", "ctrl+c", "quit")
		}
	case screenConnect:
		help = helpKeys("cmd/ctrl+v", "paste", "ctrl+e", "from env", "enter", "connect", "f1", "help", "esc", "back")
	case screenInspecting, screenModernizing, screenDatabaseLoading, screenApplying, screenRehearsing:
		help = helpKeys("esc", "cancel", "f1", "help", "ctrl+c", "quit")
	case screenReport, screenEvidence:
		help = helpKeys("↑/↓", "scroll", "pgup/pgdn", "page", "f1", "help", "esc", "home")
	case screenModernizer:
		help = helpKeys("ctrl+d", "dialect", "ctrl+l", "live schema", "ctrl+r", "run agents", "f1", "help", "esc", "home")
	case screenModernizerResult:
		help = helpKeys("tab/←/→", "change tab", "↑/↓", "scroll", "f1", "help", "esc", "edit")
	case screenAbout:
		help = helpKeys("f1", "help", "esc", "home")
	case screenDatabase:
		help = helpKeys("1-4/tab", "panels", "ctrl+r", "run/refresh", "ctrl+w", "write unlock", "i", "inspect", "f1", "help")
	case screenWriteUnlock:
		help = helpKeys("enter", "confirm", "esc", "cancel", "f1", "help")
	case screenChangeConfirm:
		help = helpKeys("ctrl+r", "rehearse", "ctrl+a", "approve", "enter", "apply", "esc", "cancel")
	}
	if width < 110 && !m.helpOpen {
		switch m.screen {
		case screenHome:
			if m.chatBusy {
				help = helpKeys("esc", "interrupt", "f1", "help")
			} else {
				help = helpKeys("f1", "help", "ctrl+c", "quit")
			}
		case screenConnect:
			help = helpKeys("ctrl+v", "paste", "enter", "connect", "esc", "back")
		case screenModernizer:
			help = helpKeys("ctrl+r", "run", "f1", "help", "esc", "back")
		case screenReport, screenEvidence, screenModernizerResult:
			help = helpKeys("↑/↓", "scroll", "f1", "help", "esc", "back")
		case screenDatabase:
			if m.dbTab == 3 {
				if width < 90 {
					help = helpKeys("ctrl+r", "run", "ctrl+w", "unlock")
				} else {
					help = helpKeys("ctrl+r", "run or review", "ctrl+w", "unlock", "esc", "chat")
				}
			} else {
				help = helpKeys("1-4", "panels", "ctrl+r", "refresh", "f1", "help")
			}
		}
	}
	if m.helpOpen {
		help = helpKeys("esc", "close help")
	}
	section := mutedStyle.Render("QUERYSHIFT") + "  /  " + statusValueStyle.Render(screenName(m.screen))
	return rule(width) + "\n" + spread(section, help, width) + "\n"
}

func helpKeys(items ...string) string {
	var parts []string
	for index := 0; index+1 < len(items); index += 2 {
		parts = append(parts, keyStyle.Render(items[index])+" "+items[index+1])
	}
	return strings.Join(parts, "   ")
}

func (m Model) bootView(width int) string {
	logoWidth := min(68, width)
	gap := max(0, 8-min(8, m.bootStep))
	logo := bootQueryStyle.Render("QUERY") + strings.Repeat(" ", gap) + bootShiftStyle.Render("SHIFT")
	pulseFrames := []string{"·  ·  ●  ·  ·", "·  ●  ·  ·  ·", "●  ·  ·  ·  ·", "·  ●  ·  ·  ·", "·  ·  ●  ·  ·", "·  ·  ·  ●  ·", "·  ·  ·  ·  ●", "·  ·  ·  ●  ·"}
	pulse := pulseFrames[m.bootStep%len(pulseFrames)]
	state := "shifting queries into focus"
	if m.bootStep >= 10 {
		state = "ready"
	}
	return lipgloss.NewStyle().Width(logoWidth).Align(lipgloss.Center).Render(
		logo + "\n\n" + goodStyle.Render(pulse) + "\n\n" + mutedStyle.Render(state),
	)
}

func (m Model) helpView(width int) string {
	panelWidth := min(108, width)
	inside := panelWidth - panelStyle.GetHorizontalFrameSize()
	if panelWidth < 90 {
		content := overlineStyle.Render("KEYBOARD REFERENCE") + "\n" +
			titleStyle.Render("One conversation, a few shortcuts") + "\n\n" +
			helpRow("Enter", "Send from the composer", inside) + "\n" +
			helpRow("Ctrl+J", "Insert a new line", inside) + "\n" +
			helpRow("Ctrl+P / /", "Open command palette", inside) + "\n" +
			helpRow("/help", "Open this help", inside) + "\n" +
			helpRow("↑ / ↓", "Choose a command", inside) + "\n" +
			helpRow("PgUp / PgDn", "Browse conversation", inside) + "\n" +
			helpRow("Cmd/Ctrl+V", "Paste connection", inside) + "\n" +
			helpRow("Ctrl+D / L / R", "Dialect / live / run", inside) + "\n" +
			helpRow("F1 / Ctrl+H", "Toggle help", inside) + "\n\n" +
			mutedStyle.Render("Read by default. Changes require WRITE plus a per-statement confirmation.")
		return renderPanel(panelStyle, panelWidth, content)
	}
	leftWidth := (inside - 3) / 2
	left := overlineStyle.Render("CHAT") + "\n" +
		helpRow("Enter", "Send from the composer", leftWidth) + "\n" +
		helpRow("Ctrl+J", "Insert a new line", leftWidth) + "\n" +
		helpRow("Ctrl+P / /", "Open command palette", leftWidth) + "\n" +
		helpRow("/help", "Open this help", leftWidth) + "\n" +
		helpRow("↑ / ↓", "Choose a command", leftWidth) + "\n" +
		helpRow("PgUp / PgDn", "Browse conversation", leftWidth) + "\n" +
		helpRow("Esc", "Go back or cancel", leftWidth) + "\n" +
		helpRow("F1 / Ctrl+H", "Toggle this help", leftWidth) + "\n\n" +
		overlineStyle.Render("COMMANDS") + "\n" +
		helpRow("/connect", "Verify a live database", leftWidth) + "\n" +
		helpRow("/schema /erd", "Browse live structure", leftWidth) + "\n" +
		helpRow("/query /inspect", "Work with the database", leftWidth) + "\n" +
		helpRow("/modernize", "Modernize with live schema", leftWidth) + "\n" +
		helpRow("/new /clear", "Reset the conversation", leftWidth) + "\n\n" +
		overlineStyle.Render("DATABASE") + "\n" +
		helpRow("Cmd/Ctrl+V", "Paste connection URL", leftWidth) + "\n" +
		helpRow("Ctrl+E", "Load DATABASE_URL", leftWidth)
	right := overlineStyle.Render("MODERNIZER") + "\n" +
		helpRow("Ctrl+D", "Change source dialect", leftWidth) + "\n" +
		helpRow("Ctrl+L", "Toggle live schema", leftWidth) + "\n" +
		helpRow("Ctrl+R", "Run the agent pipeline", leftWidth) + "\n" +
		helpRow("Tab / ← / →", "Change result tab", leftWidth) + "\n\n" +
		overlineStyle.Render("DATABASE WORKSPACE") + "\n" +
		helpRow("1 / 2 / 3 / 4", "Overview / schema / ER / change", leftWidth) + "\n" +
		helpRow("Ctrl+R", "Run read or review change", leftWidth) + "\n" +
		helpRow("Ctrl+W", "Unlock write mode", leftWidth) + "\n" +
		helpRow("i", "Run health inspection", leftWidth) + "\n\n" +
		overlineStyle.Render("SAFETY") + "\n" +
		mutedStyle.Render("Read-only by default. Every write is previewed\nand confirmed; destructive changes use a stronger token.")
	content := overlineStyle.Render("KEYBOARD REFERENCE") + "\n" +
		titleStyle.Render("One conversation, a few shortcuts") + "\n\n" +
		lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right) + "\n\n" +
		mutedStyle.Render("Press Esc to return to "+screenName(m.screen)+".")
	return renderPanel(panelStyle, panelWidth, content)
}

func helpRow(key, description string, width int) string {
	return spread(keyStyle.Render(key), mutedStyle.Render(description), width)
}

func (m Model) modelName() string {
	if configured := firstEnvironment("QUERYSHIFT_AI_MODEL", "QUERYSHIFT_AGENT_MODEL"); configured != "" {
		return configured
	}
	return "deepseek.v3.2"
}

func (m Model) evidenceContent() string {
	if len(m.chatEvidence) == 0 {
		return "No answer evidence is available."
	}
	var sections []string
	for _, item := range m.chatEvidence {
		heading := item.ID
		if heading == "" {
			heading = strings.ToUpper(item.Kind)
		}
		meta := item.Source
		if !item.ObservedAt.IsZero() {
			meta += " · " + item.ObservedAt.Local().Format("2006-01-02 15:04:05")
		}
		sections = append(sections, labelStyle.Render(heading+"  "+item.Summary)+"\n"+mutedStyle.Render(meta)+"\n\n"+item.Content)
	}
	return strings.Join(sections, "\n\n"+rule(max(24, m.viewport.Width()))+"\n\n")
}

func chatPhase(elapsed time.Duration, collectingEvidence bool) string {
	switch {
	case collectingEvidence && elapsed < 8*time.Second:
		return "Collecting read-only evidence"
	case elapsed >= 8*time.Second:
		return "Composing response"
	case elapsed >= 2*time.Second:
		return "Reasoning with live context"
	default:
		return "Grounding in schema"
	}
}

// animatedScanner is a compact, bidirectional activity signal. The bright
// head and two-cell trail stay legible without the visual jitter of a spinner.
func animatedScanner(step, width int) string {
	if width <= 0 {
		return ""
	}
	positionAt := func(value int) int {
		if width == 1 {
			return 0
		}
		cycle := 2*width - 2
		position := value % cycle
		if position < 0 {
			position += cycle
		}
		if position >= width {
			position = cycle - position
		}
		return position
	}

	head := positionAt(step)
	trailOne := positionAt(step - 1)
	trailTwo := positionAt(step - 2)
	var b strings.Builder
	for index := 0; index < width; index++ {
		switch index {
		case head:
			b.WriteString(goodStyle.Render("█"))
		case trailOne:
			b.WriteString(lipgloss.NewStyle().Foreground(colorCyan).Render("▓"))
		case trailTwo:
			b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("▒"))
		default:
			b.WriteString(lipgloss.NewStyle().Foreground(colorRule).Render("░"))
		}
	}
	return b.String()
}

func agentLoader(step int) string {
	frames := []string{"◜", "◝", "◞", "◟"}
	orbit := lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render(frames[step%len(frames)])
	return orbit + " " + animatedScanner(step, 10)
}

func renderPanel(style lipgloss.Style, width int, content string) string {
	// Lip Gloss Width is the final block width before margins; padding and
	// borders are already included. Callers separately subtract the frame only
	// when laying out content inside the panel.
	return style.Width(max(1, width)).Render(content)
}

func spread(left, right string, width int) string {
	space := max(1, width-lipgloss.Width(left)-lipgloss.Width(right))
	return left + strings.Repeat(" ", space) + right
}

func fit(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		runes := []rune(value)
		for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
			runes = runes[:len(runes)-1]
		}
		return string(runes) + "…"
	}
	return value + strings.Repeat(" ", width-lipgloss.Width(value))
}

func fitCompactHost(host string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(host) <= width {
		return host
	}
	runes := []rune(host)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func windowLines(value string, limit int, scroll int) string {
	if limit <= 0 {
		return ""
	}
	lines := strings.Split(value, "\n")
	if len(lines) <= limit {
		return value
	}
	scroll = max(0, scroll)
	end := len(lines) - scroll
	if end < limit {
		end = limit
	}
	if end > len(lines) {
		end = len(lines)
	}
	start := max(0, end-limit)
	return strings.Join(lines[start:end], "\n")
}

func tailLines(value string, limit int) string {

	lines := strings.Split(value, "\n")
	if len(lines) <= limit {
		return value
	}
	return strings.Join(lines[len(lines)-limit:], "\n")
}

func statusRow(name, value string, width int, valueStyle lipgloss.Style) string {
	return spread(statusNameStyle.Render(name), valueStyle.Render(value), width)
}

func rule(width int) string {
	return lipgloss.NewStyle().Foreground(colorRule).Render(strings.Repeat("─", max(1, width)))
}

func firstEnvironment(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func splitEnvironmentList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func screenName(value screen) string {
	switch value {
	case screenConnect:
		return "CONNECT"
	case screenInspecting:
		return "INSPECTING"
	case screenReport:
		return "DATABASE REPORT"
	case screenModernizer:
		return "MODERNIZER"
	case screenModernizing:
		return "AGENT RUN"
	case screenModernizerResult:
		return "RESULTS"
	case screenAbout:
		return "HOW IT WORKS"
	case screenDatabaseLoading:
		return "CONNECTING"
	case screenDatabase:
		return "DATABASE WORKSPACE"
	case screenWriteUnlock:
		return "WRITE UNLOCK"
	case screenChangeConfirm:
		return "CHANGE PREVIEW"
	case screenApplying:
		return "APPLYING CHANGE"
	case screenRehearsing:
		return "REHEARSING CHANGE"
	case screenEvidence:
		return "EVIDENCE"
	default:
		return "HOME"
	}
}
