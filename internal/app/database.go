package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/pgrundev/pggo"
	"github.com/sarankumar1325/queryshift/internal/conn"
	"github.com/sarankumar1325/queryshift/internal/erd"
)

// DatabaseWorkspace is the live catalog snapshot shown by the TUI. It contains
// structure only; table data is fetched only when the user runs an explicit
// read query.
type DatabaseWorkspace struct {
	Host     string
	Database string
	Version  string
	Provider string
	Schema   erd.Schema
	ER       string
	Mermaid  string
}

// DatabaseLoadUpdate represents a live status or progress update while loading a database workspace.
type DatabaseLoadUpdate struct {
	Stage  string
	Log    string
	Result *DatabaseWorkspace
	Err    error
}

const (
	// A suspended Neon database normally wakes in 3–12 seconds. Do not let a
	// broken route, DNS lookup, or TLS handshake fall through to the operating
	// system's minute-long network timeout.
	databaseConnectTimeout = 15 * time.Second
	// Loading a workspace includes connection, capability probes, catalog reads,
	// and ER rendering. Large schemas get the time left after connection, while a
	// stalled operation always returns control to the TUI promptly.
	databaseWorkspaceTimeout = 30 * time.Second
)

// StreamLoadDatabase connects to the database, introspects tables, columns, indexes,
// and foreign keys, and builds the ER model while streaming live progress updates.
func StreamLoadDatabase(ctx context.Context, connectionString string) <-chan DatabaseLoadUpdate {
	return streamLoadDatabase(ctx, connectionString, databaseConnectTimeout, databaseWorkspaceTimeout)
}

func streamLoadDatabase(ctx context.Context, connectionString string, connectTimeout, workspaceTimeout time.Duration) <-chan DatabaseLoadUpdate {
	updates := make(chan DatabaseLoadUpdate, 32)
	go func() {
		defer close(updates)
		loadCtx, cancelLoad := context.WithTimeout(ctx, workspaceTimeout)
		defer cancelLoad()
		send := func(stage, log string) {
			select {
			case <-loadCtx.Done():
			case updates <- DatabaseLoadUpdate{Stage: stage, Log: log}:
			}
		}

		connectionString = stripUnsupportedConnectionParams(strings.TrimSpace(connectionString))
		if connectionString == "" {
			updates <- DatabaseLoadUpdate{Err: fmt.Errorf("connection string is required")}
			return
		}

		send("Validating", "Parsing connection string and credentials")

		connectCtx, cancelConnect := context.WithTimeout(loadCtx, connectTimeout)
		type connectionResult struct {
			target *conn.Target
			err    error
		}
		connectionResults := make(chan connectionResult)
		go func() {
			target, err := conn.ConnectWithProgress(connectCtx, connectionString, func(stage, detail string) {
				send(stage, detail)
			})
			result := connectionResult{target: target, err: err}
			select {
			case connectionResults <- result:
			case <-connectCtx.Done():
				// A driver or resolver may return after our hard deadline. Do not
				// leak a pool if that late result happened to succeed.
				if target != nil {
					target.Close()
				}
			}
		}()

		var target *conn.Target
		var err error
		select {
		case result := <-connectionResults:
			target, err = result.target, result.err
			cancelConnect()
		case <-connectCtx.Done():
			connectErr := connectCtx.Err()
			cancelConnect()
			if errors.Is(connectErr, context.DeadlineExceeded) && ctx.Err() == nil {
				updates <- DatabaseLoadUpdate{Err: fmt.Errorf("connection timed out after %s; Neon wake-up normally completes within 3–12s—verify the endpoint or network and retry", connectTimeout)}
				return
			}
			updates <- DatabaseLoadUpdate{Err: fmt.Errorf("connect: %w", connectErr)}
			return
		}
		if err != nil {
			updates <- DatabaseLoadUpdate{Err: fmt.Errorf("connect: %s", conn.RedactConnString(err.Error()))}
			return
		}
		defer target.Close()

		schema, err := erd.IntrospectWithProgress(loadCtx, target.Pool, "", func(stage, detail string) {
			send(stage, detail)
		})
		if err != nil {
			if errors.Is(loadCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
				updates <- DatabaseLoadUpdate{Err: fmt.Errorf("database workspace load timed out after %s while reading the schema; retry or use a direct database endpoint", workspaceTimeout)}
				return
			}
			updates <- DatabaseLoadUpdate{Err: fmt.Errorf("load schema: %s", conn.RedactConnString(err.Error()))}
			return
		}

		version := postgresVersion(target.Caps.VersionNum)
		schema.Info.Version = version

		send("Rendering diagram", "Building ASCII ER diagram and Mermaid models")
		er := erd.RenderASCII(schema, false)
		mermaid := erd.RenderMermaid(schema)

		workspace := &DatabaseWorkspace{
			Host:     target.Pool.Config().Host,
			Database: target.Caps.Database,
			Version:  version,
			Provider: string(target.Caps.Provider),
			Schema:   schema,
			ER:       er,
			Mermaid:  mermaid,
		}

		send("Ready", fmt.Sprintf("Workspace loaded: %d tables, %d relationships in %s", len(schema.Tables), len(schema.Edges), workspace.Database))
		updates <- DatabaseLoadUpdate{
			Stage:  "Ready",
			Log:    "Workspace loaded successfully",
			Result: workspace,
		}
	}()
	return updates
}

// LoadDatabase reads the current schema and relationships over QueryShift's
// existing read-only connection path.
func LoadDatabase(ctx context.Context, connectionString string) (*DatabaseWorkspace, error) {
	updates := StreamLoadDatabase(ctx, connectionString)
	var lastResult *DatabaseWorkspace
	for update := range updates {
		if update.Err != nil {
			return nil, update.Err
		}
		if update.Result != nil {
			lastResult = update.Result
		}
	}
	if lastResult == nil {
		return nil, fmt.Errorf("connection failed: stream closed without a workspace")
	}
	return lastResult, nil
}

func postgresVersion(version int) string {
	major := version / 10000
	minor := (version / 100) % 100
	if major >= 10 {
		return fmt.Sprintf("PostgreSQL %d.%d", major, version%100)
	}
	return fmt.Sprintf("PostgreSQL %d.%d.%d", major, minor, version%100)
}

// FormatHostDisplay extracts the target host and database name for UI display without exposing credentials.
func FormatHostDisplay(connectionString string) string {
	cfg, err := pggo.ParseConfig(stripUnsupportedConnectionParams(strings.TrimSpace(connectionString)))
	if err == nil && cfg.Host != "" {
		host := cfg.Host
		if cfg.Port > 0 && cfg.Port != 5432 {
			host = fmt.Sprintf("%s:%d", host, cfg.Port)
		}
		if cfg.Database != "" {
			return fmt.Sprintf("%s · %s", host, cfg.Database)
		}
		return host
	}
	return ""
}

// QueryResult is a bounded result from the read-only SQL console.
type QueryResult struct {
	Columns   []string
	Rows      [][]string
	Truncated bool
	Duration  time.Duration
}

// RunReadQuery runs exactly one read statement and returns at most 100 rows.
func RunReadQuery(ctx context.Context, connectionString, sql string) (*QueryResult, error) {
	if err := validateSingleStatement(sql); err != nil {
		return nil, err
	}
	keyword := firstKeyword(sql)
	if !IsReadQuery(sql) {
		return nil, fmt.Errorf("%s is not a read query; use the write-mode apply flow", keyword)
	}

	target, err := conn.Connect(ctx, stripUnsupportedConnectionParams(strings.TrimSpace(connectionString)))
	if err != nil {
		return nil, fmt.Errorf("connect: %s", conn.RedactConnString(err.Error()))
	}
	defer target.Close()
	result := &QueryResult{}
	started := time.Now()
	err = target.ReadOnlyTx(ctx, func(tx *pggo.Tx) error {
		rows, err := tx.Query(ctx, sql, pggo.MaxRows(101))
		if err != nil {
			return err
		}
		defer rows.Close()
		for _, column := range rows.Columns() {
			result.Columns = append(result.Columns, column.Name)
		}
		for rows.Next() {
			values, err := rows.Values()
			if err != nil {
				return err
			}
			if len(result.Rows) == 100 {
				result.Truncated = true
				continue
			}
			formatted := make([]string, len(values))
			for i, value := range values {
				formatted[i] = formatCell(value)
			}
			result.Rows = append(result.Rows, formatted)
		}
		return rows.Err()
	})
	result.Duration = time.Since(started)
	if err != nil {
		return nil, fmt.Errorf("query: %s", conn.RedactConnString(err.Error()))
	}
	return result, nil
}

// IsReadQuery reports whether SQL belongs in the read-only console path.
func IsReadQuery(sql string) bool {
	switch firstKeyword(sql) {
	case "SELECT", "WITH", "EXPLAIN", "SHOW", "TABLE", "VALUES":
		return true
	default:
		return false
	}
}

func formatCell(value any) string {
	switch typed := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		return "\\x" + hex.EncodeToString(typed)
	case time.Time:
		return typed.Format(time.RFC3339)
	default:
		return fmt.Sprint(typed)
	}
}

// ChangePlan is a deterministic safety classification shown before applying a
// schema or data change.
type ChangePlan struct {
	Kind                 string `json:"kind"`
	Summary              string `json:"summary"`
	Destructive          bool   `json:"destructive"`
	Versioned            bool   `json:"versioned"`
	BackupRequired       bool   `json:"backup_required"`
	RecoveryLevel        string `json:"recovery_level"`
	RecoveryNote         string `json:"recovery_note"`
	RollbackSQL          string `json:"rollback_sql,omitempty"`
	RequiredConfirmation string `json:"required_confirmation"`
}

type ChangeResult struct {
	Command            string
	RowsAffected       int64
	Duration           time.Duration
	MigrationID        string
	MigrationPath      string
	RecoveryLevel      string
	SnapshotAfter      bool
	ContractID         string
	ExecutionMode      string
	BeforeSchemaSHA256 string
	AfterSchemaSHA256  string
}

// AnalyzeChange accepts schema/table/value changes but deliberately excludes
// server administration and multi-statement batches.
func AnalyzeChange(sql string) (ChangePlan, error) {
	if err := validateSingleStatement(sql); err != nil {
		return ChangePlan{}, err
	}
	upper := strings.ToUpper(stripLeadingSQLNoise(sql))
	keyword := firstKeyword(upper)
	plan := ChangePlan{
		Kind: keyword, Summary: keyword + " statement", RequiredConfirmation: "APPLY",
		Versioned: true, RecoveryLevel: "manual", RecoveryNote: "Schema snapshots and the exact SQL are versioned locally.",
	}
	if containsKeyword(upper, "CONCURRENTLY") {
		return ChangePlan{}, fmt.Errorf("CONCURRENTLY cannot run inside QueryShift's single-transaction write boundary; create a reviewed external migration for this operation")
	}

	switch keyword {
	case "INSERT":
		plan.Summary = "Insert table values"
		plan.RecoveryNote = "Inserted rows are not automatically reversible unless the statement uses stable keys."
	case "UPDATE":
		plan.Summary = "Update table values"
		plan.Destructive = !containsKeyword(upper, "WHERE")
		plan.BackupRequired = true
		plan.RecoveryLevel = "backup required"
		plan.RecoveryNote = "Previous column values cannot be reconstructed from SQL alone; verify a provider backup or PITR point."
	case "DELETE":
		plan.Summary = "Delete table values"
		plan.Destructive = true
		plan.BackupRequired = true
		plan.RecoveryLevel = "backup required"
		plan.RecoveryNote = "Deleted rows require a provider backup or PITR point for recovery."
	case "CREATE":
		words := sqlWords(upper)
		uniqueIndex := len(words) > 2 && words[1] == "UNIQUE" && words[2] == "INDEX"
		if !uniqueIndex && !secondKeywordAllowed(upper, "TABLE", "INDEX", "VIEW", "MATERIALIZED", "SCHEMA", "TYPE", "SEQUENCE") {
			return ChangePlan{}, fmt.Errorf("only schema objects can be created in write mode")
		}
		plan.Summary = "Create a schema object"
		plan.RollbackSQL = generatedCreateRollback(sql)
		if plan.RollbackSQL != "" {
			plan.RecoveryLevel = "rollback generated"
			plan.RecoveryNote = "A reviewed down statement is included in the local migration artifact."
		}
	case "ALTER":
		if !secondKeywordAllowed(upper, "TABLE", "INDEX", "VIEW", "MATERIALIZED", "SCHEMA", "TYPE", "SEQUENCE") {
			return ChangePlan{}, fmt.Errorf("server, database, and role administration is not allowed")
		}
		plan.Summary = "Alter a schema object"
		plan.Destructive = containsKeyword(upper, "DROP")
		if plan.Destructive {
			plan.BackupRequired = true
			plan.RecoveryLevel = "backup required"
			plan.RecoveryNote = "Dropped schema members or converted values require a provider backup for reliable recovery."
		} else {
			plan.RecoveryNote = "The before/after schema is versioned; no automatic down migration can be proven safe."
		}
	case "DROP":
		if !secondKeywordAllowed(upper, "TABLE", "INDEX", "VIEW", "MATERIALIZED", "SCHEMA", "TYPE", "SEQUENCE") {
			return ChangePlan{}, fmt.Errorf("only schema objects can be dropped in write mode")
		}
		plan.Summary = "Drop a schema object"
		plan.Destructive = true
		plan.BackupRequired = true
		plan.RecoveryLevel = "backup required"
		plan.RecoveryNote = "A schema snapshot cannot restore dropped table data; verify a provider backup or PITR point."
	case "TRUNCATE":
		plan.Summary = "Remove all values from a table"
		plan.Destructive = true
		plan.BackupRequired = true
		plan.RecoveryLevel = "backup required"
		plan.RecoveryNote = "Truncated data requires a provider backup or PITR point for recovery."
	case "COMMENT":
		words := sqlWords(upper)
		if len(words) < 3 || words[1] != "ON" || !wordAllowed(words[2], "TABLE", "COLUMN", "VIEW", "MATERIALIZED", "INDEX", "SCHEMA", "TYPE", "SEQUENCE") {
			return ChangePlan{}, fmt.Errorf("comments are limited to schema objects")
		}
		plan.Summary = "Update schema documentation"
	default:
		return ChangePlan{}, fmt.Errorf("%s is not an allowed schema or data change", keyword)
	}
	if plan.BackupRequired && plan.Destructive {
		plan.RequiredConfirmation = "BACKUP AND APPLY DESTRUCTIVE"
	} else if plan.BackupRequired {
		plan.RequiredConfirmation = "BACKUP AND APPLY"
	} else if plan.Destructive {
		plan.RequiredConfirmation = "APPLY DESTRUCTIVE"
	}
	return plan, nil
}

// ApplyChange preserves the programmatic API while using the same contract
// execution path as the TUI. Interactive callers should build and review a
// ChangeContract before calling ApplyChangeContract.
func ApplyChange(ctx context.Context, connectionString, sql, confirmation string) (*ChangeResult, error) {
	before, err := LoadDatabase(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("capture pre-change schema: %w", err)
	}
	contract, err := BuildChangeContract(sql, before)
	if err != nil {
		return nil, err
	}
	return applyChangeContract(ctx, connectionString, contract, confirmation, before)
}

// ApplyChangeContract executes only the exact statement and target approved in
// a validated ChangeContract. A target change, schema drift, contract edit, or
// mismatched approval phrase stops execution before a write connection opens.
func ApplyChangeContract(ctx context.Context, connectionString string, contract ChangeContract, confirmation string) (*ChangeResult, error) {
	if err := ValidateChangeContract(contract, nil); err != nil {
		return nil, err
	}
	if strings.TrimSpace(strings.ToUpper(confirmation)) != contract.Plan.RequiredConfirmation {
		return nil, fmt.Errorf("confirmation must exactly match %q", contract.Plan.RequiredConfirmation)
	}
	before, err := LoadDatabase(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("capture pre-change schema: %w", err)
	}
	return applyChangeContract(ctx, connectionString, contract, confirmation, before)
}

func applyChangeContract(ctx context.Context, connectionString string, contract ChangeContract, confirmation string, before *DatabaseWorkspace) (*ChangeResult, error) {
	if err := ValidateChangeContract(contract, before); err != nil {
		return nil, err
	}
	if strings.TrimSpace(strings.ToUpper(confirmation)) != contract.Plan.RequiredConfirmation {
		return nil, fmt.Errorf("confirmation must exactly match %q", contract.Plan.RequiredConfirmation)
	}
	return executeChangeContract(ctx, connectionString, contract, before, "production")
}

func executeChangeContract(ctx context.Context, connectionString string, contract ChangeContract, before *DatabaseWorkspace, executionMode string) (*ChangeResult, error) {
	beforeHash, err := workspaceSchemaSHA256(before)
	if err != nil {
		return nil, err
	}
	journal, err := startMigrationJournal(contract, before, executionMode)
	if err != nil {
		return nil, fmt.Errorf("create migration version: %w", err)
	}

	cfg, err := pggo.ParseConfig(stripUnsupportedConnectionParams(strings.TrimSpace(connectionString)))
	if err != nil {
		_ = journal.finish("failed", nil, fmt.Errorf("%s", conn.RedactConnString(err.Error())))
		return nil, fmt.Errorf("parse connection string: %s", conn.RedactConnString(err.Error()))
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = map[string]string{}
	}
	cfg.RuntimeParams["application_name"] = "queryshift-write"
	db, err := pggo.ConnectConfig(ctx, cfg)
	if err != nil {
		_ = journal.finish("failed", nil, fmt.Errorf("%s", conn.RedactConnString(err.Error())))
		return nil, fmt.Errorf("connect: %s", conn.RedactConnString(err.Error()))
	}
	defer db.Close()
	for _, setup := range []string{
		"SET statement_timeout = '30s'",
		"SET lock_timeout = '3s'",
		"SET idle_in_transaction_session_timeout = '15s'",
	} {
		if _, err := db.Exec(ctx, setup); err != nil {
			_ = journal.finish("failed", nil, err)
			return nil, fmt.Errorf("write safety setup: %s", conn.RedactConnString(err.Error()))
		}
	}

	started := time.Now()
	tx, err := db.Begin(ctx)
	if err != nil {
		_ = journal.finish("failed", nil, err)
		return nil, fmt.Errorf("begin change: %s", conn.RedactConnString(err.Error()))
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	tag, err := tx.Exec(ctx, contract.Statement)
	if err != nil {
		_ = journal.finish("failed", nil, err)
		return nil, fmt.Errorf("apply change: %s", conn.RedactConnString(err.Error()))
	}
	if err := tx.Commit(ctx); err != nil {
		_ = journal.finish("failed", nil, err)
		return nil, fmt.Errorf("commit change: %s", conn.RedactConnString(err.Error()))
	}
	committed = true
	after, snapshotErr := LoadDatabase(ctx, connectionString)
	afterHash := ""
	if snapshotErr != nil {
		_ = journal.finish("applied_snapshot_pending", nil, snapshotErr)
	} else {
		afterHash, _ = workspaceSchemaSHA256(after)
		_ = journal.finish("applied", after, nil)
	}
	return &ChangeResult{
		Command: tag.String(), RowsAffected: tag.RowsAffected(), Duration: time.Since(started),
		MigrationID: journal.id, MigrationPath: journal.sqlPath, RecoveryLevel: contract.Plan.RecoveryLevel,
		SnapshotAfter: snapshotErr == nil, ContractID: contract.ID, ExecutionMode: executionMode,
		BeforeSchemaSHA256: beforeHash, AfterSchemaSHA256: afterHash,
	}, nil
}

func secondKeywordAllowed(sql string, allowed ...string) bool {
	words := sqlWords(sql)
	if len(words) < 2 {
		return false
	}
	return wordAllowed(words[1], allowed...)
}

func wordAllowed(word string, allowed ...string) bool {
	for _, candidate := range allowed {
		if word == candidate {
			return true
		}
	}
	return false
}

func containsKeyword(sql, keyword string) bool {
	for _, word := range sqlWords(sql) {
		if word == keyword {
			return true
		}
	}
	return false
}

func firstKeyword(sql string) string {
	words := sqlWords(stripLeadingSQLNoise(sql))
	if len(words) == 0 {
		return ""
	}
	return words[0]
}

func sqlWords(sql string) []string {
	return strings.FieldsFunc(strings.ToUpper(sqlCodeOnly(sql)), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	})
}

// sqlCodeOnly removes strings, quoted identifiers, comments, and dollar-quoted
// bodies before keyword classification. A literal containing "WHERE" must not
// downgrade an unscoped UPDATE or DELETE from destructive to controlled.
func sqlCodeOnly(sql string) string {
	var out strings.Builder
	inSingle, inDouble, inLine, inBlock := false, false, false, false
	dollarTag := ""
	for i := 0; i < len(sql); i++ {
		if inLine {
			if sql[i] == '\n' {
				inLine = false
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
			continue
		}
		if inBlock {
			if i+1 < len(sql) && sql[i:i+2] == "*/" {
				inBlock = false
				out.WriteString("  ")
				i++
			} else {
				out.WriteByte(' ')
			}
			continue
		}
		if dollarTag != "" {
			if strings.HasPrefix(sql[i:], dollarTag) {
				out.WriteString(strings.Repeat(" ", len(dollarTag)))
				i += len(dollarTag) - 1
				dollarTag = ""
			} else {
				out.WriteByte(' ')
			}
			continue
		}
		if inSingle {
			out.WriteByte(' ')
			if sql[i] == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					out.WriteByte(' ')
					i++
				} else {
					inSingle = false
				}
			}
			continue
		}
		if inDouble {
			out.WriteByte(' ')
			if sql[i] == '"' {
				if i+1 < len(sql) && sql[i+1] == '"' {
					out.WriteByte(' ')
					i++
				} else {
					inDouble = false
				}
			}
			continue
		}
		switch {
		case i+1 < len(sql) && sql[i:i+2] == "--":
			inLine = true
			out.WriteString("  ")
			i++
		case i+1 < len(sql) && sql[i:i+2] == "/*":
			inBlock = true
			out.WriteString("  ")
			i++
		case sql[i] == '\'':
			inSingle = true
			out.WriteByte(' ')
		case sql[i] == '"':
			inDouble = true
			out.WriteByte(' ')
		case sql[i] == '$':
			end := strings.IndexByte(sql[i+1:], '$')
			if end < 0 {
				out.WriteByte(sql[i])
				continue
			}
			tag := sql[i : i+end+2]
			valid := true
			for _, r := range tag[1 : len(tag)-1] {
				if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
					valid = false
				}
			}
			if valid {
				dollarTag = tag
				out.WriteString(strings.Repeat(" ", len(tag)))
				i += len(tag) - 1
			} else {
				out.WriteByte(sql[i])
			}
		default:
			out.WriteByte(sql[i])
		}
	}
	return out.String()
}

func stripLeadingSQLNoise(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		switch {
		case strings.HasPrefix(sql, "--"):
			if end := strings.IndexByte(sql, '\n'); end >= 0 {
				sql = sql[end+1:]
				continue
			}
			return ""
		case strings.HasPrefix(sql, "/*"):
			if end := strings.Index(sql[2:], "*/"); end >= 0 {
				sql = sql[end+4:]
				continue
			}
			return ""
		}
		return sql
	}
}

// validateSingleStatement is intentionally conservative. It understands SQL
// strings, quoted identifiers, comments, and PostgreSQL dollar quotes so a
// semicolon inside a value does not look like a second statement.
func validateSingleStatement(sql string) error {
	if strings.TrimSpace(sql) == "" {
		return fmt.Errorf("SQL is required")
	}
	statements := 1
	inSingle, inDouble, inLine, inBlock := false, false, false, false
	dollarTag := ""
	for i := 0; i < len(sql); i++ {
		if inLine {
			if sql[i] == '\n' {
				inLine = false
			}
			continue
		}
		if inBlock {
			if i+1 < len(sql) && sql[i:i+2] == "*/" {
				inBlock = false
				i++
			}
			continue
		}
		if dollarTag != "" {
			if strings.HasPrefix(sql[i:], dollarTag) {
				i += len(dollarTag) - 1
				dollarTag = ""
			}
			continue
		}
		if inSingle {
			if sql[i] == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
				} else {
					inSingle = false
				}
			}
			continue
		}
		if inDouble {
			if sql[i] == '"' {
				if i+1 < len(sql) && sql[i+1] == '"' {
					i++
				} else {
					inDouble = false
				}
			}
			continue
		}
		switch {
		case i+1 < len(sql) && sql[i:i+2] == "--":
			inLine = true
			i++
		case i+1 < len(sql) && sql[i:i+2] == "/*":
			inBlock = true
			i++
		case sql[i] == '\'':
			inSingle = true
		case sql[i] == '"':
			inDouble = true
		case sql[i] == '$':
			if end := strings.IndexByte(sql[i+1:], '$'); end >= 0 {
				tag := sql[i : i+end+2]
				valid := true
				for _, r := range tag[1 : len(tag)-1] {
					if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
						valid = false
					}
				}
				if valid {
					dollarTag = tag
					i += len(tag) - 1
				}
			}
		case sql[i] == ';':
			if strings.TrimSpace(sql[i+1:]) != "" {
				statements++
			}
		}
	}
	if inSingle || inDouble || inBlock || dollarTag != "" {
		return fmt.Errorf("SQL contains an unterminated quote or comment")
	}
	if statements != 1 {
		return fmt.Errorf("run one SQL statement at a time")
	}
	return nil
}
