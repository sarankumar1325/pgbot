package app

import (
	"context"
	"encoding/hex"
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

// LoadDatabase reads the current schema and relationships over QueryShift's
// existing read-only connection path.
func LoadDatabase(ctx context.Context, connectionString string) (*DatabaseWorkspace, error) {
	connectionString = stripUnsupportedConnectionParams(strings.TrimSpace(connectionString))
	if connectionString == "" {
		return nil, fmt.Errorf("connection string is required")
	}
	target, err := conn.Connect(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("connect: %s", conn.RedactConnString(err.Error()))
	}
	defer target.Close()

	schema, err := erd.Introspect(ctx, target.Pool, "")
	if err != nil {
		return nil, fmt.Errorf("load schema: %s", conn.RedactConnString(err.Error()))
	}
	version := postgresVersion(target.Caps.VersionNum)
	schema.Info.Version = version
	return &DatabaseWorkspace{
		Host: target.Pool.Config().Host, Database: target.Caps.Database,
		Version: version, Provider: string(target.Caps.Provider), Schema: schema,
		ER: erd.RenderASCII(schema, false), Mermaid: erd.RenderMermaid(schema),
	}, nil
}

func postgresVersion(version int) string {
	major := version / 10000
	minor := (version / 100) % 100
	if major >= 10 {
		return fmt.Sprintf("PostgreSQL %d.%d", major, version%100)
	}
	return fmt.Sprintf("PostgreSQL %d.%d.%d", major, minor, version%100)
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
	Kind                 string
	Summary              string
	Destructive          bool
	Versioned            bool
	BackupRequired       bool
	RecoveryLevel        string
	RecoveryNote         string
	RollbackSQL          string
	RequiredConfirmation string
}

type ChangeResult struct {
	Command       string
	RowsAffected  int64
	Duration      time.Duration
	MigrationID   string
	MigrationPath string
	RecoveryLevel string
	SnapshotAfter bool
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

// ApplyChange executes one pre-confirmed statement in one short transaction.
// Confirmation is checked again here so callers cannot bypass the TUI guard.
func ApplyChange(ctx context.Context, connectionString, sql, confirmation string) (*ChangeResult, error) {
	plan, err := AnalyzeChange(sql)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(strings.ToUpper(confirmation)) != plan.RequiredConfirmation {
		return nil, fmt.Errorf("confirmation must exactly match %q", plan.RequiredConfirmation)
	}

	before, err := LoadDatabase(ctx, connectionString)
	if err != nil {
		return nil, fmt.Errorf("capture pre-change schema: %w", err)
	}
	journal, err := startMigrationJournal(sql, plan, before)
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
	tag, err := tx.Exec(ctx, sql)
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
	if snapshotErr != nil {
		_ = journal.finish("applied_snapshot_pending", nil, snapshotErr)
	} else {
		_ = journal.finish("applied", after, nil)
	}
	return &ChangeResult{
		Command: tag.String(), RowsAffected: tag.RowsAffected(), Duration: time.Since(started),
		MigrationID: journal.id, MigrationPath: journal.sqlPath, RecoveryLevel: plan.RecoveryLevel,
		SnapshotAfter: snapshotErr == nil,
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
