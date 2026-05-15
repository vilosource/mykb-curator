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
