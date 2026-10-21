package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/spf13/cobra"
)

func newChangeRunInitCmd() *cobra.Command {
	var policyPath, output string
	cmd := &cobra.Command{
		Use: "run-init <contract-file>", Short: "Create one policy-evaluated Verified Change Run", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contract, policy, err := loadContractAndPolicy(args[0], policyPath)
			if err != nil {
				return err
			}
			run, err := app.NewVerifiedChangeRun(contract, policy, time.Now())
			if err != nil {
				return err
			}
			encoded, _ := json.MarshalIndent(run, "", "  ")
			return writeChangeOutput(cmd, output, append(encoded, '\n'))
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy JSON path (default: built-in policy)")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "run output path, or - for stdout")
	return cmd
}

func newChangeBundleCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use: "bundle <run-file>", Short: "Export a credential-free Git/CI review bundle", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := decodeChangeRunFile(args[0])
			if err != nil {
				return err
			}
			if output == "" {
				output = run.ID
			}
			bundle, err := app.ExportChangeRunBundle(run, output)
			if err != nil {
				return err
			}
			encoded, _ := json.Marshal(bundle)
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return err
		},
	}
	cmd.Flags().StringVarP(&output, "output", "o", "", "bundle directory (default: run ID)")
	return cmd
}

func newChangeBundleVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use: "bundle-verify <directory>", Short: "Verify every file digest in a change bundle", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			bundle, err := app.VerifyChangeRunBundle(args[0])
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "verified bundle %s for contract %s\n", bundle.RunID, bundle.ContractID)
			return err
		},
	}
}

func decodeChangeRunFile(path string) (app.VerifiedChangeRun, error) {
	content, err := readChangeFile(path, 8<<20)
	if err != nil {
		return app.VerifiedChangeRun{}, err
	}
	var run app.VerifiedChangeRun
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&run); err != nil {
		return app.VerifiedChangeRun{}, fmt.Errorf("decode change run: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return app.VerifiedChangeRun{}, err
	}
	if err := app.VerifyChangeRunAudit(run); err != nil {
		return app.VerifiedChangeRun{}, err
	}
	return run, nil
}
