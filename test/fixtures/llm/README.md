# test/fixtures/llm/

Recorded LLM responses, keyed by `sha256(model|prompt|system|max_tokens|stop...)`. Consumed by `internal/llm.ReplayClient`.

## File format

`<hash>.json`:

```json
{
  "Text": "...response text...",
  "TokensIn": 123,
  "TokensOut": 45
}
```

## Regenerating

`make refresh-llm-fixtures` (TBD — comes with the `RecordingClient` PR in v0.5) wraps a real LLM provider and writes responses on first call. Don't regenerate casually — costs tokens.

A PR that regenerates fixtures should be reviewed against the prompt diff: did the prompt change deliberately, or is the regen masking a regression?
