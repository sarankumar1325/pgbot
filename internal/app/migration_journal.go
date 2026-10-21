package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// migrationJournal is QueryShift's local, Alembic-style history for dynamic
// changes. It deliberately lives outside the target database so QueryShift
// does not install bookkeeping tables in customer schemas.
type migrationJournal struct {
	id           string
	sqlPath      string
	metaPath     string
	contractPath string
	meta         migrationMetadata
}

type migrationMetadata struct {
	ID                 string              `json:"id"`
	CreatedAt          time.Time           `json:"created_at"`
	AppliedAt          time.Time           `json:"applied_at,omitempty"`
	Status             string              `json:"status"`
	Target             string              `json:"target"`
	TargetID           string              `json:"target_id"`
	Kind               string              `json:"kind"`
	Summary            string              `json:"summary"`
	SQLSHA256          string              `json:"sql_sha256"`
	RecoveryLevel      string              `json:"recovery_level"`
	RecoveryNote       string              `json:"recovery_note"`
	BackupAcknowledged bool                `json:"backup_acknowledged"`
	BeforeSHA256       string              `json:"before_schema_sha256"`
	AfterSHA256        string              `json:"after_schema_sha256,omitempty"`
	Error              string              `json:"error,omitempty"`
	ContractID         string              `json:"contract_id"`
	ContractVersion    string              `json:"contract_version"`
	ContractSHA256     string              `json:"contract_sha256"`
	RiskLevel          string              `json:"risk_level"`
	AffectedObjects    []ChangeObject      `json:"affected_objects,omitempty"`
	Verification       []VerificationProbe `json:"verification"`
	ExecutionMode      string              `json:"execution_mode"`
}

func startMigrationJournal(contract ChangeContract, before *DatabaseWorkspace, executionMode string) (*migrationJournal, error) {
	statement := contract.Statement
	plan := contract.Plan
	base, err := migrationStateDir()
	if err != nil {
		return nil, err
	}
	targetHash := sha256.Sum256([]byte(before.Host + "|" + before.Database))
	targetID := hex.EncodeToString(targetHash[:8])
	dir := filepath.Join(base, targetID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	statementHash := sha256.Sum256([]byte(strings.TrimSpace(statement)))
	id := time.Now().UTC().Format("20060102T150405.000000000Z") + "_" + hex.EncodeToString(statementHash[:4])
	stem := id + "_" + strings.ToLower(plan.Kind)
	sqlPath := filepath.Join(dir, stem+".sql")
	metaPath := filepath.Join(dir, stem+".json")
	contractPath := filepath.Join(dir, stem+".contract.json")

	beforeBytes, err := json.MarshalIndent(before.Schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode before schema: %w", err)
	}
	beforeHash := sha256.Sum256(beforeBytes)
	if err := writeSecure(filepath.Join(dir, stem+".before.json"), append(beforeBytes, '\n')); err != nil {
		return nil, fmt.Errorf("write before schema: %w", err)
	}

	var migration strings.Builder
	fmt.Fprintf(&migration, "-- queryshift migration %s\n", id)
	fmt.Fprintf(&migration, "-- change contract %s (sha256 %s)\n", contract.ID, contract.ContractSHA256)
	fmt.Fprintf(&migration, "-- target %s/%s (%s)\n", before.Host, before.Database, targetID)
	fmt.Fprintf(&migration, "-- recovery %s: %s\n", plan.RecoveryLevel, plan.RecoveryNote)
	migration.WriteString("-- queryshift:up\n")
	upSQL := strings.TrimSpace(statement)
	upSQL = strings.TrimSpace(strings.TrimSuffix(upSQL, ";"))
	migration.WriteString(upSQL)
	migration.WriteString(";\n\n-- queryshift:down\n")
	if plan.RollbackSQL == "" {
		migration.WriteString("-- unavailable: use the recorded schema snapshot and a verified provider backup/PITR point.\n")
	} else {
		migration.WriteString(strings.TrimSpace(plan.RollbackSQL))
		migration.WriteString(";\n")
	}
	if err := writeSecure(sqlPath, []byte(migration.String())); err != nil {
		return nil, fmt.Errorf("write migration SQL: %w", err)
	}
	contractBytes, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode change contract: %w", err)
	}
	if err := writeSecure(contractPath, append(contractBytes, '\n')); err != nil {
		return nil, fmt.Errorf("write change contract: %w", err)
	}

	j := &migrationJournal{
		id: id, sqlPath: sqlPath, metaPath: metaPath, contractPath: contractPath,
		meta: migrationMetadata{
			ID: id, CreatedAt: time.Now().UTC(), Status: "planned",
			Target: before.Host + "/" + before.Database, TargetID: targetID,
			Kind: plan.Kind, Summary: plan.Summary, SQLSHA256: hex.EncodeToString(statementHash[:]),
			RecoveryLevel: plan.RecoveryLevel, RecoveryNote: plan.RecoveryNote,
			BackupAcknowledged: plan.BackupRequired, BeforeSHA256: hex.EncodeToString(beforeHash[:]),
			ContractID: contract.ID, ContractVersion: contract.Version, ContractSHA256: contract.ContractSHA256,
			RiskLevel: contract.Risk.Level, AffectedObjects: contract.AffectedObjects, Verification: contract.Verification,
			ExecutionMode: executionMode,
		},
	}
	if err := j.writeMetadata(); err != nil {
		return nil, err
	}
	if err := updateMigrationIntegrity(dir); err != nil {
		return nil, err
	}
	return j, nil
}

func (j *migrationJournal) finish(status string, after *DatabaseWorkspace, applyErr error) error {
	j.meta.Status = status
	if strings.HasPrefix(status, "applied") {
		j.meta.AppliedAt = time.Now().UTC()
	}
	if applyErr != nil {
		j.meta.Error = applyErr.Error()
	}
	if after != nil {
		encoded, err := json.MarshalIndent(after.Schema, "", "  ")
		if err != nil {
			return err
		}
		sum := sha256.Sum256(encoded)
		j.meta.AfterSHA256 = hex.EncodeToString(sum[:])
		afterPath := strings.TrimSuffix(j.metaPath, ".json") + ".after.json"
		if err := writeSecure(afterPath, append(encoded, '\n')); err != nil {
			return err
		}
	}
	return j.writeMetadata()
}

func (j *migrationJournal) writeMetadata() error {
	encoded, err := json.MarshalIndent(j.meta, "", "  ")
	if err != nil {
		return err
	}
	return writeSecure(j.metaPath, append(encoded, '\n'))
}

func migrationStateDir() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "queryshift", "migrations"), nil
}

func writeSecure(path string, content []byte) error {
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

// updateMigrationIntegrity creates a deterministic checksum manifest. This is
// the same safety property as a migration-directory integrity file: edits to
// an already planned migration are visible before anyone trusts the history.
func updateMigrationIntegrity(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && (strings.HasSuffix(entry.Name(), ".sql") || strings.HasSuffix(entry.Name(), ".contract.json")) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	var body strings.Builder
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		fmt.Fprintf(&body, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return writeSecure(filepath.Join(dir, "queryshift.sum"), []byte(body.String()))
}

var createObjectPattern = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:UNIQUE\s+)?(MATERIALIZED\s+VIEW|TABLE|INDEX|VIEW|SCHEMA|TYPE|SEQUENCE)\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?((?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_$]*)(?:\.(?:"[^"]+"|[a-zA-Z_][a-zA-Z0-9_$]*))?)`)

func generatedCreateRollback(statement string) string {
	match := createObjectPattern.FindStringSubmatch(statement)
	if len(match) != 3 {
		return ""
	}
	return "DROP " + strings.ToUpper(strings.Join(strings.Fields(match[1]), " ")) + " IF EXISTS " + match[2]
}
