package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/spf13/cobra"
)

func newRunnerCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "runner", Short: "Create and execute signed requests inside the customer network", Args: cobra.NoArgs}
	cmd.AddCommand(newRunnerRequestCmd(), newRunnerVerifyCmd(), newRunnerExecuteCmd(), newAuditExportCmd())
	return cmd
}

func newRunnerRequestCmd() *cobra.Command {
	var environment, subject, issuer, nonce, confirmation, output string
	var roles []string
	cmd := &cobra.Command{
		Use: "request <run-file>", Short: "Create a short-lived signed runner request", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run, err := decodeChangeRunFile(args[0])
			if err != nil {
				return err
			}
			now := time.Now()
			principal := app.Principal{Subject: subject, Issuer: issuer, Roles: roles, AuthenticatedAt: now.UTC(), ExpiresAt: now.Add(time.Hour).UTC()}
			request, err := app.NewRunnerRequest(run, environment, principal, confirmation, nonce, 5*time.Minute, []byte(os.Getenv("QUERYSHIFT_RUNNER_KEY")), now)
			if err != nil {
				return err
			}
			encoded, _ := json.MarshalIndent(request, "", "  ")
			return writeChangeOutput(cmd, output, append(encoded, '\n'))
		},
	}
	cmd.Flags().StringVar(&environment, "environment", "", "configured environment profile")
	cmd.Flags().StringVar(&subject, "subject", "", "authenticated principal subject")
	cmd.Flags().StringVar(&issuer, "issuer", "", "identity issuer")
	cmd.Flags().StringArrayVar(&roles, "role", nil, "principal role (repeatable)")
	cmd.Flags().StringVar(&nonce, "nonce", "", "unique anti-replay nonce")
	cmd.Flags().StringVar(&confirmation, "change-confirmation", "", "exact contract confirmation phrase")
	cmd.Flags().StringVarP(&output, "output", "o", "-", "request output path")
	return cmd
}

func newRunnerVerifyCmd() *cobra.Command {
	return &cobra.Command{Use: "verify <request-file>", Short: "Verify runner request signature, lifetime, run, and audit chain", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		request, err := decodeRunnerRequest(args[0])
		if err != nil {
			return err
		}
		if err := app.VerifyRunnerRequest(request, []byte(os.Getenv("QUERYSHIFT_RUNNER_KEY")), time.Now()); err != nil {
			return err
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "verified runner request %s\n", request.ID)
		return err
	}}
}

func newRunnerExecuteCmd() *cobra.Command {
	var confirmation string
	cmd := &cobra.Command{Use: "execute <request-file> <environments-file>", Short: "Execute an authorized request using a local secret reference", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		request, err := decodeRunnerRequest(args[0])
		if err != nil {
			return err
		}
		if confirmation != request.ID {
			return fmt.Errorf("--confirm-request must exactly match %s", request.ID)
		}
		content, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		var environments app.EnvironmentConfig
		if err := json.Unmarshal(content, &environments); err != nil {
			return fmt.Errorf("decode environments: %w", err)
		}
		nonces, err := app.DefaultRunnerNonceStore()
		if err != nil {
			return err
		}
		result, err := app.ExecuteRunnerRequest(cmd.Context(), request, environments, app.DefaultCapabilityPolicy(), []byte(os.Getenv("QUERYSHIFT_RUNNER_KEY")), []byte(os.Getenv("QUERYSHIFT_APPROVAL_KEY")), nonces, time.Now())
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(result)
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
		return err
	}}
	cmd.Flags().StringVar(&confirmation, "confirm-request", "", "must exactly match the signed request ID")
	return cmd
}

func newAuditExportCmd() *cobra.Command {
	return &cobra.Command{Use: "audit-export <run-file>...", Short: "Export tamper-evident audit JSONL", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		runs := make([]app.VerifiedChangeRun, 0, len(args))
		for _, path := range args {
			run, err := decodeChangeRunFile(path)
			if err != nil {
				return err
			}
			runs = append(runs, run)
		}
		return app.ExportAuditJSONL(cmd.OutOrStdout(), runs)
	}}
}

func decodeRunnerRequest(path string) (app.RunnerRequest, error) {
	content, err := readChangeFile(path, 16<<20)
	if err != nil {
		return app.RunnerRequest{}, err
	}
	var request app.RunnerRequest
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return app.RunnerRequest{}, fmt.Errorf("decode runner request: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return app.RunnerRequest{}, err
	}
	if strings.TrimSpace(request.ID) == "" {
		return app.RunnerRequest{}, fmt.Errorf("runner request ID is missing")
	}
	return request, nil
}
