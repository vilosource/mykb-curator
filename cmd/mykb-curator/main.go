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
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	kbgit "github.com/vilosource/mykb-curator/internal/adapters/kb/git"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	wikipkg "github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/memory"
	"github.com/vilosource/mykb-curator/internal/cache/runstate"
	"github.com/vilosource/mykb-curator/internal/config"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/markdown"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/resolvekbrefs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/validatelinks"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
	"github.com/vilosource/mykb-curator/internal/reporter"
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
	var wiki, configPath, outDir, reportDir string
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
			return runFromConfig(cmd.Context(), cfg, outDir, reportDir)
		},
	}
	cmd.Flags().StringVar(&wiki, "wiki", "", "wiki tenant name (matches the config filename)")
	cmd.Flags().StringVar(&configPath, "config", "", "config file path (defaults to ~/.config/mykb-curator/<wiki>.yaml)")
	cmd.Flags().StringVar(&outDir, "out", "", "if set, write rendered markdown for each spec to <out>/<spec-id>.md")
	cmd.Flags().StringVar(&reportDir, "report-dir", "", "if set, write per-run report YAML to <report-dir>/<run-id>.yaml and update latest.yaml symlink")
	return cmd
}

// runFromConfig is the composition root: it constructs concrete
// adapter implementations from the config and runs the orchestrator.
//
// v0.0.1 wires the local-fs spec store. KB source, wiki target, and
// LLM client are still stubbed — concrete impls land per roadmap.
// Any spec-store type other than "local" returns a clear error so
// the user knows what's implemented vs not.
func runFromConfig(ctx context.Context, cfg *config.Config, outDir, reportDir string) error {
	specStore, err := composeSpecStore(cfg)
	if err != nil {
		return err
	}
	kbSrc, err := composeKBSource(cfg)
	if err != nil {
		return err
	}
	wikiTarget, err := composeWikiTarget(cfg)
	if err != nil {
		return err
	}

	frontendRegistry := frontends.NewRegistry()
	frontendRegistry.Register(projection.New())

	// Pass pipeline is per-run because ResolveKBRefs closes over the
	// kb snapshot. ValidateLinks's known-pages map is built from the
	// loaded specs (every spec.Page is a known target).
	buildPasses := composePassPipeline(specStore)

	onRendered := composeOnRenderedSink(ctx, cfg, wikiTarget, outDir)

	cache, cacheCloser, err := composeRunStateCache(cfg)
	if err != nil {
		return err
	}
	if cacheCloser != nil {
		defer cacheCloser()
	}

	orch := orchestrator.New(orchestrator.Deps{
		Wiki:        cfg.Wiki,
		KB:          kbSrc,
		Specs:       specStore,
		WikiTarget:  wikiTarget,
		LLM:         stubLLM{},
		Frontends:   frontendRegistry,
		BuildPasses: buildPasses,
		Backend:     markdown.New(),
		OnRendered:  onRendered,
		RunState:    cache,
	})

	report, err := orch.Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		fmt.Fprintln(os.Stderr, report.Summary())
		persistReport(report, reportDir)
		return err
	}
	fmt.Println(report.Summary())
	for _, s := range report.Specs {
		fmt.Printf("  spec=%s status=%s blocks=%d %s\n", s.ID, s.Status, s.BlocksRegenerated, s.Reason)
	}
	persistReport(report, reportDir)
	return nil
}

// persistReport writes the run report to disk if reportDir is set.
// Failures here are warnings — the run itself already happened and
// the report is on the screen.
func persistReport(report reporter.Report, reportDir string) {
	if reportDir == "" {
		return
	}
	path, err := report.WriteToDir(reportDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: could not persist run report:", err)
		return
	}
	fmt.Println("run report:", path)
}

// composeOnRenderedSink builds the OnRendered callback based on
// config + flags. Priority order:
//  1. If outDir is set, write to disk (offline preview mode).
//  2. Else if wiki target is a real wiki (mediawiki / memory),
//     upsert via the wiki target.
//  3. Else nil (render-and-discard).
func composeOnRenderedSink(ctx context.Context, cfg *config.Config, target wikipkg.Target, outDir string) func(string, []byte, ir.Document) error {
	if outDir != "" {
		return makeDiskSink(outDir)
	}
	if cfg.WikiTarget.Type == "mediawiki" || cfg.WikiTarget.Type == "memory" {
		return makeWikiUpsertSink(ctx, target)
	}
	return nil
}

func makeDiskSink(outDir string) func(string, []byte, ir.Document) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "warning: cannot create out dir:", err)
		return nil
	}
	return func(specID string, rendered []byte, _ ir.Document) error {
		safe := strings.ReplaceAll(specID, "/", "_")
		safe = strings.TrimSuffix(safe, ".spec.md")
		path := filepath.Join(outDir, safe+".md")
		return os.WriteFile(path, rendered, 0o600)
	}
}

// makeWikiUpsertSink pushes each rendered spec to the wiki under
// the page title declared in the spec's frontmatter. The spec ID
// (filename) is unrelated to the page title, so we read the title
// from the IR's Frontmatter.
func makeWikiUpsertSink(ctx context.Context, target wikipkg.Target) func(string, []byte, ir.Document) error {
	return func(specID string, rendered []byte, doc ir.Document) error {
		title := doc.Frontmatter.Title
		if title == "" {
			return fmt.Errorf("wiki upsert: spec %s produced IR with empty frontmatter.Title", specID)
		}
		summary := fmt.Sprintf("mykb-curator: rendered from %s", specID)
		_, err := target.UpsertPage(ctx, title, string(rendered), summary)
		return err
	}
}

// composePassPipeline builds the per-run pass pipeline. Returns a
// function (not a Pipeline directly) because some passes need the
// kb snapshot — captured by the orchestrator at run-time.
//
// Default pipeline: ResolveKBRefs → ApplyZoneMarkers → ValidateLinks.
// Order matters: ResolveKBRefs replaces KBRefBlocks with ProseBlocks
// so ApplyZoneMarkers sees a clean block list, and ValidateLinks
// runs last so it catches links in resolved content too.
func composePassPipeline(specStore specs.Store) func(kbpkg.Snapshot) *passes.Pipeline {
	return func(snap kbpkg.Snapshot) *passes.Pipeline {
		// Build known-pages map from the loaded specs. Best-effort:
		// any pull failure leaves the map empty, which ValidateLinks
		// will treat as "all links broken" — safe-fail behaviour.
		known := buildKnownPages(specStore)
		return passes.NewPipeline(
			resolvekbrefs.New(snap),
			zonemarkers.New(),
			validatelinks.New(known),
		)
	}
}

// buildKnownPages collects spec.Page values from the spec store into
// a set. Best-effort — errors here don't fail composition; they
// degrade ValidateLinks to conservative-mode (fails on every link).
func buildKnownPages(store specs.Store) map[string]bool {
	out := map[string]bool{}
	list, err := store.Pull(context.Background())
	if err != nil {
		return out
	}
	for _, s := range list {
		if s.Page != "" {
			out[s.Page] = true
		}
	}
	return out
}

// composeRunStateCache opens the per-wiki bbolt cache. Returns a
// nil cache (and nil closer) when no cache dir is configured —
// orchestrator handles nil RunState gracefully (first-render mode).
func composeRunStateCache(cfg *config.Config) (*runstate.Cache, func(), error) {
	cacheDir := cfg.CacheDir
	if cacheDir == "" {
		cacheDir = filepath.Join(os.Getenv("HOME"), ".cache", "mykb-curator", cfg.Wiki)
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("cache: mkdir %q: %w", cacheDir, err)
	}
	path := filepath.Join(cacheDir, "runstate.bolt")
	c, err := runstate.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return c, func() { _ = c.Close() }, nil
}

func composeWikiTarget(cfg *config.Config) (wikipkg.Target, error) {
	switch cfg.WikiTarget.Type {
	case "mediawiki":
		if cfg.WikiTarget.URL == "" {
			return nil, fmt.Errorf("wiki_target.url: required for type=mediawiki")
		}
		if cfg.WikiTarget.Auth.User == "" {
			return nil, fmt.Errorf("wiki_target.auth.user: required for type=mediawiki")
		}
		pass := os.Getenv(cfg.WikiTarget.Auth.PasswordEnv)
		if pass == "" {
			return nil, fmt.Errorf("wiki_target.auth.password_env=%q: env var unset or empty", cfg.WikiTarget.Auth.PasswordEnv)
		}
		return mediawiki.New(mediawiki.Config{
			APIURL:  cfg.WikiTarget.URL,
			BotUser: cfg.WikiTarget.Auth.User,
			BotPass: pass,
		})
	case "memory":
		bot := cfg.WikiTarget.Auth.User
		if bot == "" {
			bot = "User:Mykb-Curator"
		}
		return memory.New(bot), nil
	case "markdown":
		// markdown-target is a no-op wiki: rendering still happens
		// (so the run report is real), but pushes go via the disk
		// sink instead. Returns a memory target so HumanEditsSinceBot
		// has a working impl during reconciliation tests.
		return memory.New("User:Markdown-Dryrun"), nil
	default:
		return nil, fmt.Errorf("wiki_target.type=%q: unknown (known: mediawiki, memory, markdown)", cfg.WikiTarget.Type)
	}
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

func composeKBSource(cfg *config.Config) (kbpkg.Source, error) {
	switch cfg.KBSource.Type {
	case "local":
		if cfg.KBSource.Repo == "" {
			return nil, fmt.Errorf("kb_source.repo: required for type=local (path to the kb directory)")
		}
		return kblocal.New(cfg.KBSource.Repo), nil
	case "git":
		if cfg.KBSource.Repo == "" {
			return nil, fmt.Errorf("kb_source.repo: required for type=git (remote URL or local repo path)")
		}
		workDir := cfg.CacheDir
		if workDir == "" {
			workDir = filepath.Join(os.Getenv("HOME"), ".cache", "mykb-curator", cfg.Wiki, "kb-clone")
		} else {
			workDir = filepath.Join(workDir, "kb-clone")
		}
		src := kbgit.New(cfg.KBSource.Repo, workDir)
		if cfg.KBSource.Branch != "" {
			src = src.WithBranch(cfg.KBSource.Branch)
		}
		return src, nil
	case "daemon":
		return nil, fmt.Errorf("kb_source.type=daemon: not yet implemented (mykb v2 roadmap)")
	default:
		return nil, fmt.Errorf("kb_source.type=%q: unknown (known: local, git)", cfg.KBSource.Type)
	}
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
