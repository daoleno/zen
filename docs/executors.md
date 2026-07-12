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

## Diagnostics

`zen doctor` reports which configured executors are on `PATH` and best-effort auth hints. Guided `zen setup` can write `~/.zen/executors.toml` for you (Safe vs Autonomous, Host/Delegated). It never runs sudo, never logs into providers, and requires explicit confirmation for Autonomous. Restart `zen` after setup so the daemon reloads the file.
