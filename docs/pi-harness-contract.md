# Pi batch contract (frozen 2026-05-16)

The load-bearing unknown for the live-agent harness: how the `pi`
CLI behaves as a single-shot, non-interactive LLM consumer, and what
its machine output looks like. Resolved by capturing one real
response (`test/fixtures/pi/pong.jsonl`) — the sanctioned spike
recording, the same discipline as `RecordingLLMClient`.

## Invocation

`pi` is non-interactive with `--print`/`-p` and emits machine output
with `--mode json`. The clean, deterministic, side-effect-free
single-shot invocation the `pi-wrapper` shim must use:

```
pi --print --mode json \
   --no-tools --no-session --no-extensions --no-skills \
   [--provider <p>] [--model <m>] [--api-key <k>] \
   [--system-prompt <s>] \
   <prompt>
```

- `--print` — process the prompt and exit (no TUI).
- `--mode json` — machine output (see below).
- `--no-tools` — the editorial frontend wants pure text completion,
  not an agent with read/bash/edit/write. **Required**: without it
  `pi` may take filesystem actions.
- `--no-session --no-extensions --no-skills` — ephemeral, no session
  files, no kb-extension auto-discovery, no skills. Keeps the harness
  Pi a *plain LLM*, not the operator's `kb-pi` agent.
- `--provider/--model/--api-key` — pinned by the curator config /
  pi-harness env; omitted ⇒ pi's configured default.
- The prompt is the trailing positional arg. The system prompt goes
  via `--system-prompt` (or `--append-system-prompt`).

## Output: a JSONL event stream (NOT one JSON document)

`--mode json` writes **one JSON object per line** — an event stream,
not a single object. Observed event `type`s, in order:

```
session · agent_start · turn_start ·
message_start/message_end (the echoed user message) ·
message_start (assistant, empty) ·
message_update … (text_start / text_delta / text_end — streaming) ·
message_end (assistant, final) ·
turn_end · agent_end
```

The final answer is **not** on its own line; it must be extracted.

### Extraction algorithm (frozen)

1. Read stdout line by line; JSON-decode each line; ignore decode
   failures on individual lines (robust to interleaved noise).
2. Track the **last** event whose `type == "agent_end"`. Fallbacks
   if absent: last `type == "turn_end"`, then the last assistant
   `message_end`.
3. From that event take `messages` (agent_end/turn_end) — pick the
   **last** entry with `role == "assistant"`.
4. Completion text = concatenation of `content[i].text` for each
   `content[i].type == "text"`.
5. Token usage = that assistant message's `usage`:
   `usage.input` → TokensIn, `usage.output` → TokensOut
   (`usage.totalTokens` available; `cost` present but provider-zeroed
   here — do not rely on it).

Verified on `test/fixtures/pi/pong.jsonl` (13 lines): text =
`"pong"`, usage input=615 output=6 total=621.

### Errors

- `pi` exits non-zero on failure; the wrapper surfaces stderr + exit
  code as an HTTP 502 (`invokePi` returns the error).
- An empty stream, or no `agent_end`/`turn_end`/assistant message ⇒
  treat as an error ("pi produced no assistant message"), never an
  empty-string success (cost discipline + fail-loud).

## Consequences for the build

- `cmd/pi-wrapper` `invokePi`: exec the invocation above, parse the
  JSONL stream per the algorithm. Unit-tested with a fake `pi`
  binary that replays `pong.jsonl` (no real Pi in unit tests).
- `internal/llm/pi.go` `PiClient`: HTTP only — it talks to the
  wrapper's `/complete`; it never parses the pi stream itself (that
  stays in the wrapper, the process boundary the design intends).
- The fixture doubles as the golden for the wrapper's stream parser.
