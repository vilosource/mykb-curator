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

	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	wikipkg "github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/config"
	"github.com/vilosource/mykb-curator/internal/llm"
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

			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			return runFromConfig(cmd.Context(), cfg)
		},
	}
	cmd.Flags().StringVar(&wiki, "wiki", "", "wiki tenant name (matches the config filename)")
	cmd.Flags().StringVar(&configPath, "config", "", "config file path (defaults to ~/.config/mykb-curator/<wiki>.yaml)")
	return cmd
}

// runFromConfig is the composition root: it constructs concrete
// adapter implementations from the config and runs the orchestrator.
//
// v0.0.1 wires the local-fs spec store. KB source, wiki target, and
// LLM client are still stubbed — concrete impls land per roadmap.
// Any spec-store type other than "local" returns a clear error so
// the user knows what's implemented vs not.
func runFromConfig(ctx context.Context, cfg *config.Config) error {
	specStore, err := composeSpecStore(cfg)
	if err != nil {
		return err
	}
	kb := stubKBSource{}
	wiki := stubWikiTarget{}
	llmClient := stubLLM{}

	orch := orchestrator.New(orchestrator.Deps{
		Wiki:       cfg.Wiki,
		KB:         kb,
		Specs:      specStore,
		WikiTarget: wiki,
		LLM:        llmClient,
	})

	report, err := orch.Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		fmt.Fprintln(os.Stderr, report.Summary())
		return err
	}
	fmt.Println(report.Summary())
	for _, s := range report.Specs {
		fmt.Printf("  spec=%s status=%s %s\n", s.ID, s.Status, s.Reason)
	}
	return nil
}

func composeSpecStore(cfg *config.Config) (specs.Store, error) {
	switch cfg.SpecStore.Type {
	case "local":
		if cfg.SpecStore.Repo == "" {
			return nil, fmt.Errorf("spec_store.repo: required for type=local (path to the spec directory)")
		}
		return localfs.New(cfg.SpecStore.Repo), nil
	case "git":
		return nil, fmt.Errorf("spec_store.type=git: not yet implemented (v0.1 roadmap)")
	default:
		return nil, fmt.Errorf("spec_store.type=%q: unknown", cfg.SpecStore.Type)
	}
}

// Stub adapters fill in for the kb / wiki / llm slots until their
// concrete impls land. They are deliberately minimal: kb returns a
// hardcoded snapshot so the orchestrator's spec-validation logic
// can exercise the spec store; wiki and llm are unused in the
// current run loop (rendering pipeline not yet implemented).

type stubKBSource struct{}

func (stubKBSource) Pull(context.Context) (kbpkg.Snapshot, error) {
	return kbpkg.Snapshot{Commit: "stub"}, nil
}
func (stubKBSource) Whoami() string { return "stub-kb" }

type stubWikiTarget struct{}

func (stubWikiTarget) Whoami(context.Context) (string, error) { return "stub-wiki", nil }
func (stubWikiTarget) GetPage(context.Context, string) (*wikipkg.Page, error) {
	return nil, nil
}
func (stubWikiTarget) UpsertPage(context.Context, string, string, string) (wikipkg.Revision, error) {
	return wikipkg.Revision{}, nil
}
func (stubWikiTarget) History(context.Context, string, string) ([]wikipkg.Revision, error) {
	return nil, nil
}
func (stubWikiTarget) HumanEditsSinceBot(context.Context, string, string) (*wikipkg.HumanEdit, error) {
	return nil, nil
}

type stubLLM struct{}

func (stubLLM) Complete(context.Context, llm.Request) (llm.Response, error) {
	return llm.Response{}, fmt.Errorf("stub-llm: no impl wired yet")
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
