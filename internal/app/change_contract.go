package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sarankumar1325/queryshift/internal/erd"
)

const ChangeContractVersion = "1.0"

// ChangeContract is the immutable, machine-readable review boundary between a
// proposed database change and execution. The host verifies its digest, target,
// schema snapshot, safety analysis, and approval phrase immediately before use.
type ChangeContract struct {
	Version             string              `json:"version"`
	ID                  string              `json:"id"`
	ContractSHA256      string              `json:"contract_sha256"`
	CreatedAt           time.Time           `json:"created_at"`
	Proposer            string              `json:"proposer,omitempty"`
	Target              ChangeTarget        `json:"target"`
	Statement           string              `json:"statement"`
	StatementSHA256     string              `json:"statement_sha256"`
	SchemaSHA256        string              `json:"schema_sha256"`
	Plan                ChangePlan          `json:"plan"`
	Risk                ChangeRisk          `json:"risk"`
	AffectedObjects     []ChangeObject      `json:"affected_objects"`
	Dependencies        []string            `json:"dependencies,omitempty"`
	Verification        []VerificationProbe `json:"verification"`
	Evidence            []ContractEvidence  `json:"evidence"`
	WriteUnlockRequired bool                `json:"write_unlock_required"`
}

type ChangeTarget struct {
	Database    string `json:"database"`
	Provider    string `json:"provider,omitempty"`
	Fingerprint string `json:"fingerprint"`
}

type ChangeRisk struct {
	Level       string   `json:"level"`
	Reasons     []string `json:"reasons"`
	LockImpact  string   `json:"lock_impact"`
	DataImpact  string   `json:"data_impact"`
	Reversible  bool     `json:"reversible"`
	BackupProof bool     `json:"backup_proof_required"`
}

type ChangeObject struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Operation string `json:"operation"`
}

type VerificationProbe struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type ContractEvidence struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Source string `json:"source"`
}

// BuildChangeContract converts a reviewed statement and catalog snapshot into
// a content-addressed contract. No database values or credentials are included.
func BuildChangeContract(statement string, workspace *DatabaseWorkspace) (ChangeContract, error) {
	return BuildChangeContractForActor(statement, workspace, "")
}

func BuildChangeContractForActor(statement string, workspace *DatabaseWorkspace, proposer string) (ChangeContract, error) {
	if workspace == nil {
		return ChangeContract{}, fmt.Errorf("a verified database workspace is required")
	}
	plan, err := AnalyzeChange(statement)
	if err != nil {
		return ChangeContract{}, err
	}
	statement = strings.TrimSpace(statement)
	statementHash := sha256.Sum256([]byte(statement))
	schemaHash, err := workspaceSchemaSHA256(workspace)
	if err != nil {
		return ChangeContract{}, err
	}
	contract := ChangeContract{
		Version:         ChangeContractVersion,
		CreatedAt:       time.Now().UTC(),
		Proposer:        strings.TrimSpace(proposer),
		Target:          changeTarget(workspace),
		Statement:       statement,
		StatementSHA256: hex.EncodeToString(statementHash[:]),
		SchemaSHA256:    schemaHash,
		Plan:            plan,
		Risk:            classifyChangeRisk(statement, plan),
		AffectedObjects: affectedChangeObjects(statement),
		Verification:    verificationProbes(statement, plan),
		Evidence: []ContractEvidence{{
			ID: "CE1", Kind: "schema_snapshot", SHA256: schemaHash, Source: "queryshift.workspace",
		}, {
			ID: "CE2", Kind: "deterministic_safety_analysis", SHA256: hex.EncodeToString(statementHash[:]), Source: "queryshift.AnalyzeChange",
		}},
		WriteUnlockRequired: true,
	}
	contract.Dependencies = changeDependencies(contract.AffectedObjects, workspace.Schema)
	digest, err := changeContractDigest(contract)
	if err != nil {
		return ChangeContract{}, err
	}
	contract.ContractSHA256 = digest
	contract.ID = "qsc_" + digest[:16]
	return contract, nil
}

// ValidateChangeContract fails closed if any reviewed field was changed or if
// the connected target/schema no longer matches the review snapshot.
func ValidateChangeContract(contract ChangeContract, workspace *DatabaseWorkspace) error {
	if err := validateChangeContractIntegrity(contract); err != nil {
		return err
	}
	if workspace == nil {
		return nil
	}
	if contract.Target != changeTarget(workspace) {
		return fmt.Errorf("database target changed since review; review the change again")
	}
	return validateContractSchema(contract, workspace)
}

// ValidateChangeContractForRehearsal accepts a different target only when it
// starts from the exact reviewed schema. This prevents a rehearsal flag from
// becoming a second route to mutate the reviewed production target.
func ValidateChangeContractForRehearsal(contract ChangeContract, workspace *DatabaseWorkspace) error {
	if err := validateChangeContractIntegrity(contract); err != nil {
		return err
	}
	if workspace == nil {
		return fmt.Errorf("a verified rehearsal workspace is required")
	}
	if contract.Target.Fingerprint == changeTarget(workspace).Fingerprint {
		return fmt.Errorf("rehearsal target matches the reviewed target; use a disposable clone or branch")
	}
	return validateContractSchema(contract, workspace)
}

func validateChangeContractIntegrity(contract ChangeContract) error {
	if contract.Version != ChangeContractVersion {
		return fmt.Errorf("unsupported change contract version %q", contract.Version)
	}
	if contract.ID == "" || contract.ContractSHA256 == "" {
		return fmt.Errorf("change contract identity is missing")
	}
	digest, err := changeContractDigest(contract)
	if err != nil {
		return err
	}
	if contract.ContractSHA256 != digest || contract.ID != "qsc_"+digest[:16] {
		return fmt.Errorf("change contract integrity check failed; review the change again")
	}
	statementHash := sha256.Sum256([]byte(strings.TrimSpace(contract.Statement)))
	if contract.StatementSHA256 != hex.EncodeToString(statementHash[:]) {
		return fmt.Errorf("reviewed SQL does not match the change contract")
	}
	plan, err := AnalyzeChange(contract.Statement)
	if err != nil {
		return fmt.Errorf("revalidate reviewed SQL: %w", err)
	}
	if plan != contract.Plan {
		return fmt.Errorf("safety classification changed; review the change again")
	}
	if !contract.WriteUnlockRequired {
		return fmt.Errorf("change contract cannot disable the WRITE unlock")
	}
	return nil
}

func validateContractSchema(contract ChangeContract, workspace *DatabaseWorkspace) error {
	schemaHash, err := workspaceSchemaSHA256(workspace)
	if err != nil {
		return err
	}
	if contract.SchemaSHA256 != schemaHash {
		return fmt.Errorf("database schema changed since review; refresh and review the change again")
	}
	return nil
}

func changeContractDigest(contract ChangeContract) (string, error) {
	contract.ID = ""
	contract.ContractSHA256 = ""
	encoded, err := json.Marshal(contract)
	if err != nil {
		return "", fmt.Errorf("encode change contract: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func changeTarget(workspace *DatabaseWorkspace) ChangeTarget {
	digest := sha256.Sum256([]byte(workspace.Host + "|" + workspace.Database))
	return ChangeTarget{
		Database: workspace.Database, Provider: workspace.Provider,
		Fingerprint: hex.EncodeToString(digest[:8]),
	}
}

func workspaceSchemaSHA256(workspace *DatabaseWorkspace) (string, error) {
	// Database size and server-version display metadata can change without a DDL
	// change. Bind execution to canonical structure only so normal traffic does
	// not create false schema-drift failures.
	tables := append([]erd.Table(nil), workspace.Schema.Tables...)
	for index := range tables {
		tables[index].Columns = append([]erd.Column(nil), tables[index].Columns...)
		tables[index].Indexes = append([]erd.Index(nil), tables[index].Indexes...)
		sort.Slice(tables[index].Indexes, func(left, right int) bool {
			return tables[index].Indexes[left].Name < tables[index].Indexes[right].Name
		})
	}
	sort.Slice(tables, func(left, right int) bool {
		if tables[left].Schema != tables[right].Schema {
			return tables[left].Schema < tables[right].Schema
		}
		return tables[left].Name < tables[right].Name
	})
	edges := append([]erd.Edge(nil), workspace.Schema.Edges...)
	sort.Slice(edges, func(left, right int) bool {
		leftKey := edges[left].FromTable + "|" + edges[left].FromColumn + "|" + edges[left].ToTable + "|" + edges[left].ToColumn
		rightKey := edges[right].FromTable + "|" + edges[right].FromColumn + "|" + edges[right].ToTable + "|" + edges[right].ToColumn
		return leftKey < rightKey
	})
	encoded, err := json.Marshal(struct {
		Tables []erd.Table `json:"tables"`
		Edges  []erd.Edge  `json:"edges"`
	}{Tables: tables, Edges: edges})
	if err != nil {
		return "", fmt.Errorf("encode schema snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func classifyChangeRisk(statement string, plan ChangePlan) ChangeRisk {
	upper := strings.ToUpper(stripLeadingSQLNoise(statement))
	risk := ChangeRisk{
		Level: "controlled", Reasons: []string{"one statement in one transaction"},
		LockImpact: "bounded by a 3s lock timeout", DataImpact: "no destructive operation detected",
		Reversible: plan.RollbackSQL != "", BackupProof: plan.BackupRequired,
	}
	if plan.Kind == "ALTER" || (plan.Kind == "CREATE" && containsKeyword(upper, "INDEX") && !containsKeyword(upper, "CONCURRENTLY")) {
		risk.Level = "elevated"
		risk.Reasons = append(risk.Reasons, "operation may acquire a table-level lock")
		risk.LockImpact = "may block concurrent writes until the 3s lock timeout"
	}
	if plan.BackupRequired {
		risk.Level = "high"
		risk.Reasons = append(risk.Reasons, "previous data cannot be reconstructed from the statement")
		risk.DataImpact = "provider backup or PITR evidence is required for recovery"
	}
	if plan.Destructive {
		risk.Level = "critical"
		risk.Reasons = append(risk.Reasons, "destructive operation detected")
		risk.DataImpact = "data or schema members can be permanently removed"
	}
	return risk
}

var changeObjectPatterns = []struct {
	kind, operation string
	re              *regexp.Regexp
}{
	{"table", "insert", regexp.MustCompile(`(?is)^\s*INSERT\s+INTO\s+([^\s(;]+)`)},
	{"table", "update", regexp.MustCompile(`(?is)^\s*UPDATE\s+([^\s(;]+)`)},
	{"table", "delete", regexp.MustCompile(`(?is)^\s*DELETE\s+FROM\s+([^\s(;]+)`)},
	{"table", "truncate", regexp.MustCompile(`(?is)^\s*TRUNCATE\s+(?:TABLE\s+)?([^\s(;]+)`)},
	{"schema_object", "create", regexp.MustCompile(`(?is)^\s*CREATE\s+(?:UNIQUE\s+)?(?:TABLE|INDEX|VIEW|MATERIALIZED\s+VIEW|SCHEMA|TYPE|SEQUENCE)\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?([^\s(;]+)`)},
	{"schema_object", "alter", regexp.MustCompile(`(?is)^\s*ALTER\s+(?:TABLE|INDEX|VIEW|MATERIALIZED\s+VIEW|SCHEMA|TYPE|SEQUENCE)\s+(?:IF\s+EXISTS\s+)?([^\s(;]+)`)},
	{"schema_object", "drop", regexp.MustCompile(`(?is)^\s*DROP\s+(?:TABLE|INDEX|VIEW|MATERIALIZED\s+VIEW|SCHEMA|TYPE|SEQUENCE)\s+(?:IF\s+EXISTS\s+)?([^\s(;]+)`)},
	{"schema_object", "comment", regexp.MustCompile(`(?is)^\s*COMMENT\s+ON\s+(?:TABLE|COLUMN|VIEW|MATERIALIZED\s+VIEW|INDEX|SCHEMA|TYPE|SEQUENCE)\s+([^\s(;]+)`)},
}

var createIndexTablePattern = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:IF\s+NOT\s+EXISTS\s+)?[^\s(;]+\s+ON\s+(?:ONLY\s+)?([^\s(;]+)`)

func affectedChangeObjects(statement string) []ChangeObject {
	var objects []ChangeObject
	for _, pattern := range changeObjectPatterns {
		match := pattern.re.FindStringSubmatch(statement)
		if len(match) == 2 {
			objects = append(objects, ChangeObject{Kind: pattern.kind, Name: strings.TrimSpace(match[1]), Operation: pattern.operation})
			break
		}
	}
	if match := createIndexTablePattern.FindStringSubmatch(statement); len(match) == 2 {
		objects = append(objects, ChangeObject{Kind: "table", Name: strings.TrimSpace(match[1]), Operation: "lock/read"})
	}
	return objects
}

func changeDependencies(objects []ChangeObject, schema erd.Schema) []string {
	names := map[string]bool{}
	for _, object := range objects {
		name := unqualifiedObjectName(object.Name)
		for _, edge := range schema.Edges {
			if edge.FromTable == name {
				names[edge.ToTable+" (referenced by "+name+")"] = true
			}
			if edge.ToTable == name {
				names[edge.FromTable+" (references "+name+")"] = true
			}
		}
	}
	dependencies := make([]string, 0, len(names))
	for name := range names {
		dependencies = append(dependencies, name)
	}
	sort.Strings(dependencies)
	return dependencies
}

func unqualifiedObjectName(name string) string {
	name = strings.Trim(name, `"`)
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	return strings.Trim(name, `"`)
}

func verificationProbes(statement string, plan ChangePlan) []VerificationProbe {
	probes := []VerificationProbe{{
		ID: "VP1", Kind: "execution_receipt", Description: "Capture the server command tag, affected row count, and duration", Required: true,
	}}
	if plan.Kind == "INSERT" || plan.Kind == "UPDATE" || plan.Kind == "DELETE" || plan.Kind == "TRUNCATE" {
		probes = append(probes, VerificationProbe{
			ID: "VP2", Kind: "application_invariant", Description: "Verify the affected business invariant with an explicit read query", Required: true,
		})
	} else {
		probes = append(probes, VerificationProbe{
			ID: "VP2", Kind: "schema_diff", Description: "Capture the post-change schema and compare it with the reviewed snapshot", Required: true,
		})
	}
	if strings.EqualFold(plan.Kind, "CREATE") && strings.Contains(strings.ToUpper(statement), "INDEX") {
		probes = append(probes, VerificationProbe{
			ID: "VP3", Kind: "index_validity", Description: "Verify the new index is present and valid before relying on it", Required: true,
		})
	}
	return probes
}
