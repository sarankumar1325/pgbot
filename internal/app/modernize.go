package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sarankumar1325/queryshift/internal/conn"
	"github.com/sarankumar1325/queryshift/internal/store"
)

type ModernizeRequest struct {
	SQL              string
	SourceDialect    string
	ConnectionString string
	LiveSchema       bool
}

type ValidationIssue struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location"`
}

type IndexRecommendation struct {
	Name             string   `json:"name"`
	Table            string   `json:"table"`
	Columns          []string `json:"columns"`
	DDL              string   `json:"ddl"`
	Rationale        string   `json:"rationale"`
	Evidence         string   `json:"evidence"`
	PlannerValidated bool     `json:"planner_validated"`
	EstimatedImpact  string   `json:"estimated_impact"`
}

type TraceEvent struct {
	Stage            string `json:"stage"`
	Attempt          int    `json:"attempt"`
	TransportAttempt int    `json:"transport_attempt"`
	Agent            string `json:"agent"`
	Status           string `json:"status"`
	DurationMS       int    `json:"duration_ms"`
	Summary          string `json:"summary"`
	StopReason       string `json:"stop_reason"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CacheReadTokens  int    `json:"cache_read_tokens"`
	CacheWriteTokens int    `json:"cache_write_tokens"`
	ToolCalls        int    `json:"tool_calls"`
	ErrorCategory    string `json:"error_category"`
	Retryable        *bool  `json:"retryable"`
}

// AgentRunEvent is safe progress metadata emitted by the agent host. Event data
// intentionally excludes prompts, SQL text, connection strings, credentials,
// and model reasoning.
type AgentRunEvent struct {
	RunID            string         `json:"run_id"`
	Sequence         int            `json:"sequence"`
	Kind             string         `json:"kind"`
	Stage            string         `json:"stage"`
	LogicalAttempt   int            `json:"logical_attempt"`
	TransportAttempt int            `json:"transport_attempt"`
	Timestamp        time.Time      `json:"timestamp"`
	Data             map[string]any `json:"data"`
}

// ModernizationUpdate carries either one progress event or the terminal result.
type ModernizationUpdate struct {
	Event  *AgentRunEvent
	Result *ModernizationResult
	Err    error
}

type modernizerEnvelope struct {
	Type   string               `json:"type"`
	Event  *AgentRunEvent       `json:"event,omitempty"`
	Result *ModernizationResult `json:"result,omitempty"`
}

type ModernizationResult struct {
	RunID         string `json:"run_id"`
	SourceDialect string `json:"source_dialect"`
	TargetDialect string `json:"target_dialect"`
	OriginalSQL   string `json:"original_sql"`
	Validation    struct {
		Valid  bool              `json:"valid"`
		Issues []ValidationIssue `json:"issues"`
	} `json:"validation"`
	Optimization struct {
		SQL                  string                `json:"sql"`
		Changes              []string              `json:"changes"`
		IndexRecommendations []IndexRecommendation `json:"index_recommendations"`
		Cautions             []string              `json:"cautions"`
	} `json:"optimization"`
	Diff struct {
		Additions   int    `json:"additions"`
		Deletions   int    `json:"deletions"`
		UnifiedDiff string `json:"unified_diff"`
	} `json:"diff"`
	ConfidenceScore int          `json:"confidence_score"`
	ConfidenceBasis string       `json:"confidence_basis"`
	RiskLevel       string       `json:"risk_level"`
	RepairAttempts  int          `json:"repair_attempts"`
	Trace           []TraceEvent `json:"trace"`
}

// Modernize retains the original blocking SDK surface while using the streaming
// subprocess protocol internally.
func Modernize(ctx context.Context, request ModernizeRequest) (*ModernizationResult, error) {
	return ModernizeStream(ctx, request, nil)
}

// StreamModernize starts one modernization run and returns progress and terminal
// updates on a channel. The channel is closed after the terminal update.
func StreamModernize(ctx context.Context, request ModernizeRequest) <-chan ModernizationUpdate {
	updates := make(chan ModernizationUpdate, 16)
	go func() {
		defer close(updates)
		result, err := ModernizeStream(ctx, request, func(event AgentRunEvent) {
			select {
			case updates <- ModernizationUpdate{Event: &event}:
			case <-ctx.Done():
			}
		})
		select {
		case updates <- ModernizationUpdate{Result: result, Err: err}:
		case <-ctx.Done():
		}
	}()
	return updates
}

// ModernizeStream runs the bounded Strands/Bedrock pipeline as a local
// subprocess. SQL is sent over stdin and the optional database credential exists
// only in that child's environment. The callback receives metadata-only events.
func ModernizeStream(ctx context.Context, request ModernizeRequest, onEvent func(AgentRunEvent)) (result *ModernizationResult, err error) {
	var runEvents []AgentRunEvent
	defer func() {
		persistAgentRun(request, result, runEvents, err)
	}()

	if strings.TrimSpace(request.SQL) == "" {
		return nil, fmt.Errorf("paste a SQL query before starting the agent")
	}
	if request.SourceDialect == "" {
		request.SourceDialect = "oracle"
	}

	name, prefix, err := modernizerCommand()
	if err != nil {
		return nil, err
	}
	args := append(prefix, "-", "--source", request.SourceDialect, "--format", "ndjson", "--timeout", "180")
	if request.LiveSchema {
		if strings.TrimSpace(request.ConnectionString) == "" {
			return nil, fmt.Errorf("connect to a database before enabling live-schema validation")
		}
		args = append(args, "--live-schema")
		if current, currentErr := os.Executable(); currentErr == nil {
			args = append(args, "--queryshift-bin", current)
		}
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(request.SQL)
	cmd.Env = os.Environ()
	if request.LiveSchema {
		cmd.Env = withEnvironment(cmd.Env, "DATABASE_URL", stripUnsupportedConnectionParams(request.ConnectionString))
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open modernizer output: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start modernizer: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	// The final report can contain a large SQL diff. Lifecycle records are small,
	// but keep one bounded protocol limit for both record types.
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var envelope modernizerEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &envelope); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("modernizer returned an invalid streaming record: %w", err)
		}
		switch envelope.Type {
		case "event":
			if envelope.Event == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, fmt.Errorf("modernizer returned an empty event record")
			}
			if onEvent != nil {
				onEvent(*envelope.Event)
			}
			runEvents = append(runEvents, *envelope.Event)
		case "result":
			if envelope.Result == nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil, fmt.Errorf("modernizer returned an empty result record")
			}
			result = envelope.Result
		default:
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, fmt.Errorf("modernizer returned unknown record type %q", envelope.Type)
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("read modernizer stream: %w", err)
	}
	if err := cmd.Wait(); err != nil {
		detail := strings.TrimSpace(conn.RedactConnString(stderr.String()))
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("modernizer failed: %s", detail)
	}

	if result == nil {
		return nil, fmt.Errorf("modernizer completed without a result record")
	}
	return result, nil
}

type storedAgentEvent struct {
	Sequence         int            `json:"sequence"`
	Kind             string         `json:"kind"`
	Stage            string         `json:"stage,omitempty"`
	LogicalAttempt   int            `json:"logical_attempt"`
	TransportAttempt int            `json:"transport_attempt"`
	Timestamp        time.Time      `json:"timestamp"`
	Data             map[string]any `json:"data,omitempty"`
}

var storedAgentDataKeys = map[string]bool{
	"agent": true, "duration_ms": true, "error_type": true,
	"error_category": true, "retryable": true, "delay_seconds": true,
	"stop_reason": true, "input_tokens": true, "output_tokens": true,
	"total_tokens": true, "tool_calls": true,
	"risk_level": true, "confidence_score": true, "repair_attempts": true,
	"source_dialect": true, "live_schema": true, "model_id": true,
}

func persistAgentRun(request ModernizeRequest, result *ModernizationResult, events []AgentRunEvent, runErr error) {
	if len(events) == 0 {
		return
	}
	runID := events[0].RunID
	if result != nil && result.RunID != "" {
		runID = result.RunID
	}
	if runID == "" {
		return
	}

	sanitized := make([]storedAgentEvent, 0, len(events))
	modelID := ""
	for _, event := range events {
		data := make(map[string]any)
		for key, value := range event.Data {
			if storedAgentDataKeys[key] && scalarMetadata(value) {
				data[key] = value
			}
		}
		if event.Kind == "run_started" {
			if value, ok := data["model_id"].(string); ok {
				modelID = value
			}
		}
		sanitized = append(sanitized, storedAgentEvent{
			Sequence: event.Sequence, Kind: event.Kind, Stage: event.Stage,
			LogicalAttempt: event.LogicalAttempt, TransportAttempt: event.TransportAttempt,
			Timestamp: event.Timestamp, Data: data,
		})
	}
	raw, marshalErr := json.Marshal(sanitized)
	if marshalErr != nil {
		return
	}
	startedAt := events[0].Timestamp
	completedAt := events[len(events)-1].Timestamp
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	if completedAt.IsZero() {
		completedAt = startedAt
	}
	status := "completed"
	if runErr != nil {
		status = "failed"
	}
	var score *int
	risk := ""
	if result != nil {
		score = &result.ConfidenceScore
		risk = result.RiskLevel
	}

	st, openErr := store.Open("")
	if openErr != nil {
		return
	}
	defer st.Close()
	_ = st.SaveAgentRun(store.AgentRun{
		RunID: runID, StartedAt: startedAt, CompletedAt: completedAt,
		SourceDialect: request.SourceDialect, TargetDialect: "postgres",
		ModelID: modelID, LiveSchema: request.LiveSchema, Status: status,
		ConfidenceScore: score, RiskLevel: risk, EventsJSON: string(raw),
	})
}

func scalarMetadata(value any) bool {
	switch value.(type) {
	case nil, bool, string, float64, int, int64:
		return true
	default:
		return false
	}
}

func modernizerCommand() (string, []string, error) {
	if command, err := exec.LookPath("queryshift-modernize"); err == nil {
		return command, nil, nil
	}
	for _, candidate := range []string{
		filepath.Join("agent", ".venv", "bin", "queryshift-modernize"),
		filepath.Join("agent", ".venv", "Scripts", "queryshift-modernize.exe"),
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			absolute, absErr := filepath.Abs(candidate)
			if absErr == nil {
				return absolute, nil, nil
			}
		}
	}
	if uv, err := exec.LookPath("uv"); err == nil {
		if _, statErr := os.Stat(filepath.Join("agent", "pyproject.toml")); statErr == nil {
			return uv, []string{"run", "--project", "agent", "queryshift-modernize"}, nil
		}
	}
	return "", nil, fmt.Errorf("queryshift-modernize is not installed; run `uv sync --project agent --extra dev`")
}

func withEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	filtered := environment[:0]
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
}
