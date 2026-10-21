package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/sarankumar1325/queryshift/internal/erd"
)

const EnvironmentConfigVersion = "1.0"

type EnvironmentConfig struct {
	Version      string               `json:"version"`
	Environments []EnvironmentProfile `json:"environments"`
}

// EnvironmentProfile stores only a secret reference. DSNs must remain in the
// customer runner environment and are never serialized into project config.
type EnvironmentProfile struct {
	Name      string `json:"name"`
	Tier      string `json:"tier"`
	SecretRef string `json:"secret_ref"`
}

var environmentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
var environmentVariablePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]{0,127}$`)

func ValidateEnvironmentConfig(config EnvironmentConfig) error {
	if config.Version != EnvironmentConfigVersion {
		return fmt.Errorf("unsupported environment config version %q", config.Version)
	}
	seen := map[string]bool{}
	for _, profile := range config.Environments {
		if !environmentNamePattern.MatchString(profile.Name) {
			return fmt.Errorf("invalid environment name %q", profile.Name)
		}
		if seen[profile.Name] {
			return fmt.Errorf("duplicate environment %q", profile.Name)
		}
		seen[profile.Name] = true
		if profile.Tier != "development" && profile.Tier != "staging" && profile.Tier != "production" {
			return fmt.Errorf("environment %q has invalid tier %q", profile.Name, profile.Tier)
		}
		name := strings.TrimPrefix(profile.SecretRef, "env://")
		if name == profile.SecretRef || !environmentVariablePattern.MatchString(name) {
			return fmt.Errorf("environment %q secret_ref must be env://VARIABLE_NAME", profile.Name)
		}
	}
	return nil
}

func ResolveEnvironmentSecret(profile EnvironmentProfile) (string, error) {
	name := strings.TrimPrefix(profile.SecretRef, "env://")
	if name == profile.SecretRef || !environmentVariablePattern.MatchString(name) {
		return "", fmt.Errorf("invalid secret reference %q", profile.SecretRef)
	}
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return "", fmt.Errorf("environment secret %s is not set", name)
	}
	return value, nil
}

type SchemaDrift struct {
	Version    string         `json:"version"`
	ComparedAt time.Time      `json:"compared_at"`
	From       ChangeTarget   `json:"from"`
	To         ChangeTarget   `json:"to"`
	FromSHA256 string         `json:"from_schema_sha256"`
	ToSHA256   string         `json:"to_schema_sha256"`
	InSync     bool           `json:"in_sync"`
	Added      []SchemaChange `json:"added,omitempty"`
	Removed    []SchemaChange `json:"removed,omitempty"`
	Changed    []SchemaChange `json:"changed,omitempty"`
	Risk       string         `json:"risk"`
}

type SchemaChange struct {
	Kind     string `json:"kind"`
	Identity string `json:"identity"`
	Before   string `json:"before,omitempty"`
	After    string `json:"after,omitempty"`
}

func CompareDatabaseSchemas(from, to *DatabaseWorkspace, now time.Time) (SchemaDrift, error) {
	if from == nil || to == nil {
		return SchemaDrift{}, fmt.Errorf("both environment workspaces are required")
	}
	fromHash, err := workspaceSchemaSHA256(from)
	if err != nil {
		return SchemaDrift{}, err
	}
	toHash, err := workspaceSchemaSHA256(to)
	if err != nil {
		return SchemaDrift{}, err
	}
	left := flattenWorkspaceSchema(from.Schema)
	right := flattenWorkspaceSchema(to.Schema)
	drift := SchemaDrift{Version: "1.0", ComparedAt: now.UTC(), From: changeTarget(from), To: changeTarget(to), FromSHA256: fromHash, ToSHA256: toHash, InSync: fromHash == toHash, Risk: "none"}
	for identity, object := range right {
		if before, ok := left[identity]; !ok {
			drift.Added = append(drift.Added, SchemaChange{Kind: object.Kind, Identity: identity, After: object.Definition})
		} else if before.Definition != object.Definition {
			drift.Changed = append(drift.Changed, SchemaChange{Kind: object.Kind, Identity: identity, Before: before.Definition, After: object.Definition})
		}
	}
	for identity, object := range left {
		if _, ok := right[identity]; !ok {
			drift.Removed = append(drift.Removed, SchemaChange{Kind: object.Kind, Identity: identity, Before: object.Definition})
		}
	}
	sortSchemaChanges(drift.Added)
	sortSchemaChanges(drift.Removed)
	sortSchemaChanges(drift.Changed)
	if len(drift.Removed) > 0 {
		drift.Risk = "high"
	} else if len(drift.Changed) > 0 {
		drift.Risk = "elevated"
	} else if len(drift.Added) > 0 {
		drift.Risk = "controlled"
	}
	return drift, nil
}

type flatSchemaObject struct{ Kind, Definition string }

func flattenWorkspaceSchema(schema erd.Schema) map[string]flatSchemaObject {
	objects := map[string]flatSchemaObject{}
	for _, table := range schema.Tables {
		tableID := "table:" + table.Schema + "." + table.Name
		objects[tableID] = flatSchemaObject{Kind: "table", Definition: table.Schema + "." + table.Name}
		for _, column := range table.Columns {
			definition := column.Type
			if column.PK {
				definition += " PK"
			}
			if column.FKTarget != "" {
				definition += " FK->" + column.FKTarget
			}
			objects["column:"+table.Schema+"."+table.Name+"."+column.Name] = flatSchemaObject{Kind: "column", Definition: definition}
		}
		for _, index := range table.Indexes {
			definition := index.Def
			if index.Unique {
				definition += " UNIQUE"
			}
			objects["index:"+table.Schema+"."+table.Name+"."+index.Name] = flatSchemaObject{Kind: "index", Definition: definition}
		}
	}
	for _, edge := range schema.Edges {
		identity := "foreign_key:" + edge.FromTable + "." + edge.FromColumn + "->" + edge.ToTable + "." + edge.ToColumn
		objects[identity] = flatSchemaObject{Kind: "foreign_key", Definition: identity}
	}
	return objects
}

func sortSchemaChanges(changes []SchemaChange) {
	sort.Slice(changes, func(i, j int) bool { return changes[i].Identity < changes[j].Identity })
}

// MarshalEnvironmentConfig validates before serialization so a raw DSN can
// never accidentally be accepted as a secret reference.
func MarshalEnvironmentConfig(config EnvironmentConfig) ([]byte, error) {
	if err := ValidateEnvironmentConfig(config); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func schemaDriftDigest(drift SchemaDrift) string {
	encoded, _ := json.Marshal(drift)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
