// Command mykb-curator is the CLI entrypoint for the curator.
//
// Subcommands:
//
//	run         Execute one curator pass for a wiki (or all wikis)
//	spec init   Begin an LLM-assisted spec-authoring conversation
//	reconcile   Re-reconcile one page (handle a flagged human edit)
//	report      Show the latest run report
//	version     Print version info
//
// v0.0 walking skeleton: only `run` is wired; other subcommands stub.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/vilosource/mykb-curator/internal/config"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
)

var (
	// Version is overridden via -ldflags at build time.
	Version = "dev"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "mykb-curator",
		Short: "Maintain human-facing wikis as curated projections of mykb",
	}
	root.AddCommand(newRunCmd(), newSpecCmd(), newReconcileCmd(), newReportCmd(), newVersionCmd())
	return root
}

func newRunCmd() *cobra.Command {
	var wiki, configPath string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Execute one curator pass",
		RunE: func(cmd *cobra.Command, args []string) error {
			if wiki == "" {
				return fmt.Errorf("--wiki is required (v0.0; --all coming later)")
			}
			if configPath == "" {
				configPath = fmt.Sprintf("%s/.config/mykb-curator/%s.yaml", os.Getenv("HOME"), wiki)
			}

			// v0.0: walking-skeleton path — load config, but no real
			// adapters are wired yet. Concrete adapters land in
			// subsequent PRs per the roadmap (v0.1).
			_, err := config.Load(configPath)
			if err != nil {
				return err
			}
			return runWalkingSkeleton(cmd.Context(), wiki)
		},
	}
	cmd.Flags().StringVar(&wiki, "wiki", "", "wiki tenant name (matches the config filename)")
	cmd.Flags().StringVar(&configPath, "config", "", "config file path (defaults to ~/.config/mykb-curator/<wiki>.yaml)")
	return cmd
}

// runWalkingSkeleton is the v0.0 demo path that proves the wiring
// compiles end-to-end. It is intentionally short on substance —
// concrete adapters are not yet implemented. Replaced incrementally
// as v0.1 lands.
func runWalkingSkeleton(_ context.Context, wiki string) error {
	fmt.Printf("mykb-curator v0.0 — walking-skeleton run for wiki=%s\n", wiki)
	fmt.Println("(no adapters wired yet; see docs/DESIGN.md §17 roadmap for v0.1 deliverables)")

	// We deliberately do NOT construct an Orchestrator here yet, because
	// the concrete adapter implementations have not landed. Tests
	// exercise the Orchestrator with fakes — see
	// internal/orchestrator/orchestrator_test.go and
	// test/integration/orchestrator_skeleton_test.go.
	_ = orchestrator.Orchestrator{}

	return nil
}

func newSpecCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spec",
		Short: "Spec authoring + lifecycle commands",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Begin an LLM-assisted spec-authoring conversation (TBD)",
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("spec init: not yet implemented (v0.5 roadmap)")
		},
	})
	return cmd
}

func newReconcileCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reconcile",
		Short: "Re-reconcile a single page (TBD)",
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("reconcile: not yet implemented (v0.5 roadmap)")
		},
	}
}

func newReportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Show the latest run report (TBD)",
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("report: not yet implemented (v0.1 roadmap)")
		},
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(*cobra.Command, []string) {
			fmt.Println(Version)
		},
	}
}
