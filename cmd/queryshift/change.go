package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/spf13/cobra"
)

type changeWorkspaceLoader func(context.Context, string) (*app.DatabaseWorkspace, error)
type changeRehearser func(context.Context, string, app.ChangeContract, string) (*app.RehearsalReceipt, error)

func newChangeCmd() *cobra.Command {
	return newChangeCmdWithLoader(app.LoadDatabase)
}

func newChangeCmdWithLoader(load changeWorkspaceLoader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "change",
		Short: "Generate and verify immutable database Change Contracts",
		Long: "Create a machine-readable proof artifact for one proposed database statement,\n" +
			"or verify an existing contract against the current target and schema. These\n" +
			"commands are read-only and never apply the statement.",
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newChangePlanCmd(load), newChangeVerifyCmd(load), newChangeRehearseCmd(app.RehearseChangeContract), newChangeNeonRehearseCmd(), newChangePolicyCmd(), newChangeApproveCmd(), newChangeApprovalVerifyCmd(), newChangeRunInitCmd(), newChangeBundleCmd(), newChangeBundleVerifyCmd())
	return cmd
}

func newChangePlanCmd(load changeWorkspaceLoader) *cobra.Command {
	var connectionString, output, proposer string
	cmd := &cobra.Command{
		Use:   "plan <sql-file>",
		Short: "Create a Change Contract from SQL and a live schema snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			statement, err := readChangeFile(args[0], 1<<20)
			if err != nil {
				return err
			}
			dsn := firstNonEmpty(connectionString, os.Getenv("DATABASE_URL"), os.Getenv("QUERYSHIFT_DATABASE_URL"), pgServiceFallback())
			if dsn == "" {
				return fmt.Errorf("no connection string (pass --url or set $DATABASE_URL)")
			}
			workspace, err := load(cmd.Context(), dsn)
			if err != nil {
				return fmt.Errorf("load target schema: %w", err)
			}
			actor := firstNonEmpty(proposer, os.Getenv("QUERYSHIFT_ACTOR"))
			contract, err := app.BuildChangeContractForActor(string(statement), workspace, actor)
			if err != nil {
				return fmt.Errorf("build change contract: %w", err)
			}
			encoded, err := json.MarshalIndent(contract, "", "  ")
			if err != nil {
				return fmt.Errorf("encode change contract: %w", err)
			}
			return writeChangeOutput(cmd, output, append(encoded, '\n'))
		},
	}
	cmd.Flags().StringVar(&connectionString, "url", "", "connection string (else $DATABASE_URL)")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "contract output path, or - for stdout")
	cmd.Flags().StringVar(&proposer, "proposer", "", "proposer identity (else $QUERYSHIFT_ACTOR)")
	return cmd
}

func newChangeVerifyCmd(load changeWorkspaceLoader) *cobra.Command {
	var connectionString string
	cmd := &cobra.Command{
		Use:   "verify <contract-file>",
		Short: "Fail unless a Change Contract still matches the target schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readChangeFile(args[0], 2<<20)
			if err != nil {
				return err
			}
			contract, err := decodeChangeContract(content)
			if err != nil {
				return err
			}
			dsn := firstNonEmpty(connectionString, os.Getenv("DATABASE_URL"), os.Getenv("QUERYSHIFT_DATABASE_URL"), pgServiceFallback())
			if dsn == "" {
				return fmt.Errorf("no connection string (pass --url or set $DATABASE_URL)")
			}
			workspace, err := load(cmd.Context(), dsn)
			if err != nil {
				return fmt.Errorf("load target schema: %w", err)
			}
			if err := app.ValidateChangeContract(contract, workspace); err != nil {
				return fmt.Errorf("change contract verification failed: %w", err)
			}
			receipt := struct {
				Status       string    `json:"status"`
				ContractID   string    `json:"contract_id"`
				Target       string    `json:"target"`
				SchemaSHA256 string    `json:"schema_sha256"`
				VerifiedAt   time.Time `json:"verified_at"`
			}{"verified", contract.ID, contract.Target.Database, contract.SchemaSHA256, time.Now().UTC()}
			encoded, err := json.Marshal(receipt)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return err
		},
	}
	cmd.Flags().StringVar(&connectionString, "url", "", "connection string (else $DATABASE_URL)")
	return cmd
}

func newChangeRehearseCmd(rehearse changeRehearser) *cobra.Command {
	var connectionString, confirmation, output string
	cmd := &cobra.Command{
		Use:   "rehearse <contract-file>",
		Short: "Apply a contract to a distinct disposable target and emit a verification receipt",
		Long: "Apply the reviewed statement only when the supplied target has a different\n" +
			"fingerprint and the exact same starting schema. This command never reads\n" +
			"DATABASE_URL; use --rehearsal-url or QUERYSHIFT_REHEARSAL_DATABASE_URL.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readChangeFile(args[0], 2<<20)
			if err != nil {
				return err
			}
			contract, err := decodeChangeContract(content)
			if err != nil {
				return err
			}
			dsn := firstNonEmpty(connectionString, os.Getenv("QUERYSHIFT_REHEARSAL_DATABASE_URL"))
			if dsn == "" {
				return fmt.Errorf("no rehearsal connection string (pass --rehearsal-url or set $QUERYSHIFT_REHEARSAL_DATABASE_URL)")
			}
			receipt, err := rehearse(cmd.Context(), dsn, contract, confirmation)
			if err != nil {
				return fmt.Errorf("rehearse change: %w", err)
			}
			encoded, err := json.MarshalIndent(receipt, "", "  ")
			if err != nil {
				return fmt.Errorf("encode rehearsal receipt: %w", err)
			}
			if err := writeChangeOutput(cmd, output, append(encoded, '\n')); err != nil {
				return err
			}
			if receipt.Status == "failed" {
				return fmt.Errorf("rehearsal verification failed; inspect the receipt")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&connectionString, "rehearsal-url", "", "disposable database connection (else $QUERYSHIFT_REHEARSAL_DATABASE_URL)")
	cmd.Flags().StringVar(&confirmation, "confirm", "", "must exactly match REHEARSE <contract-id>")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "receipt output path, or - for stdout")
	return cmd
}

func decodeChangeContract(content []byte) (app.ChangeContract, error) {
	var contract app.ChangeContract
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&contract); err != nil {
		return app.ChangeContract{}, fmt.Errorf("decode change contract: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return app.ChangeContract{}, err
	}
	return contract, nil
}

func readChangeFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d-byte limit", path, limit)
	}
	if strings.TrimSpace(string(content)) == "" {
		return nil, fmt.Errorf("%s is empty", path)
	}
	return content, nil
}

func writeChangeOutput(cmd *cobra.Command, path string, content []byte) error {
	if path == "" || path == "-" {
		_, err := cmd.OutOrStdout().Write(content)
		return err
	}
	dir := filepath.Dir(path)
	temporary, err := os.CreateTemp(dir, ".queryshift-contract-*")
	if err != nil {
		return fmt.Errorf("write change contract: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure change contract: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("write change contract: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close change contract: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish change contract: %w", err)
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing contract data: %w", err)
	}
	return fmt.Errorf("change contract contains multiple JSON values")
}
