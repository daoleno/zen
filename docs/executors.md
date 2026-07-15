# Executors

Zen launches and observes AI CLIs through tmux. Configuration is optional: if `~/.zen/executors.toml` is missing, built-in defaults apply.

**One authenticated executor on `PATH` is enough** for a usable beta. You do not need Codex, Claude, Cursor, and Grok all installed.

## Built-in defaults (current code)

When the file is absent, zen uses approximately:

| Name | Default command | Notes |
| --- | --- | --- |
| `codex` | `codex` | Default **delegated** executor for Brain |
| `claude` | `claude` | |
| `agent` | `cursor-agent --force --sandbox disabled` | Permission / sandbox bypass |
| `grok` | `grok --no-alt-screen --permission-mode bypassPermissions` | Permission bypass |

These defaults are **not** changed by shipping [executors.example.toml](../executors.example.toml). The example file documents safer vs autonomous profiles for you to copy.

## Install a profile

```bash
cp executors.example.toml ~/.zen/executors.toml
# edit names/commands, then restart zen
```

`delegated_executor` names which executor Brain uses for delegated work. Only that CLI needs to be installed if you only use Brain delegation plus that tool.

## Permission bypass risks

Flags such as:

- `cursor-agent --force --sandbox disabled`
- `grok ... --permission-mode bypassPermissions`
- Codex `--dangerously-bypass-approvals-and-sandbox` (appended for **Brain-delegated** Codex sessions in current code)

…mean the agent can run tools and shell actions with little or no interactive approval. That is convenient for unattended mobile control and dangerous on a shared or sensitive host.

Recommendations:

1. Prefer a **manual / safe** profile on machines with secrets or production access.
2. Use autonomous / bypass profiles only on disposable workspaces you accept losing.
3. Remember zen can read agent transcripts under the agent’s own home directories once a session exists.

## Credentials

Zen does not store provider API keys. Authenticate each CLI the way that tool normally expects (`codex login`, Claude login, Cursor, Grok, etc.) on the daemon host before expecting sessions to work.

## Custom executors

Any `[[executors]]` entry with `name` + `command` overrides or extends the map. Unknown tools are treated as custom tmux-backed agents.

## Structured chat update contract

Work/session Chat and Brain Chat use the same provider-neutral structured conversation subscription. The daemon publishes each logical event with a stable `id`; a provider-backed progressive update reuses that ID with new content and `partial: true`, and the canonical final update reuses it with `partial: false`. `transient: true` identifies a structured provider projection that may legitimately be absent from a later snapshot, so reconnect cleanup cannot duplicate an ephemeral reasoning/tool/status row. These optional fields preserve compatibility with older clients. `partial` describes lifecycle, not token granularity: it never authorizes the app to split or time-animate a completed body.

Update granularity is intentionally limited to what each current Zen adapter receives:

- **Genuine text delta — Grok:** `updates.jsonl` exposes native message/reasoning deltas and cumulative tool-output snapshots. Zen follows the native `promptId + streamStartMs + kind` identity and incrementally tails that file; `chat_history.jsonl` supplies canonical final records.
- **Block-level — Codex:** rollout JSONL exposes semantic `agent_reasoning` records plus tool/status lifecycle events. The current rollout adapter receives the final assistant answer as one completed `agent_message`, not assistant text deltas.
- **Block-level — Claude Code and Cursor Agent:** Claude project JSONL and Cursor `agent-transcripts` contain completed semantic message/thinking/tool/turn records. Zen publishes newly persisted blocks promptly but does not pretend that a complete block is token streaming.

The CLIs themselves have richer direct protocols that the current tmux/transcript integration does not own: Codex app-server emits item-keyed assistant/reasoning/output deltas, Claude supports `stream-json` with partial message events, and Cursor supports `stream-json --stream-partial-output`. Genuine delta support for those providers requires a daemon-owned structured executor runtime that owns process or app-server I/O, translates native lifecycle notifications into this reducer, persists reconnectable snapshots, and coordinates send/interrupt/terminal attachment. Adding flags to the existing interactive tmux command is not sufficient, and parsing raw NDJSON from terminal chrome would violate the protocol boundary.

Zen does not synthesize timed typewriter output and does not derive structured Chat from terminal screenshots, prompt echoes, or pane chrome. The live Terminal path remains independent and unchanged.

## Diagnostics

`zen doctor` reports which configured executors are on `PATH` and best-effort auth hints. Guided `zen setup` can write `~/.zen/executors.toml` for you (Safe vs Autonomous, Host/Delegated). It never runs sudo, never logs into providers, and requires explicit confirmation for Autonomous. Restart `zen` after setup so the daemon reloads the file.
