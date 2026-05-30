// Package config loads and validates per-wiki curator configuration.
//
// One config file per wiki tenant, conventionally at
// ~/.config/mykb-curator/<wiki>.yaml.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the parsed per-wiki configuration.
type Config struct {
	Wiki        string            `yaml:"wiki"`
	KBSource    KBSourceConfig    `yaml:"kb_source"`
	SpecStore   SpecStoreConfig   `yaml:"spec_store"`
	WikiTarget  WikiTargetConfig  `yaml:"wiki_target"`
	KBWriteback KBWritebackConfig `yaml:"kb_writeback"`
	LLM         LLMConfig         `yaml:"llm"`
	CacheDir    string            `yaml:"cache_dir"`
	Style       StyleConfig       `yaml:"style"`
	Sinks       SinksConfig       `yaml:"report_sinks"`
	Sources     SourcesConfig     `yaml:"sources"`
	Judge       JudgeConfig       `yaml:"judge"`

	// OrphanPruning enables retiring pages whose spec was removed or
	// renamed (docs/navigation-DESIGN.md §11). MUST stay off for scoped
	// runs — a scope is a subset of specs, so pruning there would
	// wrongly retire every out-of-scope page. Enable only on the
	// canonical full-store run.
	OrphanPruning bool `yaml:"orphan_pruning"`
}

// JudgeConfig governs the output Judge and its closed refinement loop
// (DESIGN §5.7).
type JudgeConfig struct {
	// MaxRefineIterations bounds the closed Judge loop: on a failing
	// verdict, failing sections are re-synthesized with the verdict as
	// feedback, re-judged, up to this many iterations, then published
	// best-effort. A pointer so "unset" is distinguishable from an
	// explicit 0: nil ⇒ default 3 (on by default); explicit 0 ⇒ off
	// (report-only) for this wiki. Read via Config.RefineIterations.
	MaxRefineIterations *int `yaml:"max_refine_iterations"`
}

// DefaultRefineIterations is the loop budget when no judge block sets
// max_refine_iterations (on by default — DESIGN §5.7 D5).
const DefaultRefineIterations = 3

// RefineIterations returns the closed-Judge-loop budget: the configured
// value if set (including an explicit 0, which turns the loop off), else
// DefaultRefineIterations.
func (c *Config) RefineIterations() int {
	if c.Judge.MaxRefineIterations == nil {
		return DefaultRefineIterations
	}
	return *c.Judge.MaxRefineIterations
}

// SourcesConfig configures the non-kb doc-spec source resolvers
// (the reality-probe family). Only git ships today; it is read-only
// and offline so it needs no policy gate. cmd/ssh/az are deferred
// behind an execution-policy model and have no config here yet.
type SourcesConfig struct {
	Git GitSourcesConfig `yaml:"git"`
}

// GitSourcesConfig locates local clones for the read-only git:
// resolver. Root is a base dir containing clones (e.g. ~/GitLab);
// Repos is an explicit name→path override. Both optional; absent =
// git: sources stay the honest "pending" placeholder.
type GitSourcesConfig struct {
	Root  string            `yaml:"root"`
	Repos map[string]string `yaml:"repos"`
}

// SinksConfig configures the optional run-report sinks. Every field
// is opt-in; absent = that sink is not wired.
type SinksConfig struct {
	// SlackWebhookEnv names the env var holding the incoming-webhook
	// URL (secret never in config plaintext).
	SlackWebhookEnv string `yaml:"slack_webhook_env"`

	// Email, when SMTPAddr+From+To are all set, sends the summary.
	Email EmailSinkConfig `yaml:"email"`

	// KBJournal, when true, appends the summary to the active kb
	// workspace journal via the `kb` CLI.
	KBJournal bool `yaml:"kb_journal"`
}

// EmailSinkConfig is the SMTP envelope for the email sink.
type EmailSinkConfig struct {
	SMTPAddr    string   `yaml:"smtp_addr"` // host:port
	Username    string   `yaml:"username"`
	PasswordEnv string   `yaml:"password_env"`
	From        string   `yaml:"from"`
	To          []string `yaml:"to"`
}

// StyleConfig drives the deterministic ApplyStyleRules pass. All
// fields optional; absent = that rule is not applied.
type StyleConfig struct {
	// Terminology maps wrong/variant term -> canonical form. Applied
	// whole-word to prose, callouts, and headings.
	Terminology map[string]string `yaml:"terminology"`

	// HeadingCase normalises Section heading casing. "" (off),
	// "sentence", or "title".
	HeadingCase string `yaml:"heading_case"`
}

type KBSourceConfig struct {
	Type     string `yaml:"type"` // git | local | daemon
	Repo     string `yaml:"repo"` // git URL or local path
	Branch   string `yaml:"branch"`
	ReadOnly bool   `yaml:"read_only"`
}

type SpecStoreConfig struct {
	Type   string `yaml:"type"` // git | s3 | az-blob | local
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
}

type WikiTargetConfig struct {
	Type string         `yaml:"type"` // mediawiki | confluence | markdown | static
	URL  string         `yaml:"url"`
	Auth WikiAuthConfig `yaml:"auth"`

	// DisableBotAssert skips MediaWiki's assert=bot guard. Production
	// leaves this false (the assert catches "your bot lost its
	// group" silently). Dev/test wikis — including the experiment
	// harness, where Admin-as-bot's group membership is flaky on
	// fresh SQLite — set it true. Mirrors mediawiki.Config.
	DisableBotAssert bool `yaml:"disable_bot_assert"`
}

type WikiAuthConfig struct {
	Type        string `yaml:"type"` // bot-password | api-token
	User        string `yaml:"user"`
	PasswordEnv string `yaml:"password_env"` // env var name; secret resolved at run time
}

type KBWritebackConfig struct {
	Type         string `yaml:"type"` // github-pr | gitlab-mr | none
	Repo         string `yaml:"repo"`
	BaseBranch   string `yaml:"base_branch"`
	BranchPrefix string `yaml:"branch_prefix"`
}

type LLMConfig struct {
	Provider  string `yaml:"provider"` // anthropic | pi | replay
	Model     string `yaml:"model"`
	APIKeyEnv string `yaml:"api_key_env"`
	Endpoint  string `yaml:"endpoint"` // for pi: the pi-harness URL
}

// Load reads and validates a config file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: invalid %s: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks for required fields and obvious mis-configurations.
func (c *Config) Validate() error {
	if c.Wiki == "" {
		return fmt.Errorf("wiki: required")
	}
	if c.KBSource.Type == "" {
		return fmt.Errorf("kb_source.type: required")
	}
	if c.SpecStore.Type == "" {
		return fmt.Errorf("spec_store.type: required")
	}
	if c.WikiTarget.Type == "" {
		return fmt.Errorf("wiki_target.type: required")
	}
	switch c.Style.HeadingCase {
	case "", "sentence", "title":
	default:
		return fmt.Errorf("style.heading_case: %q invalid (want \"sentence\", \"title\", or empty)", c.Style.HeadingCase)
	}
	if c.Judge.MaxRefineIterations != nil && *c.Judge.MaxRefineIterations < 0 {
		return fmt.Errorf("judge.max_refine_iterations: %d invalid (must be >= 0; 0 = off)", *c.Judge.MaxRefineIterations)
	}
	return nil
}
