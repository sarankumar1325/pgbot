package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sarankumar1325/queryshift/internal/app"
	"github.com/spf13/cobra"
)

func newEnvironmentsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "environments", Short: "Compare and validate database environments", Args: cobra.NoArgs}
	cmd.AddCommand(newEnvironmentDiffCmd(app.LoadDatabase), newEnvironmentConfigCheckCmd())
	return cmd
}

func newEnvironmentDiffCmd(load changeWorkspaceLoader) *cobra.Command {
	var fromURL, toURL string
	cmd := &cobra.Command{
		Use: "diff", Short: "Compare two live schemas without reading table values", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if fromURL == "" || toURL == "" {
				return fmt.Errorf("both --from-url and --to-url are required")
			}
			from, err := load(cmd.Context(), fromURL)
			if err != nil {
				return fmt.Errorf("load source environment: %w", err)
			}
			to, err := load(cmd.Context(), toURL)
			if err != nil {
				return fmt.Errorf("load destination environment: %w", err)
			}
			drift, err := app.CompareDatabaseSchemas(from, to, time.Now())
			if err != nil {
				return err
			}
			encoded, _ := json.MarshalIndent(drift, "", "  ")
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(encoded))
			return err
		},
	}
	cmd.Flags().StringVar(&fromURL, "from-url", "", "source environment PostgreSQL URL")
	cmd.Flags().StringVar(&toURL, "to-url", "", "destination environment PostgreSQL URL")
	return cmd
}

func newEnvironmentConfigCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use: "config-check <file>", Short: "Validate credential-free environment profiles", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			content, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var config app.EnvironmentConfig
			if err := json.Unmarshal(content, &config); err != nil {
				return fmt.Errorf("decode environment config: %w", err)
			}
			if err := app.ValidateEnvironmentConfig(config); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "valid: %d credential-free environment profiles\n", len(config.Environments))
			return err
		},
	}
}
