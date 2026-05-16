package renderdiagrams

import (
	"context"
	"fmt"
	"strings"

	"github.com/vilosource/mykb-curator/internal/llm"
)

// LLMRepairer is the production Repairer: when a diagram fails to
// render it hands the broken source + the renderer's error back to
// the model and returns the model's corrected diagram. One extra LLM
// call per failed diagram — cheap relative to losing the diagram, and
// the right use of AI here (LLM-authored mermaid has an open-ended
// set of syntax mistakes that conservative regex repair cannot
// fully cover).
type LLMRepairer struct {
	c     llm.Client
	model string
}

// NewLLMRepairer constructs an LLMRepairer over the given client.
func NewLLMRepairer(c llm.Client, model string) *LLMRepairer {
	return &LLMRepairer{c: c, model: model}
}

const repairSystemPrompt = `You fix invalid diagram source for a renderer. ` +
	`Output ONLY the corrected diagram source — no explanation, no prose, ` +
	`no markdown code fences. Preserve the author's intent and content; ` +
	`change only what is needed to make it parse. For mermaid: never put ` +
	"backticks, unescaped parentheses or slashes inside node labels; quote " +
	`multi-word subgraph titles.`

// Repair asks the model to correct source that failed with renderErr.
func (r *LLMRepairer) Repair(ctx context.Context, lang, source, renderErr string) (string, error) {
	prompt := fmt.Sprintf(
		"The following %s diagram failed to render.\n\nRenderer error:\n%s\n\nDiagram source:\n%s\n\nReturn the corrected %s diagram source only.",
		lang, renderErr, source, lang)
	resp, err := r.c.Complete(ctx, llm.Request{
		Model:     r.model,
		System:    repairSystemPrompt,
		Prompt:    prompt,
		MaxTokens: 2048,
	})
	if err != nil {
		return "", fmt.Errorf("renderdiagrams: llm repair: %w", err)
	}
	return stripFence(resp.Text), nil
}

// stripFence returns the contents of the first fenced code block in
// s, or the trimmed whole string if there is no fence — models often
// wrap the fix in ```lang … ``` despite being told not to.
func stripFence(s string) string {
	if i := strings.Index(s, "```"); i >= 0 {
		rest := s[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:] // drop the ```lang line
		}
		if j := strings.Index(rest, "```"); j >= 0 {
			return strings.TrimSpace(rest[:j])
		}
	}
	return strings.TrimSpace(s)
}
