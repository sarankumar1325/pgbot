package app

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
)

type ChatToolName string

const (
	ChatToolHealthInspection   ChatToolName = "health_inspection"
	ChatToolWorkloadActivity   ChatToolName = "workload_activity"
	ChatToolQueryActivity      ChatToolName = "query_activity"
	ChatToolChangeHistory      ChatToolName = "change_history"
	ChatToolSchemaSearch       ChatToolName = "schema_search"
	ChatToolSchemaDependencies ChatToolName = "schema_dependencies"
)

// ChatToolSpec is the shared user-facing contract for a read-only agent tool.
// The TUI uses it for activity copy; the app layer owns selection and evidence
// so CLI and MCP surfaces can reuse the same behavior later.
type ChatToolSpec struct {
	Name       ChatToolName
	Source     string
	Activity   string
	Permission string
}

var chatToolRegistry = map[ChatToolName]ChatToolSpec{
	ChatToolHealthInspection: {
		Name: ChatToolHealthInspection, Source: "queryshift.inspect",
		Activity: "Inspecting health, workload, locks, vacuum, and indexes", Permission: "read-only database",
	},
	ChatToolWorkloadActivity: {
		Name: ChatToolWorkloadActivity, Source: "queryshift.inspect.activity",
		Activity: "Inspecting active sessions, waits, and blockers", Permission: "read-only database",
	},
	ChatToolQueryActivity: {
		Name: ChatToolQueryActivity, Source: "queryshift.inspect.queries",
		Activity: "Inspecting normalized query activity", Permission: "read-only database",
	},
	ChatToolChangeHistory: {
		Name: ChatToolChangeHistory, Source: "queryshift.inspect.history",
		Activity: "Comparing observed database changes", Permission: "read-only database",
	},
	ChatToolSchemaSearch: {
		Name: ChatToolSchemaSearch, Source: "queryshift.schema.search",
		Activity: "Searching the live schema", Permission: "workspace snapshot",
	},
	ChatToolSchemaDependencies: {
		Name: ChatToolSchemaDependencies, Source: "queryshift.schema.dependencies",
		Activity: "Tracing schema dependencies", Permission: "workspace snapshot",
	},
}

func ChatToolDefinition(name ChatToolName) (ChatToolSpec, bool) {
	spec, ok := chatToolRegistry[name]
	return spec, ok
}

// PlanChatTools deterministically selects read-only evidence collectors. It is
// intentionally conservative: ordinary discussion does not wake a database or
// collect workload data.
func PlanChatTools(question string, workspace *DatabaseWorkspace) []ChatToolName {
	if workspace == nil {
		return nil
	}
	lower := strings.ToLower(strings.TrimSpace(question))
	var tools []ChatToolName
	if containsChatSignal(lower, []string{
		"why is the database slow", "why is db slow", "performance", "database health", "health of the database", "database's health", "db health",
		"health check", "missing index", "unused index", "index health",
		"vacuum", "bloat", "replication lag", "connection pressure", "too many connections",
		"database latency", "throughput",
	}) {
		tools = append(tools, ChatToolHealthInspection)
	}
	if containsChatSignal(lower, []string{
		"blocking query", "blocked query", "deadlock", "lock wait", "wait event",
		"active session", "long running", "long-running", "idle in transaction", "who is connected",
	}) {
		tools = append(tools, ChatToolWorkloadActivity)
	}
	if containsChatSignal(lower, []string{
		"top query", "top queries", "slow query", "slow queries", "query latency",
		"expensive query", "most called", "pg_stat_statements", "wal heavy query",
	}) {
		tools = append(tools, ChatToolQueryActivity)
	}
	if containsChatSignal(lower, []string{
		"what changed", "recent changes", "change history", "schema history", "since last",
		"database drift", "stats reset", "server restarted",
	}) {
		tools = append(tools, ChatToolChangeHistory)
	}
	if containsChatSignal(lower, []string{
		"what depends on", "depends on", "dependency", "dependencies", "relationship",
		"references ", "referenced by", "foreign key", "impact of changing", "impact of dropping",
	}) {
		tools = append(tools, ChatToolSchemaDependencies)
	}
	if containsChatSignal(lower, []string{
		"describe table", "describe the table", "show table", "find table", "what tables",
		"which table", "what columns", "which columns", "columns in", "schema for", "index on",
	}) || strings.Contains(lower, "@") {
		tools = append(tools, ChatToolSchemaSearch)
	}
	return tools
}

// PlanChatToolsWithHistory resolves short follow-ups such as "do it", "yes",
// or "run that" against the recent conversation. Tool planning must not lose
// the user's intent merely because the confirmation itself contains no database
// nouns.
func PlanChatToolsWithHistory(question string, history []ChatTurn, workspace *DatabaseWorkspace) []ChatToolName {
	tools := PlanChatTools(question, workspace)
	if len(tools) > 0 || workspace == nil || !isToolContinuation(question) {
		return tools
	}
	start := maxInt(0, len(history)-6)
	for index := len(history) - 1; index >= start; index-- {
		text := strings.TrimSpace(history[index].Text)
		if text == "" {
			continue
		}
		if inherited := PlanChatTools(text, workspace); len(inherited) > 0 {
			return inherited
		}
	}
	return nil
}

func isToolContinuation(question string) bool {
	value := strings.ToLower(strings.TrimSpace(question))
	value = strings.Trim(value, " .!?\t\n\r")
	for _, phrase := range []string{
		"do it", "do it yourself", "do it yourseld", "run it", "run that", "check it",
		"go ahead", "proceed", "please do", "yes", "yep", "yeah", "yess", "sure", "okay", "ok",
	} {
		if value == phrase || strings.HasPrefix(value, phrase+" ") {
			return true
		}
	}
	return false
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func containsChatSignal(value string, signals []string) bool {
	for _, signal := range signals {
		if strings.Contains(value, signal) {
			return true
		}
	}
	return false
}

// WorkspaceChatEvidence executes a snapshot-only tool. It never opens a
// connection and never reads table values.
func WorkspaceChatEvidence(name ChatToolName, question string, workspace *DatabaseWorkspace) (ChatEvidence, error) {
	if workspace == nil {
		return ChatEvidence{}, fmt.Errorf("a verified workspace is required")
	}
	spec, ok := ChatToolDefinition(name)
	if !ok || isInspectionChatTool(name) {
		return ChatEvidence{}, fmt.Errorf("unsupported workspace tool %q", name)
	}
	switch name {
	case ChatToolSchemaSearch:
		return schemaSearchEvidence(spec, question, workspace), nil
	case ChatToolSchemaDependencies:
		return schemaDependencyEvidence(spec, question, workspace), nil
	default:
		return ChatEvidence{}, fmt.Errorf("unsupported workspace tool %q", name)
	}
}

// IsInspectionChatTool reports whether a tool needs one live read-only
// inspection. Callers can coalesce several such tools into a single snapshot.
func IsInspectionChatTool(name ChatToolName) bool { return isInspectionChatTool(name) }

func isInspectionChatTool(name ChatToolName) bool {
	switch name {
	case ChatToolHealthInspection, ChatToolWorkloadActivity, ChatToolQueryActivity, ChatToolChangeHistory:
		return true
	default:
		return false
	}
}

func schemaSearchEvidence(spec ChatToolSpec, question string, workspace *DatabaseWorkspace) ChatEvidence {
	terms := chatSearchTerms(question)
	type match struct {
		score int
		text  string
	}
	var matches []match
	for _, table := range workspace.Schema.Tables {
		qualified := table.Schema + "." + table.Name
		searchable := strings.ToLower(qualified)
		var details []string
		for _, column := range table.Columns {
			label := column.Name + " " + column.Type
			if column.PK {
				label += " PK"
			}
			if column.FKTarget != "" {
				label += " FK→" + column.FKTarget
			}
			details = append(details, label)
			searchable += " " + strings.ToLower(column.Name)
		}
		for _, index := range table.Indexes {
			searchable += " " + strings.ToLower(index.Name+" "+index.Def)
		}
		score := matchTerms(searchable, strings.ToLower(table.Name), terms)
		if score == 0 && len(terms) > 0 {
			continue
		}
		if len(details) > 16 {
			details = append(details[:16], fmt.Sprintf("… %d more columns", len(details)-16))
		}
		matches = append(matches, match{score: score, text: qualified + "(" + strings.Join(details, ", ") + ")"})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		return matches[i].text < matches[j].text
	})
	if len(matches) > 20 {
		matches = matches[:20]
	}
	lines := make([]string, len(matches))
	for index := range matches {
		lines[index] = "- " + matches[index].text
	}
	if len(lines) == 0 {
		lines = []string{"No matching visible tables, columns, or indexes were found."}
	}
	return ChatEvidence{
		Kind: string(spec.Name), Source: spec.Source, ObservedAt: time.Now(),
		Summary: fmt.Sprintf("Searched live schema · %d matching tables", len(matches)),
		Content: strings.Join(lines, "\n"),
	}
}

func schemaDependencyEvidence(spec ChatToolSpec, question string, workspace *DatabaseWorkspace) ChatEvidence {
	lower := strings.ToLower(question)
	selected := map[string]bool{}
	for _, table := range workspace.Schema.Tables {
		qualified := strings.ToLower(table.Schema + "." + table.Name)
		if strings.Contains(lower, qualified) || strings.Contains(lower, strings.ToLower(table.Name)) {
			selected[strings.ToLower(table.Name)] = true
			selected[qualified] = true
		}
	}
	var lines []string
	for _, edge := range workspace.Schema.Edges {
		from := strings.ToLower(edge.FromTable)
		to := strings.ToLower(edge.ToTable)
		if len(selected) > 0 && !selected[from] && !selected[to] {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s.%s → %s.%s", edge.FromTable, edge.FromColumn, edge.ToTable, edge.ToColumn))
	}
	sort.Strings(lines)
	if len(lines) > 100 {
		lines = append(lines[:100], fmt.Sprintf("- … %d more relationships", len(lines)-100))
	}
	count := len(lines)
	if count == 0 {
		lines = []string{"No matching visible foreign-key relationships were found."}
	}
	return ChatEvidence{
		Kind: string(spec.Name), Source: spec.Source, ObservedAt: time.Now(),
		Summary: fmt.Sprintf("Traced schema dependencies · %d relationships", count),
		Content: strings.Join(lines, "\n"),
	}
}

func chatSearchTerms(question string) []string {
	stop := map[string]bool{
		"a": true, "an": true, "and": true, "about": true, "describe": true, "find": true,
		"for": true, "in": true, "is": true, "me": true, "of": true, "on": true, "show": true,
		"schema": true, "table": true, "tables": true, "the": true, "what": true, "which": true,
		"with": true,
	}
	fields := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.')
	})
	seen := map[string]bool{}
	var terms []string
	for _, field := range fields {
		field = strings.TrimPrefix(field, "@")
		if len(field) < 2 || stop[field] || seen[field] {
			continue
		}
		seen[field] = true
		terms = append(terms, field)
	}
	return terms
}

func matchTerms(searchable, tableName string, terms []string) int {
	if len(terms) == 0 {
		return 1
	}
	score := 0
	for _, term := range terms {
		if tableName == term {
			score += 8
		} else if strings.Contains(searchable, term) {
			score += 2
		}
	}
	return score
}

// InspectionChatEvidence converts the deterministic inspector result into a
// bounded model input. The report contains no connection string or credentials.
func InspectionChatEvidence(inspection *Inspection) ChatEvidence {
	return InspectionChatEvidenceForTool(ChatToolHealthInspection, inspection)
}

// InspectionChatEvidenceForTool projects one bounded evidence view from a
// shared deterministic inspection. This prevents one user question from
// opening several redundant database collections.
func InspectionChatEvidenceForTool(name ChatToolName, inspection *Inspection) ChatEvidence {
	if inspection == nil {
		return ChatEvidence{}
	}
	spec, ok := ChatToolDefinition(name)
	if !ok || !isInspectionChatTool(name) {
		return ChatEvidence{}
	}
	if name == ChatToolWorkloadActivity {
		return workloadChatEvidence(spec, inspection)
	}
	if name == ChatToolQueryActivity {
		return queryActivityChatEvidence(spec, inspection)
	}
	if name == ChatToolChangeHistory {
		return changeHistoryChatEvidence(spec, inspection)
	}
	count := 0
	if inspection.Context != nil {
		for _, finding := range inspection.Context.Findings {
			if !finding.Suppressed {
				count++
			}
		}
	}
	summary := "Deterministic read-only database health inspection"
	if inspection.Context != nil {
		summary = fmt.Sprintf("Deterministic read-only database health inspection with %d active findings", count)
	}
	content := strings.TrimSpace(inspection.Report)
	if inspection.PoolerNote != "" {
		content += "\n\nPooler note: " + strings.TrimSpace(inspection.PoolerNote)
	}
	return ChatEvidence{
		Kind:       string(name),
		Source:     spec.Source,
		ObservedAt: time.Now(),
		Summary:    summary,
		Content:    content,
	}
}

func workloadChatEvidence(spec ChatToolSpec, inspection *Inspection) ChatEvidence {
	c := inspection.Context
	if c == nil {
		return ChatEvidence{Kind: string(spec.Name), Source: spec.Source, ObservedAt: time.Now(), Summary: "Workload evidence unavailable", Content: "The inspection returned no workload context."}
	}
	var lines []string
	if c.Activity != nil {
		lines = append(lines, fmt.Sprintf("Sessions: total=%d active=%d waiting=%d idle_in_transaction=%d longest_active=%.1fs longest_transaction=%.1fs", c.Activity.Total, c.Activity.Active, c.Activity.Waiting, c.Activity.IdleInTransaction, c.Activity.LongestActiveSec, c.Activity.LongestXactSec))
		keys := make([]string, 0, len(c.Activity.WaitEvents))
		for key := range c.Activity.WaitEvents {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("Wait %s: %d sessions", key, c.Activity.WaitEvents[key]))
		}
	}
	if c.Locks != nil {
		lines = append(lines, fmt.Sprintf("Blocked sessions: %d", c.Locks.BlockedCount))
		for index, chain := range c.Locks.Chains {
			if index >= 10 {
				lines = append(lines, fmt.Sprintf("… %d more blocking chains", len(c.Locks.Chains)-index))
				break
			}
			lines = append(lines, fmt.Sprintf("Blocked PID %d by %v for %.1fs on %s · %s", chain.BlockedPID, chain.BlockingPIDs, chain.WaitSeconds, chain.WaitEvent, chain.BlockedQuery))
		}
	}
	if c.WaitProfile != nil && c.WaitProfile.Available {
		lines = append(lines, fmt.Sprintf("Wait sample: %d samples over %.1fs", c.WaitProfile.Samples, c.WaitProfile.WindowSeconds))
		for index, bucket := range c.WaitProfile.Buckets {
			if index >= 8 {
				break
			}
			lines = append(lines, fmt.Sprintf("%s: %.1f%% (%d samples)", bucket.Type, bucket.Share*100, bucket.Count))
		}
	}
	if len(lines) == 0 {
		lines = []string{"Activity, locks, and wait samples were unavailable for this target."}
	}
	return ChatEvidence{Kind: string(spec.Name), Source: spec.Source, ObservedAt: c.CollectedAt, Summary: "Point-in-time workload, wait, and blocker evidence", Content: strings.Join(lines, "\n")}
}

func queryActivityChatEvidence(spec ChatToolSpec, inspection *Inspection) ChatEvidence {
	c := inspection.Context
	if c == nil || c.Queries == nil || !c.Queries.Enabled {
		reason := "pg_stat_statements query evidence is unavailable."
		if c != nil && c.Queries != nil && c.Queries.Reason != "" {
			reason += " " + c.Queries.Reason
		}
		return ChatEvidence{Kind: string(spec.Name), Source: spec.Source, ObservedAt: time.Now(), Summary: "Normalized query activity unavailable", Content: reason}
	}
	lines := []string{fmt.Sprintf("Coverage: %d entries; total execution %.1fms; evictions=%d", c.Queries.PgssCount, c.Queries.TotalExecMS, c.Queries.PgssDealloc)}
	for index, query := range c.Queries.Top {
		if index >= 12 {
			lines = append(lines, fmt.Sprintf("… %d more queries", len(c.Queries.Top)-index))
			break
		}
		lines = append(lines, fmt.Sprintf("queryid=%d total=%.1fms mean=%.1fms max=%.1fms calls=%d rows=%d wal=%dB · %s", query.QueryID, query.TotalMS, query.MeanMS, query.MaxMS, query.Calls, query.Rows, query.WALBytes, query.Query))
	}
	return ChatEvidence{Kind: string(spec.Name), Source: spec.Source, ObservedAt: c.CollectedAt, Summary: fmt.Sprintf("Top normalized query activity · %d statements", len(c.Queries.Top)), Content: strings.Join(lines, "\n")}
}

func changeHistoryChatEvidence(spec ChatToolSpec, inspection *Inspection) ChatEvidence {
	c := inspection.Context
	if c == nil {
		return ChatEvidence{Kind: string(spec.Name), Source: spec.Source, ObservedAt: time.Now(), Summary: "Change history unavailable", Content: "The inspection returned no historical context."}
	}
	var lines []string
	if c.DeltaSuppressedReason != "" {
		lines = append(lines, "Comparison suppressed: "+c.DeltaSuppressedReason)
	}
	for index, event := range c.Events {
		if index >= 30 {
			lines = append(lines, fmt.Sprintf("… %d more events", len(c.Events)-index))
			break
		}
		window := "observed in the latest comparison"
		if event.OccurredAfter != nil && event.OccurredBefore != nil {
			window = event.OccurredAfter.UTC().Format(time.RFC3339) + " to " + event.OccurredBefore.UTC().Format(time.RFC3339)
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s → %s (%s, confidence %.0f%%)", event.Kind, event.Object, event.Before, event.After, window, event.Confidence*100))
	}
	if len(lines) == 0 {
		lines = []string{"No comparable prior baseline events were available. This is not proof that no external changes occurred."}
	}
	return ChatEvidence{Kind: string(spec.Name), Source: spec.Source, ObservedAt: c.CollectedAt, Summary: fmt.Sprintf("Observed change history · %d events", len(c.Events)), Content: strings.Join(lines, "\n")}
}
