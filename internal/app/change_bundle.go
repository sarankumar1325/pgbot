package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ChangeBundle struct {
	Version    string            `json:"version"`
	RunID      string            `json:"run_id"`
	ContractID string            `json:"contract_id"`
	Files      map[string]string `json:"files"`
}

// ExportChangeRunBundle writes a deterministic, credential-free directory for
// Git review or CI verification. Existing files are replaced individually;
// unrelated directory contents are never removed.
func ExportChangeRunBundle(run VerifiedChangeRun, directory string) (ChangeBundle, error) {
	if err := run.validateIdentity(); err != nil {
		return ChangeBundle{}, err
	}
	if err := VerifyChangeRunAudit(run); err != nil {
		return ChangeBundle{}, err
	}
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return ChangeBundle{}, fmt.Errorf("bundle directory is required")
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ChangeBundle{}, fmt.Errorf("create bundle directory: %w", err)
	}

	files := map[string][]byte{}
	files["change.sql"] = []byte("-- QueryShift Change Contract " + run.Contract.ID + "\n" + strings.TrimSpace(run.Contract.Statement) + ";\n")
	files["contract.json"] = prettyJSON(run.Contract)
	files["policy.json"] = prettyJSON(run.Policy)
	files["policy-decision.json"] = prettyJSON(run.Decision)
	files["run.json"] = prettyJSON(run)
	if run.Rehearsal != nil {
		files["rehearsal.json"] = prettyJSON(run.Rehearsal)
	}
	if run.Approval != nil {
		files["approval.json"] = prettyJSON(run.Approval)
	}
	if run.Execution != nil {
		files["execution.json"] = prettyJSON(run.Execution)
	}
	if len(run.Verification) > 0 {
		files["verification.json"] = prettyJSON(run.Verification)
	}

	bundle := ChangeBundle{Version: "1.0", RunID: run.ID, ContractID: run.Contract.ID, Files: map[string]string{}}
	for name, content := range files {
		if err := writeSecure(filepath.Join(directory, name), content); err != nil {
			return ChangeBundle{}, fmt.Errorf("write %s: %w", name, err)
		}
		digest := sha256.Sum256(content)
		bundle.Files[name] = hex.EncodeToString(digest[:])
	}
	manifest := bundleManifest(bundle)
	if err := writeSecure(filepath.Join(directory, "queryshift.bundle.sum"), []byte(manifest)); err != nil {
		return ChangeBundle{}, err
	}
	if err := writeSecure(filepath.Join(directory, "bundle.json"), prettyJSON(bundle)); err != nil {
		return ChangeBundle{}, err
	}
	return bundle, nil
}

func VerifyChangeRunBundle(directory string) (ChangeBundle, error) {
	encoded, err := os.ReadFile(filepath.Join(directory, "bundle.json"))
	if err != nil {
		return ChangeBundle{}, fmt.Errorf("read bundle manifest: %w", err)
	}
	var bundle ChangeBundle
	if err := json.Unmarshal(encoded, &bundle); err != nil {
		return ChangeBundle{}, fmt.Errorf("decode bundle manifest: %w", err)
	}
	if bundle.Version != "1.0" || bundle.RunID == "" || bundle.ContractID == "" {
		return ChangeBundle{}, fmt.Errorf("invalid change bundle identity")
	}
	checksumManifest, err := os.ReadFile(filepath.Join(directory, "queryshift.bundle.sum"))
	if err != nil {
		return ChangeBundle{}, fmt.Errorf("read checksum manifest: %w", err)
	}
	if string(checksumManifest) != bundleManifest(bundle) {
		return ChangeBundle{}, fmt.Errorf("bundle checksum manifest does not match bundle.json")
	}
	for name, expected := range bundle.Files {
		if filepath.Base(name) != name {
			return ChangeBundle{}, fmt.Errorf("invalid bundle path %q", name)
		}
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return ChangeBundle{}, fmt.Errorf("read %s: %w", name, err)
		}
		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != expected {
			return ChangeBundle{}, fmt.Errorf("bundle integrity check failed for %s", name)
		}
	}
	return bundle, nil
}

func bundleManifest(bundle ChangeBundle) string {
	names := make([]string, 0, len(bundle.Files))
	for name := range bundle.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	var body strings.Builder
	for _, name := range names {
		fmt.Fprintf(&body, "%s  %s\n", bundle.Files[name], name)
	}
	return body.String()
}

func prettyJSON(value any) []byte {
	encoded, _ := json.MarshalIndent(value, "", "  ")
	return append(encoded, '\n')
}
