package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "acme.yaml")
	body := `
wiki: acme
kb_source:
  type: git
  repo: git@example.com:org/kb.git
  branch: main
  read_only: true
spec_store:
  type: git
  repo: git@example.com:org/specs.git
  branch: main
wiki_target:
  type: mediawiki
  url: https://wiki.example.com/api.php
  auth:
    type: bot-password
    user: User:Mykb-Curator
    password_env: ACME_WIKI_BOT_PASSWORD
kb_writeback:
  type: github-pr
  repo: org/kb
  base_branch: main
  branch_prefix: curator/
llm:
  provider: replay
  model: claude-opus-4-7
  api_key_env: ANTHROPIC_API_KEY
cache_dir: /tmp/cache
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Wiki != "acme" {
		t.Errorf("Wiki = %q, want %q", cfg.Wiki, "acme")
	}
	if cfg.KBSource.Type != "git" {
		t.Errorf("KBSource.Type = %q, want %q", cfg.KBSource.Type, "git")
	}
	if cfg.WikiTarget.Auth.PasswordEnv != "ACME_WIKI_BOT_PASSWORD" {
		t.Errorf("PasswordEnv = %q, want %q", cfg.WikiTarget.Auth.PasswordEnv, "ACME_WIKI_BOT_PASSWORD")
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"no kb_source", "wiki: acme\nspec_store:\n  type: git\nwiki_target:\n  type: mediawiki\n"},
		{"no spec_store", "wiki: acme\nkb_source:\n  type: git\nwiki_target:\n  type: mediawiki\n"},
		{"no wiki_target", "wiki: acme\nkb_source:\n  type: git\nspec_store:\n  type: git\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if _, err := Load(path); err == nil {
				t.Errorf("expected error for %s, got nil", tc.name)
			}
		})
	}
}

func TestLoad_DisableBotAssert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := "wiki: acme\n" +
		"kb_source:\n  type: local\n" +
		"spec_store:\n  type: local\n" +
		"wiki_target:\n  type: mediawiki\n  url: http://localhost:8181/api.php\n" +
		"  disable_bot_assert: true\n" +
		"  auth:\n    user: Admin\n    password_env: MW_PASS\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.WikiTarget.DisableBotAssert {
		t.Errorf("disable_bot_assert not parsed (got false)")
	}
}

func TestValidate_HeadingCase(t *testing.T) {
	base := func(hc string) *Config {
		return &Config{
			Wiki:       "acme",
			KBSource:   KBSourceConfig{Type: "local"},
			SpecStore:  SpecStoreConfig{Type: "local"},
			WikiTarget: WikiTargetConfig{Type: "memory"},
			Style:      StyleConfig{HeadingCase: hc},
		}
	}
	for _, ok := range []string{"", "sentence", "title"} {
		if err := base(ok).Validate(); err != nil {
			t.Errorf("heading_case %q should be valid, got %v", ok, err)
		}
	}
	if err := base("SHOUTING").Validate(); err == nil {
		t.Errorf("heading_case \"SHOUTING\" should be rejected")
	}
}

func loadRefineFixture(t *testing.T, judgeBlock string) *Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := "wiki: acme\n" +
		"kb_source:\n  type: local\n" +
		"spec_store:\n  type: local\n" +
		"wiki_target:\n  type: memory\n" +
		judgeBlock
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func TestRefineIterations_DefaultsToThreeWhenUnset(t *testing.T) {
	cfg := loadRefineFixture(t, "")
	if got := cfg.RefineIterations(); got != 3 {
		t.Errorf("RefineIterations() with no judge block = %d, want 3 (on by default)", got)
	}
}

func TestRefineIterations_ExplicitZeroIsOff(t *testing.T) {
	cfg := loadRefineFixture(t, "judge:\n  max_refine_iterations: 0\n")
	if got := cfg.RefineIterations(); got != 0 {
		t.Errorf("RefineIterations() with explicit 0 = %d, want 0 (off)", got)
	}
}

func TestRefineIterations_ExplicitValue(t *testing.T) {
	cfg := loadRefineFixture(t, "judge:\n  max_refine_iterations: 5\n")
	if got := cfg.RefineIterations(); got != 5 {
		t.Errorf("RefineIterations() with explicit 5 = %d, want 5", got)
	}
}

func TestValidate_RejectsNegativeRefineIterations(t *testing.T) {
	neg := -1
	cfg := &Config{
		Wiki:       "acme",
		KBSource:   KBSourceConfig{Type: "local"},
		SpecStore:  SpecStoreConfig{Type: "local"},
		WikiTarget: WikiTargetConfig{Type: "memory"},
		Judge:      JudgeConfig{MaxRefineIterations: &neg},
	}
	if err := cfg.Validate(); err == nil {
		t.Errorf("negative max_refine_iterations should be rejected")
	}
}
