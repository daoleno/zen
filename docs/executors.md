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
# edit executor definitions/commands, then restart zen so the catalog reloads
```

`delegated_executor` names which executor Brain uses for delegated work and ordinary default session creation. Only that CLI needs to be installed if you only use Brain delegation plus that tool.

Switch the live Delegated Executor without restarting the daemon:

```bash
zen brain set-delegated grok
```

That validates the id against the loaded catalog, persists `delegated_executor` atomically, and updates every future launch/read boundary in the same process. Existing agent sessions keep their original executor. Editing executor definitions or commands still requires a restart so the static catalog reloads.

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

## Delegated resource lifecycle

Visible Brain-delegated sessions and Calendar-launched work run inside a Zen-owned resource boundary. User-created ordinary tmux sessions and the hidden Brain host are not adopted or killed by this boundary.

- Each delegated session receives a daemon-namespaced resource ID, a durable lease under `~/.zen/run/agent-resources`, and a short private temporary directory under `~/.zen/t/<6-char digest>` (URL-safe digest of the full resource unit, with a mode-0600 ownership marker naming that unit). `TMPDIR`, `TMP`, `TEMP`, and `ZEN_BUILD_TMPDIR` point at that directory so AF_UNIX paths stay within platform limits. Zen removes it only after the exact owned session is stopped and the marker still matches. Release/Reconcile also reclaim leftover legacy directories under `~/.zen/tmp/agent-resources/<owner>/<full-unit>` for sessions launched before the short layout; live legacy directories are left alone until their session ends.
- Delegated working directories under volatile or memory-backed temporary roots such as `/tmp`, `/private/tmp`, `/var/tmp`, `/run/user`, and `/dev/shm` are rejected. When concurrent writes genuinely require a Git worktree, Brain receives `ZEN_WORKTREE_ROOT=~/.zen/worktrees`; delegation or a dirty repository alone is not a reason to create one.
- All local delegated Agents share one static daemon-owned memory pool derived from physical memory `T`: host reserve `max(20% T, 4 GiB)` (capped at `T/2`) yields shared soft `MemoryHigh = T - host reserve`, and emergency reserve `max(10% T, 2 GiB)` (capped at `T/2`) yields shared hard `MemoryMax = T - emergency reserve`. On a 32 GiB host that is approximately 25.6 GiB high and 28.8 GiB max. Limits do not depend on Session count; one Agent may use far more than the old per-Agent share while aggregate pool room remains. The default active-session ceiling is disabled. Before admit, Zen performs one cheap OS-available-memory check and returns a retryable pressure error when availability is already below the host reserve. The per-session process count defaults to 1,024.
- On Linux with cgroup v2 and a working systemd user manager, Zen configures runtime-only aggregate `MemoryAccounting`, `MemoryHigh`, and `MemoryMax` once on the owned parent `zen-agents-<owner>.slice`. Child scopes keep exact lifecycle ownership, `TasksMax`, `OOMPolicy=stop`, and control-group teardown, but carry no child `MemoryHigh`/`MemoryMax`. Linux systemd supervisors perform no portable host scanning. Linux without that facility and macOS use the same Zen supervisor/lease protocol; exactly one failover-capable pool leader (nonblocking shared file lock, retried by non-leaders) samples aggregate owned RSS and per-lease task counts once per bounded interval from one process snapshot plus one environment listing, then after a short consecutive-sample grace stops the largest exact owned lease when aggregate RSS exceeds the shared hard max or OS availability falls below the emergency reserve. Non-leaders wait on child/signals and only retry the lock.
- Watcher reconciliation is idempotent. After a daemon or tmux restart it compares live Zen tmux markers with only the current daemon's namespaced leases/scopes, reclaims orphans, and refuses foreign or malformed resource IDs.

Advanced overrides are available as `ZEN_DELEGATED_MEMORY_HIGH`, `ZEN_DELEGATED_MEMORY_MAX`, `ZEN_DELEGATED_TASKS_MAX`, and `ZEN_DELEGATED_MAX_SESSIONS`. The last setting is an explicit administrative cap on active executor sessions (disabled when unset/invalid), not a memory-derived ceiling and not the number of Agent records Zen may retain. Memory values accept integer bytes, `K/M/G/T/P/E` suffixes, or percentages. Explicit memory overrides can reduce the host reserve and should be deliberate; changing only the active-session count does not repartition the shared pool.

The Ghostty native build uses a disposable worktree because it must apply verified patches to an exact pinned source revision without mutating the shared checkout. Inside a delegated session that worktree lives under `ZEN_WORKTREE_ROOT` (default `~/.zen/worktrees`), not the owned temporary directory. Build temp may use the short owned `TMPDIR`/`ZEN_BUILD_TMPDIR`. A direct developer build uses `~/.cache/zen/build-tmp` on Linux or `~/Library/Caches/zen/build-tmp` on macOS; set `ZEN_BUILD_TMPDIR` to another durable absolute path when needed. This build-specific isolation does not imply one worktree per agent task.

## Structured chat update contract

Work/session Chat and Brain Chat use the same provider-neutral structured conversation subscription. The daemon publishes each logical event with a stable `id`; a provider-backed progressive update reuses that ID with new content and `partial: true`, and the canonical final update reuses it with `partial: false`. `transient: true` identifies a structured provider projection that may legitimately be absent from a later snapshot, so reconnect cleanup cannot duplicate an ephemeral reasoning/tool/status row. These optional fields preserve compatibility with older clients. `partial` describes lifecycle, not token granularity: it never authorizes the app to split or time-animate a completed body.

Update granularity is intentionally limited to what each current Zen adapter receives:

- **Genuine text delta — Grok:** `updates.jsonl` exposes native message/reasoning deltas and cumulative tool-output snapshots. Zen follows the native `promptId + streamStartMs + kind` identity and incrementally tails that file; `chat_history.jsonl` supplies canonical final records.
- **Block-level — Codex:** rollout JSONL exposes semantic `agent_reasoning` records plus tool/status lifecycle events. The current rollout adapter receives the final assistant answer as one completed `agent_message`, not assistant text deltas.
- **Block-level — Claude Code and Cursor Agent:** Claude project JSONL and Cursor `agent-transcripts` contain completed semantic message/thinking/tool/turn records. Zen publishes newly persisted blocks promptly but does not pretend that a complete block is token streaming.

The CLIs themselves have richer direct protocols that the current tmux/transcript integration does not own: Codex app-server emits item-keyed assistant/reasoning/output deltas, Claude supports `stream-json` with partial message events, and Cursor supports `stream-json --stream-partial-output`. Genuine delta support for those providers requires a daemon-owned structured executor runtime that owns process or app-server I/O, translates native lifecycle notifications into this reducer, persists reconnectable snapshots, and coordinates send/interrupt/terminal attachment. Adding flags to the existing interactive tmux command is not sufficient, and parsing raw NDJSON from terminal chrome would violate the protocol boundary.

Zen does not synthesize timed typewriter output and does not derive structured Chat from terminal screenshots, prompt echoes, or pane chrome. The live Terminal path remains independent and unchanged.

## Diagnostics

`zen doctor` reports which configured executors are on `PATH` and best-effort auth hints. Guided `zen setup` can write `~/.zen/executors.toml` for you (Safe vs Autonomous, Host/Delegated). It never runs sudo, never logs into providers, and requires explicit confirmation for Autonomous. Restart `zen` after setup (or after editing executor definitions/commands) so the daemon reloads the static catalog. Once zen is running, change only the Delegated Executor with `zen brain set-delegated <id>` — no restart.
