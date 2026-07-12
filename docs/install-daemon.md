# Install the daemon

## What you need

- Linux host (honest beta target)
- Go toolchain matching `daemon/go.mod`
- `tmux` on `PATH`
- At least one AI CLI on `PATH` and already authenticated (`codex`, `claude`, `cursor-agent`, or `grok`)

You do not need every executor. One is enough.

## Build from this repository

```bash
cd daemon
go build -o bin/zen ./cmd/zen/
./bin/zen
```

Or from the monorepo root:

```bash
bun run daemon:build
./bin/zen
```

## Install via Go modules (optional)

```bash
go install github.com/daoleno/zen/daemon/cmd/zen@latest
zen
```

This path depends on the published module and Go proxy state. If `@latest` fails, build from a clone as above.

## Defaults

| Setting | Value |
| --- | --- |
| Listen | `127.0.0.1:9876` |
| State directory | `~/.zen` |
| Work log | `~/.zen/work` |
| Executors file | `~/.zen/executors.toml` (optional; built-in defaults apply if missing) |
| Brain data | `~/.zen/brain` |

### Legacy state directory

Older installs used `~/.config/zen`. On first start with the default state dir, zen **copies** `identity.json`, `trusted-devices.json`, and `pairing-tokens.json` into `~/.zen` if the new files are missing. It does not delete the legacy directory.

To keep a custom location:

```bash
zen -state-dir /path/to/state
zen pair -state-dir /path/to/state https://your-host.example
```

Use the same `-state-dir` for the daemon and for `zen pair`.

## Starting and pairing are separate

1. Start `zen` so it has a stable identity and listening address.
2. Expose `http://127.0.0.1:9876` through your network layer.
3. Run `zen pair <full-origin>` to print a one-time link/QR without restarting.

There is no `-advertise-url` flag. The externally reachable origin is supplied at pair time.

## Keep it running

For a personal beta, a tmux pane or systemd user unit is enough. Example user unit (adjust paths):

```ini
[Unit]
Description=zen agent control plane
After=network.target

[Service]
ExecStart=%h/bin/zen -addr 127.0.0.1:9876 -state-dir %h/.zen
Restart=on-failure

[Install]
WantedBy=default.target
```

## Docker (advanced)

See `daemon/Dockerfile`. Prefer the host binary so tmux and local agent CLIs share the same environment. Container use requires mounting state, publishing or proxying port 9876, and arranging access to host agent tools yourself.

## Diagnostics

```bash
zen doctor
zen doctor --json
```

`zen doctor` reports whether tmux, the state directory, the listen address, and executors look ready. It does not install packages or print credentials.

Guided config write:

```bash
zen setup
# or automation:
zen setup --non-interactive --host codex --delegated codex --profile safe
```

`zen setup` stops cleanly with install hints when tmux/state-dir block readiness. Restart the daemon after it writes `~/.zen/executors.toml`.
