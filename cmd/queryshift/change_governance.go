package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/spf13/cobra"
)

func newChangePolicyCmd() *cobra.Command {
	var policyPath string
	cmd := &cobra.Command{
		Use: "policy-check <contract-file>", Short: "Evaluate a contract against enterprise change policy", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contract, policy, err := loadContractAndPolicy(args[0], policyPath)
			if err != nil {
				return err
			}
			decision, err := app.EvaluateChangePolicy(contract, policy)
			if err != nil {
				return err
			}
			encoded, _ := json.Marshal(decision)
			if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(encoded)); err != nil {
				return err
			}
			if decision.Outcome == "deny" {
				return fmt.Errorf("change policy denied contract %s", contract.ID)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy JSON path (default: built-in enterprise policy)")
	return cmd
}

func newChangeApproveCmd() *cobra.Command {
	var policyPath, approver, output string
	var roles []string
	cmd := &cobra.Command{
		Use: "approve <contract-file>", Short: "Create an expiring signed approval artifact", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			contract, policy, err := loadContractAndPolicy(args[0], policyPath)
			if err != nil {
				return err
			}
			key := []byte(os.Getenv("QUERYSHIFT_APPROVAL_KEY"))
			approval, err := app.ApproveChange(contract, policy, approver, roles, key, time.Now())
			if err != nil {
				return err
			}
			encoded, _ := json.MarshalIndent(approval, "", "  ")
			return writeChangeOutput(cmd, output, append(encoded, '\n'))
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy JSON path (default: built-in enterprise policy)")
	cmd.Flags().StringVar(&approver, "approver", "", "authenticated approver identity")
	cmd.Flags().StringArrayVar(&roles, "role", nil, "approver role (repeatable)")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "approval output path, or - for stdout")
	return cmd
}

func newChangeApprovalVerifyCmd() *cobra.Command {
	var policyPath string
	cmd := &cobra.Command{
		Use: "approval-verify <contract-file> <approval-file>", Short: "Verify approval signature, binding, roles, and expiry", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			contract, policy, err := loadContractAndPolicy(args[0], policyPath)
			if err != nil {
				return err
			}
			content, err := readChangeFile(args[1], 1<<20)
			if err != nil {
				return err
			}
			var approval app.ChangeApproval
			if err := json.Unmarshal(content, &approval); err != nil {
				return fmt.Errorf("decode approval: %w", err)
			}
			if err := app.VerifyChangeApproval(contract, policy, approval, []byte(os.Getenv("QUERYSHIFT_APPROVAL_KEY")), time.Now()); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "{\"status\":\"verified\",\"approval_id\":%q,\"contract_id\":%q}\n", approval.ID, contract.ID)
			return err
		},
	}
	cmd.Flags().StringVar(&policyPath, "policy", "", "policy JSON path (default: built-in enterprise policy)")
	return cmd
}

func loadContractAndPolicy(contractPath, policyPath string) (app.ChangeContract, app.ChangePolicy, error) {
	content, err := readChangeFile(contractPath, 2<<20)
	if err != nil {
		return app.ChangeContract{}, app.ChangePolicy{}, err
	}
	contract, err := decodeChangeContract(content)
	if err != nil {
		return app.ChangeContract{}, app.ChangePolicy{}, err
	}
	policy := app.DefaultChangePolicy()
	if strings.TrimSpace(policyPath) != "" {
		content, err := readChangeFile(policyPath, 1<<20)
		if err != nil {
			return app.ChangeContract{}, app.ChangePolicy{}, err
		}
		if err := json.Unmarshal(content, &policy); err != nil {
			return app.ChangeContract{}, app.ChangePolicy{}, fmt.Errorf("decode policy: %w", err)
		}
	}
	return contract, policy, nil
}
