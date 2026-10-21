// Command queryshift — in-database observability for PostgreSQL. Slice 1 ships the
// inspect one-shot (health report + --json) with a local baseline store that
// makes it temporal from the third run onward. The scriptable diagnostic and
// agent surfaces remain read-only; the interactive workbench has a separately
// guarded, human-confirmed path for intentional schema and data changes.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sarankumar1325/queryshift/internal/conn"
	"github.com/sarankumar1325/queryshift/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// version is set by the linker at release time (-X main.version=...).
var version = "dev"

func main() {
	// A SIGINT/SIGTERM cancels the run's context instead of killing the process
	// mid-flight: collectors abort at the next round trip, the connection pool closes,
	// and the store finishes its write. cmd.Context() in every handler is this ctx.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// One SSH connection serves every Target this process opens; drop it on the
	// way out rather than per-Target (--all-databases and `mcp` open many).
	defer conn.CloseSSHTunnel()

	root := &cobra.Command{
		Use:           "queryshift",
		Short:         "PostgreSQL intelligence and modernization workbench",
		Long:          "QueryShift opens an interactive database workspace with live schema, ER, SQL, and\nconfirmed changes. Its scriptable diagnostics and agent tools remain read-only by design.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Keep scripts predictable: the full-screen app is only the default
			// when both ends are a terminal. Piped invocations still receive help.
			if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
				return cmd.Help()
			}
			return tui.Run(cmd.Context())
		},
	}
	root.AddCommand(newTUICmd())
	root.AddCommand(newInspectCmd())
	root.AddCommand(newBaselinesCmd())
	root.AddCommand(newExplainCmd())
	root.AddCommand(newAskCmd())
	root.AddCommand(newIndexesCmd())
	root.AddCommand(newQueriesCmd())
	root.AddCommand(newVacuumCmd())
	root.AddCommand(newTablesCmd())
	root.AddCommand(newTuneCmd())
	root.AddCommand(newMCPCmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newAdviseCmd())
	root.AddCommand(newExplainFindingCmd())
	root.AddCommand(newDiffCmd())
	root.AddCommand(newLintCmd())
	root.AddCommand(newInitCmd())
	root.AddCommand(newWhyCmd())
	root.AddCommand(newLogsCmd())
	root.AddCommand(newWaitsCmd())
	root.AddCommand(newERDCmd())
	root.AddCommand(newActivityCmd())
	root.AddCommand(newReportCmd())
	root.AddCommand(newChangeCmd())
	root.AddCommand(newEnvironmentsCmd())
	root.AddCommand(newRunnerCmd())

	// --ssh-tunnel is global: every command that takes a connection can need it,
	// and it changes only HOW the DSN is reached, never what is inspected.
	var sshTunnel string
	root.PersistentFlags().StringVar(&sshTunnel, "ssh-tunnel", "",
		"reach the database through this SSH jump host ([user@]host[:port]; a bare alias is resolved via ~/.ssh/config)")

	// enteredRun distinguishes a malformed invocation (bad flags/args/unknown
	// command — cobra fails before PersistentPreRun) from an execution failure
	// (a handler ran and returned an error). B5's --fail-on makes exit codes a
	// public interface, so the two must not share code 3.
	enteredRun := false
	root.PersistentPreRun = func(*cobra.Command, []string) {
		enteredRun = true
		if sshTunnel == "" {
			sshTunnel = os.Getenv(conn.SSHTunnelEnv)
		}
		conn.SetSSHTunnel(sshTunnel)
	}

	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "queryshift: "+err.Error())
		// 64 — the invocation was malformed: cobra failed before a handler ran
		// (bad flags/args/command), OR a handler rejected a flag VALUE (tagged
		// usageError). 3 otherwise — a real connection/execution failure. The
		// finding-based codes (1 warn, 2 critical) are set by handlers directly.
		var ue usageError
		if !enteredRun || errors.As(err, &ue) {
			os.Exit(exitUsage)
		}
		os.Exit(exitFailure)
	}
}

// usageError tags an error a handler raises for a bad flag VALUE (e.g.
// --fail-on=bogus), so main maps it to exit 64 like cobra's own parse errors.
type usageError struct{ error }

// usageErrf builds a usageError.
func usageErrf(format string, a ...any) error { return usageError{fmt.Errorf(format, a...)} }
