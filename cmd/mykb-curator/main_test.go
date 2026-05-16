package main

import (
	"testing"

	"github.com/vilosource/mykb-curator/internal/config"
)

func TestComposeBackend_SelectsByWikiTarget(t *testing.T) {
	cases := map[string]string{
		"mediawiki": "mediawiki",
		"memory":    "markdown",
		"markdown":  "markdown",
		"":          "markdown",
	}
	for target, wantBackend := range cases {
		cfg := &config.Config{WikiTarget: config.WikiTargetConfig{Type: target}}
		if got := composeBackend(cfg).Name(); got != wantBackend {
			t.Errorf("wiki_target.type=%q → backend %q, want %q", target, got, wantBackend)
		}
	}
}
