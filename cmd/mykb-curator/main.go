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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	kbpkg "github.com/vilosource/mykb-curator/internal/adapters/kb"
	kbgit "github.com/vilosource/mykb-curator/internal/adapters/kb/git"
	kblocal "github.com/vilosource/mykb-curator/internal/adapters/kb/local"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/adapters/specs/localfs"
	wikipkg "github.com/vilosource/mykb-curator/internal/adapters/wiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/mediawiki"
	"github.com/vilosource/mykb-curator/internal/adapters/wiki/memory"
	"github.com/vilosource/mykb-curator/internal/cache/ircache"
	"github.com/vilosource/mykb-curator/internal/cache/runstate"
	"github.com/vilosource/mykb-curator/internal/config"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/orchestrator"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance/checks/externaltruth"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance/checks/linkrot"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance/checks/staleness"
	"github.com/vilosource/mykb-curator/internal/pipelines/maintenance/prbackend"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/backends/markdown"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/editorial"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/projection"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/ir"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/applystylerules"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/renderdiagrams"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/resolvekbrefs"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/validatelinks"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/passes/zonemarkers"
	"github.com/vilosource/mykb-curator/internal/reporter"
	"github.com/vilosource/mykb-curator/internal/reporter/sinks"
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

	llmClient, llmErr := composeLLMClient(cfg)
	if llmErr != nil {
		return llmErr
	}

	frontendRegistry := frontends.NewRegistry()
	frontendRegistry.Register(projection.New())
	if llmClient != nil {
		frontendRegistry.Register(editorial.New(llmClient, cfg.LLM.Model))
	}

	// Pass pipeline is per-run because ResolveKBRefs closes over the
	// kb snapshot. ValidateLinks's known-pages map is built from the
	// loaded specs (every spec.Page is a known target).
	buildPasses := composePassPipeline(specStore, wikiTarget, renderdiagrams.NewMermaidRenderer(""), cfg.Style)

	onRendered := composeOnRenderedSink(ctx, cfg, wikiTarget, outDir)

	cache, cacheCloser, err := composeRunStateCache(cfg)
	if err != nil {
		return err
	}
	if cacheCloser != nil {
		defer cacheCloser()
	}

	irCache, err := composeIRCache(cfg)
	if err != nil {
		return err
	}

	orchLLM := llm.Client(stubLLM{})
	if llmClient != nil {
		orchLLM = llmClient
	}

	maintPipeline, onMaint := composeMaintenance(cfg, specStore, llmClient)

	orch := orchestrator.New(orchestrator.Deps{
		Wiki:          cfg.Wiki,
		KB:            kbSrc,
		Specs:         specStore,
		WikiTarget:    wikiTarget,
		LLM:           orchLLM,
		Frontends:     frontendRegistry,
		BuildPasses:   buildPasses,
		Backend:       markdown.New(),
		OnRendered:    onRendered,
		RunState:      cache,
		IRCache:       irCache,
		Maintenance:   maintPipeline,
		OnMaintenance: onMaint,
	})

	sink := composeReportSinks(cfg)

	report, err := orch.Run(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "run failed:", err)
		fmt.Fprintln(os.Stderr, report.Summary())
		persistReport(report, reportDir)
		publishReport(ctx, sink, report)
		return err
	}
	fmt.Println(report.Summary())
	for _, s := range report.Specs {
		fmt.Printf("  spec=%s status=%s blocks=%d %s\n", s.ID, s.Status, s.BlocksRegenerated, s.Reason)
	}
	persistReport(report, reportDir)
	publishReport(ctx, sink, report)
	return nil
}

// publishReport fans the report out to the configured sinks.
// Observational only: sink failures are logged, never fatal (the
// pages already shipped).
func publishReport(ctx context.Context, sink reporter.Sink, report reporter.Report) {
	if err := sink.Publish(ctx, report); err != nil {
		fmt.Fprintln(os.Stderr, "warning: report sink:", err)
	}
}

// composeReportSinks builds the MultiSink from config. Each sink is
// opt-in; secrets are resolved from env, never config plaintext. An
// unset/incomplete sink is silently skipped (with a stderr note for
// the partially-configured case).
func composeReportSinks(cfg *config.Config) reporter.Sink {
	var enabled []reporter.Sink
	if envName := cfg.Sinks.SlackWebhookEnv; envName != "" {
		if url := os.Getenv(envName); url != "" {
			enabled = append(enabled, sinks.NewSlack(url, http.DefaultClient))
		} else {
			fmt.Fprintf(os.Stderr, "warning: report_sinks.slack_webhook_env=%q is empty; slack sink disabled\n", envName)
		}
	}
	if e := cfg.Sinks.Email; e.SMTPAddr != "" && e.From != "" && len(e.To) > 0 {
		enabled = append(enabled, sinks.NewEmail(smtpSender{cfg: e}, e.From, e.To))
	}
	if cfg.Sinks.KBJournal {
		enabled = append(enabled, sinks.NewKBJournal(execRunner{}))
	}
	return reporter.NewMultiSink(enabled...)
}

// smtpSender is the production sinks.Sender — net/smtp. PlainAuth is
// used only when a username + password env are configured (supports
// open relays / local MTAs without auth too).
type smtpSender struct{ cfg config.EmailSinkConfig }

func (s smtpSender) Send(from string, to []string, subject string, body []byte) error {
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		from, strings.Join(to, ", "), subject, body))
	var auth smtp.Auth
	if s.cfg.Username != "" && s.cfg.PasswordEnv != "" {
		host := s.cfg.SMTPAddr
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		auth = smtp.PlainAuth("", s.cfg.Username, os.Getenv(s.cfg.PasswordEnv), host)
	}
	return smtp.SendMail(s.cfg.SMTPAddr, auth, from, to, msg)
}

// execRunner is the production sinks.Runner — runs the real binary.
type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
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
// Default pipeline: ResolveKBRefs → ApplyStyleRules → RenderDiagrams
// → ApplyZoneMarkers → ValidateLinks. Order matters: ResolveKBRefs
// replaces KBRefBlocks with ProseBlocks so later passes see a clean
// block list; ApplyStyleRules normalises prose before it is wrapped;
// RenderDiagrams runs before ApplyZoneMarkers (DESIGN.md §5.4) so the
// markers wrap the final asset-ref'd diagram block; ValidateLinks
// runs last so it catches links in resolved content too.
func composePassPipeline(specStore specs.Store, uploader renderdiagrams.Uploader, renderer renderdiagrams.Renderer, style config.StyleConfig) func(kbpkg.Snapshot) *passes.Pipeline {
	styleRules := buildStyleRules(style)
	return func(snap kbpkg.Snapshot) *passes.Pipeline {
		// Build known-pages map from the loaded specs. Best-effort:
		// any pull failure leaves the map empty, which ValidateLinks
		// will treat as "all links broken" — safe-fail behaviour.
		known := buildKnownPages(specStore)
		return passes.NewPipeline(
			resolvekbrefs.New(snap),
			applystylerules.New(styleRules...),
			renderdiagrams.New(renderer, uploader),
			zonemarkers.New(),
			validatelinks.New(known),
		)
	}
}

// buildStyleRules maps the per-wiki style config onto the
// config-agnostic ApplyStyleRules Rule set. heading_case is already
// validated by config.Validate, so the constructor error here is
// unreachable in practice; it is dropped deliberately rather than
// panicking the run.
func buildStyleRules(style config.StyleConfig) []applystylerules.Rule {
	var rules []applystylerules.Rule
	if len(style.Terminology) > 0 {
		rules = append(rules, applystylerules.NewTerminologyRule(style.Terminology))
	}
	if style.HeadingCase != "" {
		if hc, err := applystylerules.NewHeadingCaseRule(style.HeadingCase); err == nil {
			rules = append(rules, hc)
		}
	}
	return rules
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

// optedInExternalTruthAreas implements the DESIGN §6.4 funding gate:
// it returns the set of kb areas that at least one spec opted into
// external-truth checking via `fact_check: external_truth`. Areas
// nobody opted into are never externally researched. Best-effort: a
// spec-store pull failure yields an empty set (check stays off).
func optedInExternalTruthAreas(store specs.Store) []string {
	list, err := store.Pull(context.Background())
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var areas []string
	for _, s := range list {
		if s.FactCheck["external_truth"] == "" {
			continue
		}
		for _, a := range s.Include.Areas {
			if !seen[a] {
				seen[a] = true
				areas = append(areas, a)
			}
		}
	}
	return areas
}

// noopWebSearch is the default WebSearch: it finds nothing, so the
// external-truth check never calls the LLM and never proposes
// anything. A real provider adapter (Brave/SerpAPI/etc.) is v2
// backlog; this keeps the funding gate + check wired and safe
// meanwhile.
type noopWebSearch struct{}

func (noopWebSearch) Search(context.Context, string) ([]externaltruth.Result, error) {
	return nil, nil
}

// composeMaintenance builds the kb-maintenance pipeline + the
// proposal handler.
//
// v0.5 defaults: staleness (90 days) + link-rot (5s HEAD timeout).
// v1.0: an external-truth check is added iff (a) at least one spec
// opted an area in via `fact_check: external_truth` (DESIGN §6.4
// funding gate) and (b) an LLM client is configured. The web-search
// provider adapter is not yet implemented; a no-op is wired so the
// gate + check are live and safe (no results ⇒ no spend, no
// proposals) until a real provider lands (v2 backlog).
//
// Returns (nil, nil) when kb_writeback.type is unset/none — the
// orchestrator skips the maintenance phase entirely.
func composeMaintenance(cfg *config.Config, specStore specs.Store, llmClient llm.Client) (*maintenance.Pipeline, func([]maintenance.MutationProposal) error) {
	if cfg.KBWriteback.Type == "" || cfg.KBWriteback.Type == "none" {
		return nil, nil
	}
	checks := []maintenance.Check{
		staleness.New(90 * 24 * time.Hour),
		linkrot.New(5 * time.Second),
	}
	if optedIn := optedInExternalTruthAreas(specStore); len(optedIn) > 0 && llmClient != nil {
		checks = append(checks, externaltruth.New(optedIn, noopWebSearch{}, llmClient, cfg.LLM.Model))
	}
	pipeline := maintenance.NewPipeline(checks...)
	// PR backend writes to the kb working tree (the git source's
	// clone dir). Only meaningful for kb_source.type=git; for local
	// sources we skip the PR backend and just log the proposal count.
	if cfg.KBSource.Type != "git" {
		return pipeline, func(props []maintenance.MutationProposal) error {
			fmt.Fprintf(os.Stderr, "maintenance: %d proposal(s); PR backend skipped (kb_source.type=%q)\n", len(props), cfg.KBSource.Type)
			return nil
		}
	}
	workDir := kbWorkDir(cfg)
	pr := prbackend.New(workDir)
	baseBranch := cfg.KBWriteback.BaseBranch
	if baseBranch == "" {
		baseBranch = "main"
	}
	return pipeline, func(props []maintenance.MutationProposal) error {
		res, err := pr.Submit(context.Background(), prbackend.Input{
			RunID:      newRunID(),
			BaseBranch: baseBranch,
			Proposals:  props,
		})
		if err != nil {
			return err
		}
		fmt.Println("maintenance: opened branch", res.Branch, "with", res.Mutations, "proposal(s)")
		fmt.Println("maintenance: push manually and open PR via gh:")
		fmt.Printf("    cd %s && git push -u origin %s\n", workDir, res.Branch)
		fmt.Printf("    gh pr create --base %s --head %s --title 'curator: maintenance proposals' --body-file curator-proposals/...\n", baseBranch, res.Branch)
		return nil
	}
}

// kbWorkDir computes the on-disk path of the kb clone (mirrors the
// composeKBSource logic but only returns the path, not the adapter).
func kbWorkDir(cfg *config.Config) string {
	if cfg.CacheDir != "" {
		return filepath.Join(cfg.CacheDir, "kb-clone")
	}
	return filepath.Join(os.Getenv("HOME"), ".cache", "mykb-curator", cfg.Wiki, "kb-clone")
}

// newRunID returns a short hex id, matching the orchestrator's own
// run-id shape. Duplicated rather than imported to avoid a cycle.
func newRunID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// composeLLMClient builds the LLM client per cfg.LLM.Provider:
//
//	anthropic → AnthropicClient (env-supplied key), wrapped in cache
//	replay    → ReplayClient against a fixtures dir
//	none/""   → nil (LLM-using frontends will not be registered)
//
// When a real client is built it's wrapped in CacheDecorator so
// repeated runs reuse responses — same files committed test fixtures
// use, so production cache can be promoted into the fixture set.
func composeLLMClient(cfg *config.Config) (llm.Client, error) {
	switch cfg.LLM.Provider {
	case "", "none":
		return nil, nil
	case "anthropic":
		key := os.Getenv(cfg.LLM.APIKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("llm.api_key_env=%q: env var unset", cfg.LLM.APIKeyEnv)
		}
		inner := llm.NewAnthropicClient(llm.AnthropicConfig{
			Endpoint: cfg.LLM.Endpoint,
			APIKey:   key,
		})
		cacheDir := filepath.Join(llmCacheDir(cfg), "anthropic")
		return llm.NewCacheDecorator(inner, cacheDir), nil
	case "replay":
		// Replay points at a directory of pre-recorded responses —
		// used for tests and deterministic CI runs.
		dir := cfg.LLM.Endpoint
		if dir == "" {
			dir = filepath.Join(llmCacheDir(cfg), "anthropic")
		}
		return llm.NewReplayClient(dir), nil
	default:
		return nil, fmt.Errorf("llm.provider=%q: unknown (known: anthropic, replay, none)", cfg.LLM.Provider)
	}
}

func llmCacheDir(cfg *config.Config) string {
	base := cfg.CacheDir
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".cache", "mykb-curator", cfg.Wiki)
	}
	return filepath.Join(base, "llm")
}

// composeIRCache opens the per-wiki IR memoisation cache. Cache dir
// defaults to <CacheDir>/ir or ~/.cache/mykb-curator/<wiki>/ir.
// Always opens — disabling memoisation is a CLI flag, not a config
// concern.
func composeIRCache(cfg *config.Config) (*ircache.Cache, error) {
	base := cfg.CacheDir
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".cache", "mykb-curator", cfg.Wiki)
	}
	return ircache.Open(filepath.Join(base, "ir"))
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
