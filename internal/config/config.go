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
	return nil
}
