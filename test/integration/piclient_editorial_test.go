//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vilosource/mykb-curator/internal/adapters/kb"
	"github.com/vilosource/mykb-curator/internal/adapters/specs"
	"github.com/vilosource/mykb-curator/internal/llm"
	"github.com/vilosource/mykb-curator/internal/pipelines/rendering/frontends/editorial"
)

// TestPiClient_DrivesEditorialFrontend integrates the real PiClient
// (over a pi-harness-shaped httptest server) with the real editorial
// frontend: the agent's markdown comes back through PiClient and is
// parsed into IR sections. This is the cross-component proof that
// llm.provider=pi is a drop-in for the editorial intelligence locus.
func TestPiClient_DrivesEditorialFrontend(t *testing.T) {
	const agentMarkdown = `## Overview

Vault is the centralised secrets manager for the platform.

## Topology

Vault runs as a Raft HA cluster with Azure Key Vault auto-unseal.
`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/complete" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"text": agentMarkdown, "tokens_in": 150, "tokens_out": 80,
		})
	}))
	defer srv.Close()

	front := editorial.New(llm.NewPiClient(srv.URL), "pinned-model")
	doc, err := front.Build(context.Background(),
		specs.Spec{Page: "Vault", Hash: "h1", Kind: "editorial", Body: "Explain Vault."},
		kb.Snapshot{Commit: "deadbeef"},
	)
	if err != nil {
		t.Fatalf("editorial Build via PiClient: %v", err)
	}
	if doc.Frontmatter.Title != "Vault" {
		t.Errorf("title = %q", doc.Frontmatter.Title)
	}
	if len(doc.Sections) != 2 {
		t.Fatalf("want 2 sections from the agent markdown, got %d: %+v", len(doc.Sections), doc.Sections)
	}
	if doc.Sections[0].Heading != "Overview" || doc.Sections[1].Heading != "Topology" {
		t.Errorf("headings = %q / %q", doc.Sections[0].Heading, doc.Sections[1].Heading)
	}
}
