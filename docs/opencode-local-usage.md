# OpenCode Local Usage

Zen Stats aggregates OpenCode model usage entirely from the OpenCode CLI's
local structured database. There is no subscription probing, no credential
read, no OpenCode-internal configuration, and no upstream API access: Stats
needs nothing beyond the data the OpenCode CLI already writes on the host.

## Data source

The OpenCode CLI stores one row per message in its SQLite database:

- Linux: `$XDG_DATA_HOME/opencode/opencode.db` (default
  `~/.local/share/opencode/opencode.db`)
- macOS: `~/Library/Application Support/opencode/opencode.db`
- Windows: `%LOCALAPPDATA%\opencode\opencode.db`

The `message` table's `data` column is JSON metadata; each assistant message
records the exact usage fields Zen aggregates:

```
role, modelID, cost, tokens.{input,output,reasoning,cache.{read,write}},
time.created, path.cwd
```

The read is a single read-only `sqlite3` query that projects only these
fields (`json_extract`); the conversation text lives in the `part` table and
is never read. `json_valid` guards keep one malformed row from failing the
whole read.

## Aggregation

Only observed facts are aggregated, keyed by the `(provider, model)` pair
recorded in each message row:

- **Request count**: one per assistant message that carries token usage (the
  input/output/reasoning/cache read/write fields as recorded).
- **Tokens**: input, output, reasoning, cache read, and cache write summed
  exactly as recorded.
- **Cost**: the recorded `cost` value. A model with rows that lack a cost
  field is reported with cost *unknown* — cost is never estimated or
  invented for OpenCode models.
- Rows without any token usage (for example interrupted or empty turns) and
  user messages contribute nothing.
- Missing token subfields contribute zero to their bucket; unavailable
  metrics are omitted.
- Quota, subscription, plan, or provider-entitlement state is never inferred
  from the provider name or anything else.

The aggregated rows flow into the same per-date model, project, and activity
aggregates as Claude Code, Codex, Grok, and Cursor Agent usage. The Models
section renders when any observed usage rows exist; with no OpenCode rows,
nothing OpenCode-specific appears.

## Security

- The database is opened read-only (`sqlite3 -readonly`); the live CLI is
  never disturbed.
- No credentials are read, logged, or serialized. No upstream requests are
  made.
