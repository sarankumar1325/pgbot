package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/spf13/cobra"
)

func newChangeNeonRehearseCmd() *cobra.Command {
	var projectID, parentID, output string
	var keep bool
	cmd := &cobra.Command{
		Use: "rehearse-neon <contract-file>", Short: "Create a disposable Neon branch, rehearse, verify, and clean it up", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := readChangeFile(args[0], 2<<20)
			if err != nil {
				return err
			}
			contract, err := decodeChangeContract(content)
			if err != nil {
				return err
			}
			provider := app.NeonRehearsalProvider{ProjectID: projectID, ParentID: parentID, APIKey: os.Getenv("NEON_API_KEY")}
			receipt, target, err := app.RehearseWithProvider(cmd.Context(), provider, contract, keep)
			if err != nil {
				return err
			}
			result := struct {
				Target  app.DisposableRehearsalTarget `json:"target"`
				Kept    bool                          `json:"kept"`
				Receipt *app.RehearsalReceipt         `json:"receipt"`
			}{Target: target, Kept: keep, Receipt: receipt}
			encoded, _ := json.MarshalIndent(result, "", "  ")
			if err := writeChangeOutput(cmd, output, append(encoded, '\n')); err != nil {
				return err
			}
			if receipt.Status != "verified" {
				return fmt.Errorf("Neon rehearsal status is %s; production remains locked", receipt.Status)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&projectID, "project-id", "", "Neon project ID")
	cmd.Flags().StringVar(&parentID, "parent-branch-id", "", "source branch ID (default: Neon project default branch)")
	cmd.Flags().BoolVar(&keep, "keep", false, "keep the rehearsal branch after verification")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "receipt output path, or - for stdout")
	_ = cmd.MarkFlagRequired("project-id")
	return cmd
}
